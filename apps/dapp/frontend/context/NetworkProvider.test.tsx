import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import { NetworkProvider, useNetwork } from "./NetworkProvider";
import { readNetworkId } from "@/lib/storageKeys";

// Issue #1233: NetworkProvider must route through safeStorage rather than
// raw localStorage, and setNetwork must never leave React state and
// persisted state disagreeing about the active network — even when the
// underlying storage accessor throws.

function TestConsumer() {
  const { currentNetwork, setNetwork } = useNetwork();
  return (
    <div>
      <span data-testid="network-id">{currentNetwork.id}</span>
      <button onClick={() => setNetwork("mainnet")}>switch to mainnet</button>
      <button onClick={() => setNetwork("testnet")}>switch to testnet</button>
    </div>
  );
}

describe("NetworkProvider (#1233)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("defaults to testnet when nothing is persisted", async () => {
    render(
      <NetworkProvider>
        <TestConsumer />
      </NetworkProvider>
    );
    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("testnet"));
  });

  it("restores a previously persisted network on mount", async () => {
    window.localStorage.setItem("nester_network_id", JSON.stringify("mainnet"));
    render(
      <NetworkProvider>
        <TestConsumer />
      </NetworkProvider>
    );
    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("mainnet"));
  });

  it("restores a legacy, non-JSON-encoded persisted network on mount", async () => {
    // Simulates a value from the OLD raw localStorage.setItem call site.
    window.localStorage.setItem("nester_network_id", "mainnet");
    render(
      <NetworkProvider>
        <TestConsumer />
      </NetworkProvider>
    );
    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("mainnet"));
  });

  it("setNetwork updates both React state and persisted state together", async () => {
    render(
      <NetworkProvider>
        <TestConsumer />
      </NetworkProvider>
    );
    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("testnet"));

    act(() => {
      screen.getByText("switch to mainnet").click();
    });

    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("mainnet"));
    expect(readNetworkId()).toBe("mainnet");
  });

  it("does not crash and falls back to the default network when localStorage.getItem throws on mount", async () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockImplementation(() => {
      throw new Error("SecurityError: storage disabled");
    });

    expect(() =>
      render(
        <NetworkProvider>
          <TestConsumer />
        </NetworkProvider>
      )
    ).not.toThrow();

    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("testnet"));
  });

  it("setNetwork does not leave React state ahead of persistence when the storage write throws", async () => {
    render(
      <NetworkProvider>
        <TestConsumer />
      </NetworkProvider>
    );
    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("testnet"));

    vi.spyOn(window.localStorage.__proto__, "setItem").mockImplementation(() => {
      throw new DOMException("QuotaExceededError");
    });

    act(() => {
      screen.getByText("switch to mainnet").click();
    });

    // React state DID move to mainnet (safeStorage.set never throws — it
    // fell back to the in-memory map instead of failing outright), and a
    // read through the same safeStorage path agrees with it: state and
    // "persistence" (in-memory fallback counts) never disagree.
    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("mainnet"));
    expect(readNetworkId()).toBe("mainnet");
  });

  it("purges cached portfolio keys on network switch, leaving unrelated keys alone", async () => {
    window.localStorage.setItem("nester_portfolio_v1:testnet:abc", "cached");
    window.localStorage.setItem("nester_portfolio_v1:mainnet:xyz", "cached");
    window.localStorage.setItem("unrelated_key", "keep-me");

    render(
      <NetworkProvider>
        <TestConsumer />
      </NetworkProvider>
    );
    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("testnet"));

    act(() => {
      screen.getByText("switch to mainnet").click();
    });
    await waitFor(() => expect(screen.getByTestId("network-id")).toHaveTextContent("mainnet"));

    expect(window.localStorage.getItem("nester_portfolio_v1:testnet:abc")).toBeNull();
    expect(window.localStorage.getItem("nester_portfolio_v1:mainnet:xyz")).toBeNull();
    expect(window.localStorage.getItem("unrelated_key")).toBe("keep-me");
  });
});
