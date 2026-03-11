"use client"

import { useState, useCallback, useRef, useEffect } from "react";
import { motion, useInView } from "framer-motion";

const FAQ_ITEMS = [
  {
    id: "what-is-nester",
    classification: "GENERAL",
    question: "What is Nester?",
    answer:
      "Nester is a DeFi yield platform that lets you deposit stablecoins and automatically earn optimized returns across multiple protocols like Aave, Blend, and Kamino — with instant fiat off-ramps to your local bank account.",
  },
  {
    id: "how-yield",
    classification: "VAULTS",
    question: "How does Nester generate yield?",
    answer:
      "Your deposits are automatically allocated across battle-tested DeFi protocols. Nester's optimization engine continuously monitors APYs and rebalances your funds to capture the best risk-adjusted returns — 24/7, without any manual intervention.",
  },
  {
    id: "is-it-safe",
    classification: "SECURITY",
    question: "Is my money safe?",
    answer:
      "Nester only integrates with audited, battle-tested protocols. Your funds are held in non-custodial smart contracts — Nester never has direct access to your deposits. All vault strategies undergo rigorous risk assessment before deployment.",
  },
  {
    id: "offramp",
    classification: "OFF-RAMP",
    question: "How does the fiat off-ramp work?",
    answer:
      "Our distributed liquidity provider network routes fiat directly to your bank via live banking APIs. Same-bank transfers settle in as little as 3 seconds. No peer-to-peer matching, no delays, no intermediaries.",
  },
  {
    id: "prometheus",
    classification: "AI",
    question: "What is Prometheus?",
    answer:
      "Prometheus is Nester's built-in AI financial advisor. It analyzes your portfolio against live market data, protocol health, and sentiment — then delivers plain-language recommendations with one-click execution. Think of it as a 24/7 co-pilot for your money.",
  },
  {
    id: "minimum",
    classification: "GENERAL",
    question: "Is there a minimum deposit?",
    answer:
      "No. You can start with any amount of supported stablecoins. Nester is designed to be accessible whether you're depositing $10 or $100,000 — the optimization engine works the same regardless of size.",
  },
  {
    id: "fees",
    classification: "GENERAL",
    question: "What are the fees?",
    answer:
      "Nester charges a small performance fee on yield earned — you only pay when you're making money. Off-ramp transactions carry a flat 0.5% conversion fee. There are no deposit fees, no withdrawal fees, and no hidden charges.",
  },
  {
    id: "supported",
    classification: "VAULTS",
    question: "Which assets and chains are supported?",
    answer:
      "Nester currently supports USDC and USDT deposits on the Stellar network, with active integrations into Aave, Blend, and Kamino protocols. Multi-chain expansion is on the roadmap — starting with Solana and Base.",
  },
];

function DissolvingBar({ width, stagger }: { width: string; stagger: number }) {
  return (
    <motion.span
      className="inline-block h-[1.1em] rounded-[2px] align-middle relative overflow-hidden"
      style={{ width }}
      initial={{ opacity: 1, scaleX: 1, filter: "blur(0px)" }}
      animate={{
        opacity: [1, 1, 0.8, 0],
        scaleX: [1, 1, 1.05, 0.4],
        y: [0, 0, -2, -8],
        filter: ["blur(0px)", "blur(0px)", "blur(2px)", "blur(12px)"],
      }}
      transition={{
        duration: 1.0,
        delay: stagger,
        ease: "easeOut",
      }}
    >
      <span className="block w-full h-full bg-black/85 rounded-[2px]" />
      <motion.span
        className="absolute inset-0 bg-white/70"
        initial={{ opacity: 0 }}
        animate={{ opacity: [0, 0.8, 0, 0.6, 0, 0.4, 0, 0] }}
        transition={{
          duration: 1.0,
          delay: stagger,
          times: [0, 0.1, 0.2, 0.35, 0.45, 0.6, 0.7, 1],
        }}
      />
    </motion.span>
  );
}

function StaticBar({ width }: { width: string }) {
  return (
    <span
      className="inline-block h-[1.1em] rounded-[2px] align-middle bg-black/85"
      style={{ width }}
    />
  );
}

const REDACTION_LINES = [
  [
    { width: "28%", stagger: 0.0 },
    { width: "18%", stagger: 0.15 },
    { width: "32%", stagger: 0.08 },
    { width: "14%", stagger: 0.22 },
  ],
  [
    { width: "22%", stagger: 0.1 },
    { width: "35%", stagger: 0.04 },
    { width: "20%", stagger: 0.18 },
    { width: "15%", stagger: 0.25 },
  ],
  [
    { width: "40%", stagger: 0.06 },
    { width: "25%", stagger: 0.2 },
    { width: "18%", stagger: 0.12 },
  ],
];

function RedactedOverlay({ phase }: { phase: "redacted" | "dissolving" | "revealed" }) {
  return (
    <div className="absolute inset-0 flex flex-col justify-center gap-[0.55em] px-0 pointer-events-none">
      {REDACTION_LINES.map((line, li) => (
        <div key={li} className="flex gap-[0.4em] flex-wrap">
          {line.map((bar, bi) =>
            phase === "dissolving" ? (
              <DissolvingBar key={`d-${li}-${bi}`} width={bar.width} stagger={bar.stagger} />
            ) : phase === "redacted" ? (
              <StaticBar key={`s-${li}-${bi}`} width={bar.width} />
            ) : null
          )}
        </div>
      ))}
    </div>
  );
}

function FaqItem({
  item,
  index,
  isOpen,
  onToggle,
}: {
  item: (typeof FAQ_ITEMS)[0];
  index: number;
  isOpen: boolean;
  onToggle: () => void;
}) {
  const [phase, setPhase] = useState<"closed" | "redacted" | "dissolving" | "revealed">("closed");

  useEffect(() => {
    if (isOpen) {
      setPhase("redacted");      const t1 = setTimeout(() => setPhase("dissolving"), 600);
      const t2 = setTimeout(() => setPhase("revealed"), 1800);
      return () => {
        clearTimeout(t1);
        clearTimeout(t2);
      };
    } else {
      setPhase("closed");
    }
  }, [isOpen]);

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-40px" }}
      transition={{ duration: 0.5, delay: index * 0.06, ease: [0.23, 1, 0.32, 1] }}
    >
      <div
        onClick={onToggle}
        className={`group cursor-pointer transition-all duration-300 ease-[cubic-bezier(0.25,1,0.5,1)] rounded-2xl ${
          isOpen
            ? "gradient-border-active gradient-border-lg shadow-sm"
            : ""
        }`}
      >
        <div
          className={`border rounded-2xl overflow-visible transition-all duration-300 ${
            isOpen
              ? "border-transparent bg-white/60"
              : "border-black/[0.07] bg-white/[0.02] group-hover:border-black/[0.18] group-hover:bg-white/30 group-hover:shadow-[0_8px_24px_rgba(0,0,0,0.03)]"
          }`}
        >
        {/* Question row */}
        <div className="flex items-start gap-4 px-6 md:px-8 py-5 md:py-6">
          {/* Classification badge */}
          <div className="flex-shrink-0 pt-0.5">
            <span
              className={`inline-block text-[8px] font-bold tracking-[0.2em] uppercase px-2.5 py-1 rounded-md border transition-colors duration-300 font-mono ${
                isOpen
                  ? "border-black/20 text-black/50 bg-black/[0.04]"
                  : "border-black/[0.08] text-black/25 group-hover:text-black/35 group-hover:border-black/12"
              }`}
            >
              {item.classification}
            </span>
          </div>

          {/* Question text */}
          <h3
            className={`flex-1 text-[15px] md:text-[17px] font-medium leading-[1.35] tracking-tight m-0 transition-colors duration-300 ${
              isOpen ? "text-black/85" : "text-black/55 group-hover:text-black/75"
            }`}
            style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
          >
            {item.question}
          </h3>

          {/* Declassify indicator */}
          <div className="flex-shrink-0 pt-0.5">
            <motion.div
              animate={{ rotate: isOpen ? 45 : 0 }}
              transition={{ duration: 0.3, ease: [0.23, 1, 0.32, 1] }}
              className={`w-7 h-7 rounded-lg border flex items-center justify-center transition-all duration-300 ${
                isOpen
                  ? "border-black/20 bg-black text-white"
                  : "border-black/[0.1] bg-transparent text-black/30 group-hover:border-black/20 group-hover:text-black/50"
              }`}
            >
              <svg width="12" height="12" viewBox="0 0 12 12" fill="none">
                <path d="M6 2.5V9.5M2.5 6H9.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
            </motion.div>
          </div>
        </div>

        {/* Answer area with redaction effect */}
        <motion.div
          initial={false}
          animate={{ height: isOpen ? "auto" : 0, opacity: isOpen ? 1 : 0 }}
          transition={{ duration: 0.5, ease: [0.23, 1, 0.32, 1] }}
          className="overflow-hidden"
        >
          <div className="px-6 md:px-8 pb-6 md:pb-8">
            <div className="ml-0 md:ml-[calc(2.5rem+1rem)] border-t border-black/[0.06] pt-5">
              {/* Stamp — slams in when dissolving starts */}
              {(phase === "dissolving" || phase === "revealed") && (
                <motion.div
                  initial={{ opacity: 0, scale: 3.5, rotate: -15, filter: "blur(4px)" }}
                  animate={{
                    opacity: 1,
                    scale: 1,
                    rotate: -3,
                    filter: "blur(0px)",
                  }}
                  transition={{
                    type: "spring",
                    damping: 12,
                    stiffness: 120,
                    duration: 0.4,
                  }}
                  className="inline-block mb-4 origin-center"
                >
                  <span className="inline-block text-[9px] font-bold tracking-[0.25em] uppercase text-red-600/80 border-[2.5px] border-red-600/50 rounded px-3 py-1 font-mono shadow-[0_0_0_1px_rgba(220,38,38,0.1)]">
                    DECLASSIFIED
                  </span>
                </motion.div>
              )}

              {/* Answer with redaction overlay */}
              <div className="relative">
                <motion.p
                  className="text-[13.5px] md:text-[15px] leading-[1.75] text-black/55 m-0 max-w-2xl"
                  style={{ fontFamily: "var(--font-inter, sans-serif)" }}
                  initial={false}
                  animate={{ 
                    opacity: (phase === "revealed" || phase === "dissolving") ? 1 : 0,
                    filter: phase === "dissolving" ? "blur(4px)" : "blur(0px)",
                    y: phase === "revealed" ? 0 : 4
                  }}
                  transition={{ duration: 0.8, ease: "easeOut" }}
                >
                  {item.answer}
                </motion.p>
                {(phase === "redacted" || phase === "dissolving") && (
                  <RedactedOverlay phase={phase} />
                )}
              </div>
            </div>
          </div>
        </motion.div>
        </div>
      </div>
    </motion.div>
  );
}

export function Faq() {
  const [openId, setOpenId] = useState<string | null>(null);
  const sectionRef = useRef<HTMLDivElement>(null);
  const isInView = useInView(sectionRef, { once: true, margin: "-100px" });

  const handleToggle = useCallback(
    (id: string) => {
      setOpenId((prev) => (prev === id ? null : id));
    },
    []
  );

  return (
    <section
      ref={sectionRef}
      className="w-full py-24 md:py-36 overflow-hidden px-6 md:px-12"
      style={{ background: "#f5f5f0" }}
    >
      <div className="max-w-4xl mx-auto w-full">
        {/* Header */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          animate={isInView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.7, ease: [0.23, 1, 0.32, 1] }}
          className="mb-14 md:mb-20"
        >
          <p className="text-[10px] font-bold tracking-[0.25em] uppercase text-black/30 mb-5 font-mono">
            Classified Intel
          </p>
          <h2
            className="text-[2rem] md:text-[2.8rem] lg:text-[3.4rem] font-light text-black leading-[1.1] tracking-tight m-0 mb-5 max-w-2xl"
            style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
          >
            Questions?{" "}
            <span
              className="italic font-medium"
              style={{ fontFamily: "var(--font-cormorant, serif)" }}
            >
              Declassified.
            </span>
          </h2>
          <p
            className="text-black/35 text-[15px] md:text-[17px] leading-[1.65] max-w-lg m-0"
            style={{ fontFamily: "var(--font-inter, sans-serif)" }}
          >
            Everything you need to know about Nester. Click to remove the redactions.
          </p>
        </motion.div>

        {/* FAQ items */}
        <div className="flex flex-col gap-3">
          {FAQ_ITEMS.map((item, i) => (
            <FaqItem
              key={item.id}
              item={item}
              index={i}
              isOpen={openId === item.id}
              onToggle={() => handleToggle(item.id)}
            />
          ))}
        </div>
      </div>
    </section>
  );
}
