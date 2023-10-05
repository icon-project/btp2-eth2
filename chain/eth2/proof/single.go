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

import (
	"encoding/binary"
	"fmt"
	"math"

	ssz "github.com/ferranbt/fastssz"
)

type SingleProof struct {
	ssz *ssz.Proof
}

func (s *SingleProof) SSZ() *ssz.Proof {
	return s.ssz
}

func (s *SingleProof) Leaf() []byte {
	return s.ssz.Leaf
}

func NewSingleProof(data []byte) (*SingleProof, error) {
	ssz := &ssz.Proof{}
	proofType := int(data[0])
	if proofType != 0 {
		return nil, fmt.Errorf("invalid proof type. %s", data)
	}
	dataOffset := 1

	idx := binary.LittleEndian.Uint64(data[dataOffset : dataOffset+8])
	ssz.Index = int(idx)
	dataOffset += 8

	ssz.Leaf = data[dataOffset : dataOffset+32]
	dataOffset += 32

	leafCount := int(math.Log2(float64(ssz.Index)))
	ssz.Hashes = make([][]byte, leafCount, leafCount)
	for i := 0; i < leafCount; i++ {
		ssz.Hashes[i] = data[dataOffset : dataOffset+32]
		dataOffset += 32
	}

	return &SingleProof{ssz: ssz}, nil
}
