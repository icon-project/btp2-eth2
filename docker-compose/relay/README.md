# How to run relay for Ethereum

## 0. Pull latest docker image
```shell
docker pull iconloop/relay-eth2:latest
```

## 1. Prepare Keystore files
Copy keystore files for source and destination network to `./keystore/`.

## 2. Modify configuration file
Modify `chains.src` and `chains.dst` of [./config/relay_config.json](config/relay_config.json).

In sample configuration, `src` is `ICON` and `dst` is `Ethereum`.

For `Ethereum`, two endpoints are required for execution and consensus clients.
* `endpoint`: for execution client
* `options.consensus_endpoint`: for consensus client. Consensus client must be a [lodestar](https://github.com/icon-project/lodestar/wiki) modified by ICON.

### 'chains_config' setting
| Key          | Description                                    |
|:-------------|:-----------------------------------------------|
| address      | BTPAddress ( btp://${Network}/${BMC Address} ) |
| endpoint     | Network endpoint                               |
| key_store    | Relay keystore                                 |
| key_password | Relay keystore password                        |
| type         | BTP2 contract type                             |

## 3. Run docker container with docker-compose
```shell
docker-compose up -d relay
```
