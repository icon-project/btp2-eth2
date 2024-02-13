package client

import (
	"context"
	"fmt"
	"io"
	nhttp "net/http"
	"strconv"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/icon-project/btp2/common/log"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

const (
	TopicLCOptimisticUpdate = "light_client_optimistic_update"
	TopicLCFinalityUpdate   = "light_client_finality_update"

	requestTimeout = 5 * time.Second
)

type ConsensusLayer struct {
	ctx     context.Context
	cancel  context.CancelFunc
	service eth2client.Service
	uri     string
	log     log.Logger
}

func (c *ConsensusLayer) Genesis() (*apiv1.Genesis, error) {
	resp, err := c.service.(*http.Service).Genesis(c.ctx, &api.GenesisOpts{})
	if err != nil {
		return nil, err
	}
	return resp.Data, err
}

func (c *ConsensusLayer) BeaconBlockHeader(blockID string) (*apiv1.BeaconBlockHeader, error) {
	resp, err := c.service.(*http.Service).BeaconBlockHeader(c.ctx, &api.BeaconBlockHeaderOpts{Block: blockID})
	if err != nil {
		if notFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Data, err
}

func (c *ConsensusLayer) BeaconBlock(blockID string) (*spec.VersionedSignedBeaconBlock, error) {
	resp, err := c.service.(*http.Service).SignedBeaconBlock(c.ctx, &api.SignedBeaconBlockOpts{Block: blockID})
	if err != nil {
		if notFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Data, err
}

func (c *ConsensusLayer) BeaconBlockRoot(blockID string) (*phase0.Root, error) {
	resp, err := c.service.(*http.Service).BeaconBlockRoot(c.ctx, &api.BeaconBlockRootOpts{Block: blockID})
	if err != nil {
		if notFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Data, err
}

func (c *ConsensusLayer) FinalityCheckpoints(stateID string) (*apiv1.Finality, error) {
	resp, err := c.service.(*http.Service).Finality(c.ctx, &api.FinalityOpts{State: stateID})
	if err != nil {
		if notFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Data, err
}

func (c *ConsensusLayer) Events(topics []string, handler eth2client.EventHandlerFunc) error {
	return c.service.(eth2client.EventsProvider).Events(c.ctx, topics, handler)
}

func (c *ConsensusLayer) LightClientBootstrap(blockRoot phase0.Root) (*deneb.LightClientBootstrap, error) {
	resp, err := c.service.(*http.Service).LightClientBootstrap(
		c.ctx,
		&api.LightClientBootstrapOpts{Block: fmt.Sprintf("%#x", blockRoot)},
	)
	if err != nil {
		return nil, err
	}
	return resp.Data, err
}

func (c *ConsensusLayer) LightClientUpdates(startPeriod, count uint64) ([]*deneb.LightClientUpdate, error) {
	resp, err := c.service.(*http.Service).LightClientUpdates(
		c.ctx,
		&api.LightClientUpdatesOpts{StartPeriod: startPeriod, Count: count},
	)
	if err != nil {
		return nil, err
	}
	return resp.Data, err
}

func (c *ConsensusLayer) LightClientOptimisticUpdate() (*deneb.LightClientOptimisticUpdate, error) {
	resp, err := c.service.(*http.Service).LightClientOptimisticUpdate(c.ctx, &api.CommonOpts{})
	if err != nil {
		return nil, err
	}
	return resp.Data, err
}

type LightClientFinalityUpdate struct {
}

func (c *ConsensusLayer) LightClientFinalityUpdate() (*deneb.LightClientFinalityUpdate, error) {
	resp, err := c.service.(*http.Service).LightClientFinalityUpdate(c.ctx, &api.CommonOpts{})
	if err != nil {
		return nil, err
	}
	return resp.Data, err
}

func (c *ConsensusLayer) GetStateProofWithGIndex(stateId string, gindex uint64) ([]byte, error) {
	// TODO this api is not public, so do not query via go-eth2-client
	url := fmt.Sprintf("%s/eth/v0/beacon/icon/proof/state/%s?gindex=%d", c.uri, stateId, gindex)
	resp, err := nhttp.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *ConsensusLayer) GetReceiptsRootProof(slot int64) ([]byte, error) {
	// TODO this api is not public, so do not query via go-eth2-client
	url := fmt.Sprintf("%s/eth/v0/beacon/icon/proof/state/receiptsRoot/%d", c.uri, slot)
	resp, err := nhttp.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// SlotToBlockNumber returns execution block number for consensus slot
// If slot has no block, returns (0, nil).
func (c *ConsensusLayer) SlotToBlockNumber(slot phase0.Slot) (uint64, error) {
	var sn phase0.Slot
	if slot == 0 {
		// get slot from finalized header
		fu, err := c.LightClientFinalityUpdate()
		if err != nil {
			return 0, err
		}
		sn = fu.FinalizedHeader.Beacon.Slot
	} else {
		sn = slot
	}

	block, err := c.BeaconBlock(strconv.FormatInt(int64(sn), 10))
	if err != nil || block == nil {
		c.log.Debugf("no block for slot %d. %+v", slot, err)
		return 0, err
	}
	return block.BlockNumber()
}

func (c *ConsensusLayer) Term() {
	c.cancel()
}

func notFoundError(err error) bool {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 404
	}
	return false
}

func NewConsensusLayer(uri string, log log.Logger) (*ConsensusLayer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	service, err := http.New(
		ctx,
		http.WithTimeout(requestTimeout),
		http.WithAddress(uri),
		http.WithLogLevel(zerolog.WarnLevel),
	)
	if err != nil {
		cancel()
		return nil, err
	}
	return &ConsensusLayer{
		ctx:     ctx,
		cancel:  cancel,
		service: service,
		uri:     uri,
		log:     log,
	}, nil
}
