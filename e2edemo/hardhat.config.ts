import { HardhatUserConfig } from "hardhat/config";
import "@nomicfoundation/hardhat-toolbox";

const config: HardhatUserConfig = {
  paths: {
    sources: "./solidity/contracts",
    tests: "./solidity/test",
    cache: "./solidity/build/cache",
    artifacts: "./solidity/build/artifacts"
  },
  solidity: {
    version: "0.8.12",
    settings: {
      optimizer: {
        enabled: true,
        runs: 10,
      },
    },
  },
  networks: {
    hardhat: {
      mining: {
        auto: false,
        interval: 3000
      }
    },
    goerli: {
      url: `https://goerli.infura.io/v3/ffbf8ebe228f4758ae82e175640275e0`,
      accounts: [`0xc8bda7442954c3ff6e7da0eeb929bca53e42e9ce028f69c75c18c39fde4d1c88`]
    },
    sepolia: {
      url: `https://sepolia.infura.io/v3/ffbf8ebe228f4758ae82e175640275e0`,
      accounts: ["0xc8bda7442954c3ff6e7da0eeb929bca53e42e9ce028f69c75c18c39fde4d1c88"]
    },
  },
};

export default config;
