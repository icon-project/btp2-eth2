package eth2

import (
	"bytes"
	"fmt"
	"math/big"
	"strconv"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/ethereum/go-ethereum/common"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	ssz "github.com/ferranbt/fastssz"
	"github.com/icon-project/btp2/chain/icon/client"
	"github.com/icon-project/btp2/common/codec"
	"github.com/icon-project/btp2/common/link"
	"github.com/icon-project/btp2/common/log"
	"github.com/icon-project/btp2/common/types"
	"github.com/stretchr/testify/assert"

	"github.com/icon-project/btp2-eth2/chain/eth2/proof"
)

const (
	iconEndpoint         = "https://berlin.net.solidwallet.io/api/v3/icon_dex"
	ethNodeAddr          = "http://1.1.1.1"
	ethExecutionEndpoint = ethNodeAddr + ":8545"
	ethConsensusEndpoint = ethNodeAddr + ":9596"

	iconBMC        = "cxb61b42e51c20054b4f5fd31b7d64af8c59579829"
	ethBMC         = "0xD99d2A85E9B0Fa3E7fbA4756699f4307BFBb80c3"
	btpAddressICON = "btp://0x3.icon/" + iconBMC
	btpAddressETH  = "btp://0xaa36a7.eth2/" + ethBMC
)

func newTestEthReceiver() *receiver {
	r := newReceiver(
		types.BtpAddress(btpAddressETH),
		types.BtpAddress(btpAddressICON),
		ethExecutionEndpoint,
		map[string]interface{}{
			"consensus_endpoint": ethConsensusEndpoint,
		},
		log.WithFields(log.Fields{log.FieldKeyPrefix: ""}),
	)
	return r.(*receiver)
}

func iconGetStatus() (*types.BMCLinkStatus, error) {
	c := client.NewClient(iconEndpoint, log.WithFields(log.Fields{log.FieldKeyPrefix: "test"}))
	p := &client.CallParam{
		ToAddress: client.Address(iconBMC),
		DataType:  "call",
		Data: client.CallData{
			Method: client.BMCGetStatusMethod,
			Params: client.BMCStatusParams{
				Target: btpAddressETH,
			},
		},
	}
	bs := &client.BMCStatus{}
	err := client.MapError(c.Call(p, bs))
	if err != nil {
		return nil, err
	}
	ls := &types.BMCLinkStatus{}
	if ls.TxSeq, err = bs.TxSeq.Value(); err != nil {
		return nil, err
	}
	if ls.RxSeq, err = bs.RxSeq.Value(); err != nil {
		return nil, err
	}
	if ls.Verifier.Height, err = bs.Verifier.Height.Value(); err != nil {
		return nil, err
	}
	if ls.Verifier.Extra, err = bs.Verifier.Extra.Value(); err != nil {
		return nil, err
	}
	return ls, nil
}

//func TestReceiver_RunMonitoring(t *testing.T) {
//	eth := newTestEthReceiver()
//	defer eth.Stop()
//
//	bls, err := iconGetStatus()
//	assert.NoError(t, err)
//
//	wg := sync.WaitGroup{}
//	wg.Add(1)
//	go func() {
//		eth.Monitoring(bls)
//	}()
//	wg.Wait()
//}

func TestReceiver_BlockUpdate(t *testing.T) {
	r := newTestEthReceiver()
	defer r.Stop()

	tests := []struct {
		name     string
		slotDiff int64
		buCount  int
	}{
		{
			name:     "without nextSyncCommittee",
			slotDiff: 0,
			buCount:  1,
		},
		{
			name:     "with nextSyncCommittee",
			slotDiff: SlotPerSyncCommitteePeriod,
			buCount:  2,
		},
	}

	for _, tt := range tests {
		fu, err := r.cl.LightClientFinalityUpdate()
		assert.NoError(t, err)
		t.Run(tt.name, func(t *testing.T) {
			bls := &types.BMCLinkStatus{}
			bls.Verifier.Height = int64(fu.FinalizedHeader.Beacon.Slot) - tt.slotDiff

			bus, err := r.makeBlockUpdateDatas(bls, fu)
			assert.NoError(t, err)
			assert.Equal(t, tt.buCount, len(bus))

			for _, bu := range bus {
				// verify next sync committee
				if bu.NextSyncCommittee != nil {
					leaf, err := bu.NextSyncCommittee.HashTreeRoot()
					assert.NoError(t, err)
					ok, err := proof.VerifyBranch(
						int(proof.GIndexStateNextSyncCommittee),
						leaf[:],
						bu.NextSyncCommitteeBranch,
						bu.AttestedHeader.Beacon.StateRoot[:],
					)
					assert.True(t, ok)
					assert.NoError(t, err)
				}
				// verify finalized header
				leaf, err := bu.FinalizedHeader.HashTreeRoot()
				assert.NoError(t, err)
				ok, err := proof.VerifyBranch(
					int(proof.GIndexStateFinalizedRoot),
					leaf[:],
					bu.FinalizedHeaderBranch,
					bu.AttestedHeader.Beacon.StateRoot[:],
				)
				assert.True(t, ok)
				assert.NoError(t, err)

				VerifySyncAggregate(t, r, bu)
			}
		})
	}
}

func VerifySyncAggregate(t *testing.T, r *receiver, bu *blockUpdateData) {
	// TODO implement
	//lcu, err := r.cl.LightClientUpdates(
	//	SlotToSyncCommitteePeriod(bu.FinalizedHeader.Beacon.Slot)-SlotPerSyncCommitteePeriod,
	//	1,
	//)
	//assert.NoError(t, err)
	//syncCommittee := lcu[0].NextSyncCommittee
}

func TestReceiver_BlockProof(t *testing.T) {
	r := newTestEthReceiver()
	defer r.Stop()

	tests := []struct {
		name     string
		slotDiff int64
	}{
		{
			name:     "at finalized slot",
			slotDiff: 0,
		},
		{
			name:     "with blockRoots",
			slotDiff: 11,
		},
		{
			name:     "with blockRoots",
			slotDiff: 12,
		},
		{
			name:     "with historicalSummaries",
			slotDiff: SlotPerHistoricalRoot + 10,
		},
		{
			name:     "with historicalSummaries",
			slotDiff: SlotPerHistoricalRoot + 11,
		},
		{
			name:     "with historicalSummaries",
			slotDiff: 2*SlotPerHistoricalRoot + 2,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s-%d", tt.name, tt.slotDiff), func(t *testing.T) {
			// get finalized header and set bls
			fu, err := r.cl.LightClientFinalityUpdate()
			assert.NoError(t, err)

			finalizedSlot := int64(fu.FinalizedHeader.Beacon.Slot)
			bls := &types.BMCLinkStatus{}
			bls.Verifier.Height = finalizedSlot

			// get header and set mp
			blockProofSlot := finalizedSlot - tt.slotDiff
			header, err := r.cl.BeaconBlockHeader(strconv.FormatInt(blockProofSlot, 10))
			assert.NoError(t, err)

			mp := &messageProofData{
				Slot: blockProofSlot,
				Header: &altair.LightClientHeader{
					Beacon: header.Header.Message,
				},
			}

			// get BlockProof
			bp, err := r.blockProofForMessageProof(bls, mp)
			assert.NotNil(t, bp)
			assert.NoError(t, err)

			// verify BlockProof
			assert.Equal(t, link.TypeBlockProof, bp.Type())
			assert.Equal(t, blockProofSlot, bp.ProofHeight())
			bpd := new(blockProofData)
			_, err = codec.RLP.UnmarshalFromBytes(bp.(*BlockProof).Payload(), bpd)
			assert.NoError(t, err)

			// ssz verify bp.proof
			if tt.slotDiff == 0 {
				assert.Equal(t, fu.FinalizedHeader.Beacon, bpd.Header.Beacon)
				assert.Nil(t, bpd.Proof)
				return
			}
			ok, err := ssz.VerifyProof(fu.FinalizedHeader.Beacon.StateRoot[:], bpd.Proof)
			assert.True(t, ok)
			assert.NoError(t, err)

			root, err := bpd.Header.Beacon.HashTreeRoot()
			assert.NoError(t, err)
			if tt.slotDiff < SlotPerHistoricalRoot {
				assert.Nil(t, bpd.HistoricalProof)

				// bp.proof.leaf == hash_tree_root(bp.header)
				assert.NoError(t, err)
				assert.Equal(t, root[:], bpd.Proof.Leaf)
			} else {
				// ssz verify bp.historicalProof
				ok, err := ssz.VerifyProof(bpd.Proof.Leaf, bpd.HistoricalProof)
				assert.True(t, ok)
				assert.NoError(t, err)

				// bp.historicalProof.leaf == hash_tree_root(bp.header)
				assert.Equal(t, root[:], bpd.HistoricalProof.Leaf)
			}
		})
	}
}

func TestReceiver_MessageProof(t *testing.T) {
	slot := int64(4289613)
	r := newTestEthReceiver()
	defer r.Stop()

	bh, err := r.cl.BeaconBlockHeader(strconv.FormatInt(slot, 10))
	assert.NoError(t, err)
	header := &deneb.LightClientHeader{
		Beacon: bh.Header.Message,
	}

	bn, err := r.cl.SlotToBlockNumber(bh.Header.Message.Slot)
	blockNum := big.NewInt(int64(bn))
	assert.NoError(t, err)
	logs, err := r.getBTPLogs(blockNum, blockNum)
	assert.NoError(t, err)

	var mp *messageProofData
	mp, err = r.makeMessageProofData(header, logs)
	assert.NoError(t, err)

	// verify receiptsRoot
	ok, err := ssz.VerifyProof(bh.Header.Message.StateRoot[:], mp.ReceiptsRootProof)
	assert.True(t, ok)
	assert.NoError(t, err)

	block, err := r.cl.BeaconBlock(fmt.Sprintf("%d", slot))
	assert.NoError(t, err)
	assert.Equal(
		t,
		block.Deneb.Message.Body.ExecutionPayload.ReceiptsRoot[:],
		mp.ReceiptsRootProof.Leaf[:],
	)

	// verify receipt
	receiptsRoot := common.BytesToHash(mp.ReceiptsRootProof.Leaf)
	for _, rp := range mp.ReceiptProofs {
		//nl := trienode.NewProofSet()
		nl := new(trienode.ProofList)
		err = rlp.DecodeBytes(rp.Proof, nl)
		assert.NoError(t, err)
		value, err := trie.VerifyProof(
			receiptsRoot,
			rp.Key,
			nl.Set(),
		)
		assert.NoError(t, err)
		var idx uint64
		err = rlp.DecodeBytes(rp.Key, &idx)
		assert.NoError(t, err)

		// check receipt
		receipt, err := receiptFromBytes(value)
		assert.NoError(t, err)
		find := false
		for _, l := range receipt.Logs {
			if bytes.Compare(l.Topics[0][:], r.fq.Topics[0][0][:]) == 0 {
				find = true
				break
			}
		}
		assert.True(t, find)
	}
}

func receiptFromBytes(bs []byte) (*etypes.Receipt, error) {
	r := new(etypes.Receipt)
	if err := r.UnmarshalBinary(bs); err != nil {
		return nil, err
	}
	return r, nil
}

func TestReceiver_makeReceiptsRootProof(t *testing.T) {
	slot := int64(4289613)
	r := newTestEthReceiver()
	defer r.Stop()

	bh, err := r.cl.BeaconBlockHeader(strconv.FormatInt(slot, 10))
	assert.NoError(t, err)

	p, err := r.makeReceiptsRootProof(slot)
	assert.NoError(t, err)
	//fmt.Printf("%+v\n", p)

	// verify receiptsRoot
	ok, err := ssz.VerifyProof(bh.Header.Message.StateRoot[:], p)
	assert.True(t, ok)
	assert.NoError(t, err)
	fmt.Printf("%#x\n", p.Leaf[:])

	block, err := r.cl.BeaconBlock(fmt.Sprintf("%d", slot))
	fmt.Printf("%#x\n", block.Deneb.Message.Body.ExecutionPayload.ReceiptsRoot[:])
	assert.NoError(t, err)
	assert.Equal(
		t,
		block.Deneb.Message.Body.ExecutionPayload.ReceiptsRoot[:],
		p.Leaf[:],
	)

}
