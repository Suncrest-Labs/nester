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

export const CONTRACT_ENV_VARS: Record<
  'testnet' | 'mainnet',
  Record<keyof NetworkConfig['contracts'], string>
> = {
  testnet: {
    vault: 'NEXT_PUBLIC_TESTNET_VAULT_CONTRACT',
    vaultToken: 'NEXT_PUBLIC_TESTNET_VAULT_TOKEN_CONTRACT',
    yieldRegistry: 'NEXT_PUBLIC_TESTNET_YIELD_REGISTRY_CONTRACT',
    allocationStrategy: 'NEXT_PUBLIC_TESTNET_ALLOCATION_STRATEGY_CONTRACT',
    treasury: 'NEXT_PUBLIC_TESTNET_TREASURY_CONTRACT',
    accessControl: 'NEXT_PUBLIC_TESTNET_ACCESS_CONTROL_CONTRACT',
  },
  mainnet: {
    vault: 'NEXT_PUBLIC_MAINNET_VAULT_CONTRACT',
    vaultToken: 'NEXT_PUBLIC_MAINNET_VAULT_TOKEN_CONTRACT',
    yieldRegistry: 'NEXT_PUBLIC_MAINNET_YIELD_REGISTRY_CONTRACT',
    allocationStrategy: 'NEXT_PUBLIC_MAINNET_ALLOCATION_STRATEGY_CONTRACT',
    treasury: 'NEXT_PUBLIC_MAINNET_TREASURY_CONTRACT',
    accessControl: 'NEXT_PUBLIC_MAINNET_ACCESS_CONTROL_CONTRACT',
  },
};

export function isPlaceholderContractId(value: string | undefined): boolean {
  if (!value || value.trim() === '') return true;
  return /^C_(TESTNET|MAINNET)_[A-Z0-9_]+_PLACEHOLDER$/i.test(value.trim()) ||
    value.toUpperCase().includes('PLACEHOLDER');
}

export function buildNetworks(
  env: Record<string, string | undefined> = process.env
): Record<string, NetworkConfig> {
  return {
    testnet: {
      id: 'testnet',
      name: 'Testnet',
      rpcUrl: 'https://soroban-testnet.stellar.org',
      horizonUrl: 'https://horizon-testnet.stellar.org',
      networkPassphrase: 'Test SDF Network ; September 2015',
      explorerUrl: 'https://testnet.stellarchain.io',
      friendbotUrl: 'https://friendbot.stellar.org',
      contracts: {
        vault: env.NEXT_PUBLIC_TESTNET_VAULT_CONTRACT || 'C_TESTNET_VAULT_PLACEHOLDER',
        vaultToken: env.NEXT_PUBLIC_TESTNET_VAULT_TOKEN_CONTRACT || 'C_TESTNET_VAULT_TOKEN_PLACEHOLDER',
        yieldRegistry: env.NEXT_PUBLIC_TESTNET_YIELD_REGISTRY_CONTRACT || 'C_TESTNET_YIELD_REGISTRY_PLACEHOLDER',
        allocationStrategy: env.NEXT_PUBLIC_TESTNET_ALLOCATION_STRATEGY_CONTRACT || 'C_TESTNET_ALLOCATION_STRATEGY_PLACEHOLDER',
        treasury: env.NEXT_PUBLIC_TESTNET_TREASURY_CONTRACT || 'C_TESTNET_TREASURY_PLACEHOLDER',
        accessControl: env.NEXT_PUBLIC_TESTNET_ACCESS_CONTROL_CONTRACT || 'C_TESTNET_ACCESS_CONTROL_PLACEHOLDER',
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
        vault: env.NEXT_PUBLIC_MAINNET_VAULT_CONTRACT || 'C_MAINNET_VAULT_PLACEHOLDER',
        vaultToken: env.NEXT_PUBLIC_MAINNET_VAULT_TOKEN_CONTRACT || 'C_MAINNET_VAULT_TOKEN_PLACEHOLDER',
        yieldRegistry: env.NEXT_PUBLIC_MAINNET_YIELD_REGISTRY_CONTRACT || 'C_MAINNET_YIELD_REGISTRY_PLACEHOLDER',
        allocationStrategy: env.NEXT_PUBLIC_MAINNET_ALLOCATION_STRATEGY_CONTRACT || 'C_MAINNET_ALLOCATION_STRATEGY_PLACEHOLDER',
        treasury: env.NEXT_PUBLIC_MAINNET_TREASURY_CONTRACT || 'C_MAINNET_TREASURY_PLACEHOLDER',
        accessControl: env.NEXT_PUBLIC_MAINNET_ACCESS_CONTROL_CONTRACT || 'C_MAINNET_ACCESS_CONTROL_PLACEHOLDER',
      },
    },
  };
}

export function validateProductionNetworkEnv(
  env: Record<string, string | undefined> = process.env
): void {
  if (env.NODE_ENV !== 'production') {
    return;
  }

  const rawNetwork = env.NEXT_PUBLIC_STELLAR_NETWORK?.trim().toLowerCase();
  if (!rawNetwork) {
    throw new Error(
      'Missing required environment variable NEXT_PUBLIC_STELLAR_NETWORK for production build (expected "testnet" or "mainnet").'
    );
  }

  if (rawNetwork !== 'testnet' && rawNetwork !== 'mainnet') {
    throw new Error(
      `Invalid NEXT_PUBLIC_STELLAR_NETWORK="${env.NEXT_PUBLIC_STELLAR_NETWORK}". Expected "testnet" or "mainnet".`
    );
  }

  const networks = buildNetworks(env);
  const selected = networks[rawNetwork];
  const envVarMap = CONTRACT_ENV_VARS[rawNetwork];
  const invalidContracts: string[] = [];

  for (const [contractKey, contractId] of Object.entries(selected.contracts) as [
    keyof NetworkConfig['contracts'],
    string,
  ][]) {
    if (isPlaceholderContractId(contractId)) {
      invalidContracts.push(`${envVarMap[contractKey]} (resolved to "${contractId}")`);
    }
  }

  if (invalidContracts.length > 0) {
    throw new Error(
      `Production build failed for network "${rawNetwork}": missing or placeholder contract IDs configured for: ${invalidContracts.join(', ')}`
    );
  }
}

export const NETWORKS: Record<string, NetworkConfig> = buildNetworks();

const configuredDefaultNetwork =
  process.env.NEXT_PUBLIC_STELLAR_NETWORK?.trim().toLowerCase();
export const DEFAULT_NETWORK =
  (configuredDefaultNetwork && NETWORKS[configuredDefaultNetwork]) ||
  NETWORKS.testnet;

