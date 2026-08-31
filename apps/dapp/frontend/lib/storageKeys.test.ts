import { beforeEach, describe, expect, it } from "vitest";
import { NETWORK_STORAGE_KEY, readNetworkId, writeNetworkId } from "./storageKeys";

describe("readNetworkId / writeNetworkId (#1233)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("returns null when nothing is persisted", () => {
    expect(readNetworkId()).toBeNull();
  });

  it("round-trips a value written via writeNetworkId", () => {
    writeNetworkId("mainnet");
    expect(readNetworkId()).toBe("mainnet");
  });

  it("reads a legacy bare (non-JSON-encoded) value from the old raw localStorage.setItem call sites", () => {
    // Simulates a value persisted by the OLD code (localStorage.setItem(key, "testnet")),
    // which is NOT JSON — safeStorage.get's JSON.parse would reject and wipe it.
    window.localStorage.setItem(NETWORK_STORAGE_KEY, "testnet");
    expect(readNetworkId()).toBe("testnet");
  });

  it("reads a JSON-encoded value written via writeNetworkId's safeStorage.set", () => {
    window.localStorage.setItem(NETWORK_STORAGE_KEY, JSON.stringify("mainnet"));
    expect(readNetworkId()).toBe("mainnet");
  });

  it("returns null for an unrecognized value rather than throwing", () => {
    window.localStorage.setItem(NETWORK_STORAGE_KEY, "not-a-real-network");
    expect(readNetworkId()).toBeNull();
  });

  it("returns null for malformed JSON rather than throwing", () => {
    window.localStorage.setItem(NETWORK_STORAGE_KEY, "{not valid json");
    expect(readNetworkId()).toBeNull();
  });
});
