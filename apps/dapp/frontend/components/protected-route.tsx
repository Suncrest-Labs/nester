"use client";

import { useEffect, ReactNode } from "react";
import { useWallet } from "./wallet-provider";
import { useRouter } from "next/navigation";

interface ProtectedRouteProps {
    children: ReactNode;
    fallback?: ReactNode;
}

/**
 * ProtectedRoute is a client component that guards routes behind wallet connection.
 *
 * Features:
 * - Checks if wallet is connected
 * - Redirects to home ("/") if not connected
 * - Shows fallback content (loading spinner, etc.) while redirecting
 * - Prevents flash of empty content
 *
 * Usage:
 * ```tsx
 * export default function MyPage() {
 *   return (
 *     <ProtectedRoute>
 *       <YourPageContent />
 *     </ProtectedRoute>
 *   );
 * }
 * ```
 */
export function ProtectedRoute({
    children,
    fallback,
}: ProtectedRouteProps) {
    const { isConnected } = useWallet();
    const router = useRouter();

    useEffect(() => {
        if (!isConnected) {
            router.push("/");
        }
    }, [isConnected, router]);

    // While checking connection status, show fallback or null
    if (!isConnected) {
        return fallback ?? null;
    }

    return children;
}
