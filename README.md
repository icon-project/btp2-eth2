# Relay System for BTP 2.0
[![Docker Image Version (latest by date)](https://img.shields.io/docker/v/iconloop/relay-eth2?color=blue&label=Docker&sort=semver)](https://hub.docker.com/r/iconloop/relay-eth2)

## Introduction

This is a `relay` implementation for BTP 2.0 protocol.

### Target chains
* ICON (BTP Block)
* Ethereum 2.0 (Beacon chain)

See [ICON BTP Standard](https://github.com/icon-project/IIPs/blob/master/IIPS/iip-25.md) and [Relay System for BTP 2.0](https://github.com/icon-project/btp2) for more details.

## Getting Started
```shell
git clone https://github.com/icon-project/btp2-eth2.git --recurse-submodules
cd btp2-eth2
# compile binary
make relay
# build docker image
make relay-image
```

## How to run `relay`
### 1. Setup beacon node with modified [loadstar](https://github.com/icon-project/lodestar/wiki#run-a-beacon-node-with-docker).
### 2. Run `relay` with docker. See [docker-compose/relay](docker-compose/relay/README.md) folder.

## E2E Testing Demo
* Follow the instruction in [End-to-End Testing Demo](e2edemo) folder.

## References
* [ICON BTP Standard](https://github.com/icon-project/IIPs/blob/master/IIPS/iip-25.md)
* [Relay System for BTP 2.0](https://github.com/icon-project/btp2)
* Ethereum consensus client [lodestar](https://github.com/icon-project/lodestar)

## License
This project is available under the [Apache License, Version 2.0](LICENSE).