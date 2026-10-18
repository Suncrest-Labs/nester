import { Container } from "@/components/container";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Security & Audit | Nester",
  description:
    "Smart contract audit status and scope, threat model, and Nester's funded bug bounty for the launch contracts and API money path.",
};

const REPO_URL = "https://github.com/Suncrest-Labs/nester";
const THREAT_MODEL_URL = `${REPO_URL}/blob/main/AUDIT_THREAT_MODEL.md`;
const SECURITY_POLICY_URL = `${REPO_URL}/blob/main/SECURITY.md`;
const REPORT_VULN_URL = `${REPO_URL}/security/advisories/new`;
const SECURITY_EMAIL = "security@nester.dev";
const BOUNTY_DOC_URL = `${REPO_URL}/blob/main/docs/security/bug-bounty.md`;

// Program parameters. Keep in sync with docs/security/bug-bounty.md — the doc
// is the program of record, this page is the public summary.
const BOUNTY_POOL = "$150,000";
const TVL_CAP_PERCENT = 10;

const bountyTiers = [
  {
    severity: "Critical",
    impact:
      "Funds at risk ≥ $250,000: principal loss or permanent freeze, unbacked share minting, withdrawal authorization bypass",
    reward: "10% of funds at risk · floor $25,000",
    ceiling: "10% of TVL · max $150,000",
  },
  {
    severity: "High",
    impact:
      "Funds at risk $50,000–$249,999: admin/role bypass reaching funds, or a DoS that locks withdrawals with no exit",
    reward: "5% of funds at risk · floor $10,000",
    ceiling: "10% of TVL · max $75,000",
  },
  {
    severity: "Medium",
    impact:
      "Funds at risk $5,000–$49,999: limited data exposure, partial auth weakness, non-critical DoS",
    reward: "Greater of $2,000 or 2% of funds at risk",
    ceiling: "10% of TVL · max $15,000",
  },
  {
    severity: "Low",
    impact:
      "Funds at risk < $5,000: information disclosure, best-practice violation with no fund path",
    reward: "$500 flat",
    ceiling: "$500",
  },
];

// Contracts submitted for external review. Keep in sync with the audit package
// and AUDIT_THREAT_MODEL.md.
const contractsInScope = [
  "vault",
  "vault_token",
  "allocation_strategy",
  "yield_registry",
  "nester",
  "treasury",
  "timelock",
];

const threats = [
  "Privilege escalation through admin or governance paths",
  "Reentrancy in deposit, withdraw, and rebalance flows",
  "Integer overflow or precision loss in accounting code",
  "Fee manipulation and rounding drift in yield accounting",
  "Unsafe cross-contract calls or stale state transitions",
];

export default function SecurityPage() {
  return (
    <main className="min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(15,23,42,0.9),_rgba(2,6,23,1)_60%)] text-white">
      <Container className="py-20 md:py-28">
        <div className="max-w-4xl">
          <p className="mb-4 text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">
            Security &amp; Audit
          </p>
          <h1 className="max-w-3xl text-4xl font-semibold tracking-tight md:text-6xl">
            Audit status, scope, and responsible disclosure.
          </h1>
          <p className="mt-6 max-w-2xl text-base leading-7 text-slate-300 md:text-lg">
            Nester&apos;s smart contracts move user funds on Stellar Mainnet, so we keep a
            public record of the audit&apos;s status, the contracts under review, and a clear
            path for security researchers to report issues.
          </p>
        </div>

        {/* Smart Contract Audit — current status (placeholder until the audit completes) */}
        <section
          id="smart-contract-audit"
          className="mt-14 rounded-3xl border border-amber-400/25 bg-amber-400/10 p-6 md:p-8"
        >
          <div className="flex flex-wrap items-center gap-3">
            <h2 className="text-lg font-medium text-white">Smart Contract Audit</h2>
            <span className="inline-flex items-center gap-2 rounded-full border border-amber-300/40 bg-amber-300/10 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-amber-200">
              <span className="h-1.5 w-1.5 rounded-full bg-amber-300" aria-hidden="true" />
              Pending
            </span>
          </div>
          <p className="mt-4 max-w-3xl text-sm leading-6 text-slate-200">
            <strong className="font-semibold text-white">Pending</strong> — an external smart
            contract audit is being scheduled with{" "}
            <span className="text-amber-200">[auditor name when confirmed]</span>. Findings will
            be triaged by severity; critical and high issues block launch.
          </p>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-300">
            {/*
              UPDATE PATH (when the audit is complete): replace this placeholder with the
              audit firm's name, the completion date, and a link to the published report.
            */}
            When the review is complete, this section will be updated with the audit firm&apos;s
            name, the completion date, and a link to the published report.
          </p>
        </section>

        <div className="mt-6 grid gap-6 lg:grid-cols-2">
          {/* Contracts in scope */}
          <section className="rounded-3xl border border-white/10 bg-white/5 p-6 backdrop-blur-sm">
            <h2 className="text-lg font-medium text-white">Contracts in Scope</h2>
            <ul className="mt-4 space-y-3 text-sm leading-6 text-slate-300">
              {contractsInScope.map((item) => (
                <li
                  key={item}
                  className="rounded-2xl border border-white/5 bg-black/20 px-4 py-3 font-mono"
                >
                  {item}
                </li>
              ))}
            </ul>
          </section>

          {/* Threat Model */}
          <section className="rounded-3xl border border-white/10 bg-white/5 p-6 backdrop-blur-sm">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-lg font-medium text-white">Threat Model</h2>
              <a
                href={THREAT_MODEL_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1.5 rounded-full border border-white/15 px-3 py-1 text-xs font-medium text-slate-200 transition-colors hover:border-white/30 hover:text-white"
              >
                Read the threat model
                <span aria-hidden="true">↗</span>
              </a>
            </div>
            <p className="mt-4 text-sm leading-6 text-slate-300">
              The full threat model documents trust assumptions, attacker capabilities, and the
              mitigations for each contract. Key areas under review:
            </p>
            <ul className="mt-4 space-y-3 text-sm leading-6 text-slate-300">
              {threats.map((item) => (
                <li key={item} className="rounded-2xl border border-white/5 bg-black/20 px-4 py-3">
                  {item}
                </li>
              ))}
            </ul>
          </section>
        </div>

        {/* Funded Bug Bounty */}
        <section
          id="bug-bounty"
          className="mt-6 rounded-3xl border border-emerald-400/20 bg-emerald-400/10 p-6 md:p-8"
        >
          <div className="flex flex-wrap items-center gap-3">
            <h2 className="text-lg font-medium text-white">Funded Bug Bounty</h2>
            <span className="inline-flex items-center gap-2 rounded-full border border-emerald-300/40 bg-emerald-300/10 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-emerald-200">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-300" aria-hidden="true" />
              Live
            </span>
          </div>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-200">
            A vault bug is worth more reported than exploited. We pay for findings in the
            launch contract set and the API money path, scale the reward to the funds at
            risk, and cap any single payout at {TVL_CAP_PERCENT}% of TVL.
          </p>

          <dl className="mt-6 grid gap-4 sm:grid-cols-3">
            <div className="rounded-2xl border border-white/10 bg-black/20 p-4">
              <dt className="text-xs font-semibold uppercase tracking-wide text-slate-400">
                Earmarked pool
              </dt>
              <dd className="mt-1 text-xl font-semibold text-white">{BOUNTY_POOL} USDC</dd>
            </div>
            <div className="rounded-2xl border border-white/10 bg-black/20 p-4">
              <dt className="text-xs font-semibold uppercase tracking-wide text-slate-400">
                Per-report cap
              </dt>
              <dd className="mt-1 text-xl font-semibold text-white">{TVL_CAP_PERCENT}% of TVL</dd>
            </div>
            <div className="rounded-2xl border border-white/10 bg-black/20 p-4">
              <dt className="text-xs font-semibold uppercase tracking-wide text-slate-400">
                First response
              </dt>
              <dd className="mt-1 text-xl font-semibold text-white">24h</dd>
              <dd className="text-xs text-slate-400">Critical: 4h</dd>
            </div>
          </dl>

          <h3 className="mt-8 text-sm font-semibold text-white">Reward tiers</h3>
          <div className="mt-3 overflow-x-auto">
            <table className="w-full min-w-[40rem] border-collapse text-left text-sm">
              <thead>
                <tr className="text-xs uppercase tracking-wide text-slate-400">
                  <th className="border-b border-white/10 py-2 pr-4 font-semibold">Severity</th>
                  <th className="border-b border-white/10 py-2 pr-4 font-semibold">Qualifying impact</th>
                  <th className="border-b border-white/10 py-2 pr-4 font-semibold">Reward</th>
                  <th className="border-b border-white/10 py-2 font-semibold">Ceiling</th>
                </tr>
              </thead>
              <tbody className="align-top text-slate-300">
                {bountyTiers.map((tier) => (
                  <tr key={tier.severity}>
                    <td className="border-b border-white/5 py-3 pr-4 font-medium text-white">
                      {tier.severity}
                    </td>
                    <td className="border-b border-white/5 py-3 pr-4 leading-6">{tier.impact}</td>
                    <td className="border-b border-white/5 py-3 pr-4 leading-6">{tier.reward}</td>
                    <td className="border-b border-white/5 py-3 leading-6">{tier.ceiling}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-3 max-w-3xl text-xs leading-5 text-slate-400">
            Funds at risk is the maximum value an attacker could move, redirect, or
            permanently freeze, measured at the report timestamp. Rewards are rounded to the
            nearest $500, paid per root cause rather than per instance, and paid in USDC on
            Stellar.
          </p>

          <h3 className="mt-8 text-sm font-semibold text-white">In scope</h3>
          <div className="mt-3 grid gap-4 sm:grid-cols-2">
            <div className="rounded-2xl border border-white/10 bg-black/20 p-5">
              <h4 className="text-sm font-semibold text-white">Launch contracts</h4>
              <p className="mt-2 text-sm leading-6 text-slate-300">
                Every Soroban contract in the launch deployment:{" "}
                <span className="font-mono text-xs text-slate-200">
                  vault, vault_factory, vault_token, allocation_strategy, yield_registry,
                  nester, treasury, timelock, access_control,
                </span>{" "}
                the four yield adapters, and the launch feature contracts.
              </p>
            </div>
            <div className="rounded-2xl border border-white/10 bg-black/20 p-5">
              <h4 className="text-sm font-semibold text-white">API money path</h4>
              <p className="mt-2 text-sm leading-6 text-slate-300">
                Deposit, withdraw, and emergency-withdraw flows; on-chain settlement and
                indexer credit; authentication and admin role checks; the global and per-vault
                pause switches; and the signer service&apos;s operation and amount policy.
              </p>
            </div>
          </div>

          <h3 className="mt-8 text-sm font-semibold text-white">Triage owner and SLA</h3>
          <ul className="mt-3 space-y-2 text-sm leading-6 text-slate-300">
            <li>
              • <strong className="font-semibold text-white">Triage owner:</strong> Security
              Lead, with the engineering on-call engineer as backup.
            </li>
            <li>• Acknowledged within 24 hours, and within 4 hours for a Critical report.</li>
            <li>
              • Severity, funds at risk, and reward band decided within 3 business days, with
              the arithmetic stated.
            </li>
            <li>• Fix targets: Critical 72 hours, High 14 days, Medium 30 days, Low 90 days.</li>
            <li>
              • Paid within 10 business days of the verified fix. Accepted Critical and High
              reports are paid 50% at triage acceptance.
            </li>
            <li>• Disclosure 90 days after the report, or once the fix ships, whichever is earlier.</li>
          </ul>
          <p className="mt-4 max-w-3xl text-xs leading-5 text-slate-400">
            The pool is held in a dedicated Stellar account controlled by the treasury
            multisig (3-of-5), segregated from operating funds and funded at the launch gate.
            Its address and balance proof are published here before mainnet deposits open, so
            the earmark is verifiable without trusting this page.
          </p>

          <div className="mt-6 grid gap-4 sm:grid-cols-2">
            <a
              href={REPORT_VULN_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="group rounded-2xl border border-white/10 bg-black/20 p-5 transition-colors hover:border-emerald-300/40"
            >
              <h3 className="text-sm font-semibold text-white">GitHub Private Reporting</h3>
              <p className="mt-2 text-sm leading-6 text-slate-300">
                Preferred. Open a private advisory under the repository&apos;s Security tab.
              </p>
              <span className="mt-3 inline-block text-xs font-medium text-emerald-300 group-hover:text-emerald-200">
                Report a vulnerability ↗
              </span>
            </a>

            <a
              href={`mailto:${SECURITY_EMAIL}?subject=%5BBOUNTY%5D%20`}
              className="group rounded-2xl border border-white/10 bg-black/20 p-5 transition-colors hover:border-emerald-300/40"
            >
              <h3 className="text-sm font-semibold text-white">Email</h3>
              <p className="mt-2 text-sm leading-6 text-slate-300">
                Send a report to{" "}
                <span className="font-mono text-emerald-300">{SECURITY_EMAIL}</span> with the
                subject line <span className="font-mono">[BOUNTY]</span>.
              </p>
              <span className="mt-3 inline-block text-xs font-medium text-emerald-300 group-hover:text-emerald-200">
                Email the security team ↗
              </span>
            </a>
          </div>

          <ul className="mt-6 space-y-2 text-sm leading-6 text-slate-300">
            <li>• Do not open a public GitHub issue for a security report.</li>
            <li>• Safe harbor: we won&apos;t pursue legal action against good-faith research.</li>
            <li>• Valid reports are credited in our Hall of Fame unless you prefer to stay anonymous.</li>
          </ul>

          <p className="mt-6 text-sm">
            <a
              href={BOUNTY_DOC_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="font-medium text-emerald-300 transition-colors hover:text-emerald-200"
            >
              Read the full bounty terms ↗
            </a>
            <span className="mx-2 text-slate-500">·</span>
            <a
              href={SECURITY_POLICY_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="font-medium text-slate-300 transition-colors hover:text-white"
            >
              Security policy ↗
            </a>
          </p>
        </section>
      </Container>
    </main>
  );
}
