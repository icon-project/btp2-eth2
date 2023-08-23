/*
 * Copyright 2023 ICON Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package eth2

import (
	"fmt"
	"math"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/icon-project/btp2/common/codec"
	"github.com/icon-project/btp2/common/errors"
	"github.com/icon-project/btp2/common/log"
	"github.com/icon-project/btp2/common/types"
	"github.com/icon-project/btp2/common/wallet"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/icon-project/btp2-eth2/chain/eth2/client"
)

const (
	txMaxDataSize    = 524288 //512 * 1024 // 512kB
	txOverheadScale  = 0.37   //base64 encoding overhead 0.36, rlp and other fields 0.01
	blockInterval    = 12 * time.Second
	txExpireDuration = 5 * blockInterval
)

var (
	txSizeLimit = int(math.Ceil(txMaxDataSize / (1 + txOverheadScale)))
	lastTXKey   = []byte("last_tx")
)

type request struct {
	rm types.RelayMessage
	transaction
}

func (r *request) ID() string {
	return r.rm.Id()
}

func newRequest(rm types.RelayMessage, txHash common.Hash) *request {
	now := time.Now()
	return &request{
		rm: rm,
		transaction: transaction{
			TxHash:   txHash,
			TS:       now,
			ExpireTS: now.Add(txExpireDuration),
		},
	}
}

func (r *request) Format(f fmt.State, c rune) {
	switch c {
	case 'v', 's':
		if f.Flag('+') {
			fmt.Fprintf(f, "request{id=%s tx=%+v)", r.ID(), r.transaction)
		} else {
			fmt.Fprintf(f, "request{%s %v)", r.ID(), r.transaction)
		}
	}
}

type sender struct {
	src  types.BtpAddress
	dst  types.BtpAddress
	w    types.Wallet
	l    log.Logger
	sc   chan *types.RelayResult
	reqs []*request
	mtx  sync.RWMutex
	db   *leveldb.DB

	cl  *client.ConsensusLayer
	el  *client.ExecutionLayer
	bmc *client.BMCClient

	gasLimit uint64
}

func newSender(src, dst types.BtpAddress, w types.Wallet, endpoint string, opt map[string]interface{}, baseDir string, l log.Logger) types.Sender {
	var err error
	s := &sender{
		src: src,
		dst: dst,
		w:   w,
		l:   l,
		sc:  make(chan *types.RelayResult),
	}

	if err = s.initDatabase(baseDir); err != nil {
		l.Panicf("fail to initialize database %v", err)
	}

	s.el, err = client.NewExecutionLayer(endpoint, l)
	if err != nil {
		l.Panicf("fail to connect to %s, %v", endpoint, err)
	}
	l.Debugf("Sender options %+v", opt)
	if clEndpoint, ok := opt["consensus_endpoint"].(string); ok {
		s.cl, err = client.NewConsensusLayer(clEndpoint, l)
		if err != nil {
			l.Panicf("fail to connect to %s, %v", clEndpoint, err)
		}
	}
	txUrl, _ := opt["execution_tx_endpoint"].(string)
	s.bmc, err = client.NewBMCClient(common.HexToAddress(s.dst.ContractAddress()), s.el.GetBackend(), txUrl, l)
	if err != nil {
		l.Panicf("fail to connect to BMC %s, %v", s.dst.ContractAddress(), err)
	}
	gasLimit, _ := opt["gas_limit"].(float64)
	s.gasLimit = uint64(gasLimit)
	return s
}

func (s *sender) initDatabase(baseDir string) error {
	var err error
	//dbDir := filepath.Join(baseDir, s.dst.NetworkAddress(), s.src.NetworkAddress())
	dbDir := filepath.Join(baseDir, s.src.NetworkAddress(), s.src.ContractAddress(),
		"sender", s.dst.NetworkAddress(), s.dst.ContractAddress())
	s.l.Debugln("open database", dbDir)
	s.db, err = leveldb.OpenFile(dbDir, nil)
	if err != nil {
		return errors.Wrap(err, "fail to open database")
	}
	defer func() {
		if err != nil {
			s.db.Close()
		}
	}()
	return nil
}

func (s *sender) Start() (<-chan *types.RelayResult, error) {
	go s.handleFinalityUpdate()

	return s.sc, nil
}

func (s *sender) Stop() {
	close(s.sc)
}

func (s *sender) GetStatus() (*types.BMCLinkStatus, error) {
	return s.getStatus(0)
}

func (s *sender) Relay(rm types.RelayMessage) (string, error) {
	if tx, err := s.relay(rm); err != nil {
		return rm.Id(), err
	} else {
		req := newRequest(rm, tx.Hash())
		s.addRequest(req)
		if err = s.putLastTransaction(&req.transaction); err != nil {
			s.removeRequest(rm.Id())
			return rm.Id(), err
		}
	}
	return rm.Id(), nil
}

func (s *sender) relay(rm types.RelayMessage) (*etypes.Transaction, error) {
	s.l.Debugf("relay src address:%s rm id:%s", s.src.String(), rm.Id())
	t, err := s.el.NewTransactOpts(s.w.(*wallet.EvmWallet).Skey, s.gasLimit)
	if err != nil {
		return nil, err
	}

	return s.bmc.HandleRelayMessage(t, s.src.String(), rm.Bytes())
}

func (s *sender) GetPreference() types.Preference {
	p := types.Preference{
		TxSizeLimit:       int64(txSizeLimit),
		MarginForLimit:    int64(0),
		LatestResult:      false,
		FilledBlockUpdate: false,
	}

	return p
}

func (s *sender) addRequest(req *request) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.l.Debugf("add request %+v", req)
	s.reqs = append(s.reqs, req)
}

func (s *sender) removeRequest(id string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for i, req := range s.reqs {
		if id == req.ID() {
			s.reqs = append(s.reqs[:i], s.reqs[i+1:]...)
			s.l.Debugf("remove request %+v", req)
			return
		}
	}
}

func (s *sender) getStatus(bn uint64) (*types.BMCLinkStatus, error) {
	if ltx, err := s.getLastTransaction(); err != nil {
		s.l.Errorf("fail to get last TX. %v", err)
		return nil, err
	} else if ltx != nil {
		s.l.Debugf("last TX. %+v", ltx)
		var pending bool
		if _, pending, err = s.el.TransactionByHash(ltx.TxHash); err != nil {
			s.l.Errorf("fail to query last TX %#x. %v", ltx.TxHash, err)
			return nil, err
		} else if pending {
			if !ltx.IsExpired() {
				s.l.Debugf("last TX %#x is not yet executed. sleep to %s", ltx.TxHash, ltx.ExpireTS)
				time.Sleep(ltx.GetDurationToExpire())
			}
		}
	}

	var status client.TypesLinkStatus
	var callOpts *bind.CallOpts
	if bn != 0 {
		callOpts = &bind.CallOpts{
			BlockNumber: big.NewInt(int64(bn)),
		}
	}
	status, err := s.bmc.GetStatus(callOpts, s.src.String())

	if err != nil {
		s.l.Errorf("Error retrieving relay status from BMC. %v", err)
		return nil, err
	}

	ls := &types.BMCLinkStatus{}
	ls.TxSeq = status.TxSeq.Int64()
	ls.RxSeq = status.RxSeq.Int64()
	ls.Verifier.Height = status.Verifier.Height.Int64()
	ls.Verifier.Extra = status.Verifier.Extra
	return ls, nil
}

func (s *sender) handleFinalityUpdate() {
	if err := s.cl.Events([]string{client.TopicLCFinalityUpdate}, func(event *api.Event) {
		update := event.Data.(*altair.LightClientFinalityUpdate)
		s.l.Debugf("handle finality_update event slot:%d", update.FinalizedHeader.Beacon.Slot)
		blockNumber, err := s.cl.SlotToBlockNumber(update.FinalizedHeader.Beacon.Slot)
		if err != nil {
			s.l.Warnf("can't convert slot to block number. %d", update.FinalizedHeader.Beacon.Slot)
			return
		}
		s.checkRelayResult(blockNumber)
	}); err != nil {
		s.l.Panicf("onError %v", err)
	}
}

func (s *sender) sendRelayResult(id string, errCode errors.Code, finalized bool) {
	s.sc <- &types.RelayResult{Id: id, Err: errCode, Finalized: finalized}
}

func (s *sender) checkRelayResult(to uint64) {
	finished := s._checkRelayResult(to)
	s._removeRequests(finished)
}

func (s *sender) _checkRelayResult(to uint64) (finished []*request) {
	finished = make([]*request, 0)
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, req := range s.reqs {
		_, pending, err := s.el.TransactionByHash(req.TxHash)
		if err != nil {
			s.l.Errorf("can't get TX via rpc. %+v. Drop %+v.", err, req)
			s.sendRelayResult(req.ID(), errors.BMVUnknown, false)
			return
		}
		if pending {
			if req.IsExpired() {
				s.l.Errorf("request is still pending. Drop %+v.", req)
				s.sendRelayResult(req.ID(), errors.BMVUnknown, false)
			}
			return
		}
		receipt, err := s.el.TransactionReceipt(req.TxHash)
		if err != nil {
			s.l.Errorf("can't get TX receipt. %+v. Drop %+v.", err, req)
			s.sendRelayResult(req.ID(), errors.BMVUnknown, false)
			return
		}
		if to < receipt.BlockNumber.Uint64() {
			s.l.Debugf("%#x is not yet finalized", req.TxHash)
			return
		}
		err = s.receiptToRevertError(receipt)
		errCode := errors.SUCCESS
		if err != nil {
			s.l.Debugf("result fail %+v. %v", req, err)
			if ec, ok := errors.CoderOf(err); ok {
				errCode = ec.ErrorCode()
			} else {
				errCode = errors.BMVUnknown
			}
		} else {
			s.l.Debugf("result success. %v", req)
		}
		s.sendRelayResult(req.ID(), errCode, true)
		finished = append(finished, req)
	}
	return
}

func (s *sender) _removeRequests(requests []*request) {
	for _, req := range requests {
		s.removeRequest(req.ID())
	}
}

func (s *sender) receiptToRevertError(receipt *etypes.Receipt) error {
	if receipt.Status == 0 {
		revertMsg, err := s.el.GetRevertMessage(receipt.TxHash)
		if err != nil {
			return err
		}
		msgs := strings.Split(revertMsg, ":")
		if len(msgs) > 2 {
			code, err := strconv.Atoi(strings.TrimLeft(msgs[1], " "))
			if err != nil {
				return err
			}
			return errors.NewRevertError(code)
		} else {
			return errors.NewRevertError(int(errors.BMVUnknown))
		}
	}
	return nil
}

func (s *sender) putLastTransaction(tx *transaction) error {
	if bs, err := tx.Bytes(); err != nil {
		return err
	} else {
		if err = s.db.Put(lastTXKey, bs, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *sender) getLastTransaction() (*transaction, error) {
	var bs []byte
	var has bool
	var err error
	if has, err = s.db.Has(lastTXKey, nil); err != nil {
		return nil, err
	} else if has {
		if bs, err = s.db.Get(lastTXKey, nil); err != nil {
			return nil, err
		} else {
			return newTransactionFromBytes(bs)
		}
	} else {
		return nil, nil
	}
}

type transaction struct {
	TxHash   common.Hash
	TS       time.Time
	ExpireTS time.Time // wait for TX propagation and pending time
}

func (tx *transaction) IsExpired() bool {
	return time.Now().After(tx.ExpireTS)
}

func (tx *transaction) GetDurationToExpire() time.Duration {
	ts := tx.ExpireTS
	return ts.Sub(time.Now())
}

func (tx *transaction) Bytes() ([]byte, error) {
	var bytes []byte
	if bs, err := codec.MarshalToBytes(&tx); err != nil {
		return nil, err
	} else {
		bytes = bs
	}
	return bytes, nil
}

func (tx *transaction) Format(f fmt.State, c rune) {
	switch c {
	case 'v', 's':
		if f.Flag('+') {
			fmt.Fprintf(f, "transaction{txHash=%#x TS=%s expireTS=%s)",
				tx.TxHash, tx.TS.Format(time.RFC3339), tx.ExpireTS.Format(time.RFC3339))
		} else {
			fmt.Fprintf(f, "transaction{%#x %s %s)",
				tx.TxHash, tx.TS.Format(time.RFC3339), tx.ExpireTS.Format(time.RFC3339))
		}
	}
}

func newTransaction(ts time.Time, txHash common.Hash) *transaction {
	return &transaction{
		TxHash:   txHash,
		TS:       ts,
		ExpireTS: ts.Add(txExpireDuration),
	}
}

func newTransactionFromBytes(bs []byte) (*transaction, error) {
	ltx := new(transaction)
	if _, err := codec.UnmarshalFromBytes(bs, &ltx); err != nil {
		return nil, err
	}
	return ltx, nil
}
