package eth2

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

const (
	SecondPerSlot                = 12
	SlotPerEpoch                 = 32
	EpochsPerSyncCommitteePeriod = 256
	SlotPerSyncCommitteePeriod   = SlotPerEpoch * EpochsPerSyncCommitteePeriod
	SlotPerHistoricalRoot        = 8192

	ForkEpochCapellaSepolia = 56832
	ForkEpochCapellaMainnet = 194048
	ForkEpochCapella        = ForkEpochCapellaSepolia
)

// SlotToEpoch returns the epoch number of the input slot.
func SlotToEpoch(s phase0.Slot) phase0.Epoch {
	return phase0.Epoch(s / SlotPerEpoch)
}

func EpochToSyncCommitteePeriod(e phase0.Epoch) uint64 {
	return uint64(e / EpochsPerSyncCommitteePeriod)
}

func SlotToSyncCommitteePeriod(s phase0.Slot) uint64 {
	return EpochToSyncCommitteePeriod(SlotToEpoch(s))
}

func IsSyncCommitteeEdge(s phase0.Slot) bool {
	return (s % SlotPerSyncCommitteePeriod) == 0
}

func IsCheckPoint(s phase0.Slot) bool {
	return (s % SlotPerEpoch) == 0
}

func SlotToBlockRootsIndex(s phase0.Slot) uint64 {
	return uint64(s % SlotPerHistoricalRoot)
}

func SlotToHistoricalSummariesIndex(s phase0.Slot) uint64 {
	return uint64(s-ForkEpochCapella*SlotPerEpoch) / SlotPerHistoricalRoot
}

func HistoricalSummariesStartSlot(s phase0.Slot) phase0.Slot {
	return phase0.Slot((ForkEpochCapella * SlotPerEpoch) + SlotToHistoricalSummariesIndex(s)*SlotPerHistoricalRoot)
}
