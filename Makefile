#-------------------------------------------------------------------------------
#
# 	Makefile for building target binaries.
#

# Configuration
BUILD_ROOT = $(abspath ./)

GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)

BUILD_PLATFORM=$(GOOS)-$(GOARCH)
BUILD_DIR = $(BUILD_ROOT)/build
CROSSBIN_ROOT=$(BUILD_DIR)/bin
ifeq ($(CROSS_COMPILE),TRUE)
BIN_DIR = $(CROSSBIN_ROOT)-$(BUILD_PLATFORM)
else
BIN_DIR = $(BUILD_ROOT)/bin
endif

GOBUILD = go build
GOBUILD_TAGS =
GOBUILD_ENVS ?= $(GOBUILD_ENVS_$(shell go env GOOS))
GOBUILD_LDFLAGS =
GOBUILD_FLAGS = -tags "$(GOBUILD_TAGS)" -ldflags "$(GOBUILD_LDFLAGS)"

GOTEST = go test
GOTEST_FLAGS = -test.short

# Build flags
GL_VERSION ?= $(shell git describe --always --tags --dirty)
GL_TAG ?= latest
BUILD_INFO = $(GOOS)/$(GOARCH) tags($(GOBUILD_TAGS))-$(shell date '+%Y-%m-%d-%H:%M:%S')

#
# Build scripts for command binaries.
#
CMDS = $(patsubst cmd/%,%,$(wildcard cmd/*))
.PHONY: $(CMDS) $(addsuffix -linux,$(CMDS))
define CMD_template
$(BIN_DIR)/$(1) $(1) : GOBUILD_LDFLAGS+=$$($(1)_LDFLAGS)
$(BIN_DIR)/$(1) $(1) :
	@ \
	rm -f $(BIN_DIR)/$(1) ; \
	echo "[#] go build ./cmd/$(1)"
	$$(GOBUILD_ENVS) \
	go build $$(GOBUILD_FLAGS) \
	    -o $(BIN_DIR)/$(1) ./cmd/$(1)
endef
$(foreach M,$(CMDS),$(eval $(call CMD_template,$(M))))


# Build flags for each command
relay_LDFLAGS = -X 'main.version=$(GL_VERSION)' -X 'main.build=$(BUILD_INFO)'
BUILD_TARGETS += relay

linux : $(addsuffix -linux,$(BUILD_TARGETS))

%-linux:
	@ \
	GOOS=linux GOARCH=amd64 \
	CROSS_COMPILE=TRUE \
	$(MAKE) $(patsubst %-linux,%,$@)

IMAGE_REPO = btp2-eth2

GODEPS_IMAGE = $(IMAGE_REPO)/go-deps:$(GL_TAG)
BUILDDEPS_IMAGE = $(GODEPS_IMAGE)
BUILDDEPS_DOCKER_DIR = $(BUILD_DIR)/builddpes

RELAY_WORK_DIR = /work

RELAY_IMAGE = $(IMAGE_REPO)/relay:$(GL_TAG)
RELAY_DOCKER_DIR = $(BUILD_ROOT)/build/relay

builddeps-% :
	@ \
 	IMAGE_GO_DEPS=$(GODEPS_IMAGE) \
	$(BUILD_ROOT)/docker/build-deps/update.sh \
		$(patsubst builddeps-%,%,$@) \
	    $(IMAGE_REPO)/$(patsubst builddeps-%,%,$@)-deps:$(GL_TAG) \
	    $(BUILD_ROOT) $(BUILDDEPS_DOCKER_DIR)

gorun-% : builddeps-go
	@ \
	docker run -t --rm \
	    -v $(BUILD_ROOT):$(RELAY_WORK_DIR) \
	    -w $(RELAY_WORK_DIR) \
	    -e "CROSS_COMPILE=TRUE" \
	    -e "GOBUILD_TAGS=$(GOBUILD_TAGS)" \
	    -e "GL_VERSION=$(GL_VERSION)" \
	    $(BUILDDEPS_IMAGE) \
	    make $(patsubst gorun-%,%,$@)

relay-image : gorun-relay
	@ echo "[#] Building image $(RELAY_IMAGE) for $(GL_VERSION)"
	@ rm -rf $(RELAY_DOCKER_DIR)
	@ \
        BIN_DIR=$(CROSSBIN_ROOT)-$$(docker inspect $(BUILDDEPS_IMAGE) --format "{{.Os}}-{{.Architecture}}") \
	RELAY_VERSION=$(GL_VERSION) \
        GOBUILD_TAGS="$(GOBUILD_TAGS)" \
	$(BUILD_ROOT)/docker/relay/build.sh $(RELAY_IMAGE) $(BUILD_ROOT) $(RELAY_DOCKER_DIR)

.PHONY: test

test :
	$(GOBUILD_ENVS) $(GOTEST) $(GOBUILD_FLAGS) ./... $(GOTEST_FLAGS)

.DEFAULT_GOAL := all
all : $(BUILD_TARGETS)
