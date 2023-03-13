#!/bin/bash
set -x

RELAY_BIN=../bin/relay
CONFIG=config/config.json
DEPLOYMENTS=deployments.json

if [ "x$1" = x ]; then
    echo "Usage: $0 <target_chain>"
    exit 1
else
    TARGET=$1
fi

if [ ! -f ${RELAY_BIN} ]; then
    (cd ..; make relay)
fi

ICON_CONFIG=$(cat ${CONFIG} | jq -r .icon)
ICON_NETWORK=$(cat ${DEPLOYMENTS} | jq -r .icon.network)
ICON_BMC_ADDRESS=$(cat ${DEPLOYMENTS} | jq -r .icon.contracts.bmc)
ICON_ENDPOINT=$(cat ${ICON_CONFIG} | jq -r .endpoint)
ICON_KEYSTORE=$(cat ${ICON_CONFIG} | jq -r .keystore)
ICON_KEYPASS=$(cat ${ICON_CONFIG} | jq -r .keypass)

TARGET_CONFIG=$(cat ${CONFIG} | jq -r .target)
TARGET_NETWORK=$(cat ${DEPLOYMENTS} | jq -r .target.network)
TARGET_ENDPOINT=$(cat ${TARGET_CONFIG} | jq -r .endpoint)
TARGET_KEYSTORE=$(cat ${TARGET_CONFIG} | jq -r .keystore)
TARGET_KEYPASS=$(cat ${TARGET_CONFIG} | jq -r .keypass)
TARGET_OPTIONS=$(cat ${TARGET_CONFIG} | jq -r '.options // empty')

echo "target options = ${TARGET_OPTIONS}"

case ${TARGET} in
  icon)
    TARGET_BMC_ADDRESS=$(cat ${DEPLOYMENTS} | jq -r .target.contracts.bmc)
  ;;
  hardhat)
    TARGET_BMC_ADDRESS=$(cat ${DEPLOYMENTS} | jq -r .target.contracts.bmcp)
  ;;
  eth2)
    TARGET_BMC_ADDRESS=$(cat ${DEPLOYMENTS} | jq -r .target.contracts.bmcp)
  ;;
  *)
    echo "Error: unknown target: $TARGET"
    exit 1
esac

SRC_ADDRESS=btp://${ICON_NETWORK}/${ICON_BMC_ADDRESS}
SRC_ENDPOINT=${ICON_ENDPOINT}
SRC_KEY_STORE=${ICON_KEYSTORE}
SRC_KEY_PASSWORD=${ICON_KEYPASS}

DST_ADDRESS=btp://${TARGET_NETWORK}/${TARGET_BMC_ADDRESS}
DST_ENDPOINT=${TARGET_ENDPOINT}
DST_KEY_STORE=${TARGET_KEYSTORE}
DST_KEY_PASSWORD=${TARGET_KEYPASS}

if [ "x$BMV_BRIDGE" = xtrue ]; then
  echo "Using Bridge mode"
else
  echo "Using BTPBlock mode"
  BMV_BRIDGE=false
fi

${RELAY_BIN} \
    --direction both \
    --src.address ${SRC_ADDRESS} \
    --src.endpoint ${SRC_ENDPOINT} \
    --src.key_store ${SRC_KEY_STORE} \
    --src.key_password ${SRC_KEY_PASSWORD} \
    --src.bridge_mode=${BMV_BRIDGE} \
    --dst.address ${DST_ADDRESS} \
    --dst.endpoint ${DST_ENDPOINT} \
    --dst.key_store ${DST_KEY_STORE} \
    --dst.key_password ${DST_KEY_PASSWORD} \
    ${TARGET_OPTIONS:+--dst.options $TARGET_OPTIONS} \
    start
