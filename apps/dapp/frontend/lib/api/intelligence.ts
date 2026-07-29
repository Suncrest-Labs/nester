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
  contexts?: MarketContextSignal[]
  disclaimer?: string
}

export interface MarketSentimentPoint {
  signal: 'bull' | 'bear' | 'neutral'
  confidence: number
  observed_at: number // unix seconds
}

export interface MarketSentimentHistory {
  days: number
  points: MarketSentimentPoint[]
}

export interface MarketContextSignal {
  protocol: string
  asset?: string | null
  signal_type: 'announcement' | 'security_concern' | 'sentiment_shift' | 'depeg_risk'
  direction: 'positive' | 'negative' | 'neutral'
  confidence: number
  summary: string
  source_url: string
  publisher: string
  observed_at: string
  corroborating_sources: string[]
  advisory_only: true
}

export interface PortfolioInsight {
  id?: string
  title: string
  body: string
  confidence: number
  action?: { label: string; href: string }
}

export interface AllocationItem {
  protocolId: string
  weight: number
}

export interface AllocationRecommendation {
  allocations: AllocationItem[]
  commentary: string
}

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  proposal?: { id: string; text: string; status?: 'pending' | 'executed' | 'declined' }
}

export interface SavingsPlanRequest {
    goal_usdc: number;
    time_horizon_months: number;
    max_monthly_contribution_usdc: number;
    vault_id?: string;
}

export interface ScheduleEntry {
    month: number;
    deposit: number;
    expected_balance: number;
    yield_earned: number;
}

export interface MilestoneProjection {
    month: number;
    expected_balance: number;
}

export interface SavingsPlanResponse {
    achievable: boolean;
    required_monthly_deposit: number;
    monthly_schedule: ScheduleEntry[];
    total_yield_earned: number;
    narrative: string;
    milestones: MilestoneProjection[];
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

export const intelligenceApi = {
  /** Per-vault AI commentary and recommendations. */
  getVaultRecommendations: (vaultId: string) =>
    apiFetch<VaultRecommendation>(`/vaults/${vaultId}/recommendations`),

  /** Bull/Bear/Neutral market sentiment summary. */
  getMarketSentiment: () =>
    apiFetch<MarketSentiment>('/market/sentiment'),

  /** Historical sentiment points (7 or 30 day) for the trend sparkline. */
  getMarketSentimentHistory: (days: 7 | 30 = 7) =>
    apiFetch<MarketSentimentHistory>(`/market/sentiment/history?days=${days}`),

  /** Portfolio-level insight cards for a given user. */
  getPortfolioInsights: (userId: string) =>
    apiFetch<PortfolioInsight[]>(`/portfolio/${userId}/insights`),

  /** Generate a concrete, personalized deposit schedule based on user goals. */
  createSavingsPlan: (request: SavingsPlanRequest) =>
    apiFetch<SavingsPlanResponse>('/intelligence/savings-plan', {
        method: 'POST',
        body: JSON.stringify(request),
    }),

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

  /** AI-suggested allocation weights for vault creation. */
  getAllocationRecommendation: (params: { strategy: string; protocolIds: string[] }) =>
    apiFetch<AllocationRecommendation>('/intelligence/allocation', {
      method: 'POST',
      body: JSON.stringify(params),
    }),

  sendMessage: (userId: string, message: string): EventSource => {
    const params = new URLSearchParams({ userId, message })
    return new EventSource(`${INTELLIGENCE_BASE}/intelligence/chat?${params}`)
  },

  confirmToolAction: (proposalId: string, approved: boolean) =>
    goApiFetch<{ status: string; assistant_message: string }>(`/intelligence/tools/${proposalId}/confirm`, {
      method: 'POST',
      body: JSON.stringify({ approved })
    }),
}

// Export as default or intelligence for backward compatibility if needed
export const intelligence = intelligenceApi;
