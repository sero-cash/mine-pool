package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/btcsuite/btcutil/base58"

	"github.com/sero-cash/go-sero/common"
	"github.com/sero-cash/go-sero/common/hexutil"

	"github.com/sero-cash/mine-pool/util"
)

type RPCClient struct {
	sync.RWMutex
	Url         string
	Name        string
	sick        bool
	sickRate    int
	successRate int
	client      *http.Client
}

type GetBlockReply struct {
	Number       string   `json:"number"`
	Hash         string   `json:"hash"`
	Nonce        string   `json:"nonce"`
	Miner        string   `json:"miner"`
	Difficulty   string   `json:"difficulty"`
	GasLimit     string   `json:"gasLimit"`
	GasUsed      string   `json:"gasUsed"`
	Transactions []Tx     `json:"transactions"`
	Uncles       []string `json:"uncles"`
	// https://github.com/ethereum/EIPs/issues/95
	SealFields []string `json:"sealFields"`
}

type GetBlockReplyPart struct {
	Number     string `json:"number"`
	Difficulty string `json:"difficulty"`
}

const receiptStatusSuccessful = "0x1"

type TxReceipt struct {
	TxHash      string `json:"transactionHash"`
	BlockNumber string `json:"blockNumber"`
	GasUsed     string `json:"gasUsed"`
	BlockHash   string `json:"blockHash"`
	Status      string `json:"status"`
}

func (r *TxReceipt) Confirmed() bool {
	return len(r.BlockHash) > 0
}

// Use with previous method
func (r *TxReceipt) Successful() bool {
	if len(r.Status) > 0 {
		return r.Status == receiptStatusSuccessful
	}
	return true
}

type Tx struct {
	Gas      string `json:"gas"`
	GasPrice string `json:"gasPrice"`
	Hash     string `json:"hash"`
}

type JSONRpcResp struct {
	Id     *json.RawMessage       `json:"id"`
	Result *json.RawMessage       `json:"result"`
	Error  map[string]interface{} `json:"error"`
}

func NewRPCClient(name, url, timeout string) *RPCClient {
	rpcClient := &RPCClient{Name: name, Url: url}
	timeoutIntv := util.MustParseDuration(timeout)
	rpcClient.client = &http.Client{
		Timeout: timeoutIntv,
	}
	return rpcClient
}

func (r *RPCClient) GetWork() ([]string, error) {
	rpcResp, err := r.doPost(r.Url, "sero_getWork", []string{})
	if err != nil {
		return nil, err
	}
	var reply []string
	err = json.Unmarshal(*rpcResp.Result, &reply)
	return reply, err
}

func (r *RPCClient) GetBlockNumber() (int64, error) {
	rpcResp, err := r.doPost(r.Url, "sero_blockNumber", nil)
	if err != nil {
		return 0, err
	}
	var reply string
	err = json.Unmarshal(*rpcResp.Result, &reply)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.Replace(reply, "0x", "", -1), 16, 64)
}

func (r *RPCClient) GetPendingBlock() (*GetBlockReplyPart, error) {
	rpcResp, err := r.doPost(r.Url, "sero_getBlockByNumber", []interface{}{"pending", false})
	if err != nil {
		return nil, err
	}
	if rpcResp.Result != nil {
		var reply *GetBlockReplyPart
		err = json.Unmarshal(*rpcResp.Result, &reply)
		return reply, err
	}
	return nil, nil
}

func (r *RPCClient) GetBlockByHeight(height int64) (*GetBlockReply, error) {
	params := []interface{}{fmt.Sprintf("0x%x", height), true}
	return r.getBlockBy("sero_getBlockByNumber", params)
}

func (r *RPCClient) GetBlockByHash(hash string) (*GetBlockReply, error) {
	params := []interface{}{hash, true}
	return r.getBlockBy("sero_getBlockByHash", params)
}

func (r *RPCClient) GetUncleByBlockNumberAndIndex(height int64, index int) (*GetBlockReply, error) {
	params := []interface{}{fmt.Sprintf("0x%x", height), fmt.Sprintf("0x%x", index)}
	return r.getBlockBy("sero_getUncleByBlockNumberAndIndex", params)
}

func (r *RPCClient) getBlockBy(method string, params []interface{}) (*GetBlockReply, error) {
	rpcResp, err := r.doPost(r.Url, method, params)
	if err != nil {
		return nil, err
	}
	if rpcResp.Result != nil {
		var reply *GetBlockReply
		err = json.Unmarshal(*rpcResp.Result, &reply)
		return reply, err
	}
	return nil, nil
}

func (r *RPCClient) GetTxReceipt(hash string) (*TxReceipt, error) {
	rpcResp, err := r.doPost(r.Url, "sero_getTransactionReceipt", []string{hash})
	if err != nil {
		return nil, err
	}
	if rpcResp.Result != nil {
		var reply *TxReceipt
		err = json.Unmarshal(*rpcResp.Result, &reply)
		return reply, err
	}
	return nil, nil
}

func (r *RPCClient) SubmitBlock(params []string) (bool, error) {
	rpcResp, err := r.doPost(r.Url, "sero_submitWork", params)
	if err != nil {
		return false, err
	}
	var reply bool
	err = json.Unmarshal(*rpcResp.Result, &reply)
	return reply, err
}

type Balance struct {
	Tkn map[string]*hexutil.Big   `json:"tkn"`
	Tkt map[string][]*common.Hash `json:"tkt"`
}

func (r *RPCClient) GetBalance(address string) (*big.Int, error) {
	rpcResp, err := r.doPost(r.Url, "sero_getBalance", []string{address, "latest"})
	if err != nil {
		return nil, err
	}
	var reply Balance
	err = json.Unmarshal(*rpcResp.Result, &reply)
	if err != nil {
		return nil, err
	}
	if v, ok := reply.Tkn["SERO"]; ok {
		return (*big.Int)(v), err
	}

	return big.NewInt(0), err
}

func (r *RPCClient) AddressUnlocked(from string) (bool, error) {
	rpcResp, err := r.doPost(r.Url, "sero_addressUnlocked", []string{from})
	var reply bool
	if err != nil {
		return false, err
	}
	err = json.Unmarshal(*rpcResp.Result, &reply)
	if err != nil {
		return false, err
	}
	return reply, err
}

func (r *RPCClient) GetPeerCount() (int64, error) {
	rpcResp, err := r.doPost(r.Url, "net_peerCount", nil)
	if err != nil {
		return 0, err
	}
	var reply string
	err = json.Unmarshal(*rpcResp.Result, &reply)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.Replace(reply, "0x", "", -1), 16, 64)
}

func (r *RPCClient) SendTransaction(from, to, gas, gasPrice, value string, autoGas bool) (string, error) {
	params := map[string]string{
		"from":  from,
		"to":    to,
		"value": value,
	}
	if !autoGas {
		params["gas"] = gas
		params["gasPrice"] = gasPrice
	}
	rpcResp, err := r.doPost(r.Url, "sero_sendTransaction", []interface{}{params})
	var reply string
	if err != nil {
		return reply, err
	}
	err = json.Unmarshal(*rpcResp.Result, &reply)
	if err != nil {
		return reply, err
	}
	/* There is an inconsistence in a "standard". Geth returns error if it can't unlock signer account,
	 * but Parity returns zero hash 0x000... if it can't send tx, so we must handle this case.
	 * https://github.com/ethereum/wiki/wiki/JSON-RPC#returns-22
	 */
	if util.IsZeroHash(reply) {
		err = errors.New("transaction is not yet available")
	}
	return reply, err
}

type ReceptionArgs struct {
	Addr     string
	Currency string
	Value    *big.Int
}

type GenTxArgs struct {
	From       string
	Receptions []ReceptionArgs
	Gas        uint64
	GasPrice   uint64
	Roots      []hexutil.Bytes
}

type GTx struct {
	Hash hexutil.Bytes
}

func base58ToHex(bs string) string {

	return hexutil.Encode(base58.Decode(bs))

}

func (r *RPCClient) GetMaxAvailable(address string) (*big.Int, error) {
	hexAddress := base58ToHex(address)
	rpcResp, err := r.doPost(r.Url, "exchange_getMaxAvailable", []string{hexAddress, "SERO"})
	if err != nil {
		return nil, err
	}
	var reply *big.Int
	err = json.Unmarshal(*rpcResp.Result, &reply)
	if err != nil {
		return nil, err
	}
	return reply, err
}

func (r *RPCClient) ClearExchange(addres string) error {
	_, err := r.doPost(r.Url, "exchange_clearUsedFlag", []string{base58ToHex(addres)})
	if err != nil {
		return err
	}
	return nil
}

func (r *RPCClient) GenTxWithSign(from string, gas uint64, gasPrice uint64, pays map[string]*big.Int) (*json.RawMessage, string, error) {
	fromAddress := base58ToHex(from)
	receptions := []ReceptionArgs{}
	for k, v := range pays {
		receptions = append(receptions, ReceptionArgs{
			Addr:     base58ToHex(k),
			Currency: "SERO",
			Value:    v,
		})
	}
	args := GenTxArgs{
		fromAddress,
		receptions, gas, gasPrice, []hexutil.Bytes{},
	}
	rpcResp, err := r.doPost(r.Url, "exchange_genTxWithSign", []interface{}{args})
	if err != nil {
		return nil, "", err
	}
	var gtx GTx
	err = json.Unmarshal(*rpcResp.Result, &gtx)

	if err != nil {
		return nil, "", err
	}
	return rpcResp.Result, hexutil.Encode(gtx.Hash), nil

}
func (r *RPCClient) CommitTx(data *json.RawMessage, txhash string) error {

	_, err := r.doPost(r.Url, "exchange_commitTx", []interface{}{*data})
	if err != nil {
		return err
	}
	return nil
}

func (r *RPCClient) SendExchangeTransactions(from string, gas uint64, gasPrice uint64, pays map[string]*big.Int) (string, error) {
	fromAddress := base58ToHex(from)
	receptions := []ReceptionArgs{}
	for k, v := range pays {
		receptions = append(receptions, ReceptionArgs{
			Addr:     base58ToHex(k),
			Currency: "SERO",
			Value:    v,
		})
	}
	args := GenTxArgs{
		fromAddress,
		receptions, gas, gasPrice, []hexutil.Bytes{},
	}

	rpcResp, err := r.doPost(r.Url, "exchange_genTxWithSign", []interface{}{args})
	if err != nil {
		return "", err
	}
	var gtx GTx
	err = json.Unmarshal(*rpcResp.Result, &gtx)

	if err != nil {
		return "", err
	}
	_, err = r.doPost(r.Url, "exchange_commitTx", []interface{}{*rpcResp.Result})
	if err != nil {
		return "", err
	}

	return hexutil.Encode(gtx.Hash), err
}

func (r *RPCClient) GetPkSynced(from string) (uint64, uint64, uint64, uint64, error) {
	fromAddress := base58ToHex(from)
	rpcResp, err := r.doPost(r.Url, "exchange_getPkSynced", []interface{}{fromAddress})
	if err != nil {
		return 0, 0, 0, 0, err
	}
	result := map[string]interface{}{}
	err = json.Unmarshal(*rpcResp.Result, &result)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	var confirmBlock, currentBlock, hightBlock, pkBlock uint64
	for k, v := range result {
		if k == "confirmedBlock" {
			confirmBlock = v.(uint64)
		}
		if k == "currentBlock" {
			currentBlock = v.(uint64)
		}
		if k == "highestBlock" {
			hightBlock = v.(uint64)
		}
		if k == "currentPKBlock" {
			pkBlock = v.(uint64)
		}
	}
	return confirmBlock, currentBlock, hightBlock, pkBlock, nil
}

func (r *RPCClient) CanTx(from string, lastTxBlock uint64) (bool, error) {
	fromAddress := base58ToHex(from)
	rpcResp, err := r.doPost(r.Url, "exchange_getPkSynced", []interface{}{fromAddress})
	if err != nil {
		return false, err
	}
	result := map[string]interface{}{}
	err = json.Unmarshal(*rpcResp.Result, &result)
	if err != nil {
		return false, err
	}

	var confirmBlock, currentBlock, hightBlock, pkBlock uint64
	for k, v := range result {
		if k == "confirmedBlock" {
			confirmBlock = v.(uint64)
		}
		if k == "currentBlock" {
			currentBlock = v.(uint64)
		}
		if k == "highestBlock" {
			hightBlock = v.(uint64)
		}
		if k == "currentPKBlock" {
			pkBlock = v.(uint64)
		}
	}
	if currentBlock == hightBlock && confirmBlock+pkBlock+128 >= currentBlock {
		if currentBlock > lastTxBlock+confirmBlock {
			if pkBlock > lastTxBlock {
				return true, nil
			} else {
				return false, errors.New("Account balance is confirming")
			}

		} else {
			return false, errors.New("Account is confirming")
		}

	} else {
		return false, errors.New("Account is syncing")
	}

}

func (r *RPCClient) doPost(url string, method string, params interface{}) (*JSONRpcResp, error) {
	jsonReq := map[string]interface{}{"jsonrpc": "2.0", "method": method, "params": params, "id": 0}
	data, _ := json.Marshal(jsonReq)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	req.Header.Set("Content-Length", (string)(len(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		r.markSick()
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp *JSONRpcResp
	err = json.NewDecoder(resp.Body).Decode(&rpcResp)
	if err != nil {
		r.markSick()
		return nil, err
	}
	if rpcResp.Error != nil {
		r.markSick()
		return nil, errors.New(rpcResp.Error["message"].(string))
	}
	return rpcResp, err
}

func (r *RPCClient) Check() bool {
	_, err := r.GetWork()
	if err != nil {
		return false
	}
	r.markAlive()
	return !r.Sick()
}

func (r *RPCClient) Sick() bool {
	r.RLock()
	defer r.RUnlock()
	return r.sick
}

func (r *RPCClient) markSick() {
	r.Lock()
	r.sickRate++
	r.successRate = 0
	if r.sickRate >= 5 {
		r.sick = true
	}
	r.Unlock()
}

func (r *RPCClient) markAlive() {
	r.Lock()
	r.successRate++
	if r.successRate >= 5 {
		r.sick = false
		r.sickRate = 0
		r.successRate = 0
	}
	r.Unlock()
}
