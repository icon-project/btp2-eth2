package proof

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGIndex_BlockRootsIdxToGIndex(t *testing.T) {
	tests := []struct {
		idx  uint64
		want uint64
	}{
		{
			idx:  6490,
			want: 309594,
		},
		{
			idx:  6491,
			want: 309595,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("idx=%d", tt.idx), func(t *testing.T) {
			ret := BlockRootsIdxToGIndex(tt.idx)
			assert.Equal(t, tt.want, ret)
		})
	}
}

func TestGIndex_HistoricalSummariesIdxToIndex(t *testing.T) {
	tests := []struct {
		idx  uint64
		want uint64
	}{
		{
			idx:  43,
			want: 3959423062,
		},
		{
			idx:  44,
			want: 3959423064,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.idx), func(t *testing.T) {
			ret := HistoricalSummariesIdxToGIndex(tt.idx)
			assert.Equal(t, tt.want, ret)
		})
	}
}
