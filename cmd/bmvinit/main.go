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

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/icon-project/btp2/common/cli"
	"github.com/icon-project/btp2/common/log"
	"github.com/spf13/cobra"

	"github.com/icon-project/btp2-eth2/chain/eth2/client"
	"github.com/icon-project/btp2-eth2/chain/eth2/client/lightclient"
)

var (
	version = "unknown"
	build   = "unknown"
)

type bmvInitData struct {
	Slot                  int64  `json:"slot"`
	GenesisValidatorsHash string `json:"genesis_validators_hash"`
	SyncCommittee         string `json:"sync_committee"`
	FinalizedHeader       string `json:"finalized_header"`
}

func main() {
	rootCmd, rootVc := cli.NewCommand(nil, nil, "btp2 util", "Generate initial data for BMV of Ethereum 2.0")
	cli.SetEnvKeyReplacer(rootVc, strings.NewReplacer(".", "_"))
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("version", version, build)
		},
	})

	genCMD := &cobra.Command{
		Use:   "gen",
		Short: "Generate initial data for BMV of Ethereum 2.0",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			outFilePath, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}
			url, err := cmd.Flags().GetString("url")
			if err != nil {
				return err
			}
			blockId, err := cmd.Flags().GetString("block-id")
			if err != nil {
				return err
			}
			initData, err := getBMVInitialData(url, blockId)
			if err != nil {
				return err
			}

			cmd.Println("Save data to", outFilePath)
			if err = cli.JsonPrettySaveFile(outFilePath, 0644, initData); err != nil {
				return err
			}

			return nil
		},
	}
	rootCmd.AddCommand(genCMD)
	genCMD.Flags().String("url", "http://20.20.5.191:9596", "URL of Beacon node API")
	genCMD.Flags().String("output", "./bmv_init_data.json", "Output file name")
	genCMD.Flags().String("block-id", "finalized", "Block ID")

	decCMD := &cobra.Command{
		Use:   "dec [file]",
		Short: "Decode file",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			bs, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			data := &bmvInitData{}
			err = json.Unmarshal(bs, data)
			if err != nil {
				return err
			}

			scData, err := hex.DecodeString(data.SyncCommittee[2:])
			if err != nil {
				return err
			}
			sc := &lightclient.SyncCommittee{}
			err = sc.UnmarshalSSZ(scData)
			if err != nil {
				return err
			}

			fhData, err := hex.DecodeString(data.FinalizedHeader[2:])
			if err != nil {
				return err
			}
			fh := &lightclient.LightClientHeader{}
			err = fh.UnmarshalSSZ(fhData)
			if err != nil {
				return err
			}

			cmd.Println("{")
			cmd.Printf("sync_committee.aggregated_pubkey: %s\n", sc.AggregatePubkey.String())
			cmd.Printf("finalized_header: %s\n", fh.String())
			cmd.Println("}")

			return nil
		},
	}
	rootCmd.AddCommand(decCMD)

	rootCmd.SilenceUsage = true
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("%+v", err)
		os.Exit(1)
	}
}

func getBMVInitialData(url, blockId string) (*bmvInitData, error) {
	l := log.WithFields(log.Fields{})
	c, err := client.NewConsensusLayer(url, l)
	if err != nil {
		return nil, err
	}

	genesis, err := c.Genesis()
	if err != nil {
		return nil, err
	}

	root, err := c.BeaconBlockRoot(blockId)
	if err != nil {
		return nil, err
	}
	bootStrap, err := c.LightClientBootstrap(*root)
	if err != nil {
		return nil, err
	}

	var syncCommittee []byte
	syncCommittee, err = bootStrap.CurrentSyncCommittee.MarshalSSZTo(syncCommittee)
	if err != nil {
		return nil, err
	}

	var finalizedHeader []byte
	finalizedHeader, err = bootStrap.Header.MarshalSSZTo(finalizedHeader)
	if err != nil {
		return nil, err
	}

	data := &bmvInitData{
		Slot:                  int64(bootStrap.Header.Beacon.Slot),
		GenesisValidatorsHash: genesis.GenesisValidatorsRoot.String(),
		SyncCommittee:         "0x" + hex.EncodeToString(syncCommittee),
		FinalizedHeader:       "0x" + hex.EncodeToString(finalizedHeader),
	}
	fmt.Printf("Get initial data at Slot(%d) from %s\n", bootStrap.Header.Beacon.Slot, url)
	return data, nil
}
