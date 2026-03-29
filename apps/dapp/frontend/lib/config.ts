export const config = {
    apiUrl: process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api/v1",
    wsUrl: process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080/ws",
    stellarNetwork: process.env.NEXT_PUBLIC_STELLAR_NETWORK || "Test SDF Network ; September 2015",
    stellarRpcUrl: process.env.NEXT_PUBLIC_STELLAR_RPC_URL || "https://soroban-testnet.stellar.org",
    stellarHorizonUrl: process.env.NEXT_PUBLIC_STELLAR_HORIZON_URL || "https://horizon-testnet.stellar.org",
    vaultContractAddress: process.env.NEXT_PUBLIC_VAULT_CONTRACT_ADDRESS || "",
    vaultTokenContractAddress: process.env.NEXT_PUBLIC_VAULT_TOKEN_CONTRACT_ADDRESS || "",
    explorerUrl: process.env.NEXT_PUBLIC_EXPLORER_URL || "https://stellar.expert/explorer/testnet",
    defaultNgnRate: Number(process.env.NEXT_PUBLIC_DEFAULT_NGN_RATE) || 1530,
    friendbotUrl: process.env.NEXT_PUBLIC_FRIENDBOT_URL || "https://friendbot.stellar.org",
    featuredWallets: (process.env.NEXT_PUBLIC_FEATURED_WALLETS || "freighter,lobstr,xbull").split(","),
};

export default config;
