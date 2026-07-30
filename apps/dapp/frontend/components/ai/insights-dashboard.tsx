"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import {
  AlertTriangle,
  ArrowRight,
  GraduationCap,
  Newspaper,
  RefreshCw,
  Sparkles,
  Target,
  TrendingUp,
  X,
} from "lucide-react"
import { intelligence, type PortfolioInsight } from "@/lib/api/intelligence"
import { useWallet } from "@/components/wallet-provider"
import { useAuth } from "@/components/auth-provider"
import { InsightCard, InsightCardSkeleton } from "@/components/ai/insightCard"
import { MarketSentimentWidget } from "@/components/ai/marketSentiment"
import { motion } from "framer-motion"
import { cn } from "@/lib/utils"

const INSIGHT_DISMISSAL_KEY_PREFIX = "nester_ai_insight_dismissed_"

function getDismissKey(id: string): string {
  return `${INSIGHT_DISMISSAL_KEY_PREFIX}${id}`
}

function isDismissed(id: string): boolean {
  if (typeof window === "undefined") return false
  return localStorage.getItem(getDismissKey(id)) === "true"
}

function markDismissed(id: string) {
  if (typeof window !== "undefined") {
    localStorage.setItem(getDismissKey(id), "true")
  }
}

interface CoachingContent {
  id: string
  title: string
  body: string
  action?: { label: string; href: string }
}

interface DigestContent {
  id: string
  title: string
  body: string
  date: string
}

interface ProjectionBand {
  month: number
  lower: number
  median: number
  upper: number
}

interface ProjectionData {
  id: string
  title: string
  goalProbability: number
  bands: ProjectionBand[]
  narrative: string
}

function ProjectionChart({ data }: { data: ProjectionData }) {
  if (data.bands.length < 2) return null

  const values = data.bands.flatMap((b) => [b.lower, b.upper, b.median])
  const minV = Math.min(...values)
  const maxV = Math.max(...values)
  const range = maxV - minV || 1
  const W = 400
  const H = 120
  const pad = 10

  const pts = data.bands.map((b, i) => {
    const x = (i / (data.bands.length - 1)) * (W - pad * 2) + pad
    const lowerY = H - pad - ((b.lower - minV) / range) * (H - pad * 2)
    const medianY = H - pad - ((b.median - minV) / range) * (H - pad * 2)
    const upperY = H - pad - ((b.upper - minV) / range) * (H - pad * 2)
    return { x, lowerY, medianY, upperY }
  })

  const bandUpperPath = pts.map(({ x, upperY }) => `${x},${upperY}`).join(" L")
  const bandLowerPath = pts.map(({ x, lowerY }) => `${x},${lowerY}`).reverse().join(" L")
  const bandPath = `M${bandUpperPath} L${bandLowerPath} Z`
  const medianPath = pts.map(({ x, medianY }) => `${x},${medianY}`).join(" L")

  return (
    <div>
      <div className="relative">
        <svg viewBox={`0 0 ${W} ${H}`} className="w-full h-full" preserveAspectRatio="none">
          <defs>
            <linearGradient id="bandGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="rgb(99,102,241)" stopOpacity="0.12" />
              <stop offset="100%" stopColor="rgb(99,102,241)" stopOpacity="0.04" />
            </linearGradient>
          </defs>
          <path d={bandPath} fill="url(#bandGrad)" />
          <path d={`M${medianPath}`} fill="none" stroke="rgb(99,102,241)" strokeWidth="1.5" />
          <path d={`M${bandUpperPath}`} fill="none" stroke="rgb(99,102,241)" strokeWidth="0.5" strokeDasharray="3 2" opacity="0.4" />
          <path d={`M${bandLowerPath}`} fill="none" stroke="rgb(99,102,241)" strokeWidth="0.5" strokeDasharray="3 2" opacity="0.4" />
        </svg>
      </div>
      <div className="mt-2 flex items-center gap-3 text-[10px] text-muted-foreground">
        <span className="flex items-center gap-1">
          <span className="h-1.5 w-3 rounded-full bg-indigo-400/30" />
          Confidence band (90%)
        </span>
        <span className="flex items-center gap-1">
          <span className="h-0.5 w-3 rounded-full bg-indigo-500" />
          Median projection
        </span>
      </div>
    </div>
  )
}

function GoalSuccessBadge({ probability }: { probability: number }) {
  const pct = Math.round(probability * 100)
  const color = pct >= 70 ? "text-emerald-600" : pct >= 40 ? "text-amber-600" : "text-red-600"
  const bg = pct >= 70 ? "bg-emerald-50" : pct >= 40 ? "bg-amber-50" : "bg-red-50"

  return (
    <span className={cn("inline-flex items-center rounded-full px-2.5 py-1 text-[11px] font-medium", bg, color)}>
      {pct}% goal probability
    </span>
  )
}

interface Props {
  showHeader?: boolean
}

type InsightItem = {
  type: "recommendation" | "projection" | "coaching" | "digest"
  data: PortfolioInsight | ProjectionData | CoachingContent | DigestContent
}

export function InsightsDashboard({ showHeader = true }: Props) {
  const { address } = useWallet()
  const { userId } = useAuth()

  const [showPrometheus, setShowPrometheus] = useState(false)
  const [selectedTab, setSelectedTab] = useState<"all" | "recommendations" | "projections" | "coaching" | "digest">("all")
  const [dismissedIds, setDismissedIds] = useState<Set<string>>(new Set())

  const insightsQuery = useQuery({
    queryKey: ["ai-insights", address],
    queryFn: () => intelligence.getPortfolioInsights(userId || address || ""),
    enabled: !!(userId || address),
    staleTime: 60_000,
  })

  const coachingQuery = useQuery({
    queryKey: ["ai-coaching", address],
    queryFn: () =>
      intelligence.coaching({
        goal: {
          target_amount: 10000,
          currency: "USD",
          deadline: new Date(Date.now() + 365 * 86400000).toISOString(),
          current_amount: 1000,
          progress_pct: 10,
        },
        portfolio: { total_balance_usd: 0, vaults: [] },
      }),
    enabled: false,
    retry: 1,
  })

  const marketSentimentQuery = useQuery({
    queryKey: ["market-sentiment"],
    queryFn: () => intelligence.getMarketSentiment(),
    staleTime: 300_000,
  })

  const handleDismiss = useCallback((id: string) => {
    markDismissed(id)
    setDismissedIds((prev) => new Set(prev).add(id))
  }, [])

  const filteredInsights = useMemo(() => {
    return (insightsQuery.data || []).filter(
      (insight) => !dismissedIds.has(insight.id || insight.title) && !isDismissed(insight.id || insight.title),
    )
  }, [insightsQuery.data, dismissedIds])

  const allItems: InsightItem[] = useMemo(() => {
    const items: InsightItem[] = []

    if (filteredInsights.length > 0) {
      for (const insight of filteredInsights) {
        items.push({ type: "recommendation", data: insight })
      }
    }

    return items
  }, [filteredInsights])

  const tabAllItems = allItems

  const tabFiltered = useMemo(() => {
    if (selectedTab === "all") return tabAllItems
    return tabAllItems.filter((item) => {
      if (selectedTab === "recommendations") return item.type === "recommendation"
      if (selectedTab === "projections") return item.type === "projection"
      if (selectedTab === "coaching") return item.type === "coaching"
      if (selectedTab === "digest") return item.type === "digest"
      return true
    })
  }, [tabAllItems, selectedTab])

  const tabs = [
    { id: "all" as const, label: "All" },
    { id: "recommendations" as const, label: "Recommendations" },
    { id: "projections" as const, label: "Projections" },
    { id: "coaching" as const, label: "Coaching" },
    { id: "digest" as const, label: "Digest" },
  ]

  const isLoading = insightsQuery.isLoading
  const isError = insightsQuery.isError
  const isEmpty = !isLoading && !isError && allItems.length === 0

  const handleRetry = () => {
    insightsQuery.refetch()
  }

  useEffect(() => {
    if (!showPrometheus) return
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setShowPrometheus(false)
    }
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [showPrometheus])

  if (!address) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-center">
        <Sparkles className="h-12 w-12 text-muted-foreground/30 mb-4" />
        <p className="text-lg font-medium text-muted-foreground">Connect your wallet to see AI insights</p>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-4xl space-y-6 py-6 px-4 sm:px-6">
      {showHeader && (
        <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">AI Insights</h1>
            <p className="text-sm text-muted-foreground mt-1">
              Personalized recommendations, projections, and coaching powered by Prometheus
            </p>
          </div>
          <button
            type="button"
            onClick={() => setShowPrometheus(true)}
            className="inline-flex items-center gap-2 rounded-full border border-border bg-background px-4 py-2 text-sm font-medium transition-colors hover:bg-secondary"
          >
            <Sparkles className="h-4 w-4" />
            Ask Prometheus
          </button>
        </motion.div>
      )}

      {isError && (
        <motion.div
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          className="rounded-2xl border border-red-200 bg-red-50 p-6"
        >
          <div className="flex items-start gap-3">
            <AlertTriangle className="h-5 w-5 text-red-500 shrink-0 mt-0.5" />
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-red-800">AI service temporarily unavailable</p>
              <p className="text-sm text-red-600/80 mt-1">
                Your core dashboard is unaffected. Insights will reappear when the service recovers.
              </p>
              <button
                type="button"
                onClick={handleRetry}
                className="mt-3 inline-flex items-center gap-1.5 rounded-full bg-red-100 px-3 py-1.5 text-xs font-medium text-red-700 transition-colors hover:bg-red-200"
              >
                <RefreshCw className="h-3 w-3" />
                Retry
              </button>
            </div>
          </div>
        </motion.div>
      )}

      <div className="flex gap-2 overflow-x-auto pb-2 scrollbar-none">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => setSelectedTab(tab.id)}
            className={cn(
              "shrink-0 rounded-full px-4 py-1.5 text-xs font-medium transition-colors",
              selectedTab === tab.id
                ? "bg-foreground text-background"
                : "bg-secondary text-muted-foreground hover:text-foreground",
            )}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {marketSentimentQuery.data && (
        <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }}>
          <MarketSentimentWidget />
        </motion.div>
      )}

      {isLoading && (
        <div className="space-y-3">
          {[1, 2, 3].map((i) => (
            <InsightCardSkeleton key={i} />
          ))}
        </div>
      )}

      {isEmpty && !isLoading && (
        <motion.div
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          className="flex flex-col items-center justify-center py-16 text-center"
        >
          <Sparkles className="h-16 w-16 text-muted-foreground/20 mb-4" />
          <p className="text-lg font-medium text-muted-foreground">No insights yet</p>
          <p className="text-sm text-muted-foreground/60 mt-2 max-w-md">
            Start depositing into vaults and setting savings goals. Prometheus will begin generating personalized insights
            as it learns about your financial patterns.
          </p>
          <Link
            href="/vaults"
            className="mt-6 inline-flex items-center gap-2 rounded-full bg-foreground px-5 py-2.5 text-sm font-medium text-background transition-opacity hover:opacity-90"
          >
            <TrendingUp className="h-4 w-4" />
            Explore vaults
            <ArrowRight className="h-4 w-4" />
          </Link>
        </motion.div>
      )}

      {tabFiltered.length === 0 && !isLoading && !isEmpty && (
        <div className="flex flex-col items-center justify-center py-10 text-center">
          <p className="text-sm text-muted-foreground">No {selectedTab === "all" ? "" : selectedTab} insights available</p>
        </div>
      )}

      <div className="space-y-4">
        {tabFiltered.map((item, index) => (
          <motion.div
            key={`${item.type}-${(item.data as PortfolioInsight).id || index}`}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: index * 0.03 }}
          >
            {item.type === "recommendation" && (
              <InsightCard
                {...(item.data as PortfolioInsight)}
                onDismiss={() => handleDismiss((item.data as PortfolioInsight).id || (item.data as PortfolioInsight).title)}
              />
            )}
            {item.type === "projection" && (
              <div className="rounded-2xl border border-border bg-card p-5">
                <div className="flex items-start justify-between gap-3 mb-4">
                  <div className="flex items-center gap-2">
                    <div className="flex h-7 w-7 items-center justify-center rounded-xl bg-secondary">
                      <Target className="h-3.5 w-3.5 text-muted-foreground" />
                    </div>
                    <p className="text-sm font-medium">{(item.data as ProjectionData).title}</p>
                  </div>
                  <GoalSuccessBadge probability={(item.data as ProjectionData).goalProbability} />
                </div>
                <ProjectionChart data={item.data as ProjectionData} />
                <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
                  {(item.data as ProjectionData).narrative}
                </p>
                <p className="mt-2 text-[10px] text-muted-foreground/50 italic">
                  This is a probabilistic projection, not a guaranteed outcome. Actual results may vary.
                </p>
              </div>
            )}
            {item.type === "coaching" && (
              <div className="rounded-2xl border border-border bg-card p-5">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <div className="flex h-7 w-7 items-center justify-center rounded-xl bg-secondary">
                      <GraduationCap className="h-3.5 w-3.5 text-muted-foreground" />
                    </div>
                    <div>
                      <p className="text-sm font-medium">{(item.data as CoachingContent).title}</p>
                      <p className="text-xs text-muted-foreground">Savings coach</p>
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => handleDismiss((item.data as CoachingContent).id)}
                    className="rounded-lg p-1 text-muted-foreground/40 hover:bg-secondary hover:text-foreground transition-colors"
                    aria-label="Dismiss coaching"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
                <p className="mt-3 pl-9 text-xs leading-relaxed text-muted-foreground">{(item.data as CoachingContent).body}</p>
                {(item.data as CoachingContent).action && (
                  <div className="mt-3 pl-9">
                    <Link
                      href={(item.data as CoachingContent).action!.href}
                      className="inline-flex items-center rounded-full border border-border bg-background px-3 py-1.5 text-[11px] font-medium transition-colors hover:bg-secondary"
                    >
                      {(item.data as CoachingContent).action!.label}
                    </Link>
                  </div>
                )}
              </div>
            )}
            {item.type === "digest" && (
              <div className="rounded-2xl border border-border bg-card p-5">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <div className="flex h-7 w-7 items-center justify-center rounded-xl bg-secondary">
                      <Newspaper className="h-3.5 w-3.5 text-muted-foreground" />
                    </div>
                    <div>
                      <p className="text-sm font-medium">{(item.data as DigestContent).title}</p>
                      <p className="text-xs text-muted-foreground">{(item.data as DigestContent).date}</p>
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => handleDismiss((item.data as DigestContent).id)}
                    className="rounded-lg p-1 text-muted-foreground/40 hover:bg-secondary hover:text-foreground transition-colors"
                    aria-label="Dismiss digest"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
                <p className="mt-3 pl-9 text-xs leading-relaxed text-muted-foreground">{(item.data as DigestContent).body}</p>
              </div>
            )}
          </motion.div>
        ))}
      </div>

      {allItems.length > 0 && !isLoading && (
        <p className="text-center text-[10px] text-muted-foreground/40 pt-2">
          AI insights are informational and not financial advice. Past performance does not guarantee future results.
        </p>
      )}

      {showPrometheus && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 px-4" role="dialog" aria-modal="true">
          <div className="w-full max-w-lg rounded-2xl bg-card border border-border p-6 shadow-xl">
            <div className="flex items-center justify-between mb-4">
              <h3 className="font-semibold text-sm">Ask Prometheus</h3>
              <button
                type="button"
                onClick={() => setShowPrometheus(false)}
                className="rounded-lg p-1 text-muted-foreground hover:bg-secondary"
                aria-label="Close Ask Prometheus modal"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <p className="text-xs text-muted-foreground mb-4">
              Ask anything about your portfolio, savings goals, or market conditions.
            </p>
            <Link
              href="/dashboard"
              className="block w-full rounded-full bg-foreground py-2.5 text-center text-sm font-medium text-background transition-opacity hover:opacity-90"
            >
              Open Prometheus Chat
            </Link>
          </div>
        </div>
      )}
    </div>
  )
}
