import { LayoutDashboard, PiggyBank, Settings, type LucideIcon } from "lucide-react";

export interface NavItem {
  label: string;
  href: string;
  icon: LucideIcon;
}

export const APP_NAV: NavItem[] = [
  { label: "Savings Rate", href: "/dashboard", icon: LayoutDashboard },
  { label: "Goals", href: "/savings", icon: PiggyBank },
  { label: "Settings", href: "/settings", icon: Settings },
];
