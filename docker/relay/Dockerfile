ARG ALPINE_VERSION
FROM alpine:${ALPINE_VERSION}
LABEL MAINTAINER="t_arch@iconloop.com"

ARG RELAY_VERSION
LABEL RELAY_VERSION="$RELAY_VERSION"

# install
COPY dist/bin/relay /relay/bin/relay
ENV PATH $PATH:/relay/bin

WORKDIR /relay

# container configuration
VOLUME ["/relay/data"]

CMD relay start
