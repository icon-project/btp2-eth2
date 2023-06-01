package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	eth2client "github.com/attestantio/go-eth2-client"
	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/multi"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/icon-project/btp2/common/errors"
	"github.com/icon-project/btp2/common/log"
	"github.com/rs/zerolog"

	"github.com/icon-project/btp2-eth2/chain/eth2/client/lightclient"
)

type ConsensusLayer struct {
	ctx     context.Context
	cancel  context.CancelFunc
	service eth2client.Service
	lc      *lightclient.LightClient
	uri     string
	cs      ConsensusConfigSpec
	log     log.Logger
}

func (c *ConsensusLayer) Genesis() (*api.Genesis, error) {
	return c.service.(eth2client.GenesisProvider).Genesis(c.ctx)
}

func (c *ConsensusLayer) BeaconBlockHeader(blockID string) (*api.BeaconBlockHeader, error) {
	return c.service.(eth2client.BeaconBlockHeadersProvider).BeaconBlockHeader(c.ctx, blockID)
}

type VersionedRawBeaconBlock struct {
	Version spec.DataVersion `json:"version"`
	Data    struct {
		Message struct {
			Body struct {
				ExecutionPayload json.RawMessage `json:"execution_payload"`
			} `json:"body"`
		}
	} `json:"data"`
}

func (v *VersionedRawBeaconBlock) BlockNumber() (bn uint64, err error) {
	switch v.Version {
	case spec.DataVersionPhase0, spec.DataVersionAltair:
		return 0, errors.Errorf("not support at %s", v.Version)
	case spec.DataVersionBellatrix:
		e := &bellatrix.ExecutionPayload{}
		if err = json.Unmarshal(v.Data.Message.Body.ExecutionPayload, e); err != nil {
			err = errors.Wrapf(err, "failed to parse %s signed beacon block, err:%s", v.Version, err.Error())
			return
		}
		return e.BlockNumber, nil
	case spec.DataVersionCapella:
		e := &capella.ExecutionPayload{}
		if err = json.Unmarshal(v.Data.Message.Body.ExecutionPayload, e); err != nil {
			err = errors.Wrapf(err, "failed to parse %s signed beacon block, err:%s", v.Version, err.Error())
			return
		}
		return e.BlockNumber, nil
	default:
		//unreachable codes
		return 0, errors.New("unknown version")
	}
}

func (c *ConsensusLayer) get(url string) ([]byte, error) {
	resp, err := http.Get(c.uri + url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if c.log.GetLevel() >= log.TraceLevel {
		c.log.Traceln(string(b))
	}
	if resp.StatusCode/100 != 2 {
		return nil, errors.Errorf("server response not success, StatusCode:%d",
			resp.StatusCode)
	}
	return b, err
}

func (c *ConsensusLayer) RawBeaconBlock(blockID string) (*VersionedRawBeaconBlock, error) {
	b, err := c.get(fmt.Sprintf("/eth/v2/beacon/blocks/%s", blockID))
	if err != nil {
		return nil, err
	}
	v := &VersionedRawBeaconBlock{}
	if err = json.Unmarshal(b, v); err != nil {
		return nil, errors.Wrapf(err, "fail to unmarshal err:%s", err.Error())
	}
	return v, nil
}

func (c *ConsensusLayer) BeaconBlockRoot(blockID string) (*phase0.Root, error) {
	return c.service.(eth2client.BeaconBlockRootProvider).BeaconBlockRoot(c.ctx, blockID)
}

func (c *ConsensusLayer) LightClientEvents(ocb lightclient.OptimisticUpdateCallbackFunc,
	fcb lightclient.FinalityUpdateCallbackFunc, ecb func(err error) (reconnect bool)) error {
	return c.lc.Events(ocb, fcb, ecb)
}

func (c *ConsensusLayer) LightClientEventsWithContext(ctx context.Context,
	ocb lightclient.OptimisticUpdateCallbackFunc,
	fcb lightclient.FinalityUpdateCallbackFunc) {
	c.lc.EventsWithContext(ctx, ocb, fcb)
}

func (c *ConsensusLayer) LightClientBootstrap(blockRoot phase0.Root) (*lightclient.LightClientBootstrap, error) {
	return c.lc.Bootstrap(blockRoot)
}

func (c *ConsensusLayer) LightClientUpdates(startPeriod, count uint64) ([]*lightclient.LightClientUpdate, error) {
	return c.lc.Updates(startPeriod, count)
}

func (c *ConsensusLayer) LightClientOptimisticUpdate() (*lightclient.LightClientOptimisticUpdate, error) {
	return c.lc.OptimisticUpdate()
}

func (c *ConsensusLayer) LightClientFinalityUpdate() (*lightclient.LightClientFinalityUpdate, error) {
	return c.lc.FinalityUpdate()
}

func (c *ConsensusLayer) GetStateProofWithGIndex(stateId string, gindex uint64) ([]byte, error) {
	// TODO this api is not public, so do not query via go-eth2-client
	return c.get(fmt.Sprintf("/eth/v0/beacon/icon/proof/state/%s?gindex=%d", stateId, gindex))
}

func (c *ConsensusLayer) GetReceiptsRootProof(slot int64) ([]byte, error) {
	// TODO this api is not public, so do not query via go-eth2-client
	return c.get(fmt.Sprintf("/eth/v0/beacon/icon/proof/state/receiptsRoot/%d", slot))
}

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
	block, err := c.RawBeaconBlock(strconv.FormatInt(int64(sn), 10))
	if err != nil {
		return 0, err
	}
	return block.BlockNumber()
}

func (c *ConsensusLayer) Spec() ConsensusConfigSpec {
	return c.cs
}

func (c *ConsensusLayer) Term() {
	c.cancel()
}

var (
	zerologLevelMap = map[log.Level]zerolog.Level{
		log.TraceLevel: zerolog.TraceLevel,
		log.DebugLevel: zerolog.DebugLevel,
		log.InfoLevel:  zerolog.InfoLevel,
		log.WarnLevel:  zerolog.WarnLevel,
		log.ErrorLevel: zerolog.ErrorLevel,
		log.FatalLevel: zerolog.FatalLevel,
		log.PanicLevel: zerolog.PanicLevel,
	}
)

func NewConsensusLayer(uri string, log log.Logger) (*ConsensusLayer, error) {
	cs, err := NewConsensusConfigSpec(uri)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	service, err := multi.New(ctx,
		multi.WithAddresses([]string{uri}),
		multi.WithLogLevel(zerologLevelMap[log.GetLevel()]))
	if err != nil {
		cancel()
		return nil, err
	}
	return &ConsensusLayer{
		ctx:     ctx,
		cancel:  cancel,
		service: service,
		lc:      lightclient.NewLightClient(uri, log),
		uri:     uri,
		cs:      cs,
		log:     log,
	}, nil
}
