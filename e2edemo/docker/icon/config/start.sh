#!/bin/sh

start_chain() {
  while true; do
    RES=$(goloop system info 2>&1)
    if [ "$?" == "0" ]; then
      break
    fi
    sleep 1
  done
  echo $RES

  CID=acbc4e
  if [ ! -e ${GOLOOP_NODE_DIR}/${CID} ]; then
    # join chain
    GENESIS=/goloop/config/genesis.zip
    goloop chain join \
        --platform icon \
        --channel icon_dex \
        --genesis ${GENESIS} \
        --tx_timeout 10000 \
        --node_cache small \
        --normal_tx_pool 1000 \
        --db_type rocksdb \
        --role 3
  fi
  goloop chain start 0x${CID}
}

# start chain in backgound
start_chain &

if [[ "${GOLOOP_P2P}" == "" ]];then
  P2P_PORT=${P2P_PORT:-8080}
  P2P_HOST=${P2P_HOST:-$(hostname -i)}
  P2P_OPT="--p2p=$(hostname -i):${P2P_PORT}"
fi

# start goloop server
exec goloop server start ${P2P_OPT}
