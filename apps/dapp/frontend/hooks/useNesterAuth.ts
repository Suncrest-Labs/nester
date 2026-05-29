"use client";

/**
 * useNesterAuth — Deprecated: use useAuth() from @/components/auth-provider instead.
 *
 * This hook is kept for backward compatibility but delegates to the centralized
 * AuthProvider which owns all auth state, challenge/verify logic, and persistence.
 */

import { useAuth } from "@/components/auth-provider";

export function useNesterAuth() {
  const { token, userId, isSigningIn, authError, signIn } = useAuth();

  return {
    token,
    userId,
    isSigningIn,
    error: authError,
    signIn: async (walletAddress: string) => {
      // Note: walletAddress is not needed here since AuthProvider
      // gets it from useWallet context directly
      return signIn();
    },
  };
}
