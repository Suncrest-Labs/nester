import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export const PANEL = "rounded-xl border bg-card p-6";
export const COLUMNS = "grid items-start gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]";

export const BUTTON_PRIMARY =
  "inline-flex items-center justify-center gap-1.5 rounded-full bg-foreground px-4 py-2 text-[13px] font-medium text-background transition-opacity hover:opacity-80 disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground focus-visible:ring-offset-2 focus-visible:ring-offset-background";
export const BUTTON_SECONDARY =
  "inline-flex items-center justify-center gap-1.5 rounded-full border px-4 py-2 text-[13px] font-medium text-foreground transition-colors hover:bg-foreground/5 disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground focus-visible:ring-offset-2 focus-visible:ring-offset-background";
export const INPUT =
  "h-11 w-full rounded-lg border bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-foreground/40";
export const SKELETON = "animate-pulse rounded-xl bg-foreground/[0.06]";

export function PageHeader({
  title,
  lede,
  actions,
}: {
  title: string;
  lede?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 className="text-[13px] font-medium text-muted-foreground">{title}</h1>
        {lede && <div className="mt-3 max-w-[60ch]">{lede}</div>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-3">{actions}</div>}
    </header>
  );
}

export function Panel({
  title,
  children,
  className,
  as: Tag = "div",
}: {
  title?: string;
  children: ReactNode;
  className?: string;
  as?: "div" | "section";
}) {
  return (
    <Tag className={cn(PANEL, className)}>
      {title && <h3 className="text-sm font-medium text-muted-foreground">{title}</h3>}
      {children}
    </Tag>
  );
}

export function Section({
  id,
  title,
  lede,
  children,
  className,
}: {
  id: string;
  title: string;
  lede?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section aria-labelledby={id} className={cn("mt-14", className)}>
      <h2 id={id} className="text-[16px] font-semibold text-foreground">
        {title}
      </h2>
      {lede && <p className="mt-1 max-w-[60ch] text-sm text-muted-foreground">{lede}</p>}
      <div className="mt-5">{children}</div>
    </section>
  );
}

export function BigNumber({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <p className={cn("font-heading text-4xl font-light tabular-nums text-foreground", className)}>
      {children}
    </p>
  );
}

export function SegmentedControl<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: { id: T; label: string }[];
  value: T;
  onChange: (id: T) => void;
}) {
  return (
    <div role="group" aria-label={label} className="inline-flex rounded-full border bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          aria-pressed={value === o.id}
          onClick={() => onChange(o.id)}
          className={cn(
            "rounded-full px-3 py-1 text-[13px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground",
            value === o.id ? "bg-foreground text-background" : "text-muted-foreground hover:text-foreground"
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function EmptyPanel({
  title,
  children,
  action,
}: {
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className={PANEL}>
      <p className="text-sm text-foreground">{title}</p>
      {children && <p className="mt-2 max-w-[60ch] text-sm text-muted-foreground">{children}</p>}
      {action && <div className="mt-4 flex flex-wrap gap-3">{action}</div>}
    </div>
  );
}
