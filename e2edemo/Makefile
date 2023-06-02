PWD = $(abspath ./)

JAVASCORE_PATH = $(PWD)/../javascore
GRADLE = ./gradlew

HARDHAT = npx hardhat
TS_NODE = npx ts-node
NETWORK ?= sepolia
CHAIN_CONFIG ?= ./chain_config.json
BMVINIT_WITH_SPEC ?= false
CONSENSUS_ENDPOINT=$(shell jq -r .chains.$(NETWORK).options $(CHAIN_CONFIG) | cut -d '=' -f2)
SOLIDITY_CONTRACTS = ./solidity/contracts

.DEFAULT_GOAL := all
all:
	@echo $(PWD)

.PHONY: build clean deploy

clean-java:
	(cd $(JAVASCORE_PATH); $(GRADLE) clean)

clean-solidity:
	$(HARDHAT) clean
	$(RM) -rf \
	    $(SOLIDITY_CONTRACTS)/bmc \
	    $(SOLIDITY_CONTRACTS)/bmv* \
	    $(SOLIDITY_CONTRACTS)/xcall

clean: clean-java clean-solidity

copy-src:
	@ echo ">>> Copy BMC/BMV/xCall solidity codes" ; \
	$(TS_NODE) scripts/setup/copy_src.ts

compile: copy-src
	@ echo ">>> Compile solidity codes" ; \
	$(HARDHAT) compile

build-bmc-java:
	@ echo ">>> Build BMC javascore" ; \
	cd $(JAVASCORE_PATH) ; \
	$(GRADLE) bmc:optimizedJar

build-bmv-java:
	@ echo ">>> Build BMV javascore" ; \
	cd $(JAVASCORE_PATH) ; \
	$(GRADLE) bmv:bridge:optimizedJar \
		bmv:eth2:optimizedJar

build-xcall-java:
	@ echo ">>> Build xCall javascore" ; \
	cd $(JAVASCORE_PATH) ; \
	$(GRADLE) xcall:optimizedJar

build-dapp-java:
	@ echo ">>> Build DApp sample javascore" ; \
	cd $(JAVASCORE_PATH) ; \
	$(GRADLE) dapp-sample:optimizedJar

build-all: compile build-bmc-java build-bmv-java build-xcall-java build-dapp-java

deploy-bmc:
	@ echo ">>> Deploy BMC contracts" ; \
	JAVASCORE_PATH=$(JAVASCORE_PATH) \
	$(HARDHAT) --network $(NETWORK) run scripts/setup/deploy_bmc.ts

deploy-bmv: bmv-init
	@ echo ">>> Deploy BMV contracts" ; \
	JAVASCORE_PATH=$(JAVASCORE_PATH) \
	$(HARDHAT) --network $(NETWORK) run scripts/setup/deploy_bmv.ts

deploy-xcall:
	@ echo ">>> Deploy xCall contracts" ; \
	JAVASCORE_PATH=$(JAVASCORE_PATH) \
	$(HARDHAT) --network $(NETWORK) run scripts/setup/deploy_xcall.ts

deploy-dapp:
	@ echo ">>> Deploy DApp contracts" ; \
	JAVASCORE_PATH=$(JAVASCORE_PATH) \
	$(HARDHAT) --network $(NETWORK) run scripts/setup/deploy_dapp.ts

deploy-all: deploy-bmc deploy-bmv deploy-xcall deploy-dapp

update-bmv: bmv-init
	@ echo ">>> Update ICON java BMV contract" ; \
	E2E_DEMO_PATH=$(PWD) \
	JAVASCORE_PATH=$(JAVASCORE_PATH) \
	$(HARDHAT) --network $(NETWORK) run scripts/setup/update_bmv.ts

bmv-init:
	@ echo ">>> Generate initial data of Java BMV for Ethereum 2.0, network=$(NETWORK), endpoint=$(CONSENSUS_ENDPOINT), with-spec=$(BMVINIT_WITH_SPEC)" ; \
	cd ..; \
	make bmvinit; \
	./bin/bmvinit gen --url $(CONSENSUS_ENDPOINT) --output $(PWD)/java_bmv_init_data.json --with-spec $(BMVINIT_WITH_SPEC)

start-nodes:
	@ echo ">>> Start nodes" ; \
	cd docker; \
	docker compose up -d icon-node

stop-nodes:
	@ echo ">>> Stop nodes" ; \
	cd docker; \
	docker compose down

setup-node:
	@ echo ">>> Setup ICON node" ; \
	$(TS_NODE) scripts/setup/setup_node.ts

run-demo:
	@ echo ">>> Run demo_dapp.ts " ; \
	$(HARDHAT) --network $(NETWORK) run scripts/demo_dapp.ts
