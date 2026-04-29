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

export interface PortfolioContextPayload {
  walletAddress: string
  positions: Array<{
    vaultId: string
    vaultName: string
    asset: string
    currentValue: number
    apy: number
    yieldEarned: number
  }>
  balances: Record<string, number>
}

export interface ChatAction {
  label: string
  href: string
}

export interface ChatResponse {
  content: string
  confidence?: number
  riskScore?: number
  actions?: ChatAction[]
}

// ── Base fetch helper ─────────────────────────────────────────────────────────

const BASE = process.env.NEXT_PUBLIC_INTELLIGENCE_API_BASE ?? 'http://localhost:8000'

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

function buildMockInsights(): PortfolioInsight[] {
  return [
    {
      title: 'Portfolio Concentration Alert',
      body: 'Your portfolio is concentrated in stable assets. Consider allocating 10-20% to growth vaults for upside while preserving safety.',
      confidence: 0.81,
      action: { label: 'Explore Vaults', href: '/vaults' },
    },
    {
      title: 'Weekly Market Summary',
      body: 'Stellar DeFi volume is steady this week with moderate risk sentiment. Yield spreads remain attractive in conservative and balanced vaults.',
      confidence: 0.74,
      action: { label: 'View Dashboard', href: '/dashboard' },
    },
  ]
}

// ── intelligence client ───────────────────────────────────────────────────────

export const intelligence = {
  /** Per-vault AI commentary and recommendations. */
  getVaultRecommendations: (vaultId: string) =>
    apiFetch<VaultRecommendation>(`/vaults/${vaultId}/recommendations`),

  /** Bull/Bear/Neutral market sentiment summary. */
  getMarketSentiment: () =>
    apiFetch<MarketSentiment>('/market/sentiment'),

  /** Portfolio-level insight cards for a wallet. Falls back to mock data for local UI work. */
  getPortfolioInsights: async (wallet: string): Promise<PortfolioInsight[]> => {
    try {
      const res = await fetch(`${BASE}/intelligence/insights/${wallet}`)
      if (!res.ok) throw new Error('insights request failed')
      const payload = await res.json() as PortfolioInsight[] | { insights?: PortfolioInsight[] }
      if (Array.isArray(payload)) return payload
      if (Array.isArray(payload.insights)) return payload.insights
      return buildMockInsights()
    } catch {
      return buildMockInsights()
    }
  },

  sendMessage: async (
    message: string,
    context: PortfolioContextPayload
  ): Promise<ChatResponse> => {
    try {
      const res = await fetch(`${BASE}/intelligence/chat`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message,
          walletAddress: context.walletAddress,
          portfolio: {
            balances: context.balances,
            positions: context.positions,
          },
        }),
      })

      if (!res.ok) throw new Error('chat request failed')

      const payload = await res.json() as
        | ChatResponse
        | {
            response?: string
            confidence?: number
            riskScore?: number
            actions?: ChatAction[]
          }

      if ('content' in payload && typeof payload.content === 'string') {
        return payload
      }

      return {
        content: payload.response ?? 'Prometheus generated a response.',
        confidence: payload.confidence,
        riskScore: payload.riskScore,
        actions: payload.actions,
      }
    } catch {
      return {
        content:
          "Prometheus is warming up. Based on your current portfolio, consider a balanced allocation and review vault APY trends before rebalancing.\n\n- Keep emergency liquidity in lower-volatility vaults\n- Rotate part of gains into conservative yield when markets are overheated\n\nThis is a mock response while the intelligence service is unavailable.",
        confidence: 0.69,
        riskScore: 0.41,
        actions: [
          { label: 'Deposit to a Vault', href: '/vaults' },
          { label: 'Review Savings', href: '/savings' },
        ],
      }
    }
  },
}