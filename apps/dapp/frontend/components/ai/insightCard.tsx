'use client'

import Link from 'next/link'
import { ChevronDown, ChevronUp, Sparkles } from 'lucide-react'
import { useState } from 'react'
import { type PortfolioInsight } from '@/lib/api/intelligence'

interface InsightCardProps extends PortfolioInsight {
  /** Optional AI reasoning text shown in the expandable toggle. */
  reasoning?: string
}

export function InsightCard({
  title,
  body,
  confidence,
  action,
  reasoning,
}: InsightCardProps) {
  const [showReasoning, setShowReasoning] = useState(false)

  const confidencePct = Math.round(confidence * 100)

  // Color the confidence badge based on score
  const badgeClass =
    confidencePct >= 80
      ? 'bg-emerald-100 text-emerald-700'
      : confidencePct >= 60
        ? 'bg-amber-100 text-amber-700'
        : 'bg-red-100 text-red-700'

  return (
    <div className="rounded-2xl border border-border bg-white p-4 transition-all hover:border-black/15 hover:shadow-sm">
      {/* Header */}
      <div className="mb-2 flex items-start justify-between gap-3">
        <div className="flex items-center gap-2">
          <div className="flex h-7 w-7 items-center justify-center rounded-xl bg-secondary">
            <Sparkles className="h-3.5 w-3.5 text-foreground/50" />
          </div>
          <p className="text-sm font-medium text-foreground">{title}</p>
        </div>
        {/* Confidence badge */}
        <span className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold ${badgeClass}`}>
          {confidencePct}% confidence
        </span>
      </div>

      {/* Body */}
      <p className="pl-9 text-xs leading-relaxed text-muted-foreground">{body}</p>

      {/* Reasoning toggle */}
      {reasoning && (
        <div className="mt-3 pl-9">
          <button
            type="button"
            onClick={() => setShowReasoning((v) => !v)}
            className="flex items-center gap-1 text-[10px] font-medium text-foreground/50 hover:text-foreground/80 transition-colors"
          >
            {showReasoning ? (
              <ChevronUp className="h-3 w-3" />
            ) : (
              <ChevronDown className="h-3 w-3" />
            )}
            {showReasoning ? 'Hide reasoning' : 'Show reasoning'}
          </button>
          {showReasoning && (
            <p className="mt-2 rounded-xl border border-border bg-secondary/20 px-3 py-2 text-[11px] leading-relaxed text-muted-foreground">
              {reasoning}
            </p>
          )}
        </div>
      )}

      {/* Optional action */}
      {action && (
        <div className="mt-3 pl-9">
          <Link
            href={action.href}
            className="inline-flex items-center rounded-full border border-black/10 bg-white px-3 py-1.5 text-[11px] font-medium text-foreground shadow-sm transition-colors hover:border-black/20"
          >
            {action.label}
          </Link>
        </div>
      )}
    </div>
  )
}

// ── Skeleton ──────────────────────────────────────────────────────────────────

export function InsightCardSkeleton() {
  return (
    <div className="rounded-2xl border border-border bg-white p-4 animate-pulse">
      <div className="mb-2 flex items-center gap-2">
        <div className="h-7 w-7 rounded-xl bg-secondary" />
        <div className="h-3.5 w-36 rounded bg-secondary" />
        <div className="ml-auto h-4 w-20 rounded-full bg-secondary" />
      </div>
      <div className="pl-9 space-y-1.5">
        <div className="h-3 w-full rounded bg-secondary" />
        <div className="h-3 w-4/5 rounded bg-secondary" />
      </div>
    </div>
  )
}