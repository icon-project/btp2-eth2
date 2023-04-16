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
	"encoding/binary"
	"fmt"
	"math"
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
)

const (
	eventSignature = "Message(string,uint256,bytes)"
)

type receiveStatus struct {
	slot int64 // finalized slot
	seq  int64 // last sequence number at slot
	bus  []*blockUpdateData
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
	// TODO call r.clearData() at r.FinalizedStatus() ?
	r.clearData(bls)
	bus := make([]link.BlockUpdate, 0)
	srcHeight := bls.Verifier.Height
	for _, rs := range r.rss {
		r.l.Debugf("Build BlockUpdate for H:%d", rs.Height())
		for _, blockUpdate := range rs.bus {
			bu := NewBlockUpdate(srcHeight, blockUpdate)
			bus = append(bus, bu)
			srcHeight = int64(blockUpdate.FinalizedHeader.Beacon.Slot)
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
	var path string
	if bls.Verifier.Height-mp.Slot < SlotPerHistoricalRoot {
		path = fmt.Sprintf("[\"blockRoots\",%d]", SlotToBlockRootsIndex(phase0.Slot(mp.Slot)))
	} else {
		// TODO need verification logic and tests
		path = fmt.Sprintf("[\"historicalRoots\",%d]", SlotToHistoricalRootsIndex(phase0.Slot(mp.Slot)))
	}
	proof, err := r.cl.GetStateProofWithPath(strconv.FormatInt(bls.Verifier.Height, 10), path)
	if err != nil {
		return nil, err
	}
	bp, err := SingleProofToSSZProof(proof)
	if err != nil {
		return nil, err
	}
	root, err := mp.Header.Beacon.HashTreeRoot()
	if bytes.Compare(root[:], bp.Leaf[:]) != 0 {
		return nil, errors.InvalidStateError.Errorf("invalid blockProofData. H:%#x != BP:%#x", root, bp.Leaf)
	}
	bpd := &blockProofData{
		Header: mp.Header,
		Proof:  bp,
	}
	r.l.Debugf("new BlockProof for slot:%d via %s", bpd.Header.Beacon.Slot, path)
	return &BlockProof{
		relayMessageItem: relayMessageItem{
			it:      link.TypeBlockProof,
			payload: codec.RLP.MustMarshalToBytes(bpd),
		},
		ph: mp.Slot,
	}, nil
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

func (r *receiver) FinalizedStatus(bls <-chan *types.BMCLinkStatus) {
	in := <-bls
	r.clearData(in)
}

func (r *receiver) clearData(bls *types.BMCLinkStatus) {
	for i, rs := range r.rss {
		if rs.Height() == bls.Verifier.Height && rs.Seq() == bls.RxSeq {
			r.l.Debugf("remove receiveStatue to %d, %+v", i, rs)
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
			bus, err := r.makeBlockUpdateDatas(bls, update)
			if err != nil {
				r.l.Debugf("failed to make blockUpdateData. %+v", err)
				return
			}
			lastSeq := r.prevRS.Seq()
			lmp := r.GetLastMessageProofDataForHeight(slot)
			if lmp != nil {
				lastSeq = lmp.EndSeq
			}
			rs := &receiveStatus{seq: lastSeq, slot: slot, bus: bus}
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
	bus := make([]*blockUpdateData, 0)
	scPeriod := SlotToSyncCommitteePeriod(update.FinalizedHeader.Beacon.Slot)

	if SlotToSyncCommitteePeriod(phase0.Slot(bls.Verifier.Height))+1 == scPeriod {
		r.l.Debugf("make NextSyncCommittee %d", scPeriod)
		lcUpdate, err := r.cl.LightClientUpdates(scPeriod, 1)
		if err != nil {
			return nil, err
		}
		if len(lcUpdate) != 1 {
			return nil, fmt.Errorf("invalid light client updates length")
		}
		lcu := lcUpdate[0]
		if !IsSyncCommitteeEdge(update.FinalizedHeader.Beacon.Slot) {
			// append blockUpdateData made by lightClientUpdate
			r.l.Debugf("finality update is not Period edge. make blockUpdateData for nextSyncCommittee")
			bu := &blockUpdateData{
				AttestedHeader:          lcu.AttestedHeader,
				FinalizedHeader:         lcu.FinalizedHeader,
				FinalizedHeaderBranch:   lcu.FinalityBranch,
				SyncAggregate:           lcu.SyncAggregate,
				SignatureSlot:           lcu.SignatureSlot,
				NextSyncCommittee:       lcu.NextSyncCommittee,
				NextSyncCommitteeBranch: lcu.NextSyncCommitteeBranch,
			}
			bus = append(bus, bu)
		} else {
			nsc = lcu.NextSyncCommittee
			nscBranch = lcu.NextSyncCommitteeBranch
		}
	}

	// append blockUpdateData made by FinalityUpdate
	r.l.Debugf("make blockUpdateData for finality update")
	bu := &blockUpdateData{
		AttestedHeader:          update.AttestedHeader,
		FinalizedHeader:         update.FinalizedHeader,
		FinalizedHeaderBranch:   update.FinalityBranch,
		SyncAggregate:           update.SyncAggregate,
		SignatureSlot:           update.SignatureSlot,
		NextSyncCommittee:       nsc,
		NextSyncCommitteeBranch: nscBranch,
	}
	bus = append(bus, bu)

	return bus, nil
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

	seq := r.seq + 1
	for i, l := range logs {
		message, err := r.bmc.ParseMessage(l)
		if err != nil {
			return nil, err
		}
		if seq != message.Seq.Int64() {
			// TODO just error?
			err = errors.IllegalArgumentError.Errorf(
				"sequence number of BTP message is not continuous (e:%d r:%d)",
				seq, message.Seq.Int64(),
			)
			return nil, err
		}
		r.l.Debugf("BTP Message#%d[seq:%d] dst:%s", i, message.Seq, r.dst.String())
		seq += 1
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
	trie := trie.NewEmpty(db)

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
		trie.Update(key, rawReceipt)
	}
	return trie, nil
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
		proof, err := rlp.EncodeToBytes(nodes.NodeList())
		if err != nil {
			return nil, err
		}
		rps = append(rps, &receiptProof{Key: key, Proof: proof})
	}
	return rps, nil
}

func (r *receiver) makeReceiptsRootProof(slot int64) (*ssz.Proof, error) {
	rrProof, err := r.cl.GetReceiptsRootProof(slot)
	if err != nil {
		err = errors.NotFoundError.Wrapf(err, "fail to make receiptsRoot proof")
		return nil, err
	}
	return TreeOffsetProofToSSZProof(rrProof)
}

func SingleProofToSSZProof(data []byte) (*ssz.Proof, error) {
	proof := &ssz.Proof{}
	proofType := int(data[0])
	if proofType != 0 {
		return nil, fmt.Errorf("invalid proof type. %d", proofType)
	}
	dataOffset := 1

	idx := binary.BigEndian.Uint64(data[dataOffset : dataOffset+8])
	proof.Index = int(idx)
	dataOffset += 8

	proof.Leaf = data[dataOffset : dataOffset+32]
	dataOffset += 32

	leafCount := int(math.Log2(float64(proof.Index)))
	proof.Hashes = make([][]byte, leafCount, leafCount)
	for i := 0; i < leafCount; i++ {
		proof.Hashes[i] = data[dataOffset : dataOffset+32]
		dataOffset += 32
	}

	return proof, nil
}

func TreeOffsetProofToSSZProof(data []byte) (*ssz.Proof, error) {
	proofType := int(data[0])
	if proofType != 1 {
		return nil, fmt.Errorf("invalid proof type. %d", proofType)
	}
	dataOffset := 1
	// leaf count
	leafCount := int(binary.LittleEndian.Uint16(data[dataOffset : dataOffset+2]))
	if len(data) < (leafCount-1)*2+leafCount*32 {
		return nil, fmt.Errorf("unable to deserialize tree offset proof: not enough bytes. %+v", data)
	}
	dataOffset += 2

	// offsets
	offsets := make([]uint16, leafCount-1, leafCount-1)
	for i := 0; i < leafCount-1; i++ {
		offsets[i] = binary.LittleEndian.Uint16(data[dataOffset+i*2 : dataOffset+i*2+2])
	}
	dataOffset += 2 * (leafCount - 1)

	// leaves
	leaves := make([][]byte, leafCount, leafCount)
	for i := 0; i < leafCount; i++ {
		leaves[i] = data[dataOffset : dataOffset+32]
		dataOffset += 32
	}

	node, err := treeOffsetProofToNode(offsets, leaves)
	if err != nil {
		return nil, err
	}

	gIndex := offsetsToGIndex(offsets)

	return node.Prove(gIndex)
}

func offsetsToGIndex(offsets []uint16) int {
	base := int(math.Pow(2, float64(len(offsets))))
	value := 0
	for _, offset := range offsets {
		value = value << 1
		if offset == 1 {
			value |= 1
		}
	}
	return base + value
}

// treeOffsetProofToNode Recreate a `Node` given offsets and leaves of a tree-offset proof
// See https://github.com/protolambda/eth-merkle-trees/blob/master/tree_offsets.md
func treeOffsetProofToNode(offsets []uint16, leaves [][]byte) (*ssz.Node, error) {
	if len(leaves) == 0 {
		return nil, fmt.Errorf("proof must contain gt 0 leaves")
	} else if len(leaves) == 1 {
		return ssz.LeafFromBytes(leaves[0]), nil
	} else {
		// the offset popped from the list is the # of leaves in the left subtree
		pivot := offsets[0]
		left, err := treeOffsetProofToNode(offsets[1:pivot], leaves[0:pivot])
		if err != nil {
			return nil, err
		}
		right, err := treeOffsetProofToNode(offsets[pivot:], leaves[pivot:])
		if err != nil {
			return nil, err
		}
		return ssz.NewNodeWithLR(left, right), nil
	}
}
