import {Contract} from "./contract";
import {IconNetwork} from "./network";

export class BMC extends Contract {
  constructor(_iconNetwork: IconNetwork, _address: string) {
    super(_iconNetwork, _address)
  }

  getStatus(link: string) {
    return this.call({
      method: 'getStatus',
      params: {
        _link: link
      }
    })
  }

  getBtpAddress() {
    return this.call({
      method: 'getBtpAddress'
    })
  }

  addVerifier(network: string, address: string) {
    return this.invoke({
      method: 'addVerifier',
      params: {
        _net: network,
        _addr: address
      }
    })
  }

  removeVerifier(network: string) {
    return this.invoke({
      method: 'removeVerifier',
      params: {
        _net: network
      }
    })
  }

  addLink(link: string) {
    return this.invoke({
      method: 'addLink',
      params: {
        _link: link
      }
    })
  }

  addBTPLink(link: string, netId: string) {
    return this.invoke({
      method: 'addBTPLink',
      params: {
        _link: link,
        _networkId: netId
      }
    })
  }

  addRelay(link: string, address: string) {
    return this.invoke({
      method: 'addRelay',
      params: {
        _link: link,
        _addr: address
      }
    })
  }

  addService(service: string, address: string) {
    return this.invoke({
      method: 'addService',
      params: {
        _svc: service,
        _addr: address
      }
    })
  }
}

export class BMV extends Contract {
  constructor(_iconNetwork: IconNetwork, _address: string) {
    super(_iconNetwork, _address)
  }
}

export function getBtpAddress(network: string, dapp: string) {
  return `btp://${network}/${dapp}`;
}
