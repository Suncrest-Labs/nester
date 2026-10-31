import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";

vi.mock("next/navigation", () => ({
  usePathname: () => "/dashboard",
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));
vi.mock("next/image", () => ({
  // eslint-disable-next-line @next/next/no-img-element
  default: (props: { alt: string }) => <img alt={props.alt} />,
}));
vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({ address: "GAWALLETADDRESS", isConnected: true, disconnect: vi.fn() }),
}));
vi.mock("@/hooks/useNetwork", () => ({
  useNetwork: () => ({ currentNetwork: { id: "mainnet", name: "Mainnet" } }),
}));
vi.mock("@/components/site-footer", () => ({ SiteFooter: () => null }));
vi.mock("@/components/notification-bell", () => ({ NotificationBell: () => null }));
vi.mock("@/components/theme-toggle", () => ({ ThemeToggle: () => null }));
vi.mock("@/components/connection-status-badge", () => ({ ConnectionStatusBadge: () => null }));

import { APP_NAV } from "@/lib/navigation";
import { AppShell } from "@/components/app-shell";
import { BottomNav } from "@/components/bottom-nav";
import { Navbar } from "@/components/navbar";

const RETIRED_LABELS = /^(portfolio|stocks|markets|vaults|history)$/i;
const RETIRED_HREF = /^\/(portfolio|stocks|vaults)(\/|$)/;

function expectNoRetiredLinks(root: HTMLElement) {
  for (const link of Array.from(root.querySelectorAll("a"))) {
    expect(link.getAttribute("href") ?? "").not.toMatch(RETIRED_HREF);
    expect((link.textContent ?? "").trim()).not.toMatch(RETIRED_LABELS);
  }
}

describe("app navigation", () => {
  it("lists Savings Rate, Yields, Goals and Settings", () => {
    expect(APP_NAV.map(({ label, href }) => [label, href])).toEqual([
      ["Savings Rate", "/dashboard"],
      ["Yields", "/yields"],
      ["Goals", "/savings"],
      ["Settings", "/settings"],
    ]);
  });

  it("app shell nav doesn't render Portfolio or Stocks", () => {
    const { container } = render(<AppShell>content</AppShell>);
    expect(screen.getAllByRole("link", { name: "Savings Rate" }).length).toBeGreaterThan(0);
    expectNoRetiredLinks(container);

    fireEvent.click(screen.getByRole("button", { name: /GAWAL/ }));
    expectNoRetiredLinks(container);

    fireEvent.click(screen.getByRole("button", { name: "Toggle navigation menu" }));
    expectNoRetiredLinks(document.body);
  });

  it("bottom nav doesn't render Portfolio or Stocks", () => {
    const { container } = render(<BottomNav />);
    expect(within(container).getByRole("link", { name: "Goals" })).toBeTruthy();
    expect(within(container).getByRole("link", { name: "Yields" })).toBeTruthy();
    expectNoRetiredLinks(container);
  });

  it("navbar doesn't render Portfolio or Stocks", () => {
    const { container } = render(<Navbar />);
    expect(screen.getAllByRole("link", { name: "Savings Rate" }).length).toBeGreaterThan(0);
    expectNoRetiredLinks(container);

    fireEvent.click(screen.getByRole("button", { name: /GAWAL/ }));
    expectNoRetiredLinks(container);
  });
});
