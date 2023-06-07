#!/bin/sh
GENESIS=${GENESIS:-/execution/genesis.json}
GENESIS_JSON=$(cat ${GENESIS})
GENESIS_OUT=${GENESIS_OUT:-${GENESIS}}
echo ${GENESIS_JSON} | jq ".+{\"timestamp\":\"0x$(printf %x $(date +%s))\"}" > ${GENESIS_OUT}

DATA_DIR=${DATA_DIR:-/execution/data}
geth --datadir=${DATA_DIR} init ${GENESIS_OUT}

GETH_PASSWORD=${GETH_PASSWORD:-/execution/geth_password.txt}
geth --datadir=${DATA_DIR} account import --password=${GETH_PASSWORD} \
  ${FEE_RECIPIENT_PK:-/execution/feereceipient.pk.txt}

CHAIN_ID=$(jq -r .config.chainId ${GENESIS_OUT})
GENESIS_BLOCK=${GENESIS_BLOCK:-/execution/genesis_block.json}
GENESIS_BLOCK_JSON=$(geth --networkid=${CHAIN_ID} --datadir=${DATA_DIR} --exec 'JSON.stringify(eth.getBlockByNumber(0))' console)
echo ${GENESIS_BLOCK_JSON}
echo ${GENESIS_BLOCK_JSON} | jq fromjson > ${GENESIS_BLOCK}

geth --networkid=${CHAIN_ID} --datadir=${DATA_DIR} --password=${GETH_PASSWORD}  $@


