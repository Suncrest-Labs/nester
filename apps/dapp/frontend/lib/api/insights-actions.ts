// ── Insight action types ──────────────────────────────────────────────────────
//
// Discriminated union that the recommendation engine can return. The Feed
// component routes each action to the correct pre-filled flow via
// `buildActionHref` below.
//
// Why a discriminated union rather than free-form { href } strings?
//  - The URL contract is enforced at compile time: a "deposit" action MUST
//    carry a vaultId; a "schedule" action MUST carry a target.
//  - Adding a new action type is a single TS exhaustiveness check.
//  - The builder is the single source of truth for routing — it can't drift
//    from the types.

export type InsightAction =
  | { kind: 'deposit'; label: string; vaultId: string; amount?: string }
  | { kind: 'lock'; label: string; vaultId: string }
  | { kind: 'schedule'; label: string; targetUsdc?: string; horizonMonths?: number }
  | { kind: 'vault'; label: string; vaultId: string }
  | { kind: 'rebalance'; label: string; vaultId: string }
  | { kind: 'url'; label: string; href: string }

/** Convert a typed action into the URL the user should land on. */
export function buildActionHref(action: InsightAction): string {
  switch (action.kind) {
    case 'deposit': {
      const params = action.amount ? `?amount=${encodeURIComponent(action.amount)}` : ''
      return `/vaults/${encodeURIComponent(action.vaultId)}/deposit${params}`
    }
    case 'lock':
      return `/vaults/${encodeURIComponent(action.vaultId)}/lock`
    case 'rebalance':
      return `/vaults/${encodeURIComponent(action.vaultId)}/rebalance`
    case 'vault':
      return `/vaults/${encodeURIComponent(action.vaultId)}`
    case 'schedule': {
      const search = new URLSearchParams()
      if (action.targetUsdc) search.set('target', action.targetUsdc)
      if (action.horizonMonths) search.set('horizon', String(action.horizonMonths))
      const query = search.toString()
      return `/savings-plan${query ? `?${query}` : ''}`
    }
    case 'url':
      return action.href
  }
}

/** Convert a legacy `PortfolioInsight.action = { label, href }` into a typed action. */
export function adaptLegacyAction(
  legacy: { label: string; href: string } | undefined,
): InsightAction | undefined {
  if (!legacy) return undefined
  // Best-effort: if the href looks like /vaults/{id}/{verb}, split it.
  const m = legacy.href.match(/^\/vaults\/([^/]+)\/(deposit|lock|rebalance)(?:\?(.*))?$/)
  if (m) {
    const [, vaultId, verb, qs] = m
    const params = new URLSearchParams(qs ?? '')
    const amount = params.get('amount') ?? undefined
    if (verb === 'deposit') return { kind: 'deposit', label: legacy.label, vaultId, amount }
    if (verb === 'lock') return { kind: 'lock', label: legacy.label, vaultId }
    if (verb === 'rebalance') return { kind: 'rebalance', label: legacy.label, vaultId }
  }
  // If the engine sent an unparseable href, fall back to a generic url action.
  return { kind: 'url', label: legacy.label, href: legacy.href }
}
