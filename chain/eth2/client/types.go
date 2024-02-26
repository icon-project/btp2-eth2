package client

import (
	"encoding/json"
	"fmt"

	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/icon-project/btp2/common/errors"
)

func checkVersion(v spec.DataVersion) error {
	if v != spec.DataVersionCapella && v != spec.DataVersionDeneb {
		return errors.UnsupportedError.Errorf("unsupported version %s", v)
	}
	return nil
}

type LightClientBootstrap struct {
	Header                     *altair.LightClientHeader
	CurrentSyncCommittee       *altair.SyncCommittee
	CurrentSyncCommitteeBranch [][]byte `ssz-size:"5,32"`
}

func toLightClientBootstrap(data *spec.VersionedLCBootstrap) (*LightClientBootstrap, error) {
	if err := checkVersion(data.Version); err != nil {
		return nil, err
	}

	switch data.Version {
	case spec.DataVersionCapella:
		d := data.Capella
		return &LightClientBootstrap{
			Header:                     d.Header.ToAltair(),
			CurrentSyncCommittee:       d.CurrentSyncCommittee,
			CurrentSyncCommitteeBranch: d.CurrentSyncCommitteeBranch,
		}, nil
	case spec.DataVersionDeneb:
		d := data.Deneb
		return &LightClientBootstrap{
			Header:                     d.Header.ToAltair(),
			CurrentSyncCommittee:       d.CurrentSyncCommittee,
			CurrentSyncCommitteeBranch: d.CurrentSyncCommitteeBranch,
		}, nil
	default:
		return nil, errors.UnsupportedError.Errorf("unsupported version %s", data.Version)
	}

}

type LightClientUpdate struct {
	AttestedHeader          *altair.LightClientHeader
	NextSyncCommittee       *altair.SyncCommittee
	NextSyncCommitteeBranch [][]byte
	FinalizedHeader         *altair.LightClientHeader
	FinalityBranch          [][]byte
	SyncAggregate           *altair.SyncAggregate
	SignatureSlot           phase0.Slot
}

func (l *LightClientUpdate) String() string {
	data, err := json.Marshal(l)
	if err != nil {
		return fmt.Sprintf("ERR: %v", err)
	}
	return string(data)
}

func toLightClientUpdate(data *spec.VersionedLCUpdate) (*LightClientUpdate, error) {
	if err := checkVersion(data.Version); err != nil {
		return nil, err
	}

	switch data.Version {
	case spec.DataVersionCapella:
		d := data.Capella
		return &LightClientUpdate{
			AttestedHeader:          d.AttestedHeader.ToAltair(),
			NextSyncCommittee:       d.NextSyncCommittee,
			NextSyncCommitteeBranch: d.NextSyncCommitteeBranch,
			FinalizedHeader:         d.FinalizedHeader.ToAltair(),
			FinalityBranch:          d.FinalityBranch,
			SyncAggregate:           d.SyncAggregate,
			SignatureSlot:           d.SignatureSlot,
		}, nil
	case spec.DataVersionDeneb:
		d := data.Deneb
		return &LightClientUpdate{
			AttestedHeader:          d.AttestedHeader.ToAltair(),
			NextSyncCommittee:       d.NextSyncCommittee,
			NextSyncCommitteeBranch: d.NextSyncCommitteeBranch,
			FinalizedHeader:         d.FinalizedHeader.ToAltair(),
			FinalityBranch:          d.FinalityBranch,
			SyncAggregate:           d.SyncAggregate,
			SignatureSlot:           d.SignatureSlot,
		}, nil
	default:
		return nil, errors.UnsupportedError.Errorf("unsupported version %s", data.Version)
	}
}

type LightClientOptimisticUpdate struct {
	AttestedHeader *altair.LightClientHeader
	SyncAggregate  *altair.SyncAggregate
	SignatureSlot  phase0.Slot
	blockNumber    uint64
}

func (l *LightClientOptimisticUpdate) Slot() phase0.Slot {
	return l.AttestedHeader.Beacon.Slot
}

func (l *LightClientOptimisticUpdate) BlockNumber() uint64 {
	return l.blockNumber
}

func (l *LightClientOptimisticUpdate) ToAltair() *altair.LightClientHeader {
	return l.AttestedHeader
}

func ToLightClientOptimisticUpdate(data *spec.VersionedLCOptimisticUpdate) (*LightClientOptimisticUpdate, error) {
	if err := checkVersion(data.Version); err != nil {
		return nil, err
	}

	switch data.Version {
	case spec.DataVersionCapella:
		d := data.Capella
		return &LightClientOptimisticUpdate{
			AttestedHeader: d.AttestedHeader.ToAltair(),
			SyncAggregate:  d.SyncAggregate,
			SignatureSlot:  d.SignatureSlot,
			blockNumber:    d.AttestedHeader.Execution.BlockNumber,
		}, nil
	case spec.DataVersionDeneb:
		d := data.Deneb
		return &LightClientOptimisticUpdate{
			AttestedHeader: d.AttestedHeader.ToAltair(),
			SyncAggregate:  d.SyncAggregate,
			SignatureSlot:  d.SignatureSlot,
			blockNumber:    d.AttestedHeader.Execution.BlockNumber,
		}, nil
	default:
		return nil, errors.UnsupportedError.Errorf("unsupported version %s", data.Version)
	}
}

type LightClientFinalityUpdate struct {
	AttestedHeader       *altair.LightClientHeader
	FinalizedHeader      *altair.LightClientHeader
	FinalityBranch       [][]byte
	SyncAggregate        *altair.SyncAggregate
	SignatureSlot        phase0.Slot
	FinalizedBlockNumber uint64
}

func ToLightClientFinalityUpdate(data *spec.VersionedLCFinalityUpdate) (*LightClientFinalityUpdate, error) {
	if err := checkVersion(data.Version); err != nil {
		return nil, err
	}

	switch data.Version {
	case spec.DataVersionCapella:
		d := data.Capella
		return &LightClientFinalityUpdate{
			AttestedHeader:       d.AttestedHeader.ToAltair(),
			FinalizedHeader:      d.FinalizedHeader.ToAltair(),
			FinalityBranch:       d.FinalityBranch,
			SyncAggregate:        d.SyncAggregate,
			SignatureSlot:        d.SignatureSlot,
			FinalizedBlockNumber: d.FinalizedHeader.Execution.BlockNumber,
		}, nil
	case spec.DataVersionDeneb:
		d := data.Deneb
		return &LightClientFinalityUpdate{
			AttestedHeader:       d.AttestedHeader.ToAltair(),
			FinalizedHeader:      d.FinalizedHeader.ToAltair(),
			FinalityBranch:       d.FinalityBranch,
			SyncAggregate:        d.SyncAggregate,
			SignatureSlot:        d.SignatureSlot,
			FinalizedBlockNumber: d.FinalizedHeader.Execution.BlockNumber,
		}, nil
	default:
		return nil, errors.UnsupportedError.Errorf("unsupported version %s", data.Version)
	}

}

type LightClientHeader interface {
	Slot() phase0.Slot
	BlockNumber() uint64
	ToAltair() *altair.LightClientHeader
}

type lightClientHeader struct {
	beacon      *phase0.BeaconBlockHeader
	blockNumber uint64
}

func (l *lightClientHeader) Slot() phase0.Slot {
	return l.beacon.Slot
}

func (l *lightClientHeader) BlockNumber() uint64 {
	return l.blockNumber
}

func (l *lightClientHeader) ToAltair() *altair.LightClientHeader {
	return &altair.LightClientHeader{Beacon: l.beacon}
}

func LightClientHeaderFromBeaconBlockHeader(bh *apiv1.BeaconBlockHeader) LightClientHeader {
	return &lightClientHeader{
		beacon: bh.Header.Message,
	}
}
