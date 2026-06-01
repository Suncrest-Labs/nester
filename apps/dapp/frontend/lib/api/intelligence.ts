// ── Types ─────────────────────────────────────────────────────────────────────

export interface VaultRecommendation {
  vaultId: string
  commentary: string
  percentileRank: number   // 0–100, e.g. 78 = "top 22% for its risk profile"
  recommendations: string[]
  confidence: number       // 0–1
}

export interface VaultSplitRecommendation {
  vault_id: string
  allocation_pct: number
  rationale: string
}

export interface VaultRecommendationPlan {
  recommended_vaults: VaultSplitRecommendation[]
  expected_yield_usdc: number
  confidence: 'high' | 'medium' | 'low'
}

export interface AnalyzeRecommendation {
  action: string
  rationale: string
  confidence: 'high' | 'medium' | 'low'
  confidence_reason: string
  data_freshness: string
  disclaimer: string
}

export interface VaultRecommendationInput {
  risk_tolerance: 'conservative' | 'moderate' | 'aggressive'
  time_horizon_months: number
  initial_deposit_usdc: number
  savings_goal?: string
}

export interface CoachingDepositItem {
  date: string
  amount_usdc: number
  note?: string
}

export interface CoachingResponse {
  progress_assessment: string
  deposit_schedule: CoachingDepositItem[]
  nudges: string[]
  confidence: string
}

export interface CoachingRequest {
  goal: {
    target_amount: number
    currency: string
    deadline: string
    description?: string
    current_amount: number
    progress_pct: number
  }
  portfolio: {
    total_balance_usd: number
    vaults: Array<Record<string, unknown>>
  }
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

import config from '@/lib/config'
import { getStoredToken } from '@/lib/api/client'

const INTELLIGENCE_BASE = '/api/v1'
const GO_API_BASE = config.apiUrl

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${INTELLIGENCE_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  })
  if (!res.ok) {
    throw new Error(`Intelligence API error ${res.status}: ${path}`)
  }
  return res.json() as Promise<T>
}

async function goApiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getStoredToken()
  const res = await fetch(`${GO_API_BASE}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init?.headers,
    },
    ...init,
  })
  const json = await res.json() as { success: boolean; data: T; error?: { message: string } }
  if (!res.ok || !json.success) {
    throw new Error(json.error?.message ?? `API error ${res.status}: ${path}`)
  }
  return json.data
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

  recommendVault: (input: VaultRecommendationInput) =>
    apiFetch<VaultRecommendationPlan>('/recommend/vault', {
      method: 'POST',
      body: JSON.stringify(input),
    }),

  coaching: (input: CoachingRequest) =>
    goApiFetch<CoachingResponse>('/intelligence/coaching', {
      method: 'POST',
      body: JSON.stringify(input),
    }),

  /** Yield recommendation (GET) — proxied through Go API when using goApiFetch. */
  getRecommendVault: () => goApiFetch<VaultRecommendationPlan>('/intelligence/recommend/vault'),

  analyze: (prompt: string) =>
    apiFetch<AnalyzeRecommendation>('/analyze', {
      method: 'POST',
      body: JSON.stringify({ prompt }),
    }),

  sendMessage: (userId: string, message: string): EventSource => {
    const params = new URLSearchParams({ userId, message })
    return new EventSource(`${INTELLIGENCE_BASE}/intelligence/chat?${params}`)
  },
}