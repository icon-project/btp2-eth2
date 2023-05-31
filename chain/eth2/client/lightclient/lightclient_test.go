package lightclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/icon-project/btp2/common/log"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestSyncCommittee(t *testing.T) {
	jsonstr := `{
		"pubkeys": [
			"0x93247f2209abcacf57b75a51dafae777f9dd38bc7053d1af526f220a7489a6d3a2753e5f3e8b1cfe39b56f43611df74a",
			"0x93247f2209abcacf57b75a51dafae777f9dd38bc7053d1af526f220a7489a6d3a2753e5f3e8b1cfe39b56f43611df74a"
		],
		"aggregate_pubkey": "0x93247f2209abcacf57b75a51dafae777f9dd38bc7053d1af526f220a7489a6d3a2753e5f3e8b1cfe39b56f43611df74a"
	}`

	var s SyncCommittee
	err := json.Unmarshal([]byte(jsonstr), &s)
	assert.NoError(t, err)
	expected := "{Pubkeys:[0x93247f2209abcacf57b75a51dafae777f9dd38bc7053d1af526f220a7489a6d3a2753e5f3e8b1cfe39b56f43611df74a,0x93247f2209abcacf57b75a51dafae777f9dd38bc7053d1af526f220a7489a6d3a2753e5f3e8b1cfe39b56f43611df74a],AggregatePubkey:0x93247f2209abcacf57b75a51dafae777f9dd38bc7053d1af526f220a7489a6d3a2753e5f3e8b1cfe39b56f43611df74a}"
	assert.Equal(t, expected, s.String())

	b, err := json.Marshal(s)
	assert.NoError(t, err)
	assert.JSONEqf(t, jsonstr, string(b), "json compare")

	b, err = s.MarshalSSZ()
	assert.NoError(t, err)
	fmt.Println("MarshalSSZ:", hex.EncodeToString(b))

	err = s.UnmarshalSSZ(b)
	assert.NoError(t, err)
	assert.Equal(t, expected, s.String())
}

func TestSyncAggregate(t *testing.T) {
	jsonstr := `{
        "sync_committee_bits": "0xffffffff",
        "sync_committee_signature": "0xb080b6a4d25d15cb7afc0a670bcea1e445445fdc04d66fefd9e1a33774049de8fe97e471535805fedf056a43936887540028ed5c3d526730c00472bc62b90e9a6c371849f464e0604b44fd80f423b2d2e52f180a22181b4551db127a1b1f6bee"
    }`
	var s SyncAggregate
	err := json.Unmarshal([]byte(jsonstr), &s)
	assert.NoError(t, err)
	expected := "{SyncCommitteeBits:0xffffffff,SyncCommitteeSignature:0xb080b6a4d25d15cb7afc0a670bcea1e445445fdc04d66fefd9e1a33774049de8fe97e471535805fedf056a43936887540028ed5c3d526730c00472bc62b90e9a6c371849f464e0604b44fd80f423b2d2e52f180a22181b4551db127a1b1f6bee}"
	assert.Equal(t, expected, s.String())

	b, err := json.Marshal(s)
	assert.NoError(t, err)
	assert.JSONEqf(t, jsonstr, string(b), "json compare")
}

const (
	endpoint = "http://load2:3000/lodestar0"
)

var (
	l = log.GlobalLogger()
)

func getGoEth2Client(t *testing.T) eth2client.Service {
	s, err := http.New(context.Background(), http.WithAddress(endpoint), http.WithLogLevel(zerolog.DebugLevel))
	if err != nil {
		assert.FailNow(t, "fail to create eth2Client err:%s", err.Error())
	}
	return s
}

func TestEvents(t *testing.T) {
	c := NewLightClient(endpoint, l)
	err := c.Events(func(update *LightClientOptimisticUpdate) {

	}, func(update *LightClientFinalityUpdate) {

	})
	assert.NoError(t, err)
}

func TestBootstrap(t *testing.T) {
	brp := getGoEth2Client(t).(eth2client.BeaconBlockRootProvider)
	r, err := brp.BeaconBlockRoot(context.Background(), "finalized")
	if err != nil {
		assert.FailNow(t, "fail to BeaconBlockRoot err:%s", err.Error())
	}
	c := NewLightClient(endpoint, l)
	resp, err := c.Bootstrap(*r)
	assert.NoError(t, err)
	fmt.Println(resp)
}

func TestUpdates(t *testing.T) {
	c := NewLightClient(endpoint, l)
	resp, err := c.Updates(0, 1)
	assert.NoError(t, err)
	for i, v := range resp {
		fmt.Println(i, v)
	}
}

func TestOptimisticUpdate(t *testing.T) {
	c := NewLightClient(endpoint, l)
	resp, err := c.OptimisticUpdate()
	assert.NoError(t, err)
	fmt.Println(resp)
}

func TestFinalityUpdate(t *testing.T) {
	c := NewLightClient(endpoint, l)
	resp, err := c.FinalityUpdate()
	assert.NoError(t, err)
	fmt.Println(resp)
}

func TestBlock(t *testing.T) {
	jsonstr := `{
  "data": {
    "message": {
      "slot": "35624",
      "proposer_index": "4",
      "parent_root": "0x0caf2e9f9958a106fc981a8ca2e444e73d541466bedc49014ffb014d7d7c683b",
      "state_root": "0x7b39cad79204a247679ee66e8752867eb0f665b7aa2564d8725eeeb05dea3fb8",
      "body": {
        "randao_reveal": "0x97e47ae3bacdf720fbf5a0deb4ce5b5d8dab8bd2274185f97ff892bd9a27fe5b11f4dbc807a13df19de8487742c0ad5f0a6a637892164111bf60a4d93e1f930fdedac4086dd5cc8faa9fe61401682f775769bb19c31c5c3578b1a70db242cec2",
        "eth1_data": {
          "deposit_root": "0x6141b76179b67d7849f34a22d0e529729fb274bbe81374c41623373b649cc63b",
          "deposit_count": "64",
          "block_hash": "0x5f0e5bc1e41e561e89ae4ca5996bd0291610f82637ebd2481fc52b895784fa6b"
        },
        "graffiti": "0x4c6f6465737461722d76312e372e320000000000000000000000000000000000",
        "proposer_slashings": [],
        "attester_slashings": [],
        "attestations": [
          {
            "aggregation_bits": "0x1f",
            "data": {
              "slot": "35623",
              "index": "0",
              "beacon_block_root": "0x0caf2e9f9958a106fc981a8ca2e444e73d541466bedc49014ffb014d7d7c683b",
              "source": {
                "epoch": "4451",
                "root": "0x1b5f892c70a7bc4c9f8b727b60e35d9ee24b7b37bc8514db1c6426ec2774b76e"
              },
              "target": {
                "epoch": "4452",
                "root": "0x4a890d8060001158927035550e4fc4492600b6239940cc1f32bda0f87ee2a54b"
              }
            },
            "signature": "0xb70a711d3b694897662050f4a9c2e462b0957d6773b8ae133f02409c186646105d9fb9b8ee9f921642cf7db0cb14437e0a85bf2bd2c8659cef9ba176b5a86d7f5158250c8df7726cdfe43848bdddee2a651dd83e94033e629878a0c6d0681bd9"
          },
          {
            "aggregation_bits": "0x1f",
            "data": {
              "slot": "35623",
              "index": "1",
              "beacon_block_root": "0x0caf2e9f9958a106fc981a8ca2e444e73d541466bedc49014ffb014d7d7c683b",
              "source": {
                "epoch": "4451",
                "root": "0x1b5f892c70a7bc4c9f8b727b60e35d9ee24b7b37bc8514db1c6426ec2774b76e"
              },
              "target": {
                "epoch": "4452",
                "root": "0x4a890d8060001158927035550e4fc4492600b6239940cc1f32bda0f87ee2a54b"
              }
            },
            "signature": "0xa9885389e695d31fdb941104b533fb609ee0ddb6a5c9697bfdc66093b679fb92d9c110a83e3726e8b4d39fdbdc5446c6178ac070c7d57592b2f73af309a4fdae9ca8af1abd27f0ac56d4342e47a2545ec84fb44b6005df8c73fe79789d3f1080"
          }
        ],
        "deposits": [],
        "voluntary_exits": [],
        "sync_aggregate": {
          "sync_committee_bits": "0xffffffff",
          "sync_committee_signature": "0x8b63d83e8cd87f436e84eb9333ab884c94721986127a92f7385a1897ffd6ad4c4104009320512049cd0dffa0eda47aad16cac35c2c374afd3fe017a64e12bc9b132fa1524be1f55b6d5e56e7662ab6db5bab9f8a1bf93ee07c03736b2e6e2820"
        },
        "execution_payload": {
          "parent_hash": "0x840fdb7b6beb3542278a6bfccf6ad19b40ea9279469d290fac520bcaa922f123",
          "fee_recipient": "0x123463a4b065722e99115d6c222f267d9cabb524",
          "state_root": "0xcf967678eab3c69b18c00df9ed994d666e2330d9ca9becd93bc255fd25fa53fe",
          "receipts_root": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
          "logs_bloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
          "prev_randao": "0xf39b1ea2474313c0dac288394738e2701db4f41b48401e6ff5b13e36a3f7e484",
          "block_number": "35568",
          "gas_limit": "30000000",
          "gas_used": "0",
          "timestamp": "1685098587",
          "extra_data": "0xd883010b07846765746888676f312e32302e33856c696e7578",
          "base_fee_per_gas": "7",
          "block_hash": "0x9c234ff501d3fa69a4c515e854253ea09b5fa72b55d8f6055e529f576963a685",
          "transactions": []
        }
      }
    },
    "signature": "0x9652e32b512dcb256cbd2030d951a6fa518f896d3d4f8d1271026c2e8d02e2976321ecbf4f107f22013f15140af0141803391d1ab02909583c916e33efe691032766f0340296a76a583d36713b2544fdfd9b2b4785d0e87f879db326728d83ca"
  },
  "version": "bellatrix",
  "execution_optimistic": false
}`
	blk := &bellatrixSignedBeaconBlockJSON{}
	err := json.Unmarshal([]byte(jsonstr), blk)
	assert.NoError(t, err)
	fmt.Printf("%+v", blk)
}

type bellatrixSignedBeaconBlockJSON struct {
	Data *bellatrix.SignedBeaconBlock `json:"data"`
}
