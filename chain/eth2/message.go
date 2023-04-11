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
	"encoding/hex"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/icon-project/btp2/common/codec"
	"github.com/icon-project/btp2/common/link"
	"github.com/icon-project/btp2/common/types"
)

type BTPRelayMessage struct {
	Messages []*TypePrefixedMessage
}

type relayMessageItem struct {
	it      link.MessageItemType
	nextBls *types.BMCLinkStatus
	payload []byte
}

func (c *relayMessageItem) Type() link.MessageItemType {
	return c.it
}

func (c *relayMessageItem) Len() int64 {
	return int64(len(c.payload))
}

func (c *relayMessageItem) UpdateBMCLinkStatus(status *types.BMCLinkStatus) error {
	return nil
}

func (c *relayMessageItem) Payload() []byte {
	return c.payload
}

type BlockProof struct {
	relayMessageItem
	ph int64
}

func (c *BlockProof) ProofHeight() int64 {
	return c.ph
}

type BlockUpdate struct {
	BlockProof
	srcHeight    int64
	targetHeight int64
}

func (c *BlockUpdate) UpdateBMCLinkStatus(bls *types.BMCLinkStatus) error {
	if c.nextBls != nil {
		bls.Verifier.Height = c.nextBls.Verifier.Height
	}
	return nil
}

func (c *BlockUpdate) SrcHeight() int64 {
	return c.srcHeight
}

func (c *BlockUpdate) TargetHeight() int64 {
	return c.targetHeight
}

func NewBlockUpdate(srcHeight int64, data *blockUpdateData) *BlockUpdate {
	targetHeight := int64(data.FinalizedHeader.Beacon.Slot)
	nextBls := &types.BMCLinkStatus{}
	nextBls.Verifier.Height = targetHeight
	return &BlockUpdate{
		srcHeight:    srcHeight,
		targetHeight: targetHeight,
		BlockProof: BlockProof{
			relayMessageItem: relayMessageItem{
				it:      link.TypeBlockUpdate,
				nextBls: nextBls,
				payload: codec.RLP.MustMarshalToBytes(data),
			},
			ph: -1, // to make BlockProof
		},
	}
}

type MessageProof struct {
	relayMessageItem
	startSeq int64
	lastSeq  int64
}

func (m *MessageProof) UpdateBMCLinkStatus(bls *types.BMCLinkStatus) error {
	if m.nextBls != nil {
		bls.RxSeq = m.nextBls.RxSeq
	}
	return nil
}

func (m *MessageProof) StartSeqNum() int64 {
	return m.startSeq
}

func (m *MessageProof) LastSeqNum() int64 {
	return m.lastSeq
}

func NewMessageProof(bls *types.BMCLinkStatus, ls int64, data *messageProofData) *MessageProof {
	nextBls := &types.BMCLinkStatus{}
	nextBls.RxSeq = ls
	return &MessageProof{
		startSeq: bls.RxSeq,
		lastSeq:  ls,
		relayMessageItem: relayMessageItem{
			it:      link.TypeMessageProof,
			nextBls: nextBls,
			payload: codec.RLP.MustMarshalToBytes(data),
		},
	}
}

type TypePrefixedMessage struct {
	Type    link.MessageItemType
	Payload []byte
}

func NewTypePrefixedMessage(rmi link.RelayMessageItem) (*TypePrefixedMessage, error) {
	tpm := &TypePrefixedMessage{}
	switch rmi.Type() {
	case link.TypeBlockUpdate:
		bu := rmi.(*BlockUpdate)
		tpm.Type = bu.Type()
		tpm.Payload = bu.Payload()
	case link.TypeBlockProof:
		bp := rmi.(*BlockProof)
		tpm.Type = bp.Type()
		tpm.Payload = bp.Payload()
	case link.TypeMessageProof:
		mp := rmi.(*MessageProof)
		tpm.Type = mp.Type()
		tpm.Payload = mp.Payload()
	default:
		return nil, fmt.Errorf("invalid message type")
	}
	return tpm, nil
}

type blockUpdateData struct {
	AttestedHeader          *altair.LightClientHeader
	FinalizedHeader         *altair.LightClientHeader
	FinalizedHeaderBranch   [][]byte
	SyncAggregate           *altair.SyncAggregate
	SignatureSlot           phase0.Slot
	NextSyncCommittee       *altair.SyncCommittee
	NextSyncCommitteeBranch [][]byte
}

func (b *blockUpdateData) RLPEncodeSelf(e codec.Encoder) error {
	e2, err := e.EncodeList()
	if err != nil {
		return err
	}
	ah, err := b.AttestedHeader.MarshalSSZ()
	if err != nil {
		return err
	}
	fh, err := b.FinalizedHeader.MarshalSSZ()
	if err != nil {
		return err
	}
	sa, err := b.SyncAggregate.MarshalSSZ()
	if err != nil {
		return err
	}
	var nsc []byte
	if b.NextSyncCommittee != nil {
		nsc, err = b.NextSyncCommittee.MarshalSSZ()
		if err != nil {
			return err
		}
	}
	if err = e2.EncodeMulti(
		ah, fh, b.FinalizedHeaderBranch, sa, b.SignatureSlot, nsc, b.NextSyncCommitteeBranch,
	); err != nil {
		return err
	}
	return nil
}

func (b *blockUpdateData) RLPDecodeSelf(d codec.Decoder) error {
	d2, err := d.DecodeList()
	if err != nil {
		return err
	}
	var ah, fh, sa, nsc []byte
	if _, err = d2.DecodeMulti(
		&ah, &fh, &b.FinalizedHeaderBranch, &sa, &b.SignatureSlot, &nsc, &b.NextSyncCommitteeBranch,
	); err != nil {
		return err
	}
	b.AttestedHeader = new(altair.LightClientHeader)
	err = b.AttestedHeader.UnmarshalSSZ(ah)
	if err != nil {
		return err
	}
	b.FinalizedHeader = new(altair.LightClientHeader)
	err = b.FinalizedHeader.UnmarshalSSZ(fh)
	if err != nil {
		return err
	}
	b.SyncAggregate = new(altair.SyncAggregate)
	err = b.SyncAggregate.UnmarshalSSZ(sa)
	if err != nil {
		return err
	}
	b.NextSyncCommittee = new(altair.SyncCommittee)
	err = b.NextSyncCommittee.UnmarshalSSZ(nsc)
	if err != nil {
		return err
	}
	return nil
}

type blockProofData struct {
	Header *altair.LightClientHeader
	Proof  *ssz.Proof // proof for BeaconState.BlockRoots or BeaconState.HistoricalRoots
}

func (b *blockProofData) RLPEncodeSelf(e codec.Encoder) error {
	e2, err := e.EncodeList()
	if err != nil {
		return err
	}
	h, err := b.Header.MarshalSSZ()
	if err != nil {
		return err
	}
	if err = e2.EncodeMulti(h, b.Proof); err != nil {
		return err
	}
	return nil
}

func (b *blockProofData) RLPDecodeSelf(d codec.Decoder) error {
	d2, err := d.DecodeList()
	if err != nil {
		return err
	}
	var bs []byte
	if _, err = d2.DecodeMulti(&bs, &b.Proof); err != nil {
		return err
	}
	b.Header = &altair.LightClientHeader{}
	err = b.Header.UnmarshalSSZ(bs)
	if err != nil {
		return err
	}
	return nil
}

func (b *blockProofData) Format(f fmt.State, c rune) {
	switch c {
	case 'v', 's':
		if f.Flag('+') {
			fmt.Fprintf(f, "blockProofData{Header:%+v Proof=%+v)", b.Header, b.Proof)
		} else {
			fmt.Fprintf(f, "blockProofData{%v %v)", b.Header, b.Proof)
		}
	}
}

type messageProofData struct {
	Slot              int64
	ReceiptsRootProof *ssz.Proof
	ReceiptProofs     []*receiptProof

	Header   *altair.LightClientHeader
	StartSeq int64
	EndSeq   int64
}

func (m *messageProofData) Height() int64 {
	return m.Slot
}

func (m *messageProofData) Contains(seq int64) bool {
	return m.StartSeq <= seq && seq <= m.EndSeq
}

func (m *messageProofData) MessageCount() int64 {
	return m.EndSeq - m.StartSeq + 1
}

func (m *messageProofData) RLPEncodeSelf(e codec.Encoder) error {
	e2, err := e.EncodeList()
	if err != nil {
		return err
	}
	if err = e2.EncodeMulti(m.Slot, m.ReceiptsRootProof, m.ReceiptProofs); err != nil {
		return err
	}
	return nil
}

func (m *messageProofData) RLPDecodeSelf(d codec.Decoder) error {
	d2, err := d.DecodeList()
	if err != nil {
		return err
	}
	if _, err = d2.DecodeMulti(&m.Slot, &m.ReceiptsRootProof, &m.ReceiptProofs); err != nil {
		return err
	}
	return nil
}

func (m *messageProofData) Format(f fmt.State, c rune) {
	switch c {
	case 'v':
		if f.Flag('+') {
			fmt.Fprintf(f, "messageProofData{Slot=%d StartSeq=%d EndSeq=%d ReceiptsRootProof=%+v ReceiptProofs=%+v)",
				m.Slot, m.StartSeq, m.EndSeq, m.ReceiptsRootProof, m.ReceiptProofs)
		} else {
			fmt.Fprintf(f, "messageProofData{%d %d %d %v %v)",
				m.Slot, m.StartSeq, m.EndSeq, m.ReceiptsRootProof, m.ReceiptProofs)
		}
	case 's':
		fmt.Fprintf(f, "messageProofData{Slot:%d StartSeq=%d EndSeq=%d)",
			m.Slot, m.StartSeq, m.EndSeq)
	}
}

type receiptProof struct {
	Key   []byte `json:"key"`   // rlp.encode(receipt index)
	Proof []byte `json:"proof"` // proof for receipt
}

func (r *receiptProof) Format(f fmt.State, c rune) {
	switch c {
	case 'v', 's':
		if f.Flag('+') {
			fmt.Fprintf(f, "receiptProof{Key=%s Proof=%s)",
				hex.EncodeToString(r.Key), hex.EncodeToString(r.Proof))
		} else {
			fmt.Fprintf(f, "receiptProof{%x %x)",
				hex.EncodeToString(r.Key), hex.EncodeToString(r.Proof))
		}
	}
}

type BMVExtra struct {
	targetSeq int64
}
