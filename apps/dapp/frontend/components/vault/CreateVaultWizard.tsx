"use client";

import { ElementType, useState, Fragment } from "react";
import { 
  INITIAL_WIZARD_DATA, 
  WizardVaultData, 
  PROTOCOL_OPTIONS,
  VaultType,
  LockPeriod,
  RebalanceFrequency
} from "../../lib/types/vault-wizard";
import { AllocationBuilder } from "./AllocationBuilder";
import { VaultFactory, VaultDeploymentResponse } from "../../lib/stellar/vault-factory";
import { 
  ChevronRight, ChevronLeft, CheckCircle2, AlertCircle, 
  Wallet, ShieldCheck, Activity, Check, Copy, ExternalLink, RefreshCw, Sparkles
} from "lucide-react";
import clsx from "clsx";
import Link from "next/link";

const STEPS = [
  { id: 1, title: "Basics" },
  { id: 2, title: "Strategy" },
  { id: 3, title: "Risk & Limits" },
  { id: 4, title: "Review" },
];

const VAULT_TYPES: { id: VaultType; title: string; desc: string; icon: ElementType }[] = [
  { id: "Stable Yield", title: "Stable Yield", desc: "Low-risk, consistent returns using stablecoins.", icon: ShieldCheck },
  { id: "Balanced", title: "Balanced", desc: "Moderate risk/reward mix using major assets.", icon: Wallet },
  { id: "Aggressive Growth", title: "Aggressive Growth", desc: "High-yield strategies with higher volatility.", icon: Activity },
];

export function CreateVaultWizard() {
  const [currentStep, setCurrentStep] = useState(1);
  const [data, setData] = useState<WizardVaultData>(INITIAL_WIZARD_DATA);
  const [isDeploying, setIsDeploying] = useState(false);
  const [deployStatus, setDeployStatus] = useState("");
  const [deployError, setDeployError] = useState("");
  const [successData, setSuccessData] = useState<VaultDeploymentResponse | null>(null);

  const openPrometheusRecommendation = () => {
    if (typeof window === "undefined") {
      return;
    }
    window.dispatchEvent(
      new CustomEvent("nester:prometheus-open", {
        detail: {
          prompt:
            "Recommend a vault for me. I want a straightforward recommendation with a clear rationale, using my risk tolerance, savings goal, and time horizon.",
        },
      })
    );
  };

  // Validation
  const canProceed = () => {
    if (currentStep === 1) {
      return data.name.trim().length > 0 && data.type !== null;
    }
    if (currentStep === 2) {
      const total = data.allocations.reduce((sum, a) => sum + a.percentage, 0);
      return Math.abs(total - 100) < 0.1;
    }
    if (currentStep === 3) {
      if (data.maxCapacity !== null && data.maxCapacity < 0) return false;
      if (data.autoRebalance && !data.rebalanceFrequency) return false;
      return true;
    }
    return true;
  };

  const handleNext = () => {
    if (canProceed() && currentStep < STEPS.length) {
      setCurrentStep(s => s + 1);
    }
  };

  const handleBack = () => {
    if (currentStep > 1) {
      setCurrentStep(s => s - 1);
    }
  };

  const handleCreate = async () => {
    if (!canProceed()) return;
    
    setIsDeploying(true);
    setDeployError("");
    
    try {
      // Connect mock wallet
      setDeployStatus("Connecting wallet...");
      await VaultFactory.connectWallet();
      
      // Deploy
      const response = await VaultFactory.createVault(data, setDeployStatus);
      
      if (response.success) {
        setSuccessData(response);
        setCurrentStep(5); // Success step
      } else {
        setDeployError(response.error || "Failed to create vault");
      }
    } catch (err: unknown) {
      setDeployError(err instanceof Error ? err.message : "An unexpected error occurred");
    } finally {
      setIsDeploying(false);
    }
  };

  // Renders
  const renderStep1 = () => (
    <div className="space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
      <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-white mb-1">Vault Basics</h2>
          <p className="text-slate-400">Let&apos;s start with the fundamental details of your new savings vault.</p>
        </div>
        <button
          type="button"
          onClick={openPrometheusRecommendation}
          className="inline-flex items-center gap-2 self-start rounded-full border border-blue-500/30 bg-blue-500/10 px-4 py-2 text-sm font-medium text-blue-300 transition-colors hover:border-blue-400/50 hover:bg-blue-500/15 hover:text-blue-200"
        >
          <Sparkles className="h-4 w-4" />
          Get recommendation
        </button>
      </div>

      <div className="space-y-4">
        <div>
          <label className="block text-sm font-medium text-slate-300 mb-2">Vault Name *</label>
          <input
            type="text"
            value={data.name}
            onChange={(e) => setData({ ...data, name: e.target.value })}
            placeholder="e.g. My Retirement Fund"
            className="w-full bg-slate-900 border border-slate-800 rounded-lg px-4 py-3 text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 transition-colors"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-slate-300 mb-2">Description (Optional)</label>
          <textarea
            value={data.description}
            onChange={(e) => setData({ ...data, description: e.target.value })}
            placeholder="What is the goal of this vault?"
            rows={3}
            className="w-full bg-slate-900 border border-slate-800 rounded-lg px-4 py-3 text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 transition-colors resize-none"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-slate-300 mb-3">Vault Strategy Type *</label>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {VAULT_TYPES.map((type) => {
              const Icon = type.icon;
              const isSelected = data.type === type.id;
              return (
                <button
                  key={type.id}
                  onClick={() => setData({ ...data, type: type.id })}
                  className={clsx(
                    "flex flex-col items-start p-4 rounded-xl border text-left transition-all",
                    isSelected 
                      ? "bg-blue-500/10 border-blue-500 shadow-[0_0_15px_rgba(59,130,246,0.1)]" 
                      : "bg-slate-900 border-slate-800 hover:border-slate-700 hover:bg-slate-800/50"
                  )}
                >
                  <Icon className={clsx("w-6 h-6 mb-3", isSelected ? "text-blue-400" : "text-slate-400")} />
                  <h4 className={clsx("font-medium mb-1", isSelected ? "text-white" : "text-slate-200")}>
                    {type.title}
                  </h4>
                  <p className="text-xs text-slate-400 line-clamp-2">{type.desc}</p>
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );

  const renderStep2 = () => (
    <div className="space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
      <div>
        <h2 className="text-2xl font-bold text-white mb-1">Allocation Strategy</h2>
        <p className="text-slate-400">Configure how your funds are distributed across different DeFi protocols.</p>
      </div>

      <AllocationBuilder
        protocols={PROTOCOL_OPTIONS}
        allocations={data.allocations}
        onChange={(allocations) => setData({ ...data, allocations })}
      />
    </div>
  );

  const renderStep3 = () => (
    <div className="space-y-8 animate-in fade-in slide-in-from-bottom-4 duration-500">
      <div>
        <h2 className="text-2xl font-bold text-white mb-1">Risk & Limits</h2>
        <p className="text-slate-400">Set boundaries and automation rules for your vault.</p>
      </div>

      <div className="space-y-6">
        <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
          <label className="block text-sm font-medium text-white mb-1">Maximum Capacity</label>
          <p className="text-sm text-slate-400 mb-4">Limit the total TVL accepted into this vault (Optional).</p>
          <div className="relative">
            <span className="absolute left-4 top-1/2 -translate-y-1/2 text-slate-400">$</span>
            <input
              type="number"
              min="0"
              value={data.maxCapacity || ""}
              onChange={(e) => setData({ ...data, maxCapacity: e.target.value ? Number(e.target.value) : null })}
              placeholder="No limit"
              className="w-full bg-slate-950 border border-slate-800 rounded-lg pl-8 pr-4 py-3 text-white placeholder-slate-600 focus:outline-none focus:border-blue-500 transition-colors"
            />
          </div>
        </div>

        <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
          <label className="block text-sm font-medium text-white mb-1">Lock Period</label>
          <p className="text-sm text-slate-400 mb-4">Require funds to remain in the vault for a minimum duration after deposit.</p>
          <div className="flex flex-wrap gap-3">
            {(["None", "30 Days", "90 Days"] as LockPeriod[]).map((period) => (
              <button
                key={period}
                onClick={() => setData({ ...data, lockPeriod: period })}
                className={clsx(
                  "px-5 py-2.5 rounded-lg text-sm font-medium transition-colors border",
                  data.lockPeriod === period
                    ? "bg-blue-500/10 border-blue-500 text-blue-400"
                    : "bg-slate-950 border-slate-800 text-slate-300 hover:border-slate-700"
                )}
              >
                {period}
              </button>
            ))}
          </div>
        </div>

        <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
          <div className="flex items-center justify-between mb-4">
            <div>
              <label className="block text-sm font-medium text-white mb-1">Auto-Rebalance</label>
              <p className="text-sm text-slate-400">Automatically adjust positions to maintain target allocations.</p>
            </div>
            <button
              onClick={() => setData({ ...data, autoRebalance: !data.autoRebalance })}
              className={clsx(
                "relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 focus:ring-offset-slate-900",
                data.autoRebalance ? "bg-blue-500" : "bg-slate-700"
              )}
            >
              <span
                className={clsx(
                  "inline-block h-4 w-4 transform rounded-full bg-white transition-transform",
                  data.autoRebalance ? "translate-x-6" : "translate-x-1"
                )}
              />
            </button>
          </div>

          {data.autoRebalance && (
            <div className="pt-4 border-t border-slate-800 animate-in fade-in slide-in-from-top-2">
              <label className="block text-sm font-medium text-slate-300 mb-3">Rebalance Frequency *</label>
              <div className="flex flex-wrap gap-3">
                {(["Daily", "Weekly", "Monthly"] as RebalanceFrequency[]).map((freq) => (
                  <button
                    key={freq}
                    onClick={() => setData({ ...data, rebalanceFrequency: freq })}
                    className={clsx(
                      "px-5 py-2.5 rounded-lg text-sm font-medium transition-colors border",
                      data.rebalanceFrequency === freq
                        ? "bg-blue-500/10 border-blue-500 text-blue-400"
                        : "bg-slate-950 border-slate-800 text-slate-300 hover:border-slate-700"
                    )}
                  >
                    {freq}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );

  const renderStep4 = () => {
    // Calculate blended APY
    let blendedApy = 0;
    data.allocations.forEach((alloc) => {
      const protocol = PROTOCOL_OPTIONS.find((p) => p.id === alloc.protocolId);
      if (protocol) {
        blendedApy += (alloc.percentage / 100) * protocol.estimatedApy;
      }
    });

    const activeAllocations = data.allocations.filter(a => a.percentage > 0);
    const estimatedGas = 0.5 + (activeAllocations.length * 0.1) + (data.autoRebalance ? 0.2 : 0);

    return (
      <div className="space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
        <div>
          <h2 className="text-2xl font-bold text-white mb-1">Review & Create</h2>
          <p className="text-slate-400">Review your vault configuration before deploying to the network.</p>
        </div>

        <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
          <div className="p-6 border-b border-slate-800 flex justify-between items-start">
            <div>
              <h3 className="text-xl font-bold text-white mb-1">{data.name}</h3>
              <p className="text-slate-400">{data.description || "No description provided."}</p>
            </div>
            <span className="px-3 py-1 bg-blue-500/10 text-blue-400 border border-blue-500/20 rounded-full text-sm font-medium">
              {data.type}
            </span>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 divide-y md:divide-y-0 md:divide-x divide-slate-800">
            <div className="p-6">
              <div className="flex justify-between items-center mb-4">
                <h4 className="font-medium text-white flex items-center gap-2">
                  <Activity className="w-4 h-4 text-emerald-400" />
                  Allocation Strategy
                </h4>
                <button onClick={() => setCurrentStep(2)} className="text-sm text-blue-400 hover:text-blue-300">Edit</button>
              </div>
              
              <div className="space-y-3">
                {activeAllocations.map(alloc => {
                  const p = PROTOCOL_OPTIONS.find(x => x.id === alloc.protocolId)!;
                  return (
                    <div key={alloc.protocolId} className="flex justify-between items-center text-sm">
                      <span className="text-slate-300 flex items-center gap-2">
                        <div className="w-2 h-2 rounded-full" style={{ backgroundColor: p.color }} />
                        {p.name}
                      </span>
                      <span className="font-medium text-white">{alloc.percentage.toFixed(1)}%</span>
                    </div>
                  );
                })}
              </div>

              <div className="mt-6 pt-4 border-t border-slate-800 flex justify-between items-center">
                <span className="text-slate-400 text-sm">Estimated Blended APY</span>
                <span className="text-lg font-bold text-emerald-400">{blendedApy.toFixed(2)}%</span>
              </div>
            </div>

            <div className="p-6 space-y-6">
              <div>
                <div className="flex justify-between items-center mb-3">
                  <h4 className="font-medium text-white flex items-center gap-2">
                    <ShieldCheck className="w-4 h-4 text-blue-400" />
                    Limits & Rules
                  </h4>
                  <button onClick={() => setCurrentStep(3)} className="text-sm text-blue-400 hover:text-blue-300">Edit</button>
                </div>
                
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-slate-400">Max Capacity</span>
                    <span className="text-white">{data.maxCapacity ? `$${data.maxCapacity.toLocaleString()}` : "Unlimited"}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-slate-400">Lock Period</span>
                    <span className="text-white">{data.lockPeriod}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-slate-400">Auto-Rebalance</span>
                    <span className="text-white">{data.autoRebalance ? `Yes (${data.rebalanceFrequency})` : "No"}</span>
                  </div>
                </div>
              </div>

              <div className="pt-6 border-t border-slate-800">
                <div className="flex justify-between items-center text-sm">
                  <span className="text-slate-400">Estimated Gas Fee</span>
                  <span className="font-medium text-white flex items-center gap-1">
                    ~{estimatedGas.toFixed(2)} XLM
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>

        {deployError && (
          <div className="bg-red-500/10 border border-red-500/20 rounded-lg p-4 flex items-start gap-3">
            <AlertCircle className="w-5 h-5 text-red-400 shrink-0 mt-0.5" />
            <div className="text-sm text-red-400">
              {deployError}
            </div>
          </div>
        )}
      </div>
    );
  };

  const renderSuccess = () => (
    <div className="py-12 flex flex-col items-center text-center animate-in zoom-in-95 duration-500">
      <div className="w-20 h-20 bg-emerald-500/10 rounded-full flex items-center justify-center mb-6">
        <CheckCircle2 className="w-10 h-10 text-emerald-400" />
      </div>
      
      <h2 className="text-3xl font-bold text-white mb-2">Vault Created!</h2>
      <p className="text-slate-400 max-w-md mb-8">
        Your new savings vault has been successfully deployed to the network and is ready to accept deposits.
      </p>

      <div className="bg-slate-900 border border-slate-800 rounded-xl p-4 w-full max-w-md mb-8 space-y-3">
        <div className="flex justify-between items-center text-sm">
          <span className="text-slate-400">Vault ID</span>
          <div className="flex items-center gap-2 text-white font-mono bg-slate-950 px-2 py-1 rounded">
            {String(successData?.vaultId)}
            <Copy className="w-3.5 h-3.5 text-slate-500 cursor-pointer hover:text-white" />
          </div>
        </div>
        <div className="flex justify-between items-center text-sm">
          <span className="text-slate-400">Contract</span>
          <div className="flex items-center gap-2 text-white font-mono bg-slate-950 px-2 py-1 rounded">
            {String(successData?.contractAddress)?.substring(0, 8)}...{String(successData?.contractAddress)?.substring(47)}
            <Copy className="w-3.5 h-3.5 text-slate-500 cursor-pointer hover:text-white" />
          </div>
        </div>
        <div className="flex justify-between items-center text-sm">
          <span className="text-slate-400">Tx Hash</span>
          <a href="#" className="flex items-center gap-1 text-blue-400 hover:text-blue-300 font-mono">
            {String(successData?.transactionHash)?.substring(0, 8)}...
            <ExternalLink className="w-3.5 h-3.5" />
          </a>
        </div>
      </div>

      <div className="flex flex-col sm:flex-row gap-4 w-full max-w-md">
        <Link 
          href={`/dashboard/vaults/${successData?.vaultId as string}`}
          className="flex-1 bg-blue-600 hover:bg-blue-700 text-white font-medium py-3 px-4 rounded-lg transition-colors flex justify-center"
        >
          Make First Deposit
        </Link>
        <Link 
          href="/dashboard"
          className="flex-1 bg-slate-800 hover:bg-slate-700 text-white font-medium py-3 px-4 rounded-lg transition-colors flex justify-center"
        >
          Return to Dashboard
        </Link>
      </div>
    </div>
  );

  return (
    <div className="max-w-4xl mx-auto w-full">
      {currentStep < 5 && (
        <div className="mb-8">
          <div className="flex items-center justify-between mb-4">
            {STEPS.map((step, idx) => (
              <Fragment key={step.id}>
                <div className="flex flex-col items-center gap-2 z-10 relative">
                  <div 
                    className={clsx(
                      "w-10 h-10 rounded-full flex items-center justify-center font-semibold text-sm transition-all duration-300",
                      currentStep > step.id ? "bg-emerald-500 text-white" : 
                      currentStep === step.id ? "bg-blue-600 text-white ring-4 ring-blue-500/20" : 
                      "bg-slate-800 text-slate-400 border border-slate-700"
                    )}
                  >
                    {currentStep > step.id ? <Check className="w-5 h-5" /> : step.id}
                  </div>
                  <span className={clsx(
                    "text-xs font-medium absolute -bottom-6 w-24 text-center",
                    currentStep >= step.id ? "text-slate-200" : "text-slate-500"
                  )}>
                    {step.title}
                  </span>
                </div>
                {idx < STEPS.length - 1 && (
                  <div className="flex-1 h-0.5 mx-2 relative -top-3">
                    <div className="absolute inset-0 bg-slate-800" />
                    <div 
                      className="absolute inset-y-0 left-0 bg-blue-500 transition-all duration-500"
                      style={{ width: currentStep > step.id ? '100%' : '0%' }}
                    />
                  </div>
                )}
              </Fragment>
            ))}
          </div>
          <div className="h-8" /> {/* Spacer for labels */}
        </div>
      )}

      <div className="bg-slate-950/50 backdrop-blur-xl border border-slate-800 rounded-2xl p-6 md:p-10 shadow-xl">
        {currentStep === 1 && renderStep1()}
        {currentStep === 2 && renderStep2()}
        {currentStep === 3 && renderStep3()}
        {currentStep === 4 && renderStep4()}
        {currentStep === 5 && renderSuccess()}

        {currentStep < 5 && (
          <div className="mt-10 pt-6 border-t border-slate-800 flex justify-between items-center">
            <button
              onClick={handleBack}
              disabled={currentStep === 1 || isDeploying}
              className="px-6 py-2.5 rounded-lg font-medium text-slate-300 hover:text-white hover:bg-slate-800 transition-colors disabled:opacity-50 disabled:pointer-events-none flex items-center gap-2"
            >
              <ChevronLeft className="w-4 h-4" /> Back
            </button>
            
            {currentStep < 4 ? (
              <button
                onClick={handleNext}
                disabled={!canProceed()}
                className="px-6 py-2.5 bg-blue-600 hover:bg-blue-700 disabled:bg-slate-800 disabled:text-slate-500 text-white rounded-lg font-medium transition-colors flex items-center gap-2"
              >
                Next Step <ChevronRight className="w-4 h-4" />
              </button>
            ) : (
              <button
                onClick={handleCreate}
                disabled={!canProceed() || isDeploying}
                className="px-8 py-2.5 bg-linear-to-r from-blue-600 to-emerald-600 hover:from-blue-500 hover:to-emerald-500 text-white rounded-lg font-medium transition-all shadow-lg disabled:opacity-50 disabled:pointer-events-none flex items-center gap-2 relative overflow-hidden group"
              >
                {isDeploying ? (
                  <>
                    <RefreshCw className="w-5 h-5 animate-spin" />
                    <span>{deployStatus}</span>
                  </>
                ) : (
                  <>
                    Deploy Vault
                    <div className="absolute inset-0 bg-white/20 translate-y-full group-hover:translate-y-0 transition-transform" />
                  </>
                )}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
