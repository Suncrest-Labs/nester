import { Users, TrendingUp, Wallet as WalletIcon } from "lucide-react";

const POSITION_ROWS = [
  { label: "Amount Deposited", Icon: WalletIcon },
  { label: "Yield Earned", Icon: TrendingUp },
  { label: "Share of Vault", Icon: Users },
] as const;

export function UserPosition() {
  return (
    <div className="rounded-2xl border border-border bg-white p-5">
      <div className="flex items-center justify-between mb-3">
        <p className="font-heading text-sm font-medium text-foreground">
          Your Position
        </p>
        <div className="flex items-center gap-1.5">
          <div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
          <span className="text-xs text-muted-foreground">Live</span>
        </div>
      </div>
      <div className="divide-y divide-border">
        {POSITION_ROWS.map(({ label, Icon }) => (
          <div
            key={label}
            className="flex items-center justify-between py-2.5"
          >
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Icon className="h-3.5 w-3.5" />
              {label}
            </div>
            <span className="text-sm font-medium text-foreground/30">—</span>
          </div>
        ))}
      </div>
    </div>
  );
}
