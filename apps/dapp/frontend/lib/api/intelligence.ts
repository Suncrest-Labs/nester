// ── Types ─────────────────────────────────────────────────────────────────────

export interface VaultRecommendation {
  vaultId: string
  commentary: string
  percentileRank: number   // 0–100, e.g. 78 = "top 22% for its risk profile"
  recommendations: string[]
  confidence: number       // 0–1
}

export interface MarketSentiment {
  signal: 'bull' | 'bear' | 'neutral'
  summary: string
  confidence: number
  updatedAt: string        // ISO timestamp
}

export interface PortfolioInsight {
  title: string
  body: string
  confidence: number
  action?: { label: string; href: string }
}

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
}

// ── Base fetch helper ─────────────────────────────────────────────────────────

const BASE = '/api/v1'

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  })
  if (!res.ok) {
    throw new Error(`Intelligence API error ${res.status}: ${path}`)
  }
  return res.json() as Promise<T>
}

// ── intelligence client ───────────────────────────────────────────────────────

export const intelligence = {
  /** Per-vault AI commentary and recommendations. */
  getVaultRecommendations: (vaultId: string) =>
    apiFetch<VaultRecommendation>(`/vaults/${vaultId}/recommendations`),

  /** Bull/Bear/Neutral market sentiment summary. */
  getMarketSentiment: () =>
    apiFetch<MarketSentiment>('/market/sentiment'),

  /** Portfolio-level insight cards for a given user. */
  getPortfolioInsights: (userId: string) =>
    apiFetch<PortfolioInsight[]>(`/portfolio/${userId}/insights`),

  sendMessage: (userId: string, message: string): EventSource => {
    const params = new URLSearchParams({ userId, message })
    return new EventSource(`${BASE}/intelligence/chat?${params}`)
  },
}