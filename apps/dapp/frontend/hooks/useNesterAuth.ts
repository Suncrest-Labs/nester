"use client";

/**
 * useNesterAuth — challenge/verify login flow + JWT persistence.
 *
 * 1. Request a nonce (challenge) from POST /api/v1/auth/challenge
 * 2. Have the wallet sign it with signMessage()
 * 3. POST /api/v1/auth/verify → get a JWT
 * 4. Persist the JWT in localStorage so apiFetch() can include it
 *
 * The hook is idempotent — calling signIn() when already authenticated is a
 * no-op.
 */

import { useState, useCallback, useEffect } from "react";
import { api } from "@/lib/api/client";

const TOKEN_KEY = "nester_auth_token";
const USER_ID_KEY = "nester_user_id";

function loadStoredToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

function loadStoredUserId(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(USER_ID_KEY);
}

function persistToken(token: string | null, userId: string | null) {
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

export function useNesterAuth() {
  const [token, setToken] = useState<string | null>(loadStoredToken);
  const [userId, setUserId] = useState<string | null>(loadStoredUserId);
  const [isSigningIn, setIsSigningIn] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Keep state in sync with other tabs
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === TOKEN_KEY) {
        setToken(e.newValue);
      }
      if (e.key === USER_ID_KEY) {
        setUserId(e.newValue);
      }
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  const signIn = useCallback(async (walletAddress: string) => {
    if (token) return; // already authenticated
    setIsSigningIn(true);
    setError(null);

    try {
      // Step 1: get challenge nonce
      const { challenge } = await api.auth.requestChallenge(walletAddress);

      // Step 2: sign with freighter / stellar-wallets-kit
      //   We use @stellar/freighter-api's signMessage (signs the raw string)
      const { signMessage } = await import("@stellar/freighter-api");
      const result = await signMessage(challenge, { address: walletAddress });
      // signMessage returns { signature: string } (base64)
      const signature =
        typeof result === "string" ? result : (result as unknown as { signature: string }).signature;

      // Step 3: verify and get JWT
      const { token: jwt } = await api.auth.verify(walletAddress, signature, challenge);

      // Step 4: look up or auto-register user
      let uid: string | null = null;
      try {
        const user = await api.users.getByWallet(walletAddress);
        uid = user.id;
      } catch {
        // user not found — auto-register
        try {
          const newUser = await api.users.register(
            walletAddress,
            walletAddress.slice(0, 8) // use first 8 chars as display name
          );
          uid = newUser.id;
        } catch {
          // ignore register failure — we still have the token
        }
      }

      persistToken(jwt, uid);
      setToken(jwt);
      setUserId(uid);
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : "Authentication failed";
      setError(msg);
      throw err;
    } finally {
      setIsSigningIn(false);
    }
  }, [token]);

  const signOut = useCallback(() => {
    persistToken(null, null);
    setToken(null);
    setUserId(null);
  }, []);

  return {
    token,
    userId,
    isAuthenticated: !!token,
    isSigningIn,
    authError: error,
    signIn,
    signOut,
  };
}
