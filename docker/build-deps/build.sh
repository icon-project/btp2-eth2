#!/bin/sh
set -e

PRE_PWD=$(pwd)
WORKDIR=$(dirname "$(readlink -f ${0})")
cd $WORKDIR

export IMAGE_GO_DEPS=${IMAGE_GO_DEPS:-btp2-eth2/go-deps:latest}

if [ $# -lt 1 ] ; then
    echo "Usage: $0 <target>"
    echo "\t <target>:  go"
    return 1
fi
TARGET=${1}
case $TARGET in
go)
    IMAGE_DEPS=${IMAGE_GO_DEPS}
;;
*)
;;
esac
if [ -z "${IMAGE_DEPS}" ]; then
  IMAGE_DEPS=btp2-eth2/${TARGET}-deps:latest
fi

./update.sh ${TARGET} "${IMAGE_DEPS}" ../..

cd $PRE_PWD
