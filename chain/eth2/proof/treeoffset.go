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
	"math"

	ssz "github.com/ferranbt/fastssz"
	"github.com/icon-project/btp2/common/errors"
)

type TreeOffsetProof struct {
	offsets []uint16
	leaves  [][]byte
}

func (t *TreeOffsetProof) Offsets() []uint16 {
	return t.offsets
}

func (t *TreeOffsetProof) Leaves() [][]byte {
	return t.leaves
}

func (t *TreeOffsetProof) GetGIndex() int {
	base := int(math.Pow(2, float64(len(t.offsets))))
	value := 0
	for _, offset := range t.offsets {
		value = value << 1
		if offset == 1 {
			value |= 1
		}
	}
	return base + value
}

func NewTreeOffsetProof(data []byte) (*TreeOffsetProof, error) {
	proofType := int(data[0])
	if proofType != 1 {
		return nil, errors.IllegalArgumentError.Errorf("invalid proof type. %d", proofType)
	}
	dataOffset := 1
	// leaf count
	leafCount := int(binary.LittleEndian.Uint16(data[dataOffset : dataOffset+2]))
	if len(data) < (leafCount-1)*2+leafCount*32 {
		return nil, errors.IllegalArgumentError.Errorf("not enough bytes. %+v", data)
	}
	dataOffset += 2

	// offsets
	offsets := make([]uint16, leafCount-1, leafCount-1)
	for i := 0; i < leafCount-1; i++ {
		offsets[i] = binary.LittleEndian.Uint16(data[dataOffset+i*2 : dataOffset+i*2+2])
	}
	dataOffset += 2 * (leafCount - 1)

	// leaves
	leaves := make([][]byte, leafCount, leafCount)
	for i := 0; i < leafCount; i++ {
		leaves[i] = data[dataOffset : dataOffset+32]
		dataOffset += 32
	}

	return &TreeOffsetProof{
		offsets: offsets,
		leaves:  leaves,
	}, nil
}

func TreeOffsetProofToSSZProof(data []byte) (*ssz.Proof, error) {
	top, err := NewTreeOffsetProof(data)

	node, err := TreeOffsetProofToNode(top.Offsets(), top.Leaves())
	if err != nil {
		return nil, err
	}

	return node.Prove(top.GetGIndex())
}

// TreeOffsetProofToNode Recreate a `Node` given offsets and leaves of a tree-offset proof
// See https://github.com/protolambda/eth-merkle-trees/blob/master/tree_offsets.md
func TreeOffsetProofToNode(offsets []uint16, leaves [][]byte) (*ssz.Node, error) {
	if len(leaves) == 0 {
		return nil, errors.IllegalArgumentError.Errorf("proof must contain gt 0 leaves")
	} else if len(leaves) == 1 {
		return ssz.LeafFromBytes(leaves[0]), nil
	} else {
		// the offset popped from the list is the # of leaves in the left subtree
		pivot := offsets[0]
		left, err := TreeOffsetProofToNode(offsets[1:pivot], leaves[0:pivot])
		if err != nil {
			return nil, err
		}
		right, err := TreeOffsetProofToNode(offsets[pivot:], leaves[pivot:])
		if err != nil {
			return nil, err
		}
		return ssz.NewNodeWithLR(left, right), nil
	}
}
