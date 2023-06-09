package client

import (
	"context"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/icon-project/btp2/common/log"
)

const (
	ForkEpochCapellaSepolia = 56832
	ForkEpochCapellaMainnet = 194048
	ForkEpochCapellaE2E     = 2147483647
	ForkEpochCapella        = ForkEpochCapellaSepolia
)

type ConsensusConfigSpec map[string]interface{}

func (c ConsensusConfigSpec) SecondPerSlot() time.Duration {
	return c["SECONDS_PER_SLOT"].(time.Duration)
}

func (c ConsensusConfigSpec) SlotPerEpoch() uint64 {
	return c["SLOTS_PER_EPOCH"].(uint64)
}

func (c ConsensusConfigSpec) SyncCommitteeSize() uint64 {
	return c["SYNC_COMMITTEE_SIZE"].(uint64)
}

func (c ConsensusConfigSpec) EpochsPerSyncCommitteePeriod() uint64 {
	return c["EPOCHS_PER_SYNC_COMMITTEE_PERIOD"].(uint64)
}

func (c ConsensusConfigSpec) SlotPerHistoricalRoot() uint64 {
	return c["SLOTS_PER_HISTORICAL_ROOT"].(uint64)
}

func (c ConsensusConfigSpec) HistoricalRootLimit() uint64 {
	return c["HISTORICAL_ROOTS_LIMIT"].(uint64)
}

func (c ConsensusConfigSpec) SlotPerSyncCommitteePeriod() uint64 {
	return c.SlotPerEpoch() * c.EpochsPerSyncCommitteePeriod()
}

// SlotToEpoch returns the epoch number of the input slot.
func (c ConsensusConfigSpec) SlotToEpoch(s phase0.Slot) phase0.Epoch {
	return phase0.Epoch(uint64(s) / c.SlotPerEpoch())
}

func (c ConsensusConfigSpec) EpochToSyncCommitteePeriod(e phase0.Epoch) uint64 {
	return uint64(e) / c.EpochsPerSyncCommitteePeriod()
}

func (c ConsensusConfigSpec) SlotToSyncCommitteePeriod(s phase0.Slot) uint64 {
	return c.EpochToSyncCommitteePeriod(c.SlotToEpoch(s))
}

func (c ConsensusConfigSpec) IsSyncCommitteeEdge(s phase0.Slot) bool {
	return (uint64(s) % c.SlotPerSyncCommitteePeriod()) == 0
}

func (c ConsensusConfigSpec) IsCheckPoint(s phase0.Slot) bool {
	return (uint64(s) % c.SlotPerEpoch()) == 0
}

func (c ConsensusConfigSpec) SlotToBlockRootsIndex(s phase0.Slot) uint64 {
	return uint64(s) % c.SlotPerHistoricalRoot()
}

func (c ConsensusConfigSpec) SlotToHistoricalSummariesIndex(s phase0.Slot) uint64 {
	return (uint64(s) - ForkEpochCapella*c.SlotPerEpoch()) / c.SlotPerHistoricalRoot()
}

func (c ConsensusConfigSpec) HistoricalSummariesStartSlot(s phase0.Slot) phase0.Slot {
	return phase0.Slot((ForkEpochCapella * c.SlotPerEpoch()) +
		c.SlotToHistoricalSummariesIndex(s)*c.SlotPerHistoricalRoot())
}

func NewConsensusConfigSpec(uri string) (ConsensusConfigSpec, error) {
	ctx := context.Background()
	s, err := http.New(ctx,
		http.WithAddress(uri),
		http.WithLogLevel(zerologLevelMap[log.GlobalLogger().GetLevel()]))
	if err != nil {
		return nil, err
	}
	return s.(eth2client.SpecProvider).Spec(ctx)
}
