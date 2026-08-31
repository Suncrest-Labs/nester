"use client";

import { cn } from "@/lib/utils";

interface SkeletonProps extends React.HTMLAttributes<HTMLDivElement> {
  animate?: boolean;
}

export function Skeleton({ className, animate = true, ...props }: SkeletonProps) {
  return (
    <div
      className={cn(
        "rounded-lg bg-black/[0.04] dark:bg-white/[0.04]",
        // `motion-reduce` drops the pulse for users who asked for less motion.
        animate && "animate-pulse motion-reduce:animate-none",
        className
      )}
      {...props}
    />
  );
}

interface SkeletonLineProps {
  width?: string | number;
  height?: string | number;
  className?: string;
}

export function SkeletonLine({ 
  width = "100%", 
  height = "1rem",
  className 
}: SkeletonLineProps) {
  return (
    <Skeleton 
      className={cn("rounded-md", className)}
      style={{ width, height }}
    />
  );
}

interface SkeletonCardProps {
  width?: string | number;
  height?: string | number;
  className?: string;
  children?: React.ReactNode;
}

export function SkeletonCard({ 
  width = "100%", 
  height = "8rem",
  className,
  children
}: SkeletonCardProps) {
  return (
    <Skeleton 
      className={cn("rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] p-6", className)}
      style={{ width, height }}
    >
      {children}
    </Skeleton>
  );
}

interface SkeletonTableProps {
  rows?: number;
  columns?: number;
  className?: string;
}

export function SkeletonTable({ rows = 3, columns = 4, className }: SkeletonTableProps) {
  return (
    <div className={cn("w-full", className)}>
      {/* Table header */}
      <div className="flex gap-4 border-b border-black/[0.05] dark:border-white/[0.05] pb-3.5 mb-4">
        {Array.from({ length: columns }).map((_, i) => (
          <SkeletonLine 
            key={`header-${i}`} 
            width={i === 0 ? "30%" : "20%"} 
            height="0.75rem"
            className="flex-1"
          />
        ))}
      </div>
      
      {/* Table rows */}
      {Array.from({ length: rows }).map((_, rowIndex) => (
        <div key={`row-${rowIndex}`} className="flex gap-4 py-4 border-b border-black/[0.04] dark:border-white/[0.04] last:border-0">
          {Array.from({ length: columns }).map((_, colIndex) => (
            <SkeletonLine 
              key={`cell-${rowIndex}-${colIndex}`}
              width={colIndex === 0 ? "30%" : "20%"} 
              height="1rem"
              className="flex-1"
            />
          ))}
        </div>
      ))}
    </div>
  );
}

interface SkeletonChartProps {
  width?: string | number;
  height?: string | number;
  className?: string;
}

/**
 * Fixed bar heights rather than random ones: the chart skeleton renders on the
 * server too, and randomised inline styles produce a hydration mismatch.
 */
const CHART_BAR_HEIGHTS = [42, 68, 35, 82, 55, 74, 48, 63];

export function SkeletonChart({ 
  width = "100%", 
  height = "12rem",
  className 
}: SkeletonChartProps) {
  return (
    <div className={cn("relative", className)} style={{ width, height }}>
      {/* Chart area */}
      <Skeleton className="w-full h-full rounded-lg" />
      
      {/* Simulated chart lines */}
      <div className="absolute inset-4 flex items-end justify-between">
        {CHART_BAR_HEIGHTS.map((barHeight, i) => (
          <div
            key={i}
            className="bg-black/[0.08] dark:bg-white/[0.08] rounded-t-sm animate-pulse motion-reduce:animate-none"
            style={{
              width: '8px',
              height: `${barHeight}%`,
              animationDelay: `${i * 0.1}s`
            }}
          />
        ))}
      </div>
    </div>
  );
}

interface LoadingRegionProps {
  /** Announced once by assistive tech instead of every shimmering block. */
  label: string;
  className?: string;
  children: React.ReactNode;
}

/**
 * Wraps a group of skeletons in a single busy region. The skeletons themselves
 * are decorative (`aria-hidden` via the container), so screen readers hear one
 * "loading" message rather than a stream of empty boxes.
 */
export function LoadingRegion({ label, className, children }: LoadingRegionProps) {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-live="polite"
      data-testid="loading-region"
      className={className}
    >
      <span className="sr-only">{label}</span>
      <div aria-hidden="true">{children}</div>
    </div>
  );
}
