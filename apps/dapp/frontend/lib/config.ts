import { getContractId } from "@/lib/contracts";

export const config = {
    apiUrl: process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api/v1",
    wsUrl: process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080/ws",
    stellarNetwork: process.env.NEXT_PUBLIC_STELLAR_NETWORK || "Test SDF Network ; September 2015",
    stellarRpcUrl: process.env.NEXT_PUBLIC_STELLAR_RPC_URL || "https://soroban-testnet.stellar.org",
    stellarHorizonUrl: process.env.NEXT_PUBLIC_STELLAR_HORIZON_URL || "https://horizon-testnet.stellar.org",
    // Contract addresses resolve through lib/contracts.ts and are null when
    // unset or malformed — never "" (#1094). An empty address is not a usable
    // default: it builds a transaction against no contract and fails far from
    // the cause. Callers that cannot proceed without one use requireContractId.
    //
    // The variables are named *_CONTRACT_ID to match deploy-testnet.sh, which
    // is what writes them; the former *_CONTRACT_ADDRESS names read here were
    // never produced by any deploy path.
    contracts: {
        vault: getContractId("vault"),
        vaultXlm: getContractId("vaultXlm"),
        vaultToken: getContractId("vaultToken"),
        usdc: getContractId("usdc"),
    },
    explorerUrl: process.env.NEXT_PUBLIC_EXPLORER_URL || "https://stellar.expert/explorer/testnet",
    defaultNgnRate: Number(process.env.NEXT_PUBLIC_DEFAULT_NGN_RATE) || 1530,
    friendbotUrl: process.env.NEXT_PUBLIC_FRIENDBOT_URL || "https://friendbot.stellar.org",
    featuredWallets: (process.env.NEXT_PUBLIC_FEATURED_WALLETS || "freighter,lobstr,xbull").split(","),
};

export default config;
