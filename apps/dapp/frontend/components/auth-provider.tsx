"use client";

import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  type ReactNode,
} from "react";
import { useWallet } from "@/components/wallet-provider";
import { api } from "@/lib/api/client";

const TOKEN_KEY = "nester_auth_token";
const USER_ID_KEY = "nester_user_id";

interface AuthContextType {
  token: string | null;
  userId: string | null;
  isAuthenticated: boolean;
  isSigningIn: boolean;
  authError: string | null;
  signIn: () => Promise<void>;
  signOut: () => void;
}

const AuthContext = createContext<AuthContextType>({
  token: null,
  userId: null,
  isAuthenticated: false,
  isSigningIn: false,
  authError: null,
  signIn: async () => {},
  signOut: () => {},
});

function readStorage(key: string): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(key);
}

function writeStorage(token: string | null, userId: string | null) {
  if (typeof window === "undefined") return;
  if (token) {
    window.localStorage.setItem(TOKEN_KEY, token);
  } else {
    window.localStorage.removeItem(TOKEN_KEY);
  }
  if (userId) {
    window.localStorage.setItem(USER_ID_KEY, userId);
  } else {
    window.localStorage.removeItem(USER_ID_KEY);
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const { address } = useWallet();

  const [token, setToken] = useState<string | null>(() => readStorage(TOKEN_KEY));
  const [userId, setUserId] = useState<string | null>(() => readStorage(USER_ID_KEY));
  const [isSigningIn, setIsSigningIn] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);

  // Clear session when wallet disconnects
  useEffect(() => {
    if (!address) {
      writeStorage(null, null);
      setToken(null);
      setUserId(null);
    }
  }, [address]);

  // Sync across browser tabs
  useEffect(() => {
    const handler = (e: StorageEvent) => {
      if (e.key === TOKEN_KEY) setToken(e.newValue);
      if (e.key === USER_ID_KEY) setUserId(e.newValue);
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  }, []);

  const signIn = useCallback(async () => {
    if (!address || token) return; // already signed in or no wallet
    setIsSigningIn(true);
    setAuthError(null);

    try {
      // 1. Request challenge nonce
      const { challenge } = await api.auth.requestChallenge(address);

      // 2. Sign with Freighter/StellarWalletsKit
      const { signMessage } = await import("@stellar/freighter-api");
      const raw = await signMessage(challenge, { address });
      // v3 returns string directly; v6 (used in SWK) returns { signature }
      const signature =
        typeof raw === "string"
          ? raw
          : (raw as unknown as { signature: string }).signature;

      // 3. Verify and receive JWT
      const { token: jwt } = await api.auth.verify(address, signature, challenge);

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

      writeStorage(jwt, uid);
      setToken(jwt);
      setUserId(uid);
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Sign-in failed";
      setAuthError(msg);
    } finally {
      setIsSigningIn(false);
    }
  }, [address, token]);

  const signOut = useCallback(() => {
    writeStorage(null, null);
    setToken(null);
    setUserId(null);
  }, []);

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
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
