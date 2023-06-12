import {BMC, getBtpAddress} from "../icon/btp";
import {IconNetwork} from "../icon/network";
import {chainType, Deployments} from "./config";
import {Contract, Jar} from "../icon";
import fs from "fs";

const {JAVASCORE_PATH, PWD, SWITCH_BMV, BMV_VERSION} = process.env
const ETH2_BMV_INIT_PATH= `${PWD}/java_bmv_init_data.json`
const switchMode= SWITCH_BMV == "true";
const deployments = Deployments.getDefault();

async function manage_btp_verifier(src: string, srcChain: any, dstChain: any, add: boolean) {
  const srcNetwork = IconNetwork.getNetwork(src);
  const bmc = new BMC(srcNetwork, srcChain.contracts.bmc);

  if (add) {
    console.log(`${src}: addVerifier ${srcChain.contracts.bmv} for ${dstChain.network}`)
    await bmc.addVerifier(dstChain.network, srcChain.contracts.bmv)
      .then((txHash) => bmc.getTxResult(txHash))
      .then((result) => {
        if (result.status != 1) {
          throw new Error(`${src}: failed to register BMV to BMC: ${result.txHash}`);
        }
      })
  } else {
    console.log(`${src}: removeVerifier ${srcChain.contracts.bmv} for ${dstChain.network}`)
    await bmc.removeVerifier(dstChain.network)
      .then((txHash) => bmc.getTxResult(txHash))
      .then((result) => {
        if (result.status != 1) {
          throw new Error(`${src}: failed to remove BMV from BMC: ${result.txHash}`);
        }
      })
  }
}

async function deploy_bmv_eth2_java_with_seq(srcNetwork: IconNetwork, srcChain: any, dstChain: any) {
  const bmvInitData = JSON.parse(fs.readFileSync(ETH2_BMV_INIT_PATH).toString());
  const content = Jar.readFromFile(JAVASCORE_PATH, "bmv/eth2", BMV_VERSION);
  const bmc = new BMC(srcNetwork, srcChain.contracts.bmc);
  const bmv = new Contract(srcNetwork)

  const bmcStatus = await bmc.getStatus(getBtpAddress(dstChain.network, dstChain.bmc))

  if (bmvInitData.slot != bmcStatus.verifier.height) {
    throw new Error(`bmvInitData and bmcStatus do not match`)
  }

  console.log(`deploy java BMV ${BMV_VERSION} with slot:${bmvInitData.slot} seq:${bmcStatus.rx_seq}`)

  const deployTxHash = await bmv.deploy({
    content: content,
    params: {
      srcNetworkID: dstChain.network,
      genesisValidatorsHash: bmvInitData.genesis_validators_hash,
      syncCommittee: bmvInitData.sync_committee,
      bmc: srcChain.contracts.bmc,
      ethBmc: dstChain.contracts.bmc,
      finalizedHeader: bmvInitData.finalized_header,
      seq: bmcStatus.rx_seq,
    }
  })
  const result = await bmv.getTxResult(deployTxHash);
  if (result.status != 1) {
    throw new Error(`BMV deployment failed: ${result.txHash}`);
  }
  srcChain.contracts.bmv = bmv.address;
  console.log(`${srcChain.network}: BMV-eth2: deployed to ${bmv.address}`);
}

async function update_bmv_eth2_java(srcNetwork: IconNetwork, srcChain: any, dstChain: any, to: string) {
  const content = Jar.readFromFile(JAVASCORE_PATH, "bmv/eth2", BMV_VERSION);
  const bmv = new Contract(srcNetwork)
  console.log(`${srcChain.network}: BMV-eth2: updated to ${to}`);
  const deployTxHash = await bmv.deploy({
    content: content,
    to: to,
    params: {}
  })
  const result = await bmv.getTxResult(deployTxHash);
  if (result.status != 1) {
    throw new Error(`BMV deployment failed: ${result.txHash}`);
  }
  srcChain.contracts.bmv = bmv.address;
}

async function main() {
  const src = deployments.getSrc();
  const dst = deployments.getDst();
  const srcChain = deployments.get(src);
  const dstChain = deployments.get(dst);
  const srcNetwork = IconNetwork.getNetwork(src);

  if (chainType(srcChain) != 'icon') {
    throw new Error(`Source chain must be an icon`)
  }
  if (switchMode) {
    await deploy_bmv_eth2_java_with_seq(srcNetwork, srcChain, dstChain);
    await manage_btp_verifier(src, srcChain, dstChain, false);
  } else {
    await manage_btp_verifier(src, srcChain, dstChain, false);
    await update_bmv_eth2_java(srcNetwork, srcChain, dstChain, srcChain.contracts.bmv);
  }
  await manage_btp_verifier(src, srcChain, dstChain, true);
  if (switchMode) {
    deployments.set(src, srcChain);
    deployments.save();
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});