import Link from "next/link";
import { ArrowLeft, Vault } from "lucide-react";

export default function VaultNotFound() {
  return (
    <div className="min-h-screen bg-background flex flex-col items-center justify-center text-center px-4">
      <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-secondary">
        <Vault className="h-8 w-8 text-muted-foreground" />
      </div>
      <h2 className="font-heading text-2xl font-light text-foreground mb-2">
        Vault not found
      </h2>
      <p className="text-sm text-muted-foreground mb-6">
        The vault you&apos;re looking for doesn&apos;t exist or has been removed.
      </p>
      <Link
        href="/dashboard/vaults"
        className="inline-flex items-center gap-2 rounded-full border border-border px-5 py-2 text-sm font-medium hover:border-black/20 transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to Vaults
      </Link>
    </div>
  );
}
