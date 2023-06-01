ARG GOLANG_VERSION
ARG ALPINE_VERSION
FROM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION}
RUN apk add make git build-base
RUN if [[ $(uname -m | grep -E '^arm|^aarch' | wc -l) == 1 ]]; then apk add binutils-gold; fi
ENV GO111MODULE on

ARG RELAY_GOMOD_SHA
LABEL RELAY_GOMOD_SHA="$RELAY_GOMOD_SHA"
ADD go.mod go.sum /relay/
WORKDIR /relay

RUN git config --global --add safe.directory /work

RUN echo "go mod download $RELAY_GOMOD_SHA" && go mod download
