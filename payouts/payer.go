package payouts

import (
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/btcsuite/btcutil/base58"

	"github.com/sero-cash/go-sero/common/hexutil"

	"github.com/sero-cash/mine-pool/rpc"
	"github.com/sero-cash/mine-pool/storage"
	"github.com/sero-cash/mine-pool/util"
)

const txCheckInterval = 300 * time.Second
const confireBlocks = 16

type PayoutsConfig struct {
	Enabled      bool   `json:"enabled"`
	RequirePeers int64  `json:"requirePeers"`
	Interval     string `json:"interval"`
	Daemon       string `json:"daemon"`
	Timeout      string `json:"timeout"`
	Address      string `json:"address"`
	Gas          string `json:"gas"`
	GasPrice     string `json:"gasPrice"`
	AutoGas      bool   `json:"autoGas"`
	Exchange     bool   `json:"exchange"`
	// In Shannon
	Threshold int64 `json:"threshold"`
	BgSave    bool  `json:"bgsave"`
}

func (self PayoutsConfig) GasHex() string {
	x := util.String2Big(self.Gas)
	return hexutil.EncodeBig(x)
}

func (self PayoutsConfig) GasPriceHex() string {
	x := util.String2Big(self.GasPrice)
	return hexutil.EncodeBig(x)
}

type PayoutsProcessor struct {
	config   *PayoutsConfig
	backend  *storage.RedisClient
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

func NewPayoutsProcessor(cfg *PayoutsConfig, backend *storage.RedisClient) *PayoutsProcessor {
	u := &PayoutsProcessor{config: cfg, backend: backend}
	u.rpc = rpc.NewRPCClient("PayoutsProcessor", cfg.Daemon, cfg.Timeout)
	return u
}

func (u *PayoutsProcessor) Start() {
	log.Println("Starting payouts")

	if u.mustResolvePayout() {
		log.Println("Running with env RESOLVE_PAYOUT=1, now trying to resolve locked payouts")
		if u.config.Exchange {
			u.resolveExchangePayouts()
		} else {
			u.resolvePayouts()
		}
		log.Println("Now you have to restart payouts module with RESOLVE_PAYOUT=0 for normal run")
		return
	}

	intv := util.MustParseDuration(u.config.Interval)
	timer := time.NewTimer(intv)
	log.Printf("Set payouts interval to %v", intv)

	if u.config.Exchange {
		payments := u.backend.GetPendingExchangePayments()
		if len(payments) > 0 {
			log.Printf("Previous payout failed, you have to resolve it. List of failed payments:\n %v",
				formatPendingPayments(payments))
			return
		}
	} else {
		payments := u.backend.GetPendingPayments()
		if len(payments) > 0 {
			log.Printf("Previous payout failed, you have to resolve it. List of failed payments:\n %v",
				formatPendingPayments(payments))
			return
		}
	}

	locked, err := u.backend.IsPayoutsLocked()
	if err != nil {
		log.Println("Unable to start payouts:", err)
		return
	}
	if locked {
		log.Println("Unable to start payouts because they are locked")
		return
	}

	// Immediately process payouts after start
	if u.config.Exchange {
		u.exhcange_process()
	} else {
		u.process()
	}
	timer.Reset(intv)

	go func() {
		for {
			select {
			case <-timer.C:
				if u.config.Exchange {
					u.exhcange_process()
				} else {
					u.process()
				}
				timer.Reset(intv)
			}
		}
	}()
}

func hexToInt64(hex string) int64 {
	n := new(big.Int)
	n, _ = n.SetString(hex[2:], 16)

	return n.Int64()
}

func (u *PayoutsProcessor) process() {
	if u.halt {
		log.Println("Payments suspended due to last critical error:", u.lastFail)
		return
	}
	mustPay := 0
	minersPaid := 0
	totalAmount := big.NewInt(0)
	payees, err := u.backend.GetPayees()
	if err != nil {
		log.Println("Error while retrieving payees from backend:", err)
		return
	}

	for _, login := range payees {
		amount, _ := u.backend.GetBalance(login)
		amountInShannon := big.NewInt(amount)

		// Shannon^2 = Wei
		amountInWei := new(big.Int).Mul(amountInShannon, util.Shannon)

		if !u.reachedThreshold(amountInShannon) {
			log.Printf("%v ammount %d not reach threshold", login, amountInShannon)
			continue
		}
		mustPay++

		// Require active peers before processing
		if !u.checkPeers() {
			break
		}
		// Require unlocked account
		if !u.isUnlockedAccount() {
			break
		}

		// Check if we have enough funds
		poolBalance, err := u.rpc.GetBalance(u.config.Address)
		if err != nil {
			u.halt = true
			u.lastFail = err
			break
		}
		if poolBalance.Cmp(amountInWei) < 0 {
			err := fmt.Errorf("Not enough balance for payment, need %s Wei, pool has %s Wei",
				amountInWei.String(), poolBalance.String())
			u.halt = true
			u.lastFail = err
			break
		}

		// Lock payments for current payout
		err = u.backend.LockPayouts(login, amount)
		if err != nil {
			log.Printf("Failed to lock payment for %s: %v", login, err)
			u.halt = true
			u.lastFail = err
			break
		}
		log.Printf("Locked payment for %s, %v Shannon", login, amount)

		// Debit miner's balance and update stats
		err = u.backend.UpdateBalance(login, amount)
		if err != nil {
			log.Printf("Failed to update balance for %s, %v Shannon: %v", login, amount, err)
			u.halt = true
			u.lastFail = err
			break
		}

		value := hexutil.EncodeBig(amountInWei)
		txHash, err := u.rpc.SendTransaction(u.config.Address, login, u.config.GasHex(), u.config.GasPriceHex(), value, u.config.AutoGas)
		if err != nil {
			log.Printf("Failed to send payment to %s, %v Shannon: %v. Check outgoing tx for %s in block explorer and docs/PAYOUTS.md",
				login, amount, err, login)
			u.halt = true
			u.lastFail = err
			break
		}

		// Log transaction hash
		err = u.backend.WritePayment(login, txHash, amount)
		if err != nil {
			log.Printf("Failed to log payment data for %s, %v Shannon, tx: %s: %v", login, amount, txHash, err)
			u.halt = true
			u.lastFail = err
			break
		}

		minersPaid++
		totalAmount.Add(totalAmount, big.NewInt(amount))
		log.Printf("Paid %v Shannon to %v, TxHash: %v", amount, login, txHash)

		// Wait for TX confirmation before further payouts
		for {
			log.Printf("Waiting for tx confirmation: %v", txHash)
			time.Sleep(5 * time.Second)
			receipt, err := u.rpc.GetTxReceipt(txHash)
			if err != nil {
				log.Printf("Failed to get tx receipt for %v: %v", txHash, err)
				continue
			}
			// Tx has been mined
			if receipt != nil && receipt.Confirmed() {
				if receipt.Successful() {
					log.Printf("Payout tx successful for %s: %s", login, txHash)
				} else {
					log.Printf("Payout tx failed for %s: %s. Address contract throws on incoming tx.", login, txHash)
				}
				txBlockNumber := hexToInt64(receipt.BlockNumber)
				currentBlockNumber, _ := u.rpc.GetBlockNumber()
				for currentBlockNumber < txBlockNumber+confireBlocks {
					time.Sleep(13 * time.Second)
					currentBlockNumber, _ = u.rpc.GetBlockNumber()
					log.Printf("%v Waiting for balance confirmation: txblockNumber %v,currentBlockNumber %v", txHash, txBlockNumber, currentBlockNumber)
				}
				break
			}
		}

	}

	if mustPay > 0 {
		log.Printf("Paid total %v Shannon to %v of %v payees", totalAmount, minersPaid, mustPay)
	} else {
		log.Println("No payees that have reached payout threshold")
	}

	// Save redis state to disk
	if minersPaid > 0 && u.config.BgSave {
		u.bgSave()
	}
}

type payInfo struct {
	miner           string
	amount          *big.Int
	amountInShannon int64
}

func (u *PayoutsProcessor) exhcange_process() {
	if u.halt {
		log.Println("payments suspended due to last critical error:", u.lastFail)
		return
	}
	confirmBlock, currentBlock, hightBlock, pkBlock, err := u.rpc.GetPkSynced(u.config.Address)
	if hightBlock < currentBlock {
		log.Println("payments suspended due to block syncing:", currentBlock, hightBlock)
		return
	}
	if pkBlock+128 < currentBlock {
		log.Println("payments suspended due to balance syncing:", currentBlock, pkBlock)
		return
	}
	payees, err := u.backend.GetPayees()
	if err != nil {
		log.Println("Error while retrieving payees from backend:", err)
		return
	}

	mustPayMiners := []payInfo{}
	totalAmount := big.NewInt(0)
	for _, login := range payees {
		out := base58.Decode(login)
		if len(out) != 64 && len(out) != 96 {
			fmt.Printf("invalid address %v,length is %v", login, len(out))
			continue
		}

		amount, err := u.backend.GetBalance(login)
		if err != nil {
			log.Printf("GetBalance login:%v err:%v ", login, err)
			return
		}
		amountInShannon := big.NewInt(amount)
		// Shannon^2 = Wei
		amountInWei := new(big.Int).Mul(amountInShannon, util.Shannon)

		if !u.reachedThreshold(amountInShannon) {
			log.Printf("%v ammount %d not reach threshold", login, amountInShannon)
			continue
		}
		totalAmount = new(big.Int).Add(totalAmount, amountInShannon)
		mustPayMiners = append(mustPayMiners, payInfo{login, amountInWei, amountInShannon.Int64()})

	}
	if len(mustPayMiners) == 0 {
		log.Println("No payees that have reached payout threshold")
		return
	}
	poolAvailableBalance, err := u.rpc.GetMaxAvailable(u.config.Address)
	if err != nil {
		log.Printf("get poolAvailableBalance err:%v ", err)
		return
	}

	mustPay := len(mustPayMiners)
	payingShannonAmount := int64(0)
	payingLogins := map[string]*big.Int{}
	minersPaid := 0

	for minersPaid < mustPay {
		payminer := mustPayMiners[minersPaid]
		if poolAvailableBalance.Cmp(payminer.amount) > 0 && len(payingLogins) < 8 {
			payingLogins[payminer.miner] = payminer.amount
			payingShannonAmount += payminer.amountInShannon
			poolAvailableBalance = new(big.Int).Add(poolAvailableBalance, new(big.Int).Mul(payminer.amount, big.NewInt(-1)))
			minersPaid++
			if minersPaid < mustPay {
				continue
			}
		}
		if len(payingLogins) == 0 {
			time.Sleep(15 * time.Second)
			poolAvailableBalance, err = u.rpc.GetMaxAvailable(u.config.Address)
			if err != nil {
				log.Printf("get poolAvailableBalance err:%v ", err)
				continue
			}
			continue

		}

		// Require unlocked account
		if !u.isUnlockedAccount() {
			return
		}
		rawData, txHash, err := u.rpc.GenTxWithSign(u.config.Address, 25000, 1000000000, payingLogins)
		if err != nil {
			log.Printf("Failed to GenTxWithSign for %s: %v", payingLogins, err)
			u.rpc.ClearExchange(u.config.Address)
			//u.halt = true
			//u.lastFail = err
			return
		}
		err = u.backend.LockPayouts("exchange_paying", payingShannonAmount)
		if err != nil {
			log.Printf("Failed to lock payment for %v: %v", payingLogins, err)
			u.rpc.ClearExchange(u.config.Address)
			u.halt = true
			u.lastFail = err
			return
		}
		log.Printf("Locked payment for %s, %v Shannon", payingLogins, payingShannonAmount)

		for p, a := range payingLogins {
			ammountInshannon := new(big.Int).Div(a, util.Shannon).Int64()
			err = u.backend.UpdateBalanceWithTx(p, ammountInshannon, txHash)
			if err != nil {
				log.Printf("Failed to UpdateBalanceWithTx for %s, %v Shannon, tx: %s: %v", p, ammountInshannon, txHash, err)
				u.rpc.ClearExchange(u.config.Address)
				//u.resolveExchangePayouts()
				u.halt = true
				u.lastFail = err
				return
			}
		}
		err = u.rpc.CommitTx(rawData, txHash)
		if err != nil {
			log.Printf("Failed to CommitTx %v :%v", txHash, err)
			u.halt = true
			u.rpc.ClearExchange(u.config.Address)
			u.lastFail = err
			return

		}
		for p, a := range payingLogins {
			ammountInshannon := new(big.Int).Div(a, util.Shannon).Int64()
			err = u.backend.WriteExchangePayment(p, txHash, ammountInshannon)
			if err != nil {
				log.Printf("Failed to log payment data for %s, %v Shannon, tx: %s: %v", p, ammountInshannon, txHash, err)
				u.halt = true
				u.lastFail = err
				return
			}
			log.Printf("Paid %v Shannon to %v with TxHash: %v", ammountInshannon, p, txHash)
		}

		err = u.backend.UnlockPayouts()
		if err != nil {
			log.Printf("Failed to  unlock payouts")
			u.halt = true
			u.lastFail = err
			return
		}
		for {
			log.Printf("Waiting for tx confirmation: %v", txHash)
			time.Sleep(5 * time.Second)
			receipt, err := u.rpc.GetTxReceipt(txHash)
			if err != nil {
				log.Printf("Failed to get tx receipt for %v: %v", txHash, err)
				continue
			}
			// Tx has been mined
			if receipt != nil && receipt.Confirmed() {
				if receipt.Successful() {
					log.Printf("Payout tx successful for %s: %s", payingLogins, txHash)
				} else {
					log.Printf("Payout tx failed for %s: %s. Address contract throws on incoming tx.", payingLogins, txHash)
				}
				txBlockNumber := hexToInt64(receipt.BlockNumber)
				currentBlockNumber, _ := u.rpc.GetBlockNumber()
				for currentBlockNumber < txBlockNumber+int64(confirmBlock) {
					time.Sleep(5 * time.Second)
					currentBlockNumber, _ = u.rpc.GetBlockNumber()
					log.Printf("Waiting for balance confirmation: %v,currentBlock:%v,txBlokc:%v", txHash, currentBlock, txBlockNumber)
				}
				break
			}
		}
		log.Printf("batch Paid total %v Shannon to %v of %v payees", payingShannonAmount, len(payingLogins), mustPay)
		payingLogins = map[string]*big.Int{}
		payingShannonAmount = 0
		poolAvailableBalance, err = u.rpc.GetMaxAvailable(u.config.Address)
		if err != nil {
			log.Printf("get poolAvailableBalance err:%v ", err)
			continue
		}
	}
	if mustPay > 0 {
		log.Printf("Paid total %v Shannon to %v of %v payees", totalAmount, minersPaid, mustPay)
	} else {
		log.Println("No payees that have reached payout threshold")
	}

	// Save redis state to disk
	if minersPaid > 0 && u.config.BgSave {
		u.bgSave()
	}
}

func (self PayoutsProcessor) isUnlockedAccount() bool {
	reply, err := self.rpc.AddressUnlocked(self.config.Address)
	if err != nil {
		log.Println("Unable to process payouts:", err)
		return false
	}
	return reply
}

func (self PayoutsProcessor) checkPeers() bool {
	n, err := self.rpc.GetPeerCount()
	if err != nil {
		log.Println("Unable to start payouts, failed to retrieve number of peers from node:", err)
		return false
	}
	if n < self.config.RequirePeers {
		log.Println("Unable to start payouts, number of peers on a node is less than required", self.config.RequirePeers)
		return false
	}
	return true
}

func (self PayoutsProcessor) reachedThreshold(amount *big.Int) bool {
	return big.NewInt(self.config.Threshold).Cmp(amount) < 0
}

func formatPendingPayments(list []*storage.PendingPayment) string {
	var s string
	for _, v := range list {
		s += fmt.Sprintf("\tAddress: %s, Amount: %v Shannon, %v\n", v.Address, v.Amount, time.Unix(v.Timestamp, 0))
	}
	return s
}

func (self PayoutsProcessor) bgSave() {
	result, err := self.backend.BgSave()
	if err != nil {
		log.Println("Failed to perform BGSAVE on backend:", err)
		return
	}
	log.Println("Saving backend state to disk:", result)
}

func (self PayoutsProcessor) resolvePayouts() {
	payments := self.backend.GetPendingPayments()

	if len(payments) > 0 {
		log.Printf("Will credit back following balances:\n%s", formatPendingPayments(payments))

		for _, v := range payments {
			err := self.backend.RollbackBalance(v.Address, v.Amount)
			if err != nil {
				log.Printf("Failed to credit %v Shannon back to %s, error is: %v", v.Amount, v.Address, err)
				return
			}
			log.Printf("Credited %v Shannon back to %s", v.Amount, v.Address)
		}
		err := self.backend.UnlockPayouts()
		if err != nil {
			log.Println("Failed to unlock payouts:", err)
			return
		}
	} else {
		log.Println("No pending payments to resolve")
	}

	if self.config.BgSave {
		self.bgSave()
	}
	log.Println("Payouts unlocked")
}

func (self PayoutsProcessor) resolveExchangePayouts() {
	payments := self.backend.GetPendingExchangePayments()
	if len(payments) > 0 {
		log.Printf("Will credit back following balances:\n%s", formatPendingPayments(payments))

		for _, v := range payments {
			err := self.backend.RollbackExchangeBalance(v.Address, v.Amount, v.TxHash)
			if err != nil {
				log.Printf("Failed to credit %v Shannon back to %s, error is: %v", v.Amount, v.Address, err)
				return
			}
			log.Printf("Credited %v Shannon back to %s", v.Amount, v.Address)
		}
		err := self.backend.UnlockPayouts()
		if err != nil {
			log.Println("Failed to unlock payouts:", err)
			return
		}
	} else {
		log.Println("No pending payments to resolve")
	}

	if self.config.BgSave {
		self.bgSave()
	}
	log.Println("Payouts unlocked")
}

func (self PayoutsProcessor) mustResolvePayout() bool {
	v, _ := strconv.ParseBool(os.Getenv("RESOLVE_PAYOUT"))
	return v
}
