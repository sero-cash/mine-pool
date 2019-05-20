package payouts

import (
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/sero-cash/go-czero-import/seroparam"

	"github.com/sero-cash/mine-pool/rpc"
	"github.com/sero-cash/mine-pool/storage"
	"github.com/sero-cash/mine-pool/util"
)

type UnlockerConfig struct {
	Enabled        bool    `json:"enabled"`
	PoolFee        float64 `json:"poolFee"`
	PoolFeeAddress string  `json:"poolFeeAddress"`
	Donate         bool    `json:"donate"`
	Depth          int64   `json:"depth"`
	ImmatureDepth  int64   `json:"immatureDepth"`
	KeepTxFees     bool    `json:"keepTxFees"`
	Interval       string  `json:"interval"`
	Daemon         string  `json:"daemon"`
	Timeout        string  `json:"timeout"`
}

const minDepth = 16

type BlockUnlocker struct {
	config   *UnlockerConfig
	backend  *storage.RedisClient
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

func NewBlockUnlocker(cfg *UnlockerConfig, backend *storage.RedisClient) *BlockUnlocker {
	if len(cfg.PoolFeeAddress) != 0 && !util.IsValidBase58Address(cfg.PoolFeeAddress) {
		log.Fatalln("Invalid poolFeeAddress", cfg.PoolFeeAddress)
	}
	if cfg.Depth < minDepth*2 {
		log.Fatalf("Block maturity depth can't be < %v, your depth is %v", minDepth*2, cfg.Depth)
	}
	if cfg.ImmatureDepth < minDepth {
		log.Fatalf("Immature depth can't be < %v, your depth is %v", minDepth, cfg.ImmatureDepth)
	}
	u := &BlockUnlocker{config: cfg, backend: backend}
	u.rpc = rpc.NewRPCClient("BlockUnlocker", cfg.Daemon, cfg.Timeout)
	return u
}

func (u *BlockUnlocker) Start() {
	log.Println("Starting block unlocker")
	intv := util.MustParseDuration(u.config.Interval)
	timer := time.NewTimer(intv)
	log.Printf("Set block unlock interval to %v", intv)

	// Immediately unlock after start
	u.unlockPendingBlocks()
	u.unlockAndCreditMiners()
	timer.Reset(intv)

	go func() {
		for {
			select {
			case <-timer.C:
				u.unlockPendingBlocks()
				u.unlockAndCreditMiners()
				timer.Reset(intv)
			}
		}
	}()
}

type UnlockResult struct {
	maturedBlocks  []*storage.BlockData
	orphanedBlocks []*storage.BlockData
	orphans        int
	uncles         int
	blocks         int
}

/* Geth does not provide consistent state when you need both new height and new job,
 * so in redis I am logging just what I have in a pool state on the moment when block found.
 * Having very likely incorrect height in database results in a weird block unlocking scheme,
 * when I have to check what the hell we actually found and traversing all the blocks with height-N and height+N
 * to make sure we will find it. We can't rely on round height here, it's just a reference point.
 * ISSUE: https://github.com/sero-cash/go-sero/issues/2333
 */
func (u *BlockUnlocker) unlockCandidates(candidates []*storage.BlockData) (*UnlockResult, error) {
	result := &UnlockResult{}

	// Data row is: "height:nonce:powHash:mixDigest:timestamp:diff:totalShares"
	for _, candidate := range candidates {
		orphan := true

		/* Search for a normal block with wrong height here by traversing 16 blocks back and forward.
		 * Also we are searching for a block that can include this one as uncle.
		 */
		for i := int64(minDepth * -1); i < minDepth; i++ {
			height := candidate.Height + i

			if height < 0 {
				continue
			}

			block, err := u.rpc.GetBlockByHeight(height)
			if err != nil {
				log.Printf("Error while retrieving block %v from node: %v", height, err)
				return nil, err
			}
			if block == nil {
				return nil, fmt.Errorf("Error while retrieving block %v from node, wrong node height", height)
			}

			if matchCandidate(block, candidate) {
				orphan = false
				result.blocks++

				err = u.handleBlock(block, candidate)
				if err != nil {
					u.halt = true
					u.lastFail = err
					return nil, err
				}
				result.maturedBlocks = append(result.maturedBlocks, candidate)
				log.Printf("Mature block %v with %v tx, hash: %v", candidate.Height, len(block.Transactions), candidate.Hash[0:10])
				break
			}

			if !orphan {
				break
			}
		}
		// Block is lost, we didn't find any valid block or uncle matching our data in a blockchain
		if orphan {
			result.orphans++
			candidate.Orphan = true
			result.orphanedBlocks = append(result.orphanedBlocks, candidate)
			log.Printf("Orphaned block %v:%v", candidate.RoundHeight, candidate.Nonce)
		}
	}
	return result, nil
}

func matchCandidate(block *rpc.GetBlockReply, candidate *storage.BlockData) bool {
	// Just compare hash if block is unlocked as immature
	if len(candidate.Hash) > 0 && strings.EqualFold(candidate.Hash, block.Hash) {
		return true
	}
	// Geth-style candidate matching
	if len(block.Nonce) > 0 {
		return strings.EqualFold(block.Nonce, candidate.Nonce)
	}
	// Parity's EIP: https://github.com/ethereum/EIPs/issues/95
	if len(block.SealFields) == 2 {
		return strings.EqualFold(candidate.Nonce, block.SealFields[1])
	}
	return false
}

func (u *BlockUnlocker) handleBlock(block *rpc.GetBlockReply, candidate *storage.BlockData) error {
	correctHeight, err := strconv.ParseInt(strings.Replace(block.Number, "0x", "", -1), 16, 64)
	if err != nil {
		return err
	}
	candidate.Height = correctHeight
	reward := getConstReward(big.NewInt(candidate.Height), big.NewInt(candidate.Difficulty))

	// Add TX fees
	extraTxReward, err := u.getExtraRewardForTx(block)
	if err != nil {
		return fmt.Errorf("Error while fetching TX receipt: %v", err)
	}
	if u.config.KeepTxFees {
		candidate.ExtraReward = extraTxReward
	} else {
		reward.Add(reward, extraTxReward)
	}

	candidate.Orphan = false
	candidate.Hash = block.Hash
	candidate.Reward = reward
	return nil
}

func (u *BlockUnlocker) unlockPendingBlocks() {
	if u.halt {
		log.Println("Unlocking suspended due to last critical error:", u.lastFail)
		return
	}

	current, err := u.rpc.GetPendingBlock()
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Unable to get current blockchain height from node: %v", err)
		return
	}
	currentHeight, err := strconv.ParseInt(strings.Replace(current.Number, "0x", "", -1), 16, 64)
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Can't parse pending block number: %v", err)
		return
	}

	candidates, err := u.backend.GetCandidates(currentHeight - u.config.ImmatureDepth)
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Failed to get block candidates from backend: %v", err)
		return
	}

	if len(candidates) == 0 {
		log.Println("No block candidates to unlock")
		return
	}

	result, err := u.unlockCandidates(candidates)
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Failed to unlock blocks: %v", err)
		return
	}
	log.Printf("Immature %v blocks, %v uncles, %v orphans", result.blocks, result.uncles, result.orphans)

	err = u.backend.WritePendingOrphans(result.orphanedBlocks)
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Failed to insert orphaned blocks into backend: %v", err)
		return
	} else {
		log.Printf("Inserted %v orphaned blocks to backend", result.orphans)
	}

	totalRevenue := new(big.Rat)
	totalMinersProfit := new(big.Rat)
	totalPoolProfit := new(big.Rat)

	for _, block := range result.maturedBlocks {
		revenue, minersProfit, poolProfit, roundRewards, err := u.calculateRewards(block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to calculate rewards for round %v: %v", block.RoundKey(), err)
			return
		}
		err = u.backend.WriteImmatureBlock(block, roundRewards)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			return
		}
		totalRevenue.Add(totalRevenue, revenue)
		totalMinersProfit.Add(totalMinersProfit, minersProfit)
		totalPoolProfit.Add(totalPoolProfit, poolProfit)

		logEntry := fmt.Sprintf(
			"IMMATURE %v: revenue %v, miners profit %v, pool profit: %v",
			block.RoundKey(),
			util.FormatRatReward(revenue),
			util.FormatRatReward(minersProfit),
			util.FormatRatReward(poolProfit),
		)
		entries := []string{logEntry}
		for login, reward := range roundRewards {
			entries = append(entries, fmt.Sprintf("\tREWARD %v: %v: %v Shannon", block.RoundKey(), login, reward))
		}
		log.Println(strings.Join(entries, "\n"))
	}

	log.Printf(
		"IMMATURE SESSION: revenue %v, miners profit %v, pool profit: %v",
		util.FormatRatReward(totalRevenue),
		util.FormatRatReward(totalMinersProfit),
		util.FormatRatReward(totalPoolProfit),
	)
}

func (u *BlockUnlocker) unlockAndCreditMiners() {
	if u.halt {
		log.Println("Unlocking suspended due to last critical error:", u.lastFail)
		return
	}

	current, err := u.rpc.GetPendingBlock()
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Unable to get current blockchain height from node: %v", err)
		return
	}
	currentHeight, err := strconv.ParseInt(strings.Replace(current.Number, "0x", "", -1), 16, 64)
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Can't parse pending block number: %v", err)
		return
	}

	immature, err := u.backend.GetImmatureBlocks(currentHeight - u.config.Depth)
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Failed to get block candidates from backend: %v", err)
		return
	}

	if len(immature) == 0 {
		log.Println("No immature blocks to credit miners")
		return
	}

	result, err := u.unlockCandidates(immature)
	if err != nil {
		u.halt = true
		u.lastFail = err
		log.Printf("Failed to unlock blocks: %v", err)
		return
	}
	log.Printf("Unlocked %v blocks, %v uncles, %v orphans", result.blocks, result.uncles, result.orphans)

	for _, block := range result.orphanedBlocks {
		err = u.backend.WriteOrphan(block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to insert orphaned block into backend: %v", err)
			return
		}
	}
	log.Printf("Inserted %v orphaned blocks to backend", result.orphans)

	totalRevenue := new(big.Rat)
	totalMinersProfit := new(big.Rat)
	totalPoolProfit := new(big.Rat)

	for _, block := range result.maturedBlocks {
		revenue, minersProfit, poolProfit, roundRewards, err := u.calculateRewards(block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to calculate rewards for round %v: %v", block.RoundKey(), err)
			return
		}
		err = u.backend.WriteMaturedBlock(block, roundRewards)
		if err != nil {
			u.halt = true
			u.lastFail = err
			log.Printf("Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			return
		}
		totalRevenue.Add(totalRevenue, revenue)
		totalMinersProfit.Add(totalMinersProfit, minersProfit)
		totalPoolProfit.Add(totalPoolProfit, poolProfit)

		logEntry := fmt.Sprintf(
			"MATURED %v: revenue %v, miners profit %v, pool profit: %v",
			block.RoundKey(),
			util.FormatRatReward(revenue),
			util.FormatRatReward(minersProfit),
			util.FormatRatReward(poolProfit),
		)
		entries := []string{logEntry}
		for login, reward := range roundRewards {
			entries = append(entries, fmt.Sprintf("\tREWARD %v: %v: %v Shannon", block.RoundKey(), login, reward))
		}
		log.Println(strings.Join(entries, "\n"))
	}

	log.Printf(
		"MATURE SESSION: revenue %v, miners profit %v, pool profit: %v",
		util.FormatRatReward(totalRevenue),
		util.FormatRatReward(totalMinersProfit),
		util.FormatRatReward(totalPoolProfit),
	)
}

func (u *BlockUnlocker) calculateRewards(block *storage.BlockData) (*big.Rat, *big.Rat, *big.Rat, map[string]int64, error) {
	revenue := new(big.Rat).SetInt(block.Reward)
	minersProfit, poolProfit := chargeFee(revenue, u.config.PoolFee)

	shares, err := u.backend.GetRoundShares(block.RoundHeight, block.Nonce)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	rewards := calculateRewardsForShares(shares, block.TotalShares, minersProfit)

	if block.ExtraReward != nil {
		extraReward := new(big.Rat).SetInt(block.ExtraReward)
		poolProfit.Add(poolProfit, extraReward)
		revenue.Add(revenue, extraReward)
	}

	if len(u.config.PoolFeeAddress) != 0 {
		address := u.config.PoolFeeAddress
		rewards[address] += weiToShannonInt64(poolProfit)
	}

	return revenue, minersProfit, poolProfit, rewards, nil
}

func calculateRewardsForShares(shares map[string]int64, total int64, reward *big.Rat) map[string]int64 {
	rewards := make(map[string]int64)

	for login, n := range shares {
		percent := big.NewRat(n, total)
		workerReward := new(big.Rat).Mul(reward, percent)
		rewards[login] += weiToShannonInt64(workerReward)
	}
	return rewards
}

// Returns new value after fee deduction and fee value.
func chargeFee(value *big.Rat, fee float64) (*big.Rat, *big.Rat) {
	feePercent := new(big.Rat).SetFloat64(fee / 100)
	feeValue := new(big.Rat).Mul(value, feePercent)
	return new(big.Rat).Sub(value, feeValue), feeValue
}

func weiToShannonInt64(wei *big.Rat) int64 {
	shannon := new(big.Rat).SetInt(util.Shannon)
	inShannon := new(big.Rat).Quo(wei, shannon)
	value, _ := strconv.ParseInt(inShannon.FloatString(0), 10, 64)
	return value
}

func (u *BlockUnlocker) getExtraRewardForTx(block *rpc.GetBlockReply) (*big.Int, error) {
	amount := new(big.Int)

	for _, tx := range block.Transactions {
		receipt, err := u.rpc.GetTxReceipt(tx.Hash)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			gasUsed := util.String2Big(receipt.GasUsed)
			gasPrice := util.String2Big(tx.GasPrice)
			fee := new(big.Int).Mul(gasUsed, gasPrice)
			amount.Add(amount, fee)
		}
	}
	return amount, nil
}

var (
	big1 = big.NewInt(1)
	big2 = big.NewInt(2)
	base = big.NewInt(1e+17)

	oriReward    = new(big.Int).Mul(big.NewInt(66773505743), big.NewInt(1000000000))
	interval     = big.NewInt(8294400)
	halveNimber  = big.NewInt(3057600)
	difficultyL1 = big.NewInt(340000000)
	difficultyL2 = big.NewInt(1700000000)
	difficultyL3 = big.NewInt(4000000000)
	difficultyL4 = big.NewInt(17000000000)

	lReward = new(big.Int).Mul(big.NewInt(176), base)
	hReward = new(big.Int).Mul(big.NewInt(445), base)

	argA, _ = new(big.Int).SetString("985347985347985", 10)
	argB, _ = new(big.Int).SetString("16910256410256400000", 10)
)

func getConstReward(Number, Difficulty *big.Int) *big.Int {

	if Number.Cmp(big.NewInt(seroparam.SIP3)) >= 0 {
		return getConstRewardv3(Number, Difficulty)
	} else {
		return getConstRewardv2(Number, Difficulty)
	}
}

func getConstRewardv2(Number, Difficulty *big.Int) *big.Int {
	rewardStd := new(big.Int).Set(oriReward)
	if Number.Uint64() >= halveNimber.Uint64() {
		i := new(big.Int).Add(new(big.Int).Div(new(big.Int).Sub(Number, halveNimber), interval), big1)
		rewardStd.Div(rewardStd, new(big.Int).Exp(big2, i, nil))
	}

	var reward *big.Int
	if Difficulty.Cmp(difficultyL1) < 0 { //<3.4
		reward = new(big.Int).Mul(big.NewInt(10), base)
	} else if Difficulty.Cmp(difficultyL2) < 0 { //<17
		ratio := new(big.Int).Add(new(big.Int).Mul(big.NewInt(56), base), new(big.Int).Mul(big.NewInt(16470000000), new(big.Int).Sub(Difficulty, difficultyL1)))
		reward = new(big.Int).Div(new(big.Int).Mul(rewardStd, ratio), oriReward)
	} else if Difficulty.Cmp(difficultyL3) < 0 { //<40
		ratio := new(big.Int).Add(new(big.Int).Mul(big.NewInt(280), base), new(big.Int).Mul(big.NewInt(2170000000), new(big.Int).Sub(Difficulty, difficultyL2)))
		reward = new(big.Int).Div(new(big.Int).Mul(rewardStd, ratio), oriReward)
	} else if Difficulty.Cmp(difficultyL4) < 0 { //<170
		ratio := new(big.Int).Add(new(big.Int).Mul(big.NewInt(330), base), new(big.Int).Mul(big.NewInt(2590000000), new(big.Int).Sub(Difficulty, difficultyL3)))
		reward = new(big.Int).Div(new(big.Int).Mul(rewardStd, ratio), oriReward)
	} else {
		reward = rewardStd
	}
	return reward
}

func getConstRewardv3(Number, Difficulty *big.Int) *big.Int {
	diff := new(big.Int).Div(Difficulty, big.NewInt(1000000000))
	reward := new(big.Int).Add(new(big.Int).Mul(argA, diff), argB)

	if reward.Cmp(lReward) < 0 {
		reward = new(big.Int).Set(lReward)
	} else if reward.Cmp(hReward) > 0 {
		reward = new(big.Int).Set(hReward)
	}

	i := new(big.Int).Add(new(big.Int).Div(new(big.Int).Sub(Number, halveNimber), interval), big1)
	reward.Div(reward, new(big.Int).Exp(big2, i, nil))

	return reward
}
