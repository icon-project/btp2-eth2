package client

import (
	"fmt"
	"sync"
	"testing"

	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/icon-project/btp2/chain/icon/client"
	"github.com/icon-project/btp2/common/log"
	"github.com/icon-project/btp2/common/types"
	"github.com/stretchr/testify/assert"
)

const (
	iconEndpoint         = "https://berlin.net.solidwallet.io/api/v3/icon_dex"
	ethNodeAddr          = "http://1.1.1.1"
	ethExecutionEndpoint = ethNodeAddr + ":8545"
	ethConsensusEndpoint = ethNodeAddr + ":9596"

	iconBMC        = "cxb61b42e51c20054b4f5fd31b7d64af8c59579829"
	ethBMC         = "0xD99d2A85E9B0Fa3E7fbA4756699f4307BFBb80c3"
	btpAddressICON = "btp://0x3.icon/" + iconBMC
	btpAddressETH  = "btp://0xaa36a7.eth2/" + ethBMC
)

func newTestConsensusLayer() (*ConsensusLayer, error) {
	return NewConsensusLayer(ethConsensusEndpoint, log.WithFields(log.Fields{log.FieldKeyPrefix: "test"}))
}

func iconGetStatus() (*types.BMCLinkStatus, error) {
	c := client.NewClient(iconEndpoint, log.WithFields(log.Fields{log.FieldKeyPrefix: "test"}))
	p := &client.CallParam{
		ToAddress: client.Address(iconBMC),
		DataType:  "call",
		Data: client.CallData{
			Method: client.BMCGetStatusMethod,
			Params: client.BMCStatusParams{
				Target: btpAddressETH,
			},
		},
	}
	bs := &client.BMCStatus{}
	err := client.MapError(c.Call(p, bs))
	if err != nil {
		return nil, err
	}
	ls := &types.BMCLinkStatus{}
	if ls.TxSeq, err = bs.TxSeq.Value(); err != nil {
		return nil, err
	}
	if ls.RxSeq, err = bs.RxSeq.Value(); err != nil {
		return nil, err
	}
	if ls.Verifier.Height, err = bs.Verifier.Height.Value(); err != nil {
		return nil, err
	}
	if ls.Verifier.Extra, err = bs.Verifier.Extra.Value(); err != nil {
		return nil, err
	}
	return ls, nil
}

//func TestReceiver_RunMonitoring(t *testing.T) {
//	eth := newTestConsensusLayer()
//	defer eth.Stop()
//
//	bls, err := iconGetStatus()
//	assert.NoError(t, err)
//
//	wg := sync.WaitGroup{}
//	wg.Add(1)
//	go func() {
//		eth.Monitoring(bls)
//	}()
//	wg.Wait()
//}

func TestConsensusLayer_genesis(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	resp, err := c.Genesis()
	assert.NoError(t, err)
	fmt.Printf("%+v\n", resp)
}

func TestConsensusLayer_LightClientUpdates(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)

	resp, err := c.LightClientUpdates(523, 2)
	assert.NoError(t, err)
	fmt.Printf("%+v\n", resp)
}

func TestConsensusLayer_Events(t *testing.T) {
	c, err := newTestConsensusLayer()
	defer c.Term()
	assert.NoError(t, err)
	eth2Topics := []string{TopicLCOptimisticUpdate, TopicLCFinalityUpdate}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err = c.Events(eth2Topics, func(event *api.Event) {
			fmt.Printf("%s\n", event.Topic)
			if event.Topic == TopicLCFinalityUpdate {
				u, err := ToLightClientFinalityUpdate(event.Data.(*spec.VersionedLCFinalityUpdate))
				assert.NoError(t, err)
				fmt.Printf("%+v\n", u)
			} else if event.Topic == TopicLCOptimisticUpdate{
				u, err := ToLightClientOptimisticUpdate(event.Data.(*spec.VersionedLCOptimisticUpdate))
				assert.NoError(t, err)
				fmt.Printf("%+v\n", u)
			}
		})
		assert.NoError(t, err)
	}()
	wg.Wait()
}
