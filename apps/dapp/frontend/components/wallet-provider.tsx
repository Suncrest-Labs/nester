"use client";

import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useState,
    type ReactNode,
} from "react";
import { getInstallUrl } from "@/lib/wallet-install-urls";
import { config } from "@/lib/config";
import { useNetwork } from "@/hooks/useNetwork";

export interface WalletInfo {
    id: string;
    name: string;
    icon: string;
    url: string;
    installUrl: string;
    isAvailable: boolean;
}

interface WalletState {
    address: string | null;
    isConnected: boolean;
    isConnecting: boolean;
    isInitializing: boolean;
    wallets: WalletInfo[];
    walletsLoaded: boolean;
    selectedWalletId: string | null;
    connect: (walletId: string) => Promise<void>;
    disconnect: () => void;
}

const WalletContext = createContext<WalletState>({
    address: null,
    isConnected: false,
    isConnecting: false,
    isInitializing: true,
    wallets: [],
    walletsLoaded: false,
    selectedWalletId: null,
    connect: async () => { },
    disconnect: () => { },
});

export function useWallet() {
    return useContext(WalletContext);
}

function extractErrorMessage(err: unknown): string {
    if (err instanceof Error) return err.message;
    if (typeof err === "string") return err;
    if (err && typeof err === "object") {
        const e = err as Record<string, unknown>;
        if (typeof e.message === "string") return e.message;
        if (typeof e.error === "object" && e.error !== null) {
            const inner = e.error as Record<string, unknown>;
            if (typeof inner.message === "string") return inner.message;
        }
    }
    return "Connection was rejected or timed out";
}

export function WalletProvider({ children }: { children: ReactNode }) {
    const { currentNetwork } = useNetwork();
    const [address, setAddress] = useState<string | null>(null);
    const [isConnecting, setIsConnecting] = useState(false);
    const [isInitializing, setIsInitializing] = useState(true);
    const [wallets, setWallets] = useState<WalletInfo[]>([]);
    const [walletsLoaded, setWalletsLoaded] = useState(false);
    const [selectedWalletId, setSelectedWalletId] = useState<string | null>(
        null
    );
    const [kitReady, setKitReady] = useState(false);

    useEffect(() => {
        if (typeof window === "undefined") return;

        const initKit = async () => {
            // E2E test bypass: if the page has injected a mock wallet session
            // via window.__e2e_wallet__, restore it directly without calling
            // the real wallet kit (which requires a browser extension).
            // Only active in test environments — never in production.
            const e2eWallet = process.env.NODE_ENV !== "production"
                ? (window as unknown as Record<string, unknown>)
                    .__e2e_wallet__ as
                    | { address: string; walletId: string }
                    | undefined
                : undefined;
            if (e2eWallet?.address && e2eWallet?.walletId) {
                setAddress(e2eWallet.address);
                setSelectedWalletId(e2eWallet.walletId);
                setWallets([
                    {
                        id: e2eWallet.walletId,
                        name: "Freighter",
                        icon: "",
                        url: "https://freighter.app",
                        installUrl: "https://freighter.app",
                        isAvailable: true,
                    },
                ]);
                setWalletsLoaded(true);
                setKitReady(true);
                setIsInitializing(false);
                return;
            }

            try {
                const { StellarWalletsKit } = await import(
                    "@creit.tech/stellar-wallets-kit"
                );
                const { defaultModules } = await import(
                    "@creit.tech/stellar-wallets-kit/modules/utils"
                );

                StellarWalletsKit.init({
                    modules: defaultModules(),
                    network: currentNetwork.networkPassphrase as never,
                });

                setKitReady(true);

                const supported =
                    await StellarWalletsKit.refreshSupportedWallets();
                const walletList: WalletInfo[] = supported.map(
                    (w: {
                        id: string;
                        name: string;
                        icon: string;
                        url: string;
                        isAvailable: boolean;
                    }) => ({
                        id: w.id,
                        name: w.name,
                        icon: w.icon,
                        url: w.url,
                        installUrl: getInstallUrl(w.id, w.url),
                        isAvailable: w.isAvailable,
                    })
                );
                setWallets(walletList);
                setWalletsLoaded(true);

                // Check if there's a previously saved session
                const savedWalletId =
                    localStorage.getItem("nester_wallet_id");
                const savedAddress =
                    localStorage.getItem("nester_wallet_addr");
                if (savedWalletId && savedAddress) {
                    const savedWallet = walletList.find(
                        (w) => w.id === savedWalletId && w.isAvailable
                    );
                    if (savedWallet) {
                        try {
                            StellarWalletsKit.setWallet(savedWalletId);
                            // Use the module directly to request the address
                            const walletModule = StellarWalletsKit.selectedModule;
                            const { address: addr } =
                                await walletModule.getAddress();
                            if (addr) {
                                // Update the kit's internal state
                                const { activeAddress } = await import(
                                    "@creit.tech/stellar-wallets-kit/state"
                                );
                                activeAddress.value = addr;
                                setAddress(addr);
                                setSelectedWalletId(savedWalletId);
                            }
                        } catch {
                            localStorage.removeItem("nester_wallet_id");
                            localStorage.removeItem("nester_wallet_addr");
                        }
                    }
                }
            } catch (err) {
                console.error("Failed to initialize wallet kit:", err);
                setWalletsLoaded(true);
            } finally {
                setIsInitializing(false);
            }
        };

        initKit();
    }, [currentNetwork.networkPassphrase]);

    const connect = useCallback(
        async (walletId: string) => {
            if (typeof window === "undefined" || !kitReady) return;
            setIsConnecting(true);

            try {
                const wallet = wallets.find((w) => w.id === walletId);
                if (!wallet) {
                    throw new Error("Wallet not found");
                }

                // Not installed → open Chrome Web Store / install page
                if (!wallet.isAvailable) {
                    window.open(wallet.installUrl, "_blank");
                    setIsConnecting(false);
                    return;
                }

                const { StellarWalletsKit } = await import(
                    "@creit.tech/stellar-wallets-kit"
                );

                // Set the wallet as active module
                StellarWalletsKit.setWallet(walletId);

                // Call the module directly to request the address from the wallet extension
                const walletModule = StellarWalletsKit.selectedModule;
                const { address: addr } = await walletModule.getAddress();

                if (addr) {
                    // Update the kit's internal state signal
                    const { activeAddress } = await import(
                        "@creit.tech/stellar-wallets-kit/state"
                    );
                    activeAddress.value = addr;

                    setAddress(addr);
                    setSelectedWalletId(walletId);
                    localStorage.setItem("nester_wallet_id", walletId);
                    localStorage.setItem("nester_wallet_addr", addr);
                }
            } catch (err) {
                const message = extractErrorMessage(err);
                console.error("Wallet connection failed:", message);
                throw new Error(message);
            } finally {
                setIsConnecting(false);
            }
        },
        [kitReady, wallets]
    );

    const disconnect = useCallback(async () => {
        try {
            const { StellarWalletsKit } = await import(
                "@creit.tech/stellar-wallets-kit"
            );
            await StellarWalletsKit.disconnect();
        } catch {
            // Disconnect may not be supported by all wallets
        }
        setAddress(null);
        setSelectedWalletId(null);
        localStorage.removeItem("nester_wallet_id");
        localStorage.removeItem("nester_wallet_addr");
    }, []);

    return (
        <WalletContext.Provider
            value={{
                address,
                isConnected: !!address,
                isConnecting,
                isInitializing,
                wallets,
                walletsLoaded,
                selectedWalletId,
                connect,
                disconnect,
            }}
        >
            {children}
        </WalletContext.Provider>
    );
}
