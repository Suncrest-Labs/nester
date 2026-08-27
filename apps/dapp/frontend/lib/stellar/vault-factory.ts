import {
  Contract,
  rpc as SorobanRpc,
  TransactionBuilder,
  BASE_FEE,
  nativeToScVal,
  Address,
  Transaction,
} from "@stellar/stellar-sdk";

import { getCurrentNetwork } from "@/lib/stellar/transaction";

// Deliberately re-use the signer's resolver rather than reading
// NEXT_PUBLIC_NETWORK here. The wallet signs against
// localStorage["nester_network_id"]; resolving the network differently on the
// build side means switching networks in the UI signs a transaction built for
// a different passphrase, which fails deployment outright.

export interface CreateVaultParams {
  name: string;
  description?: string;
  ownerAddress: string;
  signTransaction: (xdr: string) => Promise<string>;
}

export interface CreateVaultResult {
  contractAddress: string;
  transactionHash: string;
  vaultId: string;
}

export interface VaultDeploymentResponse {
  success: boolean;
  vaultId?: string;
  contractAddress?: string;
  transactionHash?: string;
  error?: string;
}

/**
 * Create a new vault by invoking the factory contract on Soroban.
 * 
 * Flow:
 * 1. Validate factory contract ID is configured
 * 2. Fetch account sequence number
 * 3. Build create_vault transaction
 * 4. Simulate to populate footprint
 * 5. Sign via wallet
 * 6. Submit and poll for confirmation
 * 7. Extract contract address from return value
 */
export async function createVault(
  params: CreateVaultParams
): Promise<CreateVaultResult> {
  // Evaluate at runtime so tests can set environment variables
  const FACTORY_CONTRACT_ID =
    process.env.NEXT_PUBLIC_VAULT_FACTORY_CONTRACT_ID;
  const NETWORK = getCurrentNetwork();
  const NETWORK_PASSPHRASE = NETWORK.networkPassphrase;
  const RPC_URL =
    process.env.NEXT_PUBLIC_STELLAR_RPC_URL || NETWORK.rpcUrl;

  if (!FACTORY_CONTRACT_ID) {
    throw new Error(
      "NEXT_PUBLIC_VAULT_FACTORY_CONTRACT_ID is not configured"
    );
  }

  const server = new SorobanRpc.Server(RPC_URL);
  const contract = new Contract(FACTORY_CONTRACT_ID);

  // Fetch account sequence number
  const account = await server.getAccount(params.ownerAddress);

  // Build the transaction
  const transaction = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: NETWORK_PASSPHRASE,
  })
    .addOperation(
      contract.call(
        "create_vault",
        nativeToScVal(params.name, { type: "string" }),
        new Address(params.ownerAddress).toScVal()
      )
    )
    .setTimeout(30)
    .build();

  // Simulate to populate footprint
  const simResult = await server.simulateTransaction(transaction);

  if (SorobanRpc.Api.isSimulationError(simResult)) {
    throw new Error(
      `Simulation failed: ${(simResult as SorobanRpc.Api.SimulateTransactionErrorResponse).error}`
    );
  }

  // Prepare transaction with simulation results
  const preparedTx = SorobanRpc.assembleTransaction(transaction, simResult).build();
  const preparedXdr = preparedTx.toXDR();

  // Sign via wallet
  const signedXdr = await params.signTransaction(preparedXdr);

  // Submit transaction
  const submitResult = await server.sendTransaction(
    new Transaction(signedXdr, NETWORK_PASSPHRASE)
  );

  if (submitResult.status === "ERROR") {
    throw new Error(
      `Transaction failed: ${submitResult.errorResult?.toXDR("base64") ?? "unknown error"}`
    );
  }

  // Poll for result
  const hash = submitResult.hash;
  let getResult = await server.getTransaction(hash);

  const maxAttempts = 30;
  let attempts = 0;

  while (
    getResult.status === SorobanRpc.Api.GetTransactionStatus.NOT_FOUND &&
    attempts < maxAttempts
  ) {
    await new Promise((resolve) => setTimeout(resolve, 1000));
    getResult = await server.getTransaction(hash);
    attempts++;
  }

  if (getResult.status === SorobanRpc.Api.GetTransactionStatus.FAILED) {
    throw new Error(
      `Transaction failed on-chain: ${hash}`
    );
  }

  if (getResult.status === SorobanRpc.Api.GetTransactionStatus.NOT_FOUND) {
    throw new Error(`Transaction timed out waiting for confirmation: ${hash}`);
  }

  // Extract contract address from result — NEVER generate it
  const returnValue = getResult.returnValue;
  if (!returnValue) {
    throw new Error("No return value from vault creation transaction");
  }

  const contractAddress = Address.fromScVal(returnValue).toString();

  return {
    contractAddress,
    transactionHash: hash,
    vaultId: contractAddress, // use actual contract address as vault ID
  };
}

/**
 * Wrapper for the wizard component that bridges CreateVaultWizard's expected interface
 * to the real createVault implementation.
 */
export class VaultFactory {
  /**
   * Create a vault from wizard data
   */
  static async createVault(
    data: any, // WizardVaultData
    onProgress?: (status: string) => void,
    params?: {
      ownerAddress: string;
      signTransaction: (xdr: string) => Promise<string>;
    }
  ): Promise<VaultDeploymentResponse> {
    try {
      if (!params) {
        throw new Error("Missing required parameters: ownerAddress and signTransaction");
      }

      if (onProgress) onProgress("Preparing transaction...");

      const result = await createVault({
        name: data.name,
        description: data.description,
        ownerAddress: params.ownerAddress,
        signTransaction: params.signTransaction,
      });

      if (onProgress) onProgress("Vault created successfully!");

      return {
        success: true,
        vaultId: result.vaultId,
        contractAddress: result.contractAddress,
        transactionHash: result.transactionHash,
      };
    } catch (error: unknown) {
      const errorMessage =
        error instanceof Error
          ? error.message
          : "Failed to deploy vault to network";

      return {
        success: false,
        error: errorMessage,
      };
    }
  }
}
