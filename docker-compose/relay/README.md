# How to run relay for Etheruem

## 1. Prepare Keystore files
Copy keystore files for source and destination network to `./keystore/`.

## 2. Modify configuration file
Modify `src` and `dst` of `./config/relay_config.json`

In sample configuration, `src` is `icon` and `dst` is `Ethereum`.

## 3. Run docker container with docker-compose
```shell
docker-compose up -d relay
```
