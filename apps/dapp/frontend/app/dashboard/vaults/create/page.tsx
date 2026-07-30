import { CreateVaultWizard } from "../../../../components/vault/CreateVaultWizard";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { Metadata } from "next";

export const metadata: Metadata = {
  title: "Create Vault | Nester",
  description: "Create a new DeFi savings vault with custom allocation strategies.",
};

export default function CreateVaultPage() {
  return (
    <div className="min-h-screen bg-slate-950 p-4 md:p-8">
      <div className="max-w-4xl mx-auto mb-8">
        <Link 
          href="/dashboard/vaults" 
          className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-white transition-colors mb-6"
        >
          <ArrowLeft className="w-4 h-4" />
          Back to Vaults
        </Link>
        
        <h1 className="text-3xl md:text-4xl font-bold text-white mb-2 tracking-tight">
          Create New Vault
        </h1>
        <p className="text-slate-400 text-lg">
          Configure a custom savings strategy powered by Soroban smart contracts.
        </p>
      </div>

      <CreateVaultWizard />
    </div>
  );
}
