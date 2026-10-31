import { formatPercent } from "./format";

interface RateHeroProps {
  rate7d: string | null;
  rate30d: string | null;
}

const NO_HISTORY = "Not enough history yet";

export function RateHero({ rate7d, rate30d }: RateHeroProps) {
  return (
    <section aria-labelledby="savings-rate-label" className="py-2">
      {rate7d === null ? (
        <p className="font-heading text-3xl font-light text-muted-foreground sm:text-4xl">
          {NO_HISTORY}
        </p>
      ) : (
        <p className="font-heading text-6xl font-light tabular-nums tracking-tight text-foreground sm:text-7xl">
          {formatPercent(rate7d, 2)}
        </p>
      )}
      <p id="savings-rate-label" className="mt-3 text-sm text-muted-foreground">
        Realized rate, last 7 days — not guaranteed
      </p>
      <p className="mt-1 text-sm text-foreground/70">
        {rate30d === null ? (
          <>Last 30 days: {NO_HISTORY}</>
        ) : (
          <>
            Last 30 days: <span className="tabular-nums">{formatPercent(rate30d, 2)}</span>
          </>
        )}
      </p>
    </section>
  );
}
