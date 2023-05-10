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
	"bytes"
	"fmt"
	"math/big"
	"strconv"
	"sync"

	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/light"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	ssz "github.com/ferranbt/fastssz"
	"github.com/icon-project/btp2/common/codec"
	"github.com/icon-project/btp2/common/errors"
	"github.com/icon-project/btp2/common/link"
	"github.com/icon-project/btp2/common/log"
	"github.com/icon-project/btp2/common/types"

	"github.com/icon-project/btp2-eth2/chain/eth2/client"
	"github.com/icon-project/btp2-eth2/chain/eth2/proof"
)

const (
	eventSignature = "Message(string,uint256,bytes)"
)

type receiveStatus struct {
	slot int64 // finalized slot
	seq  int64 // last sequence number at slot
	buds []*blockUpdateData
}

func (r *receiveStatus) Height() int64 {
	return r.slot
}

func (r *receiveStatus) Seq() int64 {
	return r.seq
}

func (r *receiveStatus) Format(f fmt.State, c rune) {
	switch c {
	case 'v', 's':
		if f.Flag('+') {
			fmt.Fprintf(f, "receiveStatus{slot=%d seq=%d)", r.slot, r.seq)
		} else {
			fmt.Fprintf(f, "receiveStatus{%d %d)", r.slot, r.seq)
		}
	}
}

type receiver struct {
	l      log.Logger
	src    types.BtpAddress
	dst    types.BtpAddress
	cl     *client.ConsensusLayer
	el     *client.ExecutionLayer
	bmc    *client.BMC
	nid    int64
	rsc    chan link.ReceiveStatus
	seq    int64
	prevRS *receiveStatus
	rss    []*receiveStatus
	mps    []*messageProofData
	fq     *ethereum.FilterQuery
	ht     map[int64]*ssz.Node
}

func NewReceiver(src, dst types.BtpAddress, endpoint string, opt map[string]interface{}, l log.Logger) link.Receiver {
	var err error
	r := &receiver{
		src: src,
		dst: dst,
		l:   l,
		rsc: make(chan link.ReceiveStatus),
		rss: make([]*receiveStatus, 0),
		fq: &ethereum.FilterQuery{
			Addresses: []common.Address{common.HexToAddress(src.ContractAddress())},
			Topics: [][]common.Hash{
				{crypto.Keccak256Hash([]byte(eventSignature))},
			},
		},
		ht: make(map[int64]*ssz.Node, 0),
	}
	r.el, err = client.NewExecutionLayer(endpoint, l)
	if err != nil {
		l.Panicf("failed to connect to %s, %v", endpoint, err)
	}
	r.cl, err = client.NewConsensusLayer(opt["consensus_endpoint"].(string), l)
	if err != nil {
		l.Panicf("failed to connect to %s, %v", opt["consensus_endpoint"].(string), err)
	}
	r.bmc, err = client.NewBMC(common.HexToAddress(r.src.ContractAddress()), r.el.GetBackend())
	if err != nil {
		l.Panicf("fail to get instance of BMC %s, %v", r.src.ContractAddress(), err)
	}
	return r
}

func (r *receiver) Start(bls *types.BMCLinkStatus) (<-chan link.ReceiveStatus, error) {
	r.l.Debugf("Start eth2 receiver with BMCLinkStatus %+v", bls)

	go func() {
		r.Monitoring(bls)
	}()

	return r.rsc, nil
}

func (r *receiver) Stop() {
	close(r.rsc)
	r.cl.Term()
}

func (r *receiver) getStatus() (*types.BMCLinkStatus, error) {
	status, err := r.bmc.GetStatus(nil, r.dst.String())
	if err != nil {
		r.l.Errorf("Error retrieving status %s from BMC. %v", r.dst.String(), err)
		return nil, err
	}

	ls := &types.BMCLinkStatus{}
	ls.TxSeq = status.TxSeq.Int64()
	ls.RxSeq = status.RxSeq.Int64()
	ls.Verifier.Height = status.Verifier.Height.Int64()
	ls.Verifier.Extra = status.Verifier.Extra
	return ls, nil
}

func (r *receiver) GetStatus() (link.ReceiveStatus, error) {
	return r.rss[len(r.rss)-1], nil
}

func (r *receiver) GetHeightForSeq(seq int64) int64 {
	mp := r.GetMessageProofDataForSeq(seq)
	if mp == nil {
		return 0
	}
	return mp.Height()
}

func (r *receiver) GetMessageProofDataForSeq(seq int64) *messageProofData {
	for _, mp := range r.mps {
		if mp.Contains(seq) {
			return mp
		}
	}
	return nil
}

func (r *receiver) GetMessageProofDataForHeight(height int64) *messageProofData {
	for _, mp := range r.mps {
		if height == mp.Slot {
			return mp
		}
	}
	return nil
}

func (r *receiver) GetLastMessageProofDataForHeight(height int64) *messageProofData {
	var lmp *messageProofData
	for _, mp := range r.mps {
		if mp.Slot <= height {
			lmp = mp
		}
		if mp.Slot >= height {
			break
		}
	}
	return lmp
}

func (r *receiver) GetMarginForLimit() int64 {
	return 0
}

func (r *receiver) BuildBlockUpdate(bls *types.BMCLinkStatus, limit int64) ([]link.BlockUpdate, error) {
	bus := make([]link.BlockUpdate, 0)
	srcHeight := bls.Verifier.Height
	for i, rs := range r.rss {
		r.l.Debugf("Build BlockUpdate #%d for %+v", i, rs)
		for _, bud := range rs.buds {
			bu := NewBlockUpdate(srcHeight, bud)
			bus = append(bus, bu)
			srcHeight = int64(bud.FinalizedHeader.Beacon.Slot)
		}
	}
	return bus, nil
}

func (r *receiver) BuildBlockProof(bls *types.BMCLinkStatus, height int64) (link.BlockProof, error) {
	r.l.Debugf("Build BlockProof for H:%d", height)
	if height > bls.Verifier.Height {
		return nil, errors.InvalidStateError.Errorf("%d slot is not yet finalized", height)
	}
	mp := r.GetMessageProofDataForHeight(height)
	if mp == nil {
		return nil, errors.InvalidStateError.Errorf("there is no message at slot %d", height)
	}

	return r.blockProofForMessageProof(bls, mp)
}

func (r *receiver) blockProofForMessageProof(bls *types.BMCLinkStatus, mp *messageProofData) (link.BlockProof, error) {
	var bpd *blockProofData
	var err error

	if bls.Verifier.Height-mp.Slot < SlotPerHistoricalRoot {
		bpd, err = r.blockProofDataViaBlockRoots(bls, mp)
	} else {
		bpd, err = r.blockProofDataViaHistoricalSummaries(bls, mp)
	}
	if err != nil {
		return nil, err
	}
	r.l.Debugf("new BlockProof for slot:%d", bpd.Header.Beacon.Slot)
	return &BlockProof{
		relayMessageItem: relayMessageItem{
			it:      link.TypeBlockProof,
			payload: codec.RLP.MustMarshalToBytes(bpd),
		},
		ph: mp.Slot,
	}, nil
}

func (r *receiver) blockProofDataViaBlockRoots(bls *types.BMCLinkStatus, mp *messageProofData) (*blockProofData, error) {
	header := mp.Header.Beacon
	gindex := proof.BlockRootsIdxToGIndex(SlotToBlockRootsIndex(header.Slot))
	r.l.Debugf("make blockProofData with blockRoots. slot:%d, gIndex:%d", header.Slot, gindex)
	bp, err := r.cl.GetStateProofWithGIndex(strconv.FormatInt(bls.Verifier.Height, 10), gindex)
	if err != nil {
		return nil, err
	}
	blockProof, err := proof.NewSingleProof(bp)
	if err != nil {
		return nil, err
	}
	root, err := header.HashTreeRoot()
	if err != nil {
		return nil, err
	}
	if bytes.Compare(root[:], blockProof.Leaf()) != 0 {
		return nil, errors.InvalidStateError.Errorf("invalid blockProofData. H:%#x != BP:%#x", root, blockProof.Leaf())
	}
	return &blockProofData{
		Header: mp.Header,
		Proof:  blockProof.SSZ(),
	}, nil
}

func (r *receiver) blockProofDataViaHistoricalSummaries(bls *types.BMCLinkStatus, mp *messageProofData) (*blockProofData, error) {
	header := mp.Header.Beacon
	gindex := proof.HistoricalSummariesIdxToGIndex(SlotToHistoricalSummariesIndex(header.Slot))
	r.l.Debugf("make blockProofData with historicalSummaries. slot:%d, gIndex:%d", header.Slot, gindex)
	bp, err := r.cl.GetStateProofWithGIndex(strconv.FormatInt(bls.Verifier.Height, 10), gindex)
	if err != nil {
		return nil, err
	}
	blockProof, err := proof.NewSingleProof(bp)
	if err != nil {
		return nil, err
	}

	tr, err := r.getHistoricalSummariesTrie(mp)
	if err != nil {
		return nil, err
	}
	gindex = proof.ArrayIdxToGIndex(1, SlotPerHistoricalRoot, SlotToBlockRootsIndex(header.Slot), 1)
	hProof, err := tr.Prove(int(gindex))
	if err != nil {
		return nil, err
	}

	return &blockProofData{
		Header:          mp.Header,
		Proof:           blockProof.SSZ(),
		HistoricalProof: hProof,
	}, nil
}

func (r *receiver) getHistoricalSummariesTrie(mp *messageProofData) (*ssz.Node, error) {
	header := mp.Header.Beacon
	roots := make([][]byte, SlotPerHistoricalRoot, SlotPerHistoricalRoot)
	start := int64(HistoricalSummariesStartSlot(header.Slot))
	if _, ok := r.ht[start]; !ok {
		var prevRoot []byte
		for i := int64(1); i < SlotPerHistoricalRoot; i++ {
			root, err := r.cl.BeaconBlockRoot(strconv.FormatInt(start-i, 10))
			if root != nil && err == nil {
				prevRoot = root[:]
				break
			}
		}
		for i := int64(0); i < SlotPerHistoricalRoot; i++ {
			root, err := r.cl.BeaconBlockRoot(strconv.FormatInt(start+i, 10))
			if root == nil || err != nil {
				if i > 0 {
					roots[i] = roots[i-1]
				} else {
					roots[i] = prevRoot
				}
			} else {
				roots[i] = root[:]
			}
		}
		rfs := &proof.RootsForHistory{
			Roots: roots,
		}
		tr, err := rfs.GetTree()
		if err != nil {
			return nil, err
		}
		r.ht[start] = tr
	}
	return r.ht[start], nil
}

func (r *receiver) BuildMessageProof(bls *types.BMCLinkStatus, limit int64) (link.MessageProof, error) {
	r.l.Debugf("Build MessageProof for bls:%+v", bls)
	mpd := r.GetMessageProofDataForSeq(bls.RxSeq + 1)
	if mpd == nil {
		return nil, nil
	}
	if bls.Verifier.Height < mpd.Height() {
		return nil, nil
	}

	// TODO handle oversize mp
	r.l.Debugf("new MessageProof with h:%d, seq:%d-%d", mpd.Height(), mpd.StartSeq, mpd.EndSeq)
	return NewMessageProof(bls, mpd.EndSeq, mpd), nil
}

func (r *receiver) BuildRelayMessage(rmis []link.RelayMessageItem) ([]byte, error) {
	bm := &BTPRelayMessage{
		Messages: make([]*TypePrefixedMessage, 0),
	}

	for i, rmi := range rmis {
		r.l.Debugf("Build relay message #%d. type:%d, len:%d", i, rmi.Type(), rmi.Len())
		tpm, err := NewTypePrefixedMessage(rmi)
		if err != nil {
			return nil, err
		}
		bm.Messages = append(bm.Messages, tpm)
	}

	rb, err := codec.RLP.MarshalToBytes(bm)
	if err != nil {
		return nil, err
	}

	return rb, nil
}

func (r *receiver) FinalizedStatus(blsc <-chan *types.BMCLinkStatus) {
	go func() {
		for {
			select {
			case bls := <-blsc:
				r.l.Debugf("finalizedStatus %+v", bls)
				r.clearData(bls)
			}
		}
	}()
}

func (r *receiver) clearData(bls *types.BMCLinkStatus) {
	for i, rs := range r.rss {
		if rs.Height() == bls.Verifier.Height && rs.Seq() == bls.RxSeq {
			r.l.Debugf("remove receiveStatue to %d/%d, %+v", i, len(r.rss), rs)
			r.rss = r.rss[i+1:]
			break
		}
	}
	for i, mp := range r.mps {
		if mp.EndSeq == bls.RxSeq {
			r.l.Debugf("remove messageProofData to %d, %s", i, mp)
			r.mps = r.mps[i+1:]
			break
		}
	}
}

func (r *receiver) Monitoring(bls *types.BMCLinkStatus) error {
	once := new(sync.Once)
	if bls.Verifier.Height < 1 {
		err := fmt.Errorf("cannot catchup from zero height")
		r.l.Debug(err)
		return err
	}
	if bls.RxSeq != 0 {
		r.seq = bls.RxSeq
	}

	if r.prevRS == nil {
		r.prevRS = &receiveStatus{
			slot: bls.Verifier.Height,
			seq:  bls.RxSeq,
		}
	}

	eth2Topics := []string{client.TopicLCOptimisticUpdate, client.TopicLCFinalityUpdate}
	r.l.Debugf("Start ethereum monitoring")
	if err := r.cl.Events(eth2Topics, func(event *api.Event) {
		if event.Topic == client.TopicLCOptimisticUpdate {
			update := event.Data.(*altair.LightClientOptimisticUpdate)
			slot := update.AttestedHeader.Beacon.Slot
			r.l.Debugf("Get light client optimistic update. slot:%d", slot)
			once.Do(func() {
				r.l.Debugf("Check undelivered messages")
				status, err := r.getStatus()
				if err != nil {
					r.l.Panicf("%+v", err)
				}
				if bls.RxSeq < status.TxSeq {
					err = r.handleUndeliveredMessages(
						bls.Verifier.Height-SlotPerEpoch, bls.RxSeq+1,
						int64(slot)-1, status.TxSeq,
					)
					if err != nil {
						r.l.Panicf("failed to add missing message. %+v", err)
					}
				}
			})
			mp, err := r.makeMessageProofData(update.AttestedHeader)
			if err != nil {
				r.l.Warnf("fail to make messageProofData. %+v", err)
				return
			}
			if mp != nil {
				r.seq += mp.MessageCount()
				r.mps = append(r.mps, mp)
				r.l.Debugf("append new mp: %s", mp)
			}
		} else if event.Topic == client.TopicLCFinalityUpdate {
			update := event.Data.(*altair.LightClientFinalityUpdate)
			slot := int64(update.FinalizedHeader.Beacon.Slot)
			r.l.Debugf("Get light client finality update. slot:%d", slot)
			if bls.Verifier.Height >= slot {
				r.l.Debugf("skip already processed finality update")
				return
			}
			buds, err := r.makeBlockUpdateDatas(bls, update)
			if err != nil {
				r.l.Warnf("failed to make blockUpdateData. %+v", err)
				return
			}
			lastSeq := r.prevRS.Seq()
			lmp := r.GetLastMessageProofDataForHeight(slot)
			if lmp != nil {
				lastSeq = lmp.EndSeq
			}
			rs := &receiveStatus{seq: lastSeq, slot: slot, buds: buds}
			r.rss = append(r.rss, rs)
			r.rsc <- rs
			r.prevRS = rs
		}
	}); err != nil {
		r.l.Debugf("onError %+v", err)
	}
	return nil
}

func (r *receiver) makeBlockUpdateDatas(
	bls *types.BMCLinkStatus,
	update *altair.LightClientFinalityUpdate,
) ([]*blockUpdateData, error) {
	var nsc *altair.SyncCommittee
	var nscBranch [][]byte
	buds := make([]*blockUpdateData, 0)
	scPeriod := SlotToSyncCommitteePeriod(update.FinalizedHeader.Beacon.Slot)
	blsSCPeriod := SlotToSyncCommitteePeriod(phase0.Slot(bls.Verifier.Height))

	if scPeriod > blsSCPeriod {
		lcUpdate, err := r.cl.LightClientUpdates(blsSCPeriod, scPeriod-blsSCPeriod)
		if err != nil {
			return nil, err
		}
		for _, lcu := range lcUpdate {
			r.l.Debugf("make old blockUpdateData for lightClient update. scPeriod=%d", SlotToSyncCommitteePeriod(lcu.SignatureSlot))
			bud := &blockUpdateData{
				AttestedHeader:          lcu.AttestedHeader,
				FinalizedHeader:         lcu.FinalizedHeader,
				FinalizedHeaderBranch:   lcu.FinalityBranch,
				SyncAggregate:           lcu.SyncAggregate,
				SignatureSlot:           lcu.SignatureSlot,
				NextSyncCommittee:       lcu.NextSyncCommittee,
				NextSyncCommitteeBranch: lcu.NextSyncCommitteeBranch,
			}
			buds = append(buds, bud)
		}
	}

	if IsSyncCommitteeEdge(update.FinalizedHeader.Beacon.Slot) {
		r.l.Debugf("make NextSyncCommittee for scPeriod=%d", scPeriod)
		lcUpdate, err := r.cl.LightClientUpdates(scPeriod, 1)
		if err != nil {
			return nil, err
		}
		lcu := lcUpdate[0]
		nsc = lcu.NextSyncCommittee
		nscBranch = lcu.NextSyncCommitteeBranch
	}

	// append blockUpdateData made by FinalityUpdate
	r.l.Debugf("make blockUpdateData for finality update")
	bud := &blockUpdateData{
		AttestedHeader:          update.AttestedHeader,
		FinalizedHeader:         update.FinalizedHeader,
		FinalizedHeaderBranch:   update.FinalityBranch,
		SyncAggregate:           update.SyncAggregate,
		SignatureSlot:           update.SignatureSlot,
		NextSyncCommittee:       nsc,
		NextSyncCommitteeBranch: nscBranch,
	}
	buds = append(buds, bud)

	return buds, nil
}

func (r *receiver) handleUndeliveredMessages(from, fromSeq, to, toSeq int64) error {
	r.l.Debugf("start to find undelivered BTP messages. from %d(%d) to %d(%d)", from, fromSeq, to, toSeq)
	for i := from; i <= to; i++ {
		bh, err := r.cl.BeaconBlockHeader(strconv.FormatInt(i, 10))
		if bh == nil || err != nil {
			continue
		}
		mp, err := r.makeMessageProofData(&altair.LightClientHeader{Beacon: bh.Header.Message})
		if err != nil {
			return err
		}
		if mp == nil {
			continue
		}
		r.seq += mp.MessageCount()
		r.mps = append(r.mps, mp)
		r.l.Debugf("append undelivered mp: %s", mp)
		if r.seq >= toSeq {
			break
		}
	}
	return nil
}

func (r *receiver) makeMessageProofData(header *altair.LightClientHeader) (mp *messageProofData, err error) {
	slot := int64(header.Beacon.Slot)
	elBlockNum, err := r.cl.SlotToBlockNumber(phase0.Slot(slot))
	if err != nil {
		err = errors.NotFoundError.Wrapf(err, "fail to get block number")
		return
	}

	bn := big.NewInt(int64(elBlockNum))
	logs, err := r.getEventLogs(bn, bn)
	if err != nil {
		return
	}
	if len(logs) == 0 {
		return
	}
	r.l.Debugf("Get %d BTP messages at slot:%d, blockNum:%d", len(logs), slot, elBlockNum)

	receiptProofs, err := r.makeReceiptProofs(bn, logs)
	if err != nil {
		return
	}

	receiptsRootProof, err := r.makeReceiptsRootProof(slot)
	if err != nil {
		return
	}

	bms, _ := r.bmc.ParseMessage(logs[0])
	bme, _ := r.bmc.ParseMessage(logs[len(logs)-1])

	mp = &messageProofData{
		Slot:              slot,
		ReceiptsRootProof: receiptsRootProof,
		ReceiptProofs:     receiptProofs,
		Header:            header,
		StartSeq:          bms.Seq.Int64(),
		EndSeq:            bme.Seq.Int64(),
	}

	return
}

func (r *receiver) getEventLogs(from, to *big.Int) ([]etypes.Log, error) {
	fq := *r.fq
	fq.FromBlock = from
	fq.ToBlock = to
	logs, err := r.el.FilterLogs(fq)
	for err != nil {
		return nil, err
	}

	idxToSkip := -1
	seq := r.seq + 1
	for i, l := range logs {
		message, err := r.bmc.ParseMessage(l)
		if err != nil {
			return nil, err
		}
		if seq < message.Seq.Int64() {
			// TODO just error?
			err = errors.IllegalArgumentError.Errorf(
				"sequence number of BTP message is not continuous (e:%d r:%d)",
				seq, message.Seq.Int64(),
			)
			return nil, err
		} else if seq > message.Seq.Int64() {
			r.l.Debugf("skip already relayed message. (i:%d e:%d r:%d)", i, seq, message.Seq.Int64())
			idxToSkip = i
		} else {
			r.l.Debugf("BTP Message#%d[seq:%d] dst:%s", i, message.Seq, r.dst.String())
		}
		seq += 1
	}
	if idxToSkip > -1 {
		logs = logs[idxToSkip+1:]
	}
	return logs, nil
}

// makeReceiptProofs make proofs for receipts which has BTP message
func (r *receiver) makeReceiptProofs(bn *big.Int, logs []etypes.Log) ([]*receiptProof, error) {
	receipts, err := r.getReceipts(bn)
	if err != nil {
		return nil, err
	}

	receiptTrie, err := trieFromReceipts(receipts)
	if err != nil {
		return nil, err
	}

	return getReceiptProofs(receiptTrie, logs)
}

func (r *receiver) getReceipts(bn *big.Int) ([]*etypes.Receipt, error) {
	block, err := r.el.BlockByNumber(bn)
	if err != nil {
		err = errors.NotFoundError.Wrapf(err, "fail to get block %s", bn)
		return nil, err
	}

	receipts := make([]*etypes.Receipt, 0)
	for _, tx := range block.Transactions() {
		receipt, err := r.el.TransactionReceipt(tx.Hash())
		if err != nil {
			err = errors.NotFoundError.Wrapf(err, "fail to get transactionReceipt %#x", tx.Hash())
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

// trieFromReceipts make receipt MPT with receipts
func trieFromReceipts(receipts etypes.Receipts) (*trie.Trie, error) {
	db := trie.NewDatabase(rawdb.NewMemoryDatabase())
	tr := trie.NewEmpty(db)

	for _, r := range receipts {
		key, err := rlp.EncodeToBytes(r.TransactionIndex)
		if err != nil {
			err = errors.UnknownError.Wrapf(err, "fail to encode TX index %d", r.TransactionIndex)
			return nil, err
		}

		rawReceipt, err := r.MarshalBinary()
		if err != nil {
			err = errors.UnknownError.Wrapf(err, "fail to marshal TX receipt %#x", r.TxHash)
			return nil, err
		}
		tr.Update(key, rawReceipt)
	}
	return tr, nil
}

func getReceiptProofs(tr *trie.Trie, logs []etypes.Log) ([]*receiptProof, error) {
	keys := make(map[uint]bool)
	rps := make([]*receiptProof, 0)
	for _, l := range logs {
		idx := l.TxIndex
		if _, ok := keys[idx]; ok {
			continue
		}
		keys[idx] = true
		key, err := rlp.EncodeToBytes(idx)
		if err != nil {
			return nil, err
		}
		nodes := light.NewNodeSet()
		err = tr.Prove(key, 0, nodes)
		if err != nil {
			return nil, err
		}
		p, err := rlp.EncodeToBytes(nodes.NodeList())
		if err != nil {
			return nil, err
		}
		rps = append(rps, &receiptProof{Key: key, Proof: p})
	}
	return rps, nil
}

func (r *receiver) makeReceiptsRootProof(slot int64) (*ssz.Proof, error) {
	rrProof, err := r.cl.GetReceiptsRootProof(slot)
	if err != nil {
		err = errors.NotFoundError.Wrapf(err, "fail to make receiptsRoot proof")
		return nil, err
	}
	return proof.TreeOffsetProofToSSZProof(rrProof)
}
