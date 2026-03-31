import { Clock, AlertTriangle } from "lucide-react";

interface VaultTermsProps {
  maturityTerms: string;
  earlyWithdrawalPenalty: string;
}

export function VaultTerms({
  maturityTerms,
  earlyWithdrawalPenalty,
}: VaultTermsProps) {
  return (
    <div className="rounded-2xl border border-border bg-white p-5">
      <p className="font-heading text-sm font-medium text-foreground mb-3">
        Terms
      </p>
      <div className="divide-y divide-border">
        <div className="flex items-start justify-between py-2.5">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Clock className="h-3.5 w-3.5 shrink-0" />
            Maturity
          </div>
          <span className="text-xs font-medium text-foreground text-right max-w-[60%]">
            {maturityTerms}
          </span>
        </div>
        <div className="flex items-start justify-between py-2.5">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
            Early Exit
          </div>
          <span className="text-xs font-medium text-foreground text-right max-w-[60%]">
            {earlyWithdrawalPenalty}
          </span>
        </div>
      </div>
    </div>
  );
}
