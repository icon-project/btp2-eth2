#!/bin/sh
function file_exists() {
  if [[ "$#" -ne 1 ]]; then
    echo "Usage: $0 TARGET_FILE [RETRY RETRY_INTERVAL_SEC]"
    exit 1
  fi
  TARGET_FILE=$1
  RETRY_LIMIT=${2:-10}
  RETRY_INTERVAL_SEC=${3:-1}
  RETRY=0
  while [[ ! -f "${TARGET_FILE}" ]]; do
    RETRY=$((RETRY+1))
    if [[ "${RETRY}" -ge ${RETRY_LIMIT} ]];then
      echo "not found $TARGET_FILE"
      exit 1
    fi
    echo "retry=$RETRY/$RETRY_LIMIT, sleep $RETRY_INTERVAL_SEC for $TARGET_FILE"
    sleep $RETRY_INTERVAL_SEC
  done
  echo "found $TARGET_FILE"
}

GENESIS_BLOCK=${GENESIS_BLOCK:-/execution/genesis_block.json}
file_exists ${GENESIS_BLOCK}
GENESIS_HASH=$(jq -r .hash ${GENESIS_BLOCK})
GENESIS_TIMESTAMP=$(jq -r .timestamp ${GENESIS_BLOCK})
MIN_GENESIS_TIME=$((16#${GENESIS_TIMESTAMP#0x}))
GENESIS_TIME=$((${MIN_GENESIS_TIME}+${GENESIS_DELAY:-30}))

BOOTNODES_FILE=${BOOTNODES_FILE}
if [[ "${BOOTNODES_FILE}" != "" ]];then
  file_exists ${BOOTNODES_FILE}
  BOOTNODES_FILE_OPT="--bootnodesFile=${BOOTNODES_FILE}"
fi

CMD="node ./packages/cli/bin/lodestar $@ \
  --enr.ip=$(hostname -i) \
  --genesisTime=${GENESIS_TIME} \
  --genesisEth1Hash=${GENESIS_HASH} \
  --terminal-total-difficulty-override=0 \
  --terminal-block-hash-epoch-override=0 \
  --terminal-block-hash-override=${GENESIS_HASH} \
  --params.ALTAIR_FORK_EPOCH=0 \
  --params.BELLATRIX_FORK_EPOCH=0 \
  --params.SECONDS_PER_SLOT=${PARAMS_SECONDS_PER_SLOT:-3} \
  ${BOOTNODES_FILE_OPT}"
echo $CMD
$CMD
