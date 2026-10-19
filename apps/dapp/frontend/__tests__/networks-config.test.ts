import { afterEach, describe, expect, it, vi } from "vitest";

const DEAD_HOST = "soroban-rpc.mainnet.stellar.org";

async function loadNetworks() {
  vi.resetModules();
  return (await import("@/lib/networks")).NETWORKS;
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("mainnet network config", () => {
  it("does not default to an RPC host that does not exist", async () => {
    vi.stubEnv("NEXT_PUBLIC_MAINNET_RPC_URL", "");
    const networks = await loadNetworks();

    expect(networks.mainnet.rpcUrl).toMatch(/^https:\/\//);
    expect(networks.mainnet.rpcUrl).not.toContain(DEAD_HOST);
  });

  it("lets a deployment choose its own RPC provider", async () => {
    vi.stubEnv("NEXT_PUBLIC_MAINNET_RPC_URL", "https://rpc.provider.example");
    const networks = await loadNetworks();

    expect(networks.mainnet.rpcUrl).toBe("https://rpc.provider.example");
  });

  it("keeps the testnet RPC independent of the mainnet override", async () => {
    vi.stubEnv("NEXT_PUBLIC_MAINNET_RPC_URL", "https://rpc.provider.example");
    const networks = await loadNetworks();

    expect(networks.testnet.rpcUrl).toBe("https://soroban-testnet.stellar.org");
  });
});
