import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { AuthProvider, useAuth } from "@/components/auth-provider";
import {
  setAccessToken,
  getAccessToken,
  clearTokens,
  ACCESS_TOKEN_STORAGE_KEY,
} from "@/lib/auth/token-store";
import { api } from "@/lib/api/client";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: vi.fn(() => ({ address: "GABC123" })),
}));

vi.mock("@/lib/api/client", () => ({
  api: {
    auth: {
      refresh: vi.fn(),
      requestChallenge: vi.fn(),
      verify: vi.fn(),
      logout: vi.fn(),
      logoutAll: vi.fn(),
    },
    users: {
      getByWallet: vi.fn(),
      register: vi.fn(),
    },
  },
  ApiError: class ApiError extends Error {
    constructor(
      message: string,
      public readonly status: number
    ) {
      super(message);
    }
  },
}));

function AuthConsumer() {
  const { token } = useAuth();
  return (
    <div>
      <span data-testid="token">{token ?? "none"}</span>
      <button
        onClick={() => {
          setAccessToken("jwt-test");
        }}
      >
        Set Token
      </button>
    </div>
  );
}

describe("AuthProvider & In-Memory Token Store (#1506)", () => {
  beforeEach(() => {
    window.localStorage.clear();
    clearTokens();
    vi.clearAllMocks();
  });

  it("keeps the access token in memory only and never writes to localStorage", () => {
    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>
    );
    expect(screen.getByTestId("token")).toHaveTextContent("none");

    fireEvent.click(screen.getByRole("button", { name: /set token/i }));

    expect(screen.getByTestId("token")).toHaveTextContent("jwt-test");
    expect(getAccessToken()).toBe("jwt-test");
    expect(window.localStorage.getItem(ACCESS_TOKEN_STORAGE_KEY)).toBeNull();
    expect(window.localStorage.getItem("nester_token")).toBeNull();
  });

  it("purges legacy nester_auth_token from localStorage on first load", () => {
    window.localStorage.setItem(ACCESS_TOKEN_STORAGE_KEY, "legacy-leaked-jwt");
    window.localStorage.setItem("nester_token", "legacy-alt-jwt");

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>
    );

    expect(window.localStorage.getItem(ACCESS_TOKEN_STORAGE_KEY)).toBeNull();
    expect(window.localStorage.getItem("nester_token")).toBeNull();
  });

  it("restores the session on reload via /auth/refresh cookie without localStorage", async () => {
    vi.mocked(api.auth.refresh).mockResolvedValueOnce({
      access_token: "refreshed-jwt-token",
      expires_in: 300,
      token_type: "Bearer",
    });

    render(
      <AuthProvider>
        <AuthConsumer />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId("token")).toHaveTextContent("refreshed-jwt-token");
    });
    expect(getAccessToken()).toBe("refreshed-jwt-token");
    expect(window.localStorage.getItem(ACCESS_TOKEN_STORAGE_KEY)).toBeNull();
  });

  it("clears token when wallet disconnects", async () => {
    const { useWallet } = await import("@/components/wallet-provider");
    vi.mocked(useWallet).mockReturnValue({
      address: null,
      user: null,
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
