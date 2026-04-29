export interface NetworkConfig {
  id: 'testnet' | 'mainnet';
  name: string;
  rpcUrl: string;
  horizonUrl: string;
  networkPassphrase: string;
  explorerUrl: string;
  friendbotUrl?: string;
  contracts: {
    vault: string;
    vaultToken: string;
    yieldRegistry: string;
    allocationStrategy: string;
    treasury: string;
    accessControl: string;
  };
}

export const NETWORKS: Record<string, NetworkConfig> = {
  testnet: {
    id: 'testnet',
    name: 'Testnet',
    rpcUrl: 'https://soroban-testnet.stellar.org',
    horizonUrl: 'https://horizon-testnet.stellar.org',
    networkPassphrase: 'Test SDF Network ; September 2015',
    explorerUrl: 'https://testnet.stellarchain.io',
    friendbotUrl: 'https://friendbot.stellar.org',
    contracts: {
      vault: process.env.NEXT_PUBLIC_TESTNET_VAULT_CONTRACT || 'C_TESTNET_VAULT_PLACEHOLDER',
      vaultToken: process.env.NEXT_PUBLIC_TESTNET_VAULT_TOKEN_CONTRACT || 'C_TESTNET_VAULT_TOKEN_PLACEHOLDER',
      yieldRegistry: process.env.NEXT_PUBLIC_TESTNET_YIELD_REGISTRY_CONTRACT || 'C_TESTNET_YIELD_REGISTRY_PLACEHOLDER',
      allocationStrategy: process.env.NEXT_PUBLIC_TESTNET_ALLOCATION_STRATEGY_CONTRACT || 'C_TESTNET_ALLOCATION_STRATEGY_PLACEHOLDER',
      treasury: process.env.NEXT_PUBLIC_TESTNET_TREASURY_CONTRACT || 'C_TESTNET_TREASURY_PLACEHOLDER',
      accessControl: process.env.NEXT_PUBLIC_TESTNET_ACCESS_CONTROL_CONTRACT || 'C_TESTNET_ACCESS_CONTROL_PLACEHOLDER',
    },
  },
  mainnet: {
    id: 'mainnet',
    name: 'Mainnet',
    rpcUrl: 'https://soroban-rpc.mainnet.stellar.org',
    horizonUrl: 'https://horizon.stellar.org',
    networkPassphrase: 'Public Global Stellar Network ; September 2015',
    explorerUrl: 'https://stellarchain.io',
    contracts: {
      vault: process.env.NEXT_PUBLIC_MAINNET_VAULT_CONTRACT || 'C_MAINNET_VAULT_PLACEHOLDER',
      vaultToken: process.env.NEXT_PUBLIC_MAINNET_VAULT_TOKEN_CONTRACT || 'C_MAINNET_VAULT_TOKEN_PLACEHOLDER',
      yieldRegistry: process.env.NEXT_PUBLIC_MAINNET_YIELD_REGISTRY_CONTRACT || 'C_MAINNET_YIELD_REGISTRY_PLACEHOLDER',
      allocationStrategy: process.env.NEXT_PUBLIC_MAINNET_ALLOCATION_STRATEGY_CONTRACT || 'C_MAINNET_ALLOCATION_STRATEGY_PLACEHOLDER',
      treasury: process.env.NEXT_PUBLIC_MAINNET_TREASURY_CONTRACT || 'C_MAINNET_TREASURY_PLACEHOLDER',
      accessControl: process.env.NEXT_PUBLIC_MAINNET_ACCESS_CONTROL_CONTRACT || 'C_MAINNET_ACCESS_CONTROL_PLACEHOLDER',
    },
  },
};

export const DEFAULT_NETWORK = NETWORKS.testnet;
