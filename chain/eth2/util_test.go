//go:build bet_test
// +build bet_test

package eth2

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/ethereum/go-ethereum/common"
	"github.com/icon-project/btp2/common/codec"
	"github.com/icon-project/btp2/common/errors"
	"github.com/icon-project/btp2/common/link"
	"github.com/icon-project/btp2/common/log"
	"github.com/icon-project/btp2/common/types"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/assert"
)

func newTestSender(src, dest types.BtpAddress) *sender {
	s := newSender(
		src,
		dest,
		nil,
		"http://18.176.88.124:8545",
		map[string]interface{}{
			"consensus_endpoint": "http://18.176.88.124:9596",
		},
		".test",
		log.WithFields(log.Fields{log.FieldKeyPrefix: "test"}),
	)
	return s.(*sender)
}

//func Test_getNextSyncCommittee(t *testing.T) {
//	r := newReceiver(
//		types.BtpAddress("btp://0xaa36a7.eth/0x11167e875E08a113706e8bA3010ac37329b0E6b2"),
//		types.BtpAddress("btp://0x42.icon/cx8642ab29e608915b43e677d9bcb17ec902b4ec8b"),
//	)
//	defer r.Stop()
//
//	bh, err := r.cl.BeaconBlockHeader("head")
//	assert.NoError(t, err)
//
//	period := SlotToSyncCommitteePeriod(bh.Header.Message.Slot)
//
//	nextSlot := (period + 1) * SlotPerSyncCommitteePeriod
//	leftSlot := nextSlot - uint64(bh.Header.Message.Slot)
//	leftSec, err := time.ParseDuration(fmt.Sprintf("%ds", leftSlot*12))
//	fmt.Printf("Next SyncCommittee Period: %d at slot: %d. Left %d slot, %s\n",
//		period, nextSlot, leftSlot, leftSec.String(),
//	)
//}

func Test_DecodeRelayMessage(t *testing.T) {
	// expect SyncAggregate at Slot 2547553
	saBitsStr := "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000111111110111011110111111111101111111011100111111110111001110111111000001101111111101111101110010000111111011100101011001110111"
	saBits, err := toSyncAggregateBits(saBitsStr)
	assert.NoError(t, err)
	expectSA := &altair.SyncAggregate{SyncCommitteeBits: saBits}
	saSigStr := "81cbe70e9b6b5afcc265cca8958c37f7c1aa92f3c2c4f07f603c12327ac2461a2392b1e97e63f068eabafe9a0ba9ffe6163f25972a2b0583ee4638029588cb6f5ec6d04d509dab87f286dec00c9c7204240fbab7ad2819d39d5c0cd56a4cddb7"
	saSig, err := hex.DecodeString(saSigStr)
	assert.NoError(t, err)
	copy(expectSA.SyncCommitteeSignature[:], saSig)

	// BlockUpdate for Slot 2547488
	rmStr := "f9026bf90268f9026501b90261f9025eb8740400000060df260000000000cc0000000000000078847f77c3f0788abe9fe5e555ecc7e36c0ae5404bdb05b8c1f2515f85bd088e6c4f8ce0bca2523679ce34ce728a49e1030cf2178037105559a5aec1472aee13c84ee914d66cf3aa9cdaa3094572b1d4ad269586d55ff3b694368fd63c361f7ab8740400000020df2600000000005b050000000000009176274471de7034ac49c8e62244cd4dcf94836e33990daefd8f3a8cdb5144b17bb438d39dda063750dd4e2bb1857db87373aaee379fae3ac6203cd43147058a6c072a928149baf7f341f15e8f341f49a4ccfc6c4a70026933e04f5452a49e13f8c6a0f936010000000000000000000000000000000000000000000000000000000000a03d4be5d019ba15ea3ef304a83b8a067f2e79f46a3fac8069306a6c814a0a35eba0647dc7a40ec9cb5a0be75dbef6aeeb5b5949370294308d423b2f0fff271d7deda0116fe39b9856b95186b281b3bba5c59a6fd811e1fb2fbbc632225651512fce07a0c4ae826b9fd0695c7cebe9c57ac342ca17a485a1a8850f8649412e06aa788fd5a0cc70fa680f553d734e9f9890cb78ebaa19951581d694c82f643315cdf19e5289b8a0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000fcbbf7bfbff3efdc0ff6ef3be1776aee81cbe70e9b6b5afcc265cca8958c37f7c1aa92f3c2c4f07f603c12327ac2461a2392b1e97e63f068eabafe9a0ba9ffe6163f25972a2b0583ee4638029588cb6f5ec6d04d509dab87f286dec00c9c7204240fbab7ad2819d39d5c0cd56a4cddb78326df61f800f800"
	rmBytes, err := hex.DecodeString(rmStr)
	assert.NoError(t, err)

	rm := &BTPRelayMessage{}
	_, err = codec.RLP.UnmarshalFromBytes(rmBytes, rm)
	assert.NoError(t, err)

	for _, m := range rm.Messages {
		switch m.Type {
		case link.TypeBlockUpdate:
			bu := &blockUpdateData{}
			_, err = codec.RLP.UnmarshalFromBytes(m.Payload, bu)
			assert.NoError(t, err)
			assert.Equal(t, expectSA.SyncCommitteeBits.Count(), bu.SyncAggregate.SyncCommitteeBits.Count())
			assert.Equal(t, expectSA.SyncCommitteeBits, bu.SyncAggregate.SyncCommitteeBits)
			assert.Equal(t, expectSA.SyncCommitteeSignature, bu.SyncAggregate.SyncCommitteeSignature)
			fmt.Printf("%s\n", bu)
		}
	}
}

func toSyncAggregateBits(in string) (bitfield.Bitvector512, error) {
	if len(in) != 512 {
		return nil, errors.Errorf("Invalid length %d", len(in))
	}
	rst := bitfield.NewBitvector512()
	for i := 0; i < 64; i++ {
		currentByteStr := in[i*8 : (i+1)*8]
		currentByteInt, _ := strconv.ParseUint(currentByteStr, 2, 8)
		currentByte := byte(currentByteInt)
		rst[i] = currentByte
	}
	return rst, nil
}

func Test_EthRevertMessage(t *testing.T) {
	txHash := common.HexToHash("0xfb9a68914db86ef3aef1465130692aab0f0c290058e2ae1339ac145f764dd73b")
	s := newTestSender(
		types.BtpAddress("btp://0x7.icon/cxf1b0808f09138fffdb890772315aeabb37072a8a"),
		types.BtpAddress("btp://0xaa36a7.eth2/0x50DD9479c45085dC64c6F0a0796040C7768f25CE"),
	)

	revertMsg, err := s.el.GetRevertMessage(txHash)
	assert.NoError(t, err)
	fmt.Println(revertMsg)
}

func Test_getBlockByNumber(t *testing.T) {
	s := newTestSender(
		types.BtpAddress("btp://0x7.icon/cxf1b0808f09138fffdb890772315aeabb37072a8a"),
		types.BtpAddress("btp://0xaa36a7.eth2/0x50DD9479c45085dC64c6F0a0796040C7768f25CE"),
	)

	//block, err := s.el.BlockByNumber(big.NewInt(-1))
	block, err := s.el.BlockByNumber(nil)
	assert.NoError(t, err)
	fmt.Println(block.BaseFee())
}

func Test_test(t *testing.T) {
	opt := make(map[string]interface{}, 0)

	txUrl, _ := opt["test"].(string)
	if len(txUrl) == 0 {
		fmt.Println("empty test option")
	} else {
		fmt.Printf("%s\n", txUrl)
	}
}
