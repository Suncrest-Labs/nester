"use client";

import {
    createContext,
    useContext,
    useState,
    useEffect,
    type ReactNode,
} from "react";
import { useWallet } from "@/components/wallet-provider";

interface AuthContextType {
    token: string | null;
    setToken: (token: string | null) => void;
}

const AuthContext = createContext<AuthContextType>({
    token: null,
    setToken: () => {},
});

export function AuthProvider({ children }: { children: ReactNode }) {
    const [token, setTokenState] = useState<string | null>(null);
    const { address } = useWallet();

    // Clear token synchronously if wallet disconnects (using render phase)
    const tokenToUse = address ? token : null;

    const setToken = (newToken: string | null) => {
        setTokenState(newToken);
    };

    return (
        <AuthContext.Provider value={{ token: tokenToUse, setToken }}>
            {children}
        </AuthContext.Provider>
    );
}

export function useAuth() {
    return useContext(AuthContext);
}
