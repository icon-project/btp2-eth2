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

package proof

import "math/bits"

const (
	GIndexStateBlockRoots          = uint64(37)
	GIndexStateNextSyncCommittee   = uint64(55)
	GIndexStateFinalizedRoot       = uint64(105)
	GIndexStateHistoricalSummaries = uint64(118)
)

func ArrayIdxToGIndex(base, len, idx, elementSize uint64) uint64 {
	offset := (len + idx) * elementSize
	return (base-1)<<(63-bits.LeadingZeros64(offset)) + offset
}

func BlockRootsIdxToGIndex(idx, slotPerHistoricalRoot uint64) uint64 {
	return ArrayIdxToGIndex(GIndexStateBlockRoots, slotPerHistoricalRoot, idx, 1)
}

func HistoricalSummariesIdxToGIndex(idx, historicalRootsLimit uint64) uint64 {
	return ArrayIdxToGIndex(GIndexStateHistoricalSummaries, historicalRootsLimit, idx, 2)
}
