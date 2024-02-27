package client_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/icon-project/btp2/common/log"
	"github.com/stretchr/testify/assert"

	"github.com/icon-project/btp2-eth2/chain/eth2"
	"github.com/icon-project/btp2-eth2/chain/eth2/client"
	"github.com/icon-project/btp2-eth2/chain/eth2/proof"
)

const (
	iconEndpoint         = "https://berlin.net.solidwallet.io/api/v3/icon_dex"
	ethNodeAddr          = "http://127.0.0.1"
	ethExecutionEndpoint = ethNodeAddr + ":8545"
	ethConsensusEndpoint = ethNodeAddr + ":9596"

	iconBMC        = "cxb61b42e51c20054b4f5fd31b7d64af8c59579829"
	ethBMC         = "0xD99d2A85E9B0Fa3E7fbA4756699f4307BFBb80c3"
	btpAddressICON = "btp://0x3.icon/" + iconBMC
	btpAddressETH  = "btp://0xaa36a7.eth2/" + ethBMC
)

func newTestConsensusLayer() (*client.ConsensusLayer, error) {
	return client.NewConsensusLayer(ethConsensusEndpoint, log.WithFields(log.Fields{log.FieldKeyPrefix: "test"}))
}

func TestConsensusLayer_genesis(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	resp, err := c.Genesis()
	assert.NoError(t, err)
	fmt.Printf("%+v\n", resp)
}

func TestConsensusLayer_BeaconBlocks(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	fc, err := c.FinalityCheckpoints("head")
	assert.NoError(t, err)

	bh, err := c.BeaconBlockHeader("finalized")
	assert.NoError(t, err)
	assert.Equal(t, fc.Finalized.Root, bh.Root)

	block, err := c.BeaconBlock("finalized")
	assert.NoError(t, err)
	blockRoot, err := block.Root()
	assert.NoError(t, err)
	assert.Equal(t, fc.Finalized.Root, blockRoot)

	root, err := c.BeaconBlockRoot("finalized")
	assert.Equal(t, fc.Finalized.Root, *root)
	assert.NoError(t, err)
}

func TestConsensusLayer_Events(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer func() {
		c.Term()
	}()
	assert.NoError(t, err)
	ch := make(chan *api.Event)
	defer func() {
		close(ch)
	}()
	go func(c *client.ConsensusLayer, ch chan *api.Event) {
		eth2Topics := []string{client.TopicLCOptimisticUpdate, client.TopicLCFinalityUpdate}
		err = c.Events(eth2Topics, func(event *api.Event) {
			ch <- event
		})
		assert.NoError(t, err)
	}(c, ch)

	oCount := 0
	fCount := 0
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func(ch chan *api.Event) {
		timeout := time.Second * 25
		for {
			select {
			case e := <-ch:
				if e.Topic == client.TopicLCFinalityUpdate {
					u, err := client.ToLightClientFinalityUpdate(e.Data.(*spec.VersionedLCFinalityUpdate))
					assert.NoError(t, err)
					assert.True(t, eth2.IsCheckPoint(u.FinalizedHeader.Beacon.Slot))
					fmt.Printf("%+v\n", u)
					fCount++
					wg.Done()
					return
				} else if e.Topic == client.TopicLCOptimisticUpdate {
					u, err := client.ToLightClientOptimisticUpdate(e.Data.(*spec.VersionedLCOptimisticUpdate))
					assert.NoError(t, err)
					fmt.Printf("%+v\n", u)
					oCount++
				}
			case <-time.After(timeout):
				wg.Done()
				return
			}
		}
	}(ch)
	wg.Wait()
	assert.True(t, oCount > 0, oCount)
	assert.Equal(t, 1, fCount)
}

func TestConsensusLayer_LightClientBootstrap(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	fc, err := c.FinalityCheckpoints("head")
	assert.NoError(t, err)

	_, err = c.LightClientBootstrap(fc.Finalized.Root)
	assert.NoError(t, err)
}

func TestConsensusLayer_LightClientUpdates(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	header, err := c.BeaconBlockHeader("finalized")
	assert.NoError(t, err)
	startPeriod := eth2.SlotToSyncCommitteePeriod(header.Header.Message.Slot)

	resp, err := c.LightClientUpdates(startPeriod, 1)
	assert.NoError(t, err)
	fmt.Printf("%+v\n", resp)
}

func TestConsensusLayer_LightClientOptimisticUpdate(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	_, err = c.LightClientOptimisticUpdate()
	assert.NoError(t, err)
}

func TestConsensusLayer_LightClientFinalityUpdate(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	_, err = c.LightClientFinalityUpdate()
	assert.NoError(t, err)
}

func TestConsensusLayer_GetStateProofWithGIndex(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	bh, err := c.BeaconBlockHeader("finalized")
	assert.NoError(t, err)

	// proof for BeaconState.BlockRoots
	gindex := proof.BlockRootsIdxToGIndex(eth2.SlotToBlockRootsIndex(bh.Header.Message.Slot))
	_, err = c.GetStateProofWithGIndex("finalized", gindex)
	assert.NoError(t, err)

	// proof for BeaconState.HistoricalSummaries
	gindex = proof.HistoricalSummariesIdxToGIndex(eth2.SlotToBlockRootsIndex(bh.Header.Message.Slot))
	_, err = c.GetStateProofWithGIndex("finalized", gindex)
	assert.NoError(t, err)
}
