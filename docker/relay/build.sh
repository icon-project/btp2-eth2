#!/bin/sh
set -e

PRE_PWD=$(pwd)
WORKDIR=$(dirname "$(readlink -f ${0})")
cd $WORKDIR

export IMAGE_REPO=${IMAGE_REPO:-btp2-eth2}

export RELAY_VERSION=${RELAY_VERSION:-$(git describe --always --tags --dirty)}
IMAGE_RELAY=${IMAGE_RELAY:-${IMAGE_REPO}/relay:latest}

./update.sh "${IMAGE_RELAY}" ../..

cd $PRE_PWD
