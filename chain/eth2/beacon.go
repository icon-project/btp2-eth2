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

package eth2

import (
	"fmt"

	"github.com/icon-project/btp2-eth2/chain/eth2/client"
)

type BeaconNetwork int

const (
	Mainnet BeaconNetwork = iota + 1
	Sepolia
)

var networkNames = []string{
	"",
	"mainnet",
	"sepolia",
}

func (b BeaconNetwork) String() string {
	return networkNames[b]
}

const (
	mainnetGenesisValidatorsRoot = "0x4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95"
	sepoliaGenesisValidatorsRoot = "0xd8ea171f3c94aea21ebc42a1ed61052acf3f9209c00e4efbaaddac09ed9b8078"
)

func getBeaconNetwork(c *client.ConsensusLayer) (BeaconNetwork, error) {
	genesis, err := c.Genesis()
	if err != nil {
		return 0, err
	}

	switch fmt.Sprintf("%#x", genesis.GenesisValidatorsRoot) {
	case mainnetGenesisValidatorsRoot:
		return Mainnet, nil
	case sepoliaGenesisValidatorsRoot:
		return Sepolia, nil
	default:
		return 0, fmt.Errorf("invalid target network")
	}
}
