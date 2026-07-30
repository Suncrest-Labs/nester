"use client";

import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  useRef,
  type ReactNode,
} from "react";
import { useWallet } from "@/components/wallet-provider";
import { api, ApiError } from "@/lib/api/client";
import {
  getAccessToken,
  getUserId as readUserId,
  setAccessToken,
  setUserId as writeUserId,
  clearTokens,
  ACCESS_TOKEN_STORAGE_KEY,
  USER_ID_STORAGE_KEY,
} from "@/lib/auth/token-store";

interface AuthContextType {
  token: string | null;
  userId: string | null;
  isAuthenticated: boolean;
  isSigningIn: boolean;
  authError: string | null;
  signIn: () => Promise<void>;
  signOut: () => Promise<void>;
  signOutAllDevices: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType>({
  token: null,
  userId: null,
  isAuthenticated: false,
  isSigningIn: false,
  authError: null,
  signIn: async () => {},
  signOut: async () => {},
  signOutAllDevices: async () => {},
});

/** JWT `exp` claim (seconds since epoch), or null if it can't be read. */
function decodeExpiry(token: string): number | null {
  const parts = token.split(".");
  if (parts.length < 2) return null;
  try {
    const payload = JSON.parse(atob(parts[1].replace(/-/g, "+").replace(/_/g, "/")));
    return typeof payload.exp === "number" ? payload.exp : null;
  } catch {
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const { address } = useWallet();

  const [token, setToken] = useState<string | null>(() => getAccessToken() || null);
  const [userId, setUserId] = useState<string | null>(() => readUserId());
  const [isSigningIn, setIsSigningIn] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearSession = useCallback(() => {
    if (refreshTimerRef.current) {
      clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
    clearTokens();
    setToken(null);
    setUserId(null);
  }, []);

  // Proactively refresh a bit before the short-lived access token expires,
  // so the 5-minute lifetime stays invisible — most page views never hit the
  // reactive 401-retry path in the API client at all.
  const scheduleProactiveRefresh = useCallback((accessToken: string) => {
    if (refreshTimerRef.current) {
      clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
    const exp = decodeExpiry(accessToken);
    if (!exp) return;
    const msUntilExpiry = exp * 1000 - Date.now();
    const delay = Math.max(msUntilExpiry - 30_000, 5_000); // refresh 30s early

    const attempt = async () => {
      try {
        const refreshed = await api.auth.refresh();
        setToken(refreshed.access_token);
        scheduleProactiveRefresh(refreshed.access_token);
      } catch (err) {
        // Only a genuine rejection of the refresh token itself (401/403 —
        // expired, reused, revoked, device mismatch) means the session is
        // actually over. A transient failure (network blip, 5xx) doesn't
        // mean that, so don't force a logout over it — retry shortly and
        // let the still-valid refresh token carry the session through.
        const isAuthRejection = err instanceof ApiError && (err.status === 401 || err.status === 403);
        if (!isAuthRejection) {
          refreshTimerRef.current = setTimeout(attempt, 15_000);
          return;
        }
        clearSession();
      }
    };

    refreshTimerRef.current = setTimeout(attempt, delay);
  }, [clearSession]);

  // Clear session when wallet disconnects
  useEffect(() => {
    if (!address) {
      clearSession();
    }
  }, [address, clearSession]);

  // Pick up an already-active session (e.g. page reload) with proactive refresh.
  useEffect(() => {
    if (token) scheduleProactiveRefresh(token);
    return () => {
      if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Sync across browser tabs
  useEffect(() => {
    const handler = (e: StorageEvent) => {
      if (e.key === ACCESS_TOKEN_STORAGE_KEY) {
        setToken(e.newValue);
        if (e.newValue) scheduleProactiveRefresh(e.newValue);
      }
      if (e.key === USER_ID_STORAGE_KEY) setUserId(e.newValue);
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  }, [scheduleProactiveRefresh]);

  const signIn = useCallback(async () => {
    if (!address || token) return; // already signed in or no wallet
    setIsSigningIn(true);
    setAuthError(null);

    try {
      // 1. Request challenge nonce
      const { challenge } = await api.auth.requestChallenge(address);

      // 2. Sign with Freighter
      const { signMessage } = await import("@stellar/freighter-api");
      const { signedMessage, error: signError } = await signMessage(challenge, { address });
      if (signError || !signedMessage) {
        throw new Error(signError?.message || "Wallet declined to sign the message");
      }
      // Freighter sends the signature across the extension bridge as a
      // JSON-serialized Buffer ({ type: "Buffer", data: [...] }), a real
      // Uint8Array, or (newer protocol) an already-encoded string — never
      // assume a live Buffer instance survived serialization.
      const bytes: number[] | string =
        typeof signedMessage === "string"
          ? signedMessage
          : Array.isArray(signedMessage)
            ? signedMessage
            : signedMessage instanceof Uint8Array
              ? Array.from(signedMessage)
              : Array.isArray((signedMessage as { data?: number[] }).data)
                ? (signedMessage as { data: number[] }).data
                : null!;
      if (bytes === null) {
        throw new Error("Unexpected signed message format from wallet");
      }
      const signature =
        typeof bytes === "string" ? bytes : btoa(String.fromCharCode(...bytes));

      // 3. Verify and receive the access token. The refresh token is set
      // directly as an httpOnly cookie by the server — this client never
      // sees it.
      const { access_token } = await api.auth.verify(address, signature, challenge);

      // Persist the token before resolving the user record so that lookup/
      // register call carries a valid Authorization header.
      setAccessToken(access_token);
      setToken(access_token);
      scheduleProactiveRefresh(access_token);

      // 4. Resolve / create user record
      let uid: string | null = null;
      try {
        const user = await api.users.getByWallet(address);
        uid = user.id;
      } catch {
        try {
          const newUser = await api.users.register(
            address,
            `${address.slice(0, 4)}…${address.slice(-4)}`
          );
          uid = newUser.id;
        } catch {
          // token is still valid even if user create failed
        }
      }

      writeUserId(uid);
      setUserId(uid);
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Sign-in failed";
      setAuthError(msg);
    } finally {
      setIsSigningIn(false);
    }
  }, [address, token, scheduleProactiveRefresh]);

  const signOut = useCallback(async () => {
    try {
      await api.auth.logout();
    } catch {
      // best-effort — the local session is cleared regardless
    }
    clearSession();
  }, [clearSession]);

  const signOutAllDevices = useCallback(async () => {
    try {
      await api.auth.logoutAll();
    } catch {
      // best-effort — the local session is cleared regardless
    }
    clearSession();
  }, [clearSession]);

  return (
    <AuthContext.Provider
      value={{
        token,
        userId,
        isAuthenticated: !!token,
        isSigningIn,
        authError,
        signIn,
        signOut,
        signOutAllDevices,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
