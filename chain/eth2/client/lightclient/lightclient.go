package lightclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/icon-project/btp2/common"
	"github.com/icon-project/btp2/common/errors"
	"github.com/icon-project/btp2/common/log"
	"github.com/r3labs/sse/v2"
)

const (
	TopicLCOptimisticUpdate = "light_client_optimistic_update"
	TopicLCFinalityUpdate   = "light_client_finality_update"
)

type LightClientBootstrap struct {
	Header                     *LightClientHeader `json:"header"`
	CurrentSyncCommittee       *SyncCommittee     `json:"current_sync_committee"`
	CurrentSyncCommitteeBranch []string           `json:"current_sync_committee_branch"`
}

type LightClientHeader struct {
	Beacon *phase0.BeaconBlockHeader `json:"beacon"`
}

func (l *LightClientHeader) String() string {
	data, err := json.Marshal(l)
	if err != nil {
		return fmt.Sprintf("ERR: %v", err)
	}
	return string(data)
}

// MarshalSSZ ssz marshals the LightClientHeader object
func (l *LightClientHeader) MarshalSSZ() ([]byte, error) {
	return ssz.MarshalSSZ(l)
}

// MarshalSSZTo ssz marshals the LightClientHeader object to a target array
func (l *LightClientHeader) MarshalSSZTo(buf []byte) (dst []byte, err error) {
	dst = buf
	offset := int(4)

	// Offset (0) 'Beacon'
	dst = ssz.WriteOffset(dst, offset)
	if l.Beacon == nil {
		l.Beacon = new(phase0.BeaconBlockHeader)
	}
	offset += l.Beacon.SizeSSZ()

	// Field (0) 'Beacon'
	if dst, err = l.Beacon.MarshalSSZTo(dst); err != nil {
		return
	}

	return
}

// UnmarshalSSZ ssz unmarshals the LightClientHeader object
func (l *LightClientHeader) UnmarshalSSZ(buf []byte) error {
	var err error
	size := uint64(len(buf))
	if size < 4 {
		return ssz.ErrSize
	}

	tail := buf
	var o0 uint64

	// Offset (0) 'Beacon'
	if o0 = ssz.ReadOffset(buf[0:4]); o0 > size {
		return ssz.ErrOffset
	}

	if o0 < 4 {
		return ssz.ErrInvalidVariableOffset
	}

	// Field (0) 'Beacon'
	{
		buf = tail[o0:]
		if l.Beacon == nil {
			l.Beacon = new(phase0.BeaconBlockHeader)
		}
		if err = l.Beacon.UnmarshalSSZ(buf); err != nil {
			return err
		}
	}
	return err
}

// SizeSSZ returns the ssz encoded size in bytes for the LightClientHeader object
func (l *LightClientHeader) SizeSSZ() (size int) {
	size = 4

	// Field (0) 'Beacon'
	if l.Beacon == nil {
		l.Beacon = new(phase0.BeaconBlockHeader)
	}
	size += l.Beacon.SizeSSZ()

	return
}

// HashTreeRoot ssz hashes the LightClientHeader object
func (l *LightClientHeader) HashTreeRoot() ([32]byte, error) {
	return ssz.HashWithDefaultHasher(l)
}

// HashTreeRootWith ssz hashes the LightClientHeader object with a hasher
func (l *LightClientHeader) HashTreeRootWith(hh ssz.HashWalker) (err error) {
	indx := hh.Index()

	// Field (0) 'Beacon'
	if err = l.Beacon.HashTreeRootWith(hh); err != nil {
		return
	}

	hh.Merkleize(indx)
	return
}

// GetTree ssz hashes the LightClientHeader object
func (l *LightClientHeader) GetTree() (*ssz.Node, error) {
	return ssz.ProofTree(l)
}

type LightClientUpdate struct {
	AttestedHeader          *LightClientHeader `json:"attested_header"`
	NextSyncCommittee       *SyncCommittee     `json:"next_sync_committee"`
	NextSyncCommitteeBranch []common.HexBytes  `json:"next_sync_committee_branch"`
	FinalizedHeader         *LightClientHeader `json:"finalized_header"`
	FinalityBranch          []common.HexBytes  `json:"finality_branch"`
	SyncAggregate           *SyncAggregate     `json:"sync_aggregate"`
	SignatureSlot           Slot               `json:"signature_slot"`
}

type LightClientOptimisticUpdate struct {
	AttestedHeader *LightClientHeader `json:"attested_header"` // The beacon block header that is attested to by the sync committee
	SyncAggregate  *SyncAggregate     `json:"sync_aggregate"`  // Sync committee aggregate signature
	SignatureSlot  Slot               `json:"signature_slot"`  // Slot at which the aggregate signature was created (untrusted)
}

type LightClientFinalityUpdate struct {
	AttestedHeader  *LightClientHeader `json:"attested_header"`  // The beacon block header that is attested to by the sync committee
	FinalizedHeader *LightClientHeader `json:"finalized_header"` // The finalized beacon block header attested to by Merkle branch
	FinalityBranch  []common.HexBytes  `json:"finality_branch"`
	SyncAggregate   *SyncAggregate     `json:"sync_aggregate"` // Sync committee aggregate signature
	SignatureSlot   Slot               `json:"signature_slot"` // Slot at which the aggregate signature was created (untrusted)
}

type LightClient struct {
	http.Client
	endpoint string
	l        log.Logger
}

func NewLightClient(endpoint string, l log.Logger) *LightClient {
	return &LightClient{Client: http.Client{}, endpoint: endpoint, l: l}
}

func (c *LightClient) isTraceEnabled() bool {
	return c.l.GetLevel() >= log.TraceLevel
}

func (c *LightClient) logBody(rc io.ReadCloser) io.ReadCloser {
	if c.isTraceEnabled() && rc != nil {
		b, _ := io.ReadAll(rc)
		c.l.Traceln(string(b))
		return io.NopCloser(bytes.NewBuffer(b))
	}
	return rc
}

func (c *LightClient) Get(url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", c.endpoint+url, nil)
	if err != nil {
		return nil, err
	}
	req.Body = c.logBody(req.Body)
	if resp, err = c.Client.Do(req); err != nil {
		return
	}
	resp.Body = c.logBody(resp.Body)
	if resp.StatusCode/100 != 2 {
		err = errors.Errorf("server response not success, StatusCode:%d",
			resp.StatusCode)
		return
	}
	return
}

type OptimisticUpdateCallbackFunc func(*LightClientOptimisticUpdate)
type FinalityUpdateCallbackFunc func(*LightClientFinalityUpdate)

func (c *LightClient) newSSEClientAndHandler(ocb OptimisticUpdateCallbackFunc, fcb FinalityUpdateCallbackFunc) (*sse.Client, func(*sse.Event)) {
	topics := make([]string, 0)
	if ocb != nil {
		topics = append(topics, TopicLCOptimisticUpdate)
	}
	if fcb != nil {
		topics = append(topics, TopicLCFinalityUpdate)
	}
	sseClient := sse.NewClient(c.endpoint + "/eth/v1/events?topics=" + strings.Join(topics, ","))
	handler := func(msg *sse.Event) {
		evt := string(msg.Event)
		if c.isTraceEnabled() {
			c.l.Traceln(evt, string(msg.Data))
		}
		switch evt {
		case TopicLCOptimisticUpdate:
			update := &LightClientOptimisticUpdate{}
			err := json.Unmarshal(msg.Data, update)
			if err != nil {
				return
			}
			ocb(update)
		case TopicLCFinalityUpdate:
			update := &LightClientFinalityUpdate{}
			err := json.Unmarshal(msg.Data, update)
			if err != nil {
				return
			}
			fcb(update)
		}
	}
	return sseClient, handler
}

func (c *LightClient) Events(ocb OptimisticUpdateCallbackFunc, fcb FinalityUpdateCallbackFunc, ecb func(err error) (reconnect bool)) error {
	sseClient, handler := c.newSSEClientAndHandler(ocb, fcb)
	for {
		err := sseClient.SubscribeRaw(handler)
		if ecb != nil && ecb(err) {
			c.l.Debugf("fail to SubscribeRaw err:%+v", err)
			continue
		}
		return err
	}
}

func (c *LightClient) EventsWithContext(ctx context.Context, ocb OptimisticUpdateCallbackFunc, fcb FinalityUpdateCallbackFunc) {
	sseClient, handler := c.newSSEClientAndHandler(ocb, fcb)
	go func() {
		for {
			select {
			case <-time.After(time.Second):
				c.l.Debugln("try to SubscribeRawWithContext")
				if err := sseClient.SubscribeRawWithContext(ctx, handler); err != nil {
					c.l.Warnf("fail to SubscribeRawWithContext err:%+v", err)
				}
				c.l.Debugln("SubscribeRawWithContext stopped")
			case <-ctx.Done():
				c.l.Debugln("SubscribeRawWithContext context done")
				return
			}
		}
	}()
}

func (c *LightClient) Bootstrap(blockRoot phase0.Root) (*LightClientBootstrap, error) {
	resp, err := c.Get(fmt.Sprintf("/eth/v1/beacon/light_client/bootstrap/%#x", blockRoot))
	if err != nil {
		return nil, err
	}
	v := struct {
		Version string               `json:"version"`
		Data    LightClientBootstrap `json:"data"`
	}{}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	return &v.Data, nil
}

func (c *LightClient) Updates(startPeriod, count uint64) ([]*LightClientUpdate, error) {
	resp, err := c.Get(fmt.Sprintf("/eth/v1/beacon/light_client/updates?start_period=%d&count=%d", startPeriod, count))
	if err != nil {
		return nil, err
	}
	v := []struct {
		Version string             `json:"version"`
		Data    *LightClientUpdate `json:"data"`
	}{{}}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	var ret []*LightClientUpdate
	for _, el := range v {
		ret = append(ret, el.Data)
	}
	return ret, nil
}

func (c *LightClient) OptimisticUpdate() (*LightClientOptimisticUpdate, error) {
	resp, err := c.Get("/eth/v1/beacon/light_client/optimistic_update")
	if err != nil {
		return nil, err
	}
	v := struct {
		Version string                      `json:"version"`
		Data    LightClientOptimisticUpdate `json:"data"`
	}{}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	return &v.Data, nil
}

func (c *LightClient) FinalityUpdate() (*LightClientFinalityUpdate, error) {
	resp, err := c.Get("/eth/v1/beacon/light_client/finality_update")
	if err != nil {
		return nil, err
	}
	v := struct {
		Version string                    `json:"version"`
		Data    LightClientFinalityUpdate `json:"data"`
	}{}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	return &v.Data, nil
}
