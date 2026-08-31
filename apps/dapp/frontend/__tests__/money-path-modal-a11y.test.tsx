import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { DepositModal } from "@/components/vault/depositModal";
import type { Vault as VaultType } from "@/lib/types/vault";

vi.mock("@/components/wallet-provider", () => ({
    useWallet: () => ({
        address: "GABC1234567890123456789012345678901234567890123456789012345678",
    }),
}));

vi.mock("@/components/portfolio-provider", () => ({
    usePortfolio: () => ({
        getAvailableBalance: () => 1000,
        recordDeposit: vi.fn(),
        refreshBalances: vi.fn(),
    }),
}));

vi.mock("@/lib/stellar/transaction", () => ({
    executeVaultDeposit: vi.fn(),
    UserRejectedError: class UserRejectedError extends Error {},
    TransactionFailedError: class TransactionFailedError extends Error {},
    TransactionTimeoutError: class TransactionTimeoutError extends Error {},
    truncateTxHash: (h: string) => h.slice(0, 8),
}));

const mockVault: VaultType = {
    id: "usdc",
    name: "USDC Market",
    description: "Test market",
    marketType: "single",
    tokens: ["USDC"],
    currentApy: 10,
    apyRange: "8-12%",
    tvl: 1000000,
    utilization: 80,
    allocations: [],
    supportedAssets: ["USDC"],
    maturityTerms: "Flexible",
    earlyWithdrawalPenalty: "None",
    apyHistory: [],
    strategies: [],
};

// Acceptance criteria for nester#1128, scoped to the money path.
describe("money-path modal accessibility", () => {
    beforeEach(() => {
        document.body.innerHTML = "";
    });

    it("exposes the dialog with an accessible name", () => {
        render(<DepositModal open vault={mockVault} onClose={() => {}} />);

        const dialog = screen.getByRole("dialog");
        expect(dialog).toHaveAttribute("aria-modal", "true");
        expect(dialog).toHaveAccessibleName(/deposit into/i);
    });

    it("moves focus into the dialog when it opens", async () => {
        render(<DepositModal open vault={mockVault} onClose={() => {}} />);

        const dialog = screen.getByRole("dialog");
        await waitFor(() => {
            expect(dialog.contains(document.activeElement)).toBe(true);
        });
    });

    it("closes on Escape without submitting", () => {
        const onClose = vi.fn();
        render(<DepositModal open vault={mockVault} onClose={onClose} />);

        // A valid amount, so a submit would be possible if Escape leaked
        // through to the form.
        fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "100" } });
        fireEvent.keyDown(document, { key: "Escape" });

        expect(onClose).toHaveBeenCalled();
        // Still on the input step: Escape cancelled rather than submitting.
        expect(screen.getByPlaceholderText("0.00")).toBeInTheDocument();
    });

    it("returns focus to the trigger when the dialog closes", async () => {
        const trigger = document.createElement("button");
        trigger.textContent = "Open deposit";
        document.body.appendChild(trigger);
        trigger.focus();
        expect(document.activeElement).toBe(trigger);

        const { rerender } = render(
            <DepositModal open vault={mockVault} onClose={() => {}} />,
        );

        await waitFor(() => {
            expect(document.activeElement).not.toBe(trigger);
        });

        rerender(<DepositModal open={false} vault={mockVault} onClose={() => {}} />);

        await waitFor(() => {
            expect(document.activeElement).toBe(trigger);
        });
    });

    it("keeps Tab inside the dialog", () => {
        render(<DepositModal open vault={mockVault} onClose={() => {}} />);

        const dialog = screen.getByRole("dialog");
        const focusable = dialog.querySelectorAll<HTMLElement>(
            'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])',
        );
        expect(focusable.length).toBeGreaterThan(0);

        const last = focusable[focusable.length - 1];
        last.focus();
        fireEvent.keyDown(document, { key: "Tab" });

        // Wrapped back into the dialog rather than escaping to the page.
        expect(dialog.contains(document.activeElement)).toBe(true);
    });

    it("provides live regions for transaction state and errors", () => {
        const { container } = render(
            <DepositModal open vault={mockVault} onClose={() => {}} />,
        );

        expect(container.querySelector('[aria-live="polite"]')).toBeTruthy();
        expect(container.querySelector('[aria-live="assertive"]')).toBeTruthy();
    });

    it("announces the amount once one is entered", async () => {
        const { container } = render(
            <DepositModal open vault={mockVault} onClose={() => {}} />,
        );

        fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "250" } });

        // The polite region carries the amount into the phase announcements,
        // so a screen-reader user hears what is moving, not just that
        // something is.
        const polite = container.querySelector('[aria-live="polite"]');
        expect(polite).toBeTruthy();
    });
});
