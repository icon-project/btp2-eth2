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

package client

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/icon-project/btp2/common/log"
)

type BMCClient struct {
	bmc   *BMC
	txBmc *BMC

	log log.Logger
}

func NewBMCClient(address common.Address, backend bind.ContractBackend, txUrl string, l log.Logger) (*BMCClient, error) {
	var bmc, txBmc *BMC
	var err error
	bmc, err = NewBMC(address, backend)
	if err != nil {
		return nil, err
	}
	if len(txUrl) > 0 {
		l.Debugf("make TX BMC with %s", txUrl)
		rpcClient, err := rpc.Dial(txUrl)
		if err != nil {
			return nil, err
		}
		txBmc, err = NewBMC(address, ethclient.NewClient(rpcClient))
		if err != nil {
			return nil, err
		}
	}
	return &BMCClient{
		bmc:   bmc,
		txBmc: txBmc,
		log:   l,
	}, nil
}

func (c *BMCClient) HandleRelayMessage(opts *bind.TransactOpts, _prev string, _msg []byte) (*etypes.Transaction, error) {
	if c.txBmc != nil {
		return c.txBmc.HandleRelayMessage(opts, _prev, _msg)
	} else {
		return c.bmc.HandleRelayMessage(opts, _prev, _msg)
	}
}
func (c *BMCClient) GetStatus(opts *bind.CallOpts, _link string) (TypesLinkStatus, error) {
	return c.bmc.GetStatus(opts, _link)
}
