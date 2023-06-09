package eth2

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/icon-project/btp2/chain/icon"
	"github.com/icon-project/btp2/common/codec"
	"github.com/icon-project/btp2/common/log"
	"github.com/icon-project/btp2/common/types"
	"github.com/icon-project/btp2/common/wallet"
	"github.com/stretchr/testify/assert"
)

func newSender(src, dest types.BtpAddress) *sender {
	s := NewSender(
		src,
		dest,
		nil,
		"https://sepolia.infura.io/v3/ffbf8ebe228f4758ae82e175640275e0",
		map[string]interface{}{
			"consensus_endpoint": "http://20.20.5.191:9596",
		},
		log.WithFields(log.Fields{log.FieldKeyPrefix: "test"}),
	)
	return s.(*sender)
}
func newICONSender(src, dest types.BtpAddress) types.Sender {
	f, err := os.Open("/Users/eunsoopark/Work/btp2-eth2/e2edemo/docker/icon/config/keystore_berlin.json")
	if err != nil {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	w, err := wallet.DecryptKeyStore(data, []byte("qwer1234%"))
	if err != nil {
		return nil
	}
	return icon.NewSender(
		src,
		dest,
		w,
		"https://berlin.net.solidwallet.io/api/v3/icon_dex",
		map[string]interface{}{},
		log.WithFields(log.Fields{log.FieldKeyPrefix: "test"}),
	)
}

func Test_checkRelayStatus(t *testing.T) {
	iconSender := newICONSender(
		types.BtpAddress("btp://0xaa36a7.eth2/0x50DD9479c45085dC64c6F0a0796040C7768f25CE"),
		types.BtpAddress("btp://0x7.icon/cxf1b0808f09138fffdb890772315aeabb37072a8a"),
	)
	defer iconSender.Stop()
	iconStatus, err := iconSender.GetStatus()
	assert.NoError(t, err)
	assert.NotNil(t, iconStatus)

	extra := &BMVExtra{}
	_, err = codec.RLP.UnmarshalFromBytes(iconStatus.Verifier.Extra, extra)
	assert.NoError(t, err)

	fmt.Printf("icon.BMC.getStatus(): %+v\n", iconStatus)
	fmt.Printf("icon.BMC.getStatus().Extra: %+v\n", extra)

	ethSender := newSender(
		types.BtpAddress("btp://0x7.icon/cxf1b0808f09138fffdb890772315aeabb37072a8a"),
		types.BtpAddress("btp://0xaa36a7.eth2/0x50DD9479c45085dC64c6F0a0796040C7768f25CE"),
	)
	defer ethSender.Stop()
	ethStatus, err := ethSender.getStatus(0)
	assert.NoError(t, err)
	assert.NotNil(t, ethStatus)

	fmt.Printf("eth2.BMC.getStatus(): %+v\n", ethStatus)

	if iconStatus.TxSeq > ethStatus.RxSeq {
		fmt.Println("# Need to check ICON -> ETH2 message")
	} else {
		fmt.Println("> ICON -> ETH2 working")
	}
	if ethStatus.TxSeq > iconStatus.RxSeq {
		fmt.Println("# Need to check ETH2 -> ICON message")
		ethReceiver := newReceiver(
			types.BtpAddress("btp://0xaa36a7.eth2/0x50DD9479c45085dC64c6F0a0796040C7768f25CE"),
			types.BtpAddress("btp://0x7.icon/cxf1b0808f09138fffdb890772315aeabb37072a8a"),
		)
		defer ethReceiver.Stop()

		ou, err := ethReceiver.cl.LightClientOptimisticUpdate()
		assert.NoError(t, err)
		assert.NotNil(t, ou)

		fu, err := ethReceiver.cl.LightClientFinalityUpdate()
		assert.NoError(t, err)
		assert.NotNil(t, fu)

		mps, err := ethReceiver.makeMessageProofDataByRange(
			extra.LastMsgSlot+1, iconStatus.RxSeq+1,
			int64(ou.AttestedHeader.Beacon.Slot), ethStatus.TxSeq,
		)
		if int(ethStatus.TxSeq-iconStatus.RxSeq) != len(mps) {
			assert.Fail(t, "Can't find all undelivered messages.",
				"e:%d != r:%d. ETH2.BMC error?\n", ethStatus.TxSeq-iconStatus.RxSeq, len(mps))
			return
		}
		fmt.Printf("# Checking undelivered %d messages\n", len(mps))
		notRelayed := 0
		for i, mp := range mps {
			if int64(fu.FinalizedHeader.Beacon.Slot) >= mp.Slot {
				fmt.Printf("\t%d: %+v is not relaied to ICON. Check reason.\n", i, mp)
				notRelayed++
			} else {
				fmt.Printf("\t%d: %+v is waiting for Ethereum finalization\n", i, mp)
			}
		}
		if notRelayed > 0 {
			fmt.Println("> ETH2 -> ICON is not working")
		}
	} else {
		fmt.Println("> ETH2 -> ICON working")
	}
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
//	fmt.Printf("Next SyncCommittee Perriod: %d at slot: %d. Left %d slot, %s\n",
//		period, nextSlot, leftSlot, leftSec.String(),
//	)
//}

//func Test_DecodeRelayMessage(t *testing.T) {
//	data := ""
//	bm := &BTPRelayMessage{}
//	codec.RLP.UnmarshalFromBytes(data, bm)
//}
