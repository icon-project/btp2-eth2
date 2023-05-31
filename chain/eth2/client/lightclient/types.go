package lightclient

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/icon-project/btp2/common"
	"github.com/icon-project/btp2/common/errors"
)

const (
	BLSPubKeyLen    = len(phase0.BLSPubKey{})
	BLSSignatureLen = len(phase0.BLSSignature{})
)

type BLSPubKey phase0.BLSPubKey

func (b BLSPubKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(common.HexBytes(b[:]))
}

func (b *BLSPubKey) UnmarshalJSON(input []byte) error {
	var hb common.HexBytes
	if err := json.Unmarshal(input, &hb); err != nil {
		return err
	}
	if len(hb) != BLSPubKeyLen {
		return errors.New("invalid length")
	}
	copy(b[:], hb)
	return nil
}
func (b BLSPubKey) String() string {
	return phase0.BLSPubKey(b).String()
}

type SyncCommittee struct {
	Pubkeys         []BLSPubKey `json:"pubkeys"`
	AggregatePubkey BLSPubKey   `json:"aggregate_pubkey"`
}

func (s *SyncCommittee) String() string {
	l := make([]string, len(s.Pubkeys))
	for i, p := range s.Pubkeys {
		l[i] = p.String()
	}
	return fmt.Sprintf("{Pubkeys:[%s],AggregatePubkey:%s}",
		strings.Join(l, ","), s.AggregatePubkey)
}

func (s *SyncCommittee) MarshalSSZ() ([]byte, error) {
	return ssz.MarshalSSZ(s)
}

func (s *SyncCommittee) MarshalSSZTo(buf []byte) (dst []byte, err error) {
	dst = buf
	for _, p := range s.Pubkeys {
		dst = append(dst, p[:]...)
	}
	dst = append(dst, s.AggregatePubkey[:]...)
	return
}

func (s *SyncCommittee) UnmarshalSSZ(buf []byte) error {
	end := len(buf)
	if end < BLSPubKeyLen {
		return ssz.ErrSize
	}
	copy(s.AggregatePubkey[:], buf[end-BLSPubKeyLen:])
	end -= BLSPubKeyLen
	if end%BLSPubKeyLen != 0 {
		return ssz.ErrSize
	}
	s.Pubkeys = s.Pubkeys[:0]
	for start := 0; start < end; start += BLSPubKeyLen {
		var p BLSPubKey
		copy(p[:], buf[start:start+BLSPubKeyLen])
		s.Pubkeys = append(s.Pubkeys, p)
	}
	return nil
}

func (s *SyncCommittee) SizeSSZ() (size int) {
	size = len(s.Pubkeys) * BLSPubKeyLen
	size += BLSPubKeyLen
	return
}

type BLSSignature phase0.BLSSignature

func (b BLSSignature) MarshalJSON() ([]byte, error) {
	return json.Marshal(common.HexBytes(b[:]))
}

func (b *BLSSignature) UnmarshalJSON(input []byte) error {
	var hb common.HexBytes
	if err := json.Unmarshal(input, &hb); err != nil {
		return err
	}
	if len(hb) != BLSSignatureLen {
		return errors.New("invalid length")
	}
	copy(b[:], hb)
	return nil
}
func (b BLSSignature) String() string {
	return phase0.BLSSignature(b).String()
}

type SyncAggregate struct {
	SyncCommitteeBits      common.HexBytes `json:"sync_committee_bits"`
	SyncCommitteeSignature BLSSignature    `json:"sync_committee_signature"`
}

func (s *SyncAggregate) String() string {
	return fmt.Sprintf("{SyncCommitteeBits:%s,SyncCommitteeSignature:%s}",
		s.SyncCommitteeBits, s.SyncCommitteeSignature)
}

func (s *SyncAggregate) MarshalSSZ() ([]byte, error) {
	return ssz.MarshalSSZ(s)
}

func (s *SyncAggregate) MarshalSSZTo(buf []byte) (dst []byte, err error) {
	dst = buf
	dst = append(dst, s.SyncCommitteeBits...)
	dst = append(dst, s.SyncCommitteeSignature[:]...)
	return
}

func (s *SyncAggregate) UnmarshalSSZ(buf []byte) error {
	end := len(buf)
	if end < BLSSignatureLen {
		return ssz.ErrSize
	}
	copy(s.SyncCommitteeSignature[:], buf[end-BLSSignatureLen:])
	end -= BLSSignatureLen
	if end < 1 {
		return ssz.ErrSize
	}
	s.SyncCommitteeBits = append(s.SyncCommitteeBits, buf[:end]...)
	return nil
}

func (s *SyncAggregate) SizeSSZ() (size int) {
	size = len(s.SyncCommitteeBits.Bytes())
	size += BLSSignatureLen
	return
}

type Slot phase0.Slot

func (s Slot) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("%d", s))
}

func (s *Slot) UnmarshalJSON(input []byte) error {
	var str string
	err := json.Unmarshal(input, &str)
	if err != nil {
		return err
	}
	v, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return err
	}
	*s = Slot(v)
	return nil
}
