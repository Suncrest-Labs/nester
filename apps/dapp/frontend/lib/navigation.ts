import { History, LayoutDashboard, PiggyBank, TrendingUp, type LucideIcon } from "lucide-react";

export interface NavItem {
  label: string;
  href: string;
  icon: LucideIcon;
}

export const APP_NAV: NavItem[] = [
  { label: "Savings", href: "/dashboard", icon: LayoutDashboard },
  { label: "Activity", href: "/activity", icon: History },
  { label: "Markets", href: "/yields", icon: TrendingUp },
  { label: "Goals", href: "/savings", icon: PiggyBank },
];
