import { type Page, type BrowserContext } from "@playwright/test";

/**
 * A fake Stellar address used across all E2E tests.
 * The portfolio provider seeds 10,000 USDC for any connected address,
 * so tests can deposit/withdraw without needing a real wallet extension.
 */
export const TEST_ADDRESS =
    "GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN";

/**
 * Injects a mock wallet session into localStorage so the WalletProvider
 * treats the browser as already connected — no extension popup needed.
 *
 * The provider reads `nester_wallet_id` and `nester_wallet_addr` on mount
 * and restores the session without calling the wallet extension.
 */
export async function injectWalletSession(
    page: Page,
    address = TEST_ADDRESS
): Promise<void> {
    await page.addInitScript(
        ({ addr, walletId }: { addr: string; walletId: string }) => {
            window.localStorage.setItem("nester_wallet_id", walletId);
            window.localStorage.setItem("nester_wallet_addr", addr);
        },
        { addr: address, walletId: "freighter" }
    );
}

/**
 * Seeds an existing vault position into localStorage so withdrawal tests
 * can run without going through the full deposit flow first.
 *
 * Mirrors the StoredPosition shape used by PortfolioProvider.
 */
export async function seedVaultPosition(
    page: Page,
    address: string,
    opts: {
        vaultId: "conservative" | "balanced" | "growth" | "defi500";
        principal: number;
        /** Days ago the deposit was made — controls yield accrual */
        depositedDaysAgo?: number;
        /** Whether the position should already be matured */
        matured?: boolean;
    }
): Promise<string> {
    const positionId = `test-position-${Date.now()}`;

    await page.addInitScript(
        ({
            key,
            position,
            balances,
        }: {
            key: string;
            position: Record<string, unknown>;
            balances: Record<string, number>;
        }) => {
            const existing = window.localStorage.getItem(key);
            const state = existing
                ? JSON.parse(existing)
                : { balances, positions: [], transactions: [] };

            state.positions = [position, ...(state.positions ?? [])];
            window.localStorage.setItem(key, JSON.stringify(state));
        },
        {
            key: `nester_portfolio_v1:${address}`,
            position: buildPosition(positionId, opts),
            balances: { USDC: 10000, USDT: 2500, XLM: 850 },
        }
    );

    return positionId;
}

function buildPosition(
    id: string,
    opts: {
        vaultId: string;
        principal: number;
        depositedDaysAgo?: number;
        matured?: boolean;
    }
): Record<string, unknown> {
    const vaultMeta: Record<string, { name: string; lockDays: number; apy: number; penalty: number }> = {
        conservative: { name: "Conservative", lockDays: 30, apy: 0.07, penalty: 0.1 },
        balanced:     { name: "Balanced",     lockDays: 45, apy: 0.095, penalty: 0.1 },
        growth:       { name: "Growth",       lockDays: 60, apy: 0.13, penalty: 0.1 },
        defi500:      { name: "DeFi500 Index",lockDays: 90, apy: 0.108, penalty: 0.1 },
    };

    const meta = vaultMeta[opts.vaultId] ?? vaultMeta.conservative;
    const depositedDaysAgo = opts.depositedDaysAgo ?? (opts.matured ? meta.lockDays + 5 : 10);

    const now = new Date();
    const depositedAt = new Date(now);
    depositedAt.setDate(depositedAt.getDate() - depositedDaysAgo);

    const maturityAt = new Date(depositedAt);
    maturityAt.setDate(maturityAt.getDate() + meta.lockDays);

    return {
        id,
        vaultId: opts.vaultId,
        vaultName: meta.name,
        asset: "USDC",
        principal: opts.principal,
        shares: opts.principal,
        apy: meta.apy,
        depositedAt: depositedAt.toISOString(),
        maturityAt: maturityAt.toISOString(),
        earlyWithdrawalPenaltyPct: meta.penalty,
    };
}

/**
 * Clears all Nester localStorage keys to give each test a clean slate.
 * Call this in beforeEach when tests must not share portfolio state.
 */
export async function clearWalletSession(context: BrowserContext): Promise<void> {
    await context.addInitScript(() => {
        const keysToRemove: string[] = [];
        for (let i = 0; i < window.localStorage.length; i++) {
            const key = window.localStorage.key(i);
            if (key && key.startsWith("nester_")) {
                keysToRemove.push(key);
            }
        }
        keysToRemove.forEach((k) => window.localStorage.removeItem(k));
    });
}
