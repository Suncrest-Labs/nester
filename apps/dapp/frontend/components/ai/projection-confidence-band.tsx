'use client'

/**
 * ProjectionConfidenceBand
 *
 * Renders a probabilistic projection as a confidence BAND, not a single
 * deterministic line. Honest uncertainty is the point — a single line in a
 * financial UI is a trust liability.
 *
 * Today the underlying projection API returns a deterministic timeline
 * (see `lib/api/projection.ts` → `projectionApi.calculateProjection`). This
 * component therefore derives an estimated uncertainty band from the user's
 * AI confidence score: a 0–1 confidence shrinks/widens the band around the
 * center line, and we LABEL the band clearly so users never confuse an
 * estimated range with a guaranteed forecast.
 *
 * When the real Monte Carlo engine ships (companion issue to #868), this
 * component will switch to consuming the engine's quantiles directly — the
 * SVG contract below stays the same.
 */

import { useMemo } from 'react'
import { AlertTriangle } from 'lucide-react'

export interface ProjectionPoint {
  /** ISO month label or month index. */
  label: string
  /** Center (expected) value. */
  value: number
}

export interface ProjectionConfidenceBandProps {
  /** Sorted chronological projection points. */
  points: ProjectionPoint[]
  /** 0–1 AI confidence; higher = tighter band. */
  confidence: number
  /** Goal value to derive a probability chip ("X% likely to reach goal"). */
  goalValue?: number
  /** Compact mode for embedding on the main dashboard. */
  compact?: boolean
}

/**
 * Width of the band, as a fraction of the centre value, when confidence is 0.
 * When confidence is 1, the band collapses to zero. Tuned so a 0.5 confidence
 * (e.g. "medium") gives a ±15% band — wide enough to be honest, narrow
 * enough to be visually informative.
 */
const MAX_BAND_FRACTION = 0.30

export function ProjectionConfidenceBand({
  points,
  confidence,
  goalValue,
  compact = false,
}: ProjectionConfidenceBandProps) {
  const safeConfidence = Math.max(0, Math.min(1, confidence))
  const bandFraction = (1 - safeConfidence) * MAX_BAND_FRACTION

  const { upperPath, lowerPath, centerPath, height, width, maxValue } = useMemo(
    () => buildPaths(points, bandFraction),
    [points, bandFraction],
  )

  if (points.length < 2) {
    return (
      <div className="rounded-xl border border-border bg-secondary/20 p-4 text-xs text-muted-foreground">
        Not enough data points to render a confidence band.
      </div>
    )
  }

  // Estimated probability of reaching goal (last point's upper band vs goal).
  const goalProbability = useMemo(() => {
    if (goalValue === undefined || points.length === 0) return undefined
    const final = points[points.length - 1]
    // Crude probability proxy: ratio of upper-band to goal.
    const upper = final.value * (1 + bandFraction)
    if (upper <= 0) return 0
    // If upper >= goal, cap at 95% (never claim certainty).
    if (upper >= goalValue) return Math.min(0.95, 0.6 + safeConfidence * 0.3)
    // Otherwise scale linearly.
    return Math.min(0.9, (upper / goalValue) * 0.9)
  }, [goalValue, points, bandFraction, safeConfidence])

  const probabilityLabel =
    goalProbability !== undefined
      ? `${Math.round(goalProbability * 100)}% likely to reach goal`
      : null

  return (
    <div
      className={`rounded-2xl border border-border bg-white dark:bg-[#100F0F] ${
        compact ? 'p-3' : 'p-4'
      }`}
      role="figure"
      aria-label={
        probabilityLabel
          ? `Projection confidence band. ${probabilityLabel}.`
          : 'Projection confidence band.'
      }
    >
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <AlertTriangle className="h-3.5 w-3.5 text-amber-500" aria-hidden="true" />
          <p className={`font-medium text-foreground ${compact ? 'text-xs' : 'text-sm'}`}>
            Estimated projection
          </p>
        </div>
        {probabilityLabel && (
          <span
            className={`shrink-0 rounded-full bg-secondary px-2.5 py-0.5 ${
              compact ? 'text-[10px]' : 'text-[11px]'
            } font-medium text-foreground/70`}
          >
            {probabilityLabel}
          </span>
        )}
      </div>

      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="h-32 w-full"
        preserveAspectRatio="none"
        role="img"
        aria-hidden="true"
      >
        <defs>
          <linearGradient id="projectionBand" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="rgb(99,102,241)" stopOpacity="0.22" />
            <stop offset="100%" stopColor="rgb(99,102,241)" stopOpacity="0.04" />
          </linearGradient>
        </defs>
        {/* Band as a single closed polygon */}
        <path d={`${upperPath} ${lowerPath}`} fill="url(#projectionBand)" />
        {/* Upper and lower edges */}
        <path
          d={upperPath}
          fill="none"
          stroke="rgb(99,102,241)"
          strokeOpacity="0.35"
          strokeWidth="1"
          strokeDasharray="2 2"
        />
        <path
          d={lowerPath}
          fill="none"
          stroke="rgb(99,102,241)"
          strokeOpacity="0.35"
          strokeWidth="1"
          strokeDasharray="2 2"
        />
        {/* Center line */}
        <path
          d={centerPath}
          fill="none"
          stroke="rgb(99,102,241)"
          strokeWidth="2"
        />
      </svg>

      <p className="mt-2 text-[10px] leading-relaxed text-muted-foreground">
        Estimated ±{Math.round(bandFraction * 100)}% range based on AI confidence
        ({Math.round(safeConfidence * 100)}%).{' '}
        <span className="font-medium">Not financial advice.</span> Past returns and
        current APY are not guarantees of future yield.
        {maxValue > 0 && goalValue !== undefined && (
          <>
            {' '}
            Final projected balance ≈{' '}
            {Math.round(points[points.length - 1].value).toLocaleString()} of{' '}
            {Math.round(goalValue).toLocaleString()} target.
          </>
        )}
      </p>
    </div>
  )
}

// ── Pure path builders (testable in isolation) ────────────────────────────────

export function buildPaths(
  points: ProjectionPoint[],
  bandFraction: number,
): {
  upperPath: string
  lowerPath: string
  centerPath: string
  width: number
  height: number
  maxValue: number
} {
  const width = 320
  const height = 110
  const padX = 6
  const padY = 8

  if (points.length === 0) {
    return {
      upperPath: '',
      lowerPath: '',
      centerPath: '',
      width,
      height,
      maxValue: 0,
    }
  }

  const values = points.map((p) => p.value)
  const minVal = Math.min(0, ...values) // anchor at 0
  const maxValRaw = Math.max(...values)
  const maxVal = maxValRaw <= 0 ? 1 : maxValRaw

  const xStep = (width - padX * 2) / Math.max(1, points.length - 1)

  const toY = (v: number) => {
    const ratio = (v - minVal) / (maxVal - minVal || 1)
    return height - padY - ratio * (height - padY * 2)
  }

  const upper: Array<[number, number]> = []
  const lower: Array<[number, number]> = []
  const center: Array<[number, number]> = []

  points.forEach((p, i) => {
    const x = padX + i * xStep
    const upperY = toY(p.value * (1 + bandFraction))
    const lowerY = toY(p.value * (1 - bandFraction))
    const centerY = toY(p.value)
    upper.push([x, upperY])
    lower.push([x, lowerY])
    center.push([x, centerY])
  })

  const upperPath = `M ${upper.map(([x, y]) => `${x} ${y}`).join(' L ')}`
  const lowerPath = `M ${lower.map(([x, y]) => `${x} ${y}`).join(' L ')}`
  const centerPath = `M ${center.map(([x, y]) => `${x} ${y}`).join(' L ')}`

  return {
    upperPath,
    lowerPath,
    centerPath,
    width,
    height,
    maxValue: maxVal,
  }
}
