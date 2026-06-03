import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { AuthProvider, useAuth } from "@/components/auth-provider";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: vi.fn(() => ({ address: "GABC123" })),
}));

function AuthConsumer() {
  const { token } = useAuth();
  return (
    <div>
      <span data-testid="token">{token ?? "none"}</span>
      <button onClick={() => {
        window.localStorage.setItem("nester_auth_token", "jwt-test");
        window.dispatchEvent(new StorageEvent("storage", { key: "nester_auth_token", newValue: "jwt-test" }));
      }}>Set Token</button>
    </div>
  );
}

describe("AuthProvider", () => {
  it("allows setting auth token for WebSocket", () => {
    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>
    );
    expect(screen.getByTestId("token")).toHaveTextContent("none");
    fireEvent.click(screen.getByRole("button", { name: /set token/i }));
    expect(screen.getByTestId("token")).toHaveTextContent("jwt-test");
  });

  it("clears token when wallet disconnects", async () => {
    const { useWallet } = await import("@/components/wallet-provider");
    vi.mocked(useWallet).mockReturnValue({
      address: null,
      connect: vi.fn(),
      disconnect: vi.fn(),
      isConnecting: false,
      isConnected: false,
      wallets: [],
      walletsLoaded: true,
      selectedWalletId: null,
    } as ReturnType<typeof useWallet>);

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>
    );
    expect(screen.getByTestId("token")).toHaveTextContent("none");
  });
});
