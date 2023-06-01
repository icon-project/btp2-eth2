#!/bin/sh

BASE_DIR=$(dirname $0)
. ${BASE_DIR}/../version.sh

build_image() {
    if [ $# -lt 1 ] ; then
        echo "Usage: $0 <image_name> [<src_dir>] [<build_dir>]"
        return 1
    fi

    local TAG=$1
    local SRC_DIR=$2
    if [ -z "${SRC_DIR}" ] ; then
        SRC_DIR="."
    fi
    local BUILD_DIR=$3

    # Prepare build directory if it's set
    if [ "${BUILD_DIR}" != "" ] ; then
        rm -rf ${BUILD_DIR}
        mkdir -p ${BUILD_DIR}
        cp ${BASE_DIR}/* ${BUILD_DIR}
    else
        BUILD_DIR=${BASE_DIR}
    fi

    BIN_DIR=${BIN_DIR:-${SRC_DIR}/bin}
    if [ "${GOBUILD_TAGS}" != "" ] ; then
        RELAY_VERSION="${RELAY_VERSION}-tags(${GOBUILD_TAGS})"
    fi

    # copy required files to ${BUILD_DIR}/dist
    rm -rf ${BUILD_DIR}/dist
    mkdir -p ${BUILD_DIR}/dist/bin/
    cp ${BIN_DIR}/relay ${BUILD_DIR}/dist/bin/
    cp -f ${BIN_DIR}/gstool ${BUILD_DIR}/dist/bin/

    CDIR=$(pwd)
    cd ${BUILD_DIR}

    echo "Building image ${TAG}"
    docker build \
        --build-arg ALPINE_VERSION="${ALPINE_VERSION}" \
        --build-arg RELAY_VERSION="${RELAY_VERSION}" \
        --tag ${TAG} .
    local result=$?

    cd ${CDIR}
    rm -rf ${BUILD_DIR}/dist
    return $result
}

build_image "$@"
