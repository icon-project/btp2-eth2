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
	"strconv"
	"strings"
	"sync"
	"time"

	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/icon-project/btp2/common/errors"
	"github.com/icon-project/btp2/common/log"
	"github.com/icon-project/btp2/common/types"
	"github.com/icon-project/btp2/common/wallet"

	"github.com/icon-project/btp2-eth2/chain/eth2/client"
)

const (
	txMaxDataSize       = 524288 //512 * 1024 // 512kB
	txOverheadScale     = 0.37   //base64 encoding overhead 0.36, rlp and other fields 0.01
	GetTXResultInterval = SecondPerSlot * time.Second
	txPendingMAX        = 3 // unit: finality
)

var (
	txSizeLimit = int(math.Ceil(txMaxDataSize / (1 + txOverheadScale)))
)

type request struct {
	rm             types.RelayMessage
	txHash         common.Hash
	txPendingCount int
}

func (r *request) RelayMessage() types.RelayMessage {
	return r.rm
}

func (r *request) ID() string {
	return r.rm.Id()
}

func (r *request) TxHash() common.Hash {
	return r.txHash
}

func (r *request) SetTxHash(txHash common.Hash) {
	r.txHash = txHash
}

func (r *request) SetTxPendingCount(value int) {
	r.txPendingCount = value
}

func (r *request) IncTxPendingCount() int {
	r.txPendingCount += 1
	return r.txPendingCount
}

func (r *request) Format(f fmt.State, c rune) {
	switch c {
	case 'v', 's':
		if f.Flag('+') {
			fmt.Fprintf(f, "request{id=%s txHash=%#x txPendingCount=%d)", r.ID(), r.txHash, r.txPendingCount)
		} else {
			fmt.Fprintf(f, "request{%s %#x %d)", r.ID(), r.txHash, r.txPendingCount)
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
		s.addRequest(&request{rm: rm, txHash: tx.Hash()})
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

func (s *sender) checkRelayResult(to uint64) {
	finished := make([]*request, 0)
	s.mtx.RLock()
	for i, req := range s.reqs {
		_, pending, err := s.el.TransactionByHash(req.TxHash())
		if err != nil {
			s.l.Warnf("can't get TX %#x. %v", req.TxHash(), err)
			break
		}
		if pending {
			s.l.Debugf("TX %#x is not yet executed.", req.TxHash())
			if req.IncTxPendingCount() == txPendingMAX {
				s.l.Debugf("resend rm %s", req.ID())
				if tx, err := s.relay(req.RelayMessage()); err != nil {
					s.l.Errorf("fail to resend relay message %s", req.ID())
				} else {
					req.SetTxHash(tx.Hash())
					req.SetTxPendingCount(0)
				}
			}
			s.reqs[i] = req
			s.l.Debugf("update req: %+v", s.reqs[i])
			break
		}
		receipt, err := s.el.TransactionReceipt(req.TxHash())
		if err != nil {
			s.l.Warnf("can't get TX receipt for %#x. %v", req.TxHash(), err)
			break
		}
		if to < receipt.BlockNumber.Uint64() {
			s.l.Debugf("%#x is not yet finalized", req.TxHash())
			break
		}
		err = s.receiptToRevertError(receipt)
		errCode := errors.SUCCESS
		if err != nil {
			s.l.Debugf("result fail %v. %v", req, err)
			if ec, ok := errors.CoderOf(err); ok {
				errCode = ec.ErrorCode()
			} else {
				errCode = errors.BMVUnknown
			}
		} else {
			s.l.Debugf("result success. %v", req)
		}
		s.sc <- &types.RelayResult{
			Id:        req.ID(),
			Err:       errCode,
			Finalized: true,
		}
		finished = append(finished, req)
	}
	s.mtx.RUnlock()

	for _, req := range finished {
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
