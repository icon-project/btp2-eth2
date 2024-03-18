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
	"time"

	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
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

	maxFinalityUpdateRetry   = 5
	maxFilterLogsBlockNumber = int64(128)
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
	bn     BeaconNetwork
	cl     *client.ConsensusLayer
	el     *client.ExecutionLayer
	bmc    *client.BMC
	nid    int64
	rsc    chan interface{}
	seq    int64
	prevRS *receiveStatus
	rss    []*receiveStatus
	mps    []*messageProofData
	fq     *ethereum.FilterQuery
	ht     map[int64]*ssz.Node                 // HistoricalSummaries Trie
	cp     map[int64]*phase0.BeaconBlockHeader // save checkpoint to validate gathered mps
}

func newReceiver(src, dst types.BtpAddress, endpoint string, opt map[string]interface{}, l log.Logger) link.Receiver {
	var err error
	r := &receiver{
		src: src,
		dst: dst,
		l:   l,
		rsc: make(chan interface{}),
		rss: make([]*receiveStatus, 0),
		fq: &ethereum.FilterQuery{
			Addresses: []common.Address{common.HexToAddress(src.ContractAddress())},
			Topics: [][]common.Hash{
				{crypto.Keccak256Hash([]byte(eventSignature))},
			},
		},
		ht: make(map[int64]*ssz.Node),
		cp: make(map[int64]*phase0.BeaconBlockHeader),
	}

	r.cl, err = client.NewConsensusLayer(opt["consensus_endpoint"].(string), l)
	if err != nil {
		l.Panicf("failed to connect to %s, %v", opt["consensus_endpoint"].(string), err)
	}
	r.bn, err = getBeaconNetwork(r.cl)
	if err != nil {
		l.Panicf("fail to get beacon network, %v", err)
	}
	r.l.Debugf("Detected beacon network : %s", r.bn.String())

	r.el, err = client.NewExecutionLayer(endpoint, l)
	if err != nil {
		l.Panicf("failed to connect to %s, %v", endpoint, err)
	}
	r.bmc, err = client.NewBMC(common.HexToAddress(r.src.ContractAddress()), r.el.GetBackend())
	if err != nil {
		l.Panicf("fail to get instance of BMC %s, %v", r.src.ContractAddress(), err)
	}
	return r
}

func (r *receiver) Start(bls *types.BMCLinkStatus) (<-chan interface{}, error) {
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

// getBlockNum find execution block number for consensus slot
// if slot does not have block, returns block number of prev slot
func (r *receiver) getBlockNum(slot int64) (int64, int64, error) {
	bn, err := r.cl.SlotToBlockNumber(phase0.Slot(slot))
	if err != nil {
		return 0, 0, err
	}
	if bn == 0 {
		return r.getBlockNum(slot - 1)
	}
	return slot, int64(bn), nil
}

func (r *receiver) findSlotForBlockNumber(blockNumber, fromSlot, toSlot int64) (int64, error) {
	low := fromSlot
	high := toSlot
	for low <= high {
		slot, bn, err := r.getBlockNum((low + high) / 2)
		if err != nil {
			return 0, err
		}
		if bn == blockNumber {
			r.l.Debugf("findSlotForBlockNumber() slot=%d with bn=%d fromSlot=%d toSlot=%d",
				slot, blockNumber, fromSlot, toSlot)
			return slot, nil
		} else if bn > blockNumber {
			high = slot - 1
		} else {
			if slot == low-1 {
				// there is no block at slot (low+high)/2
				low++
			} else {
				low = slot + 1
			}
		}
	}
	return 0, nil
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

func (r *receiver) getLastMessageSeq() int64 {
	ls := r.seq
	if len(r.mps) > 0 {
		ls = r.mps[len(r.mps)-1].EndSeq
	}
	return ls
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
	r.l.Debugf("Build BlockProof for slot:%d", height)
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
	if bls.Verifier.Height == mp.Slot {
		r.l.Debugf("make blockProofData with empty proof. slot:%d, state:%d", mp.Slot, bls.Verifier.Height)
		bpd = &blockProofData{Header: mp.Header, Proof: nil, HistoricalProof: nil}
	} else if bls.Verifier.Height-mp.Slot < SlotPerHistoricalRoot {
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
		r.l.Debugf("Invalid mp.header: %+v", header)
		return nil, errors.InvalidStateError.Errorf("invalid blockProofData. H:%#x != BP:%#x", root, blockProof.Leaf())
	}
	return &blockProofData{
		Header: mp.Header,
		Proof:  blockProof.SSZ(),
	}, nil
}

func (r *receiver) blockProofDataViaHistoricalSummaries(bls *types.BMCLinkStatus, mp *messageProofData) (*blockProofData, error) {
	header := mp.Header.Beacon
	gindex := proof.HistoricalSummariesIdxToGIndex(SlotToHistoricalSummariesIndex(r.bn, header.Slot))
	r.l.Debugf("make blockProofData with historicalSummaries. finalized: %d, slot:%d, gIndex:%d",
		bls.Verifier.Height, header.Slot, gindex)
	bp, err := r.cl.GetStateProofWithGIndex(strconv.FormatInt(bls.Verifier.Height, 10), gindex)
	if err != nil {
		return nil, err
	}
	blockProof, err := proof.NewSingleProof(bp)
	if err != nil {
		return nil, err
	}

	tr, err := r.getHistoricalSummariesTrie(mp.Slot)
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

func (r *receiver) getHistoricalSummariesTrie(slot int64) (*ssz.Node, error) {
	roots := make([][]byte, SlotPerHistoricalRoot)
	start := int64(HistoricalSummariesStartSlot(r.bn, phase0.Slot(slot)))
	r.l.Debugf("getHistoricalSummariesTrie slot:%d start:%d", slot, start)
	if ht, ok := r.ht[start]; !ok {
		var prevRoot []byte
		// find prevRoot
		for i := int64(1); i < SlotPerHistoricalRoot; i++ {
			root, err := r.cl.BeaconBlockRoot(strconv.FormatInt(start-i, 10))
			if root != nil && err == nil {
				prevRoot = root[:]
				break
			}
		}
		// get historical roots
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
		r.l.Debugf("getHistoricalSummariesTrie made trie at %d", start)
	} else {
		r.l.Debugf("getHistoricalSummariesTrie get trie from %d", start)
		return ht, nil
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
	r.l.Debugf("new MessageProof with slot:%d, seq:%d-%d", mpd.Height(), mpd.StartSeq, mpd.EndSeq)
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
	var lastFinalityUpdate *phase0.BeaconBlockHeader
	if err := r.cl.Events(eth2Topics, func(event *api.Event) {
		if event.Topic == client.TopicLCOptimisticUpdate {
			//update := event.Data.(*deneb.LightClientOptimisticUpdate)
			update, err := client.ToLightClientOptimisticUpdate(event.Data.(*spec.VersionedLCOptimisticUpdate))
			slot := int64(update.AttestedHeader.Beacon.Slot)
			r.l.Debugf("Get light client optimistic update. slot:%d", slot)
			once.Do(func() {
				r.l.Debugf("Check undelivered messages")
				status, err := r.getStatus()
				if err != nil {
					r.l.Panicf("%+v", err)
				}
				// handle undelivered messages
				extra := &BMVExtra{}
				_, err = codec.RLP.UnmarshalFromBytes(bls.Verifier.Extra, extra)
				if err != nil {
					r.l.Panicf("%+v", err)
				}
				fu, err := r.cl.LightClientFinalityUpdate()
				if err != nil {
					r.l.Panicf("failed to get Finality Update. %+v", err)
				}
				fSlot := int64(fu.FinalizedHeader.Beacon.Slot)
				var fromSlot int64
				if extra.LastMsgSeq != 0 {
					fromSlot = extra.LastMsgSlot + 1
				} else {
					fromSlot = bls.Verifier.Height + 1
				}
				if bls.RxSeq < status.TxSeq {
					r.l.Debugf("Find undelivered messages with: %+v, %+v", bls, extra)
					mps, err := r.messageProofDatasByRange(
						fromSlot, bls.RxSeq+1,
						slot-1, status.TxSeq,
					)
					if err != nil {
						r.l.Panicf("failed to add missing message. %+v", err)
					}
					for _, mp := range mps {
						if fSlot-mp.Slot >= SlotPerHistoricalRoot {
							_, err = r.getHistoricalSummariesTrie(mp.Slot)
							if err != nil {
								r.l.Panicf("failed to make historicalSummariesTrie. %+v", err)
							}
						}
					}
					r.mps = append(r.mps, mps...)
					r.l.Debugf("append %d messageProofDatas by range", len(mps))
				}

				r.addCheckPointsByRange(fSlot, slot-1)
			})
			mp, err := r.messageProofDataFromHeader(update)
			if err != nil {
				r.l.Warnf("fail to make messageProofData. %+v", err)
				return
			}
			if mp != nil {
				r.mps = append(r.mps, mp)
				r.l.Debugf("append new mp: %s", mp)
			}
			r.addCheckPoint(update.AttestedHeader.Beacon)
		} else if event.Topic == client.TopicLCFinalityUpdate {
			update, err := client.ToLightClientFinalityUpdate(event.Data.(*spec.VersionedLCFinalityUpdate))
			if err != nil {
				r.l.Panicf("failed to convert Finality Update. %+v", err)
			}
			slot := int64(update.FinalizedHeader.Beacon.Slot)
			r.l.Debugf("Get light client finality update. slot:%d", slot)
			if !IsCheckPoint(update.FinalizedHeader.Beacon.Slot) {
				r.l.Debugf("skip slot %d since it is not checkpoint", slot)
				return
			}
			if lastFinalityUpdate != nil && lastFinalityUpdate.StateRoot == update.FinalizedHeader.Beacon.StateRoot {
				r.l.Debugf("skip already processed finality update")
				return
			}
			retry := 0
		readFinalityUpdate:
			fu, err := r.cl.LightClientFinalityUpdate()
			if err != nil {
				r.l.Panicf("failed to get Finality Update. %+v", err)
			}
			if fu.FinalizedHeader.Beacon.Slot > update.FinalizedHeader.Beacon.Slot {
				r.l.Debugf("skip old finality update. Maybe it is due to undelivered message processing")
				return
			}
			// update with new value
			if retry > 0 {
				update = fu
			}
			if err = validateFinalityUpdate(update); err != nil {
				r.l.Debugf("invalid finality update. %v", err)
				if retry < maxFinalityUpdateRetry {
					time.Sleep(blockInterval + time.Second)
					retry++
					r.l.Debugf("retry reading Finality Update. %d", retry)
					goto readFinalityUpdate
				} else {
					r.l.Debugf("skip this Finality Update(slot:%d)", slot)
					return
				}
			}
			lastFinalityUpdate = update.FinalizedHeader.Beacon
			buds, err := r.makeBlockUpdateDatas(bls, update)
			if err != nil {
				r.l.Warnf("failed to make blockUpdateData. %+v", err)
				return
			}
			err = r.validateMessageProofData(bls, update)
			if err != nil {
				r.l.Warnf("failed to validate messageProofData. %+v", err)
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

func validateFinalityUpdate(u *client.LightClientFinalityUpdate) error {
	if u.FinalizedHeader.Beacon.Slot > u.AttestedHeader.Beacon.Slot {
		return errors.Errorf("invalid slot. finalized header > attested header")
	}
	if u.AttestedHeader.Beacon.Slot >= u.SignatureSlot {
		return errors.Errorf("invalid slot. attested header >= signature")
	}

	if u.SyncAggregate.SyncCommitteeBits.Count()*3 < u.SyncAggregate.SyncCommitteeBits.Len()*2 {
		return errors.Errorf("not enough SyncAggregate participants")
	}

	leaf, err := u.FinalizedHeader.HashTreeRoot()
	if err != nil {
		return err
	}
	_, err = proof.VerifyBranch(
		int(proof.GIndexStateFinalizedRoot),
		leaf[:],
		u.FinalityBranch,
		u.AttestedHeader.Beacon.StateRoot[:],
	)
	if err != nil {
		return err
	}

	return nil
}

func (r *receiver) makeBlockUpdateDatas(
	bls *types.BMCLinkStatus,
	update *client.LightClientFinalityUpdate,
) ([]*blockUpdateData, error) {
	var nsc *altair.SyncCommittee
	var nscBranch [][]byte
	buds := make([]*blockUpdateData, 0)
	scPeriod := SlotToSyncCommitteePeriod(update.FinalizedHeader.Beacon.Slot)
	blsSCPeriod := SlotToSyncCommitteePeriod(phase0.Slot(bls.Verifier.Height))

	if scPeriod > blsSCPeriod {
		r.l.Debugf("get light cilent updates from %d to %d", blsSCPeriod, scPeriod)
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

func (r *receiver) messageProofDatasByRange(fromSlot, fromSeq, toSlot, toSeq int64) ([]*messageProofData, error) {
	r.l.Debugf("start to find BTP messages. from %d(%d) to %d(%d)", fromSlot, fromSeq, toSlot, toSeq)
	mps := make([]*messageProofData, 0)

	//fbn, err := r.cl.SlotToBlockNumber(phase0.Slot(fromSlot))
	_, fbn, err := r.getBlockNum(fromSlot)
	if err != nil {
		return nil, err
	}
	_, tbn, err := r.getBlockNum(toSlot)
	//tbn, err := r.cl.SlotToBlockNumber(phase0.Slot(toSlot))
	if err != nil {
		return nil, err
	}

	fSlot := fromSlot
	for seq := fromSeq; seq <= toSeq; {
		logs, err := r.getBTPLogsWithSeq(seq, fbn, tbn)
		if err != nil {
			return nil, err
		}
		if len(logs) == 0 {
			return nil, errors.NotFoundError.Errorf("can't find BTP message with seq %d", seq)
		}

		bn := int64(logs[0].BlockNumber)
		slot, err := r.findSlotForBlockNumber(bn, fSlot, toSlot)
		if err != nil {
			return nil, err
		}
		r.l.Debugf("%d BTP messages at slot:%d, blockNum:%d", len(logs), slot, bn)

		bh, err := r.cl.BeaconBlockHeader(strconv.FormatInt(slot, 10))
		if err != nil {
			return nil, err
		}
		if bh == nil {
			return nil, errors.NotFoundError.Errorf("there is no header for %d", slot)
		}

		mp, err := r.makeMessageProofData(client.LightClientHeaderFromBeaconBlockHeader(bh), logs)
		if err != nil {
			return nil, err
		}
		if mp == nil {
			return nil, errors.InvalidStateError.Errorf("failed to make messageProofData for seq %d", seq)
		}
		mps = append(mps, mp)

		// update seq, fbn and fSlot
		seq = mp.EndSeq + 1
		fbn = bn + 1
		fSlot = slot + 1
	}

	return mps, nil
}

func (r *receiver) messageProofDataFromHeader(header client.LightClientHeader) (*messageProofData, error) {
	bn := big.NewInt(int64(header.BlockNumber()))
	logs, err := r.getBTPLogs(bn, bn)
	if err != nil {
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
			r.l.Debugf("BTP Message#%d [seq:%d bn:%d] ", i, message.Seq, l.BlockNumber)
		}
		seq += 1
	}
	if idxToSkip > -1 {
		logs = logs[idxToSkip+1:]
	}
	if len(logs) == 0 {
		return nil, nil
	}
	r.l.Debugf("Get %d BTP messages at slot:%d, blockNum:%d", len(logs), header.Slot(), bn)
	return r.makeMessageProofData(header, logs)
}

// makeMessageProofData make messageProofData with LightClientHeader and logs.
func (r *receiver) makeMessageProofData(header client.LightClientHeader, logs []etypes.Log) (mp *messageProofData, err error) {
	slot := int64(header.Slot())
	if len(logs) == 0 {
		return
	}

	receiptsRootProof, err := r.makeReceiptsRootProof(slot)
	if err != nil {
		return
	}

	receiptProofs, err := r.makeReceiptProofs(logs)
	if err != nil {
		return
	}

	bms, _ := r.bmc.ParseMessage(logs[0])
	bme, _ := r.bmc.ParseMessage(logs[len(logs)-1])

	mp = &messageProofData{
		Slot:              slot,
		ReceiptsRootProof: receiptsRootProof,
		ReceiptProofs:     receiptProofs,
		Header:            header.ToAltair(),
		StartSeq:          bms.Seq.Int64(),
		EndSeq:            bme.Seq.Int64(),
	}
	r.seq = mp.EndSeq
	r.l.Debugf("make messageProofData for BN:%d. %s", logs[0].BlockNumber, mp)
	return
}

// getBTPLogs returns all BTP logs in blocks specified by from and to
func (r *receiver) getBTPLogs(from, to *big.Int) ([]etypes.Log, error) {
	fq := *r.fq
	fq.FromBlock = from
	fq.ToBlock = to
	return r.el.FilterLogs(fq)
}

// getBTPLogsWithSeq find the block that contains BTP event log containing seq and
// returns all BTP event logs in that block with a sequence number greater than or equal to seq.
func (r *receiver) getBTPLogsWithSeq(seq int64, from, to int64) ([]etypes.Log, error) {
	r.l.Debugf("getBTPLogsWithSeq() seq=%d fromBN=%d toBN=%d", seq, from, to)
	for bn := from; bn < to; bn++ {
		fbn := big.NewInt(int64(bn))
		if bn+maxFilterLogsBlockNumber > to {
			bn = to
		} else {
			bn += maxFilterLogsBlockNumber
		}
		tbn := big.NewInt(int64(bn))

		logs, err := r.getBTPLogs(fbn, tbn)
		if err != nil {
			return nil, err
		}
		var s, e int
		blockNum := uint64(0)
		for i, l := range logs {
			message, err := r.bmc.ParseMessage(l)
			if err != nil {
				return nil, err
			}
			if message.Seq.Int64() == seq || l.BlockNumber == blockNum {
				r.l.Debugf("BTP message(seq:%d) at BN: %d", message.Seq, l.BlockNumber)
				if blockNum == 0 {
					s = i
					blockNum = l.BlockNumber
				}
				e = i + 1
			}
		}
		if blockNum != 0 {
			return logs[s:e], nil
		}
	}
	return nil, nil
}

// makeReceiptProofs make proofs for receipts which has BTP message. logs must be in same block.
func (r *receiver) makeReceiptProofs(logs []etypes.Log) ([]*receiptProof, error) {
	if len(logs) == 0 {
		return nil, nil
	}
	bn := big.NewInt(int64(logs[0].BlockNumber))
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

// getReceipts returns all receipts in the block specified by bn.
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
	db := trie.NewDatabase(rawdb.NewMemoryDatabase(), nil)
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

// getReceiptProofs get slice of receiptProof of logs from receipt Proof Trie tr.
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
		nodes := trienode.NewProofSet()
		err = tr.Prove(key, nodes)
		if err != nil {
			return nil, err
		}
		//p, err := rlp.EncodeToBytes(nodes.NodeList())
		p, err := rlp.EncodeToBytes(nodes.List())
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

func (r *receiver) addCheckPoint(bh *phase0.BeaconBlockHeader) {
	if IsCheckPoint(bh.Slot) {
		r.l.Debugf("add checkpoint at %d, %+v", bh.Slot, bh.String())
		r.cp[int64(bh.Slot)] = bh
	}
}

func (r *receiver) addCheckPointsByRange(from, to int64) {
	for start := int64(SlotToEpoch(phase0.Slot(from))+1) * SlotPerEpoch; start <= to; start += SlotPerEpoch {
		bh, err := r.cl.BeaconBlockHeader(strconv.FormatInt(start, 10))
		if bh == nil || err != nil {
			continue
		}
		r.addCheckPoint(bh.Header.Message)
	}
}

func (r *receiver) validateMessageProofData(bls *types.BMCLinkStatus, update *client.LightClientFinalityUpdate) error {
	status, err := r.getStatus()
	if err != nil {
		return err
	}
	if status.TxSeq == bls.RxSeq {
		// there are no BTP messages
		return nil
	}

	aSlot := int64(update.AttestedHeader.Beacon.Slot)
	fSlot := int64(update.FinalizedHeader.Beacon.Slot)
	valid := false
	r.l.Debugf("validate messageProofDatas at aSlot:%d, fSlot:%d, rStatus:%+v, bls:%+v.", aSlot, fSlot, status, bls)
	if cp, ok := r.cp[fSlot]; ok {
		valid = cp.String() == update.FinalizedHeader.Beacon.String()
		if !valid {
			r.l.Debugf("made messageProofData with invalid optimistic header")
		} else {
			if status.TxSeq > bls.RxSeq {
				if status.TxSeq != r.getLastMessageSeq() {
					r.l.Debugf("there is missing BTP message. (seq: %d ~ %d)",
						r.getLastMessageSeq()+1, status.TxSeq,
					)
					valid = false
				}
			}
		}
	} else {
		r.l.Debugf("no checkpoint")
		valid = false
	}

	if valid {
		delete(r.cp, fSlot)
	} else {
		r.l.Debugf("rebuild messageProofDatas seq")
		r.mps = make([]*messageProofData, 0)
		r.cp = make(map[int64]*phase0.BeaconBlockHeader)
		r.seq = r.prevRS.Seq()

		mps, err := r.messageProofDatasByRange(
			r.prevRS.Height()+1, r.prevRS.Seq()+1, aSlot, status.TxSeq,
		)
		if err != nil {
			return err
		}
		if len(mps) > 0 {
			r.mps = append(r.mps, mps...)
			r.l.Debugf("append %d messageProofDatas by range", len(mps))
		}

		r.addCheckPointsByRange(fSlot, aSlot)
	}

	return nil
}
