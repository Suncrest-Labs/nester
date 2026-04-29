"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { LayoutDashboard, Vault, Globe, BarChart3, Menu } from "lucide-react";
import { cn } from "@/lib/utils";
import { useWallet } from "@/components/wallet-provider";

const BOTTOM_NAV = [
    { label: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
    { label: "Vaults", href: "/vaults", icon: Vault },
    { label: "Settlements", href: "/offramp", icon: Globe },
    { label: "History", href: "/portfolio", icon: BarChart3 },
];

export function BottomNav() {
    const pathname = usePathname();
    const { isConnected } = useWallet();

    // Only show if connected and not on home page
    if (!isConnected || pathname === "/") return null;

    return (
        <nav className="fixed bottom-0 left-0 right-0 z-40 flex items-center justify-around border-t border-border bg-white/90 backdrop-blur-md pb-safe pt-2 px-2 md:hidden">
            {BOTTOM_NAV.map((item) => {
                const active = pathname === item.href || pathname.startsWith(item.href + "/");
                return (
                    <Link
                        key={item.href}
                        href={item.href}
                        className={cn(
                            "flex min-h-[44px] min-w-[44px] flex-col items-center justify-center gap-1 p-2 transition-colors",
                            active ? "text-brand-purple" : "text-foreground/50 hover:text-foreground"
                        )}
                    >
                        <item.icon className="h-5 w-5" />
                        <span className="text-[10px] font-medium">{item.label}</span>
                    </Link>
                );
            })}
        </nav>
    );
}
