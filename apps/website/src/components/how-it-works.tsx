"use client"

import React, { useState, useRef, useEffect } from "react";
import { motion, useInView, AnimatePresence } from "framer-motion";
import Image from "next/image";

/* ─────────────────────────────────────────────────────────────────
   STEP CONFIG
───────────────────────────────────────────────────────────────── */
const STEPS = [
  {
    id: "deposit",
    step: "01",
    label: "Deposit USDC",
    headline: "One product.\nDeposit USDC,\nreceive shares.",
    body: "The Nester Savings Rate is a single USDC product. You deposit USDC and receive shares; the share price rises as the underlying Stellar yield venues, Blend and DeFindex, pay yield. There are no tiers to choose.",
    tab: "Deposit USDC and receive savings shares.",
  },
  {
    id: "optimize",
    step: "02",
    label: "Earn a Realized Rate",
    headline: "Earn what the\nvenues actually\npay.",
    body: "The rate you see is the realized trailing 7-day rate of the share price. It is not guaranteed and it can go down. A 10% performance fee applies only to gains above the previous high-water mark.",
    tab: "The rate shown is realized, never promised.",
  },
  {
    id: "safeguards",
    step: "03",
    label: "Safeguards",
    headline: "Caps, a reserve,\nand a public\ntimelock.",
    body: "Each venue has an absolute cap and a percentage cap. Changes that add risk wait behind a public timelock (3 days by default), and a guardian can only reduce risk instantly. If a venue loses funds, all savers share the loss pro-rata, partially offset by the fee reserve.",
    tab: "Venue caps, a locked reserve and a public timelock.",
  },
  {
    id: "architecture",
    step: "04",
    label: "The Stack",
    headline: "Three layers.\nOne savings\nrate.",
    body: "Savings shares, yield venues and on-chain safeguards. Nester is non-custodial: you can withdraw to your own wallet at any time while assets are liquid. Live on Stellar testnet and not yet audited.",
    tab: "The architecture, end to end.",
  },
];

/* ─────────────────────────────────────────────────────────────────
   UTILITY: Animated cursor
───────────────────────────────────────────────────────────────── */
function Cursor({ x, y, clicking }: { x: number; y: number; clicking: boolean }) {
  return (
    <motion.div
      className="absolute pointer-events-none z-30"
      animate={{ left: x, top: y }}
      transition={{ duration: 0.55, ease: [0.23, 1, 0.32, 1] }}
      style={{ left: x, top: y }}
    >
      <motion.svg
        width="20" height="24" viewBox="0 0 20 24" fill="none"
        animate={{ scale: clicking ? 0.82 : 1 }}
        transition={{ duration: 0.12 }}
      >
        <path
          d="M3 2L3 18L7.5 13.5L10.5 20L13 19L10 12.5L16 12.5L3 2Z"
          fill="black" stroke="white" strokeWidth="1.2"
        />
      </motion.svg>
    </motion.div>
  );
}



/* ─────────────────────────────────────────────────────────────────
   PANEL 1 — DEPOSIT  (cursor clicks product → venue bars fill → confirm)
───────────────────────────────────────────────────────────────── */
const vaultRows = [
  { name: "Nester Savings Rate", apy: "USDC",  bar: 100 },
  { name: "Blend",               apy: "venue", bar: 55 },
  { name: "DeFindex",            apy: "venue", bar: 45 },
];

function DepositScene({ playing }: { playing: boolean }) {
  // sequence: 0=idle, 1=cursor moves to the product, 2=click, 3=bars animate, 4=confirm button
  const [seq, setSeq] = useState(0);
  const [selectedIdx, setSelectedIdx] = useState(-1);
  const [barsVisible, setBarsVisible] = useState(false);
  const [confirmed, setConfirmed] = useState(false);

  useEffect(() => {
    if (!playing) { setSeq(0); setSelectedIdx(-1); setBarsVisible(false); setConfirmed(false); return; }    setSeq(0); setSelectedIdx(-1); setBarsVisible(false); setConfirmed(false);    const t1 = setTimeout(() => setSeq(1), 600);
    const t2 = setTimeout(() => { setSeq(2); setSelectedIdx(0); }, 1400);
    const t3 = setTimeout(() => { setSeq(3); setBarsVisible(true); }, 1900);
    const t4 = setTimeout(() => setSeq(4), 3200);
    const t5 = setTimeout(() => { setSeq(5); setConfirmed(true); }, 3900);
    return () => [t1, t2, t3, t4, t5].forEach(clearTimeout);
  }, [playing]);

  // Cursor targets (relative to panel)
  const cursorPos = [
    { x: 280, y: 30  },   // 0 idle top-right
    { x: 120, y: 52 },    // 1 hover product row
    { x: 120, y: 52 },    // 2 click
    { x: 120, y: 52 },    // 3 still
    { x: 178, y: 222 },  // 4 hover button
    { x: 178, y: 222 },  // 5 click button
  ];

  const pos = cursorPos[Math.min(seq, cursorPos.length - 1)];

  return (
    <div className="relative w-full select-none" style={{ height: 310 }}>
      <Cursor x={pos.x} y={pos.y} clicking={seq === 2 || seq === 5} />

      {/* Product and venue rows */}
      <div className="flex flex-col gap-2 pt-2">
        {vaultRows.map((v, i) => {
          const isSelected = selectedIdx === i;
          return (
            <motion.div
              key={v.name}
              initial={{ opacity: 0, x: -12 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: i * 0.06, duration: 0.35 }}
              className={`rounded-xl ${isSelected ? 'gradient-border-active' : ''}`}
            >
              <div
                className={`rounded-[10px] px-4 py-3 flex items-center gap-3 transition-all duration-300 ${
                  isSelected
                    ? "bg-[hsl(0,0%,90%)] shadow-sm"
                    : "border border-black/[0.08] bg-white/60"
                }`}
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1.5">
                    <span className={`text-[12px] font-semibold ${isSelected ? "text-black" : "text-black/60"}`}>{v.name}</span>
                    {isSelected && (
                      <motion.span
                        initial={{ opacity: 0, scale: 0.7 }}
                        animate={{ opacity: 1, scale: 1 }}
                        className="text-[8px] font-bold tracking-wider uppercase rounded-full px-2 py-0.5 text-black/50"
                        style={{ border: "1px solid rgba(0,0,0,0.15)" }}
                      >
                        Selected
                      </motion.span>
                    )}
                  </div>
                  <div className="w-full h-[3px] bg-black/[0.07] rounded-full overflow-hidden">
                    <motion.div
                      className="h-full rounded-full"
                      style={{ background: isSelected ? "linear-gradient(90deg, #6025f5, #ff5555 50%, #facc15)" : "rgba(0,0,0,0.7)" }}
                      initial={{ width: 0 }}
                      animate={{ width: barsVisible ? `${v.bar}%` : 0 }}
                      transition={{ duration: 1.1, delay: i * 0.1, ease: [0.23, 1, 0.32, 1] }}
                    />
                  </div>
                </div>
                <span className={`text-[13px] font-bold tabular-nums flex-shrink-0 ${isSelected ? "text-black" : "text-black/40"}`}>{v.apy}</span>
              </div>
            </motion.div>
          );
        })}
      </div>

      {/* Confirm button */}
      <motion.div
        className="mt-3"
        initial={{ opacity: 0 }}
        animate={{ opacity: seq >= 3 ? 1 : 0 }}
        transition={{ duration: 0.3 }}
      >
        <motion.div
          className={`w-full py-3 rounded-xl text-center text-[11px] font-bold tracking-[0.15em] uppercase transition-all duration-300 ${
            confirmed ? "bg-black text-white" : "bg-black/10 text-black/60 border border-black/15"
          }`}
          animate={confirmed ? { scale: [1, 0.97, 1] } : {}}
          transition={{ duration: 0.2 }}
        >
          {confirmed ? "✓ Depositing USDC…" : "Deposit USDC"}
        </motion.div>
      </motion.div>
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   PANEL 2 — OPTIMIZE  (node graph: Nester hub → protocol nodes)
   Layout (SVG coords, container 390×310):
     Hub:   cx=195, cy=62  (w=72, so left=159 top=26)
     Blend: cx=195, cy=248 (w=80, so left=155 top=218)
     Nester(right)→reuse as a second node: cx=322, cy=228 (left=282 top=198)
───────────────────────────────────────────────────────────────── */

// Node positions — calculated so paths are accurate
// Hub is at top, nodes sit below it but above the footer (which is ~52px, absolute bottom-0 in 320px container)
const HUB = { cx: 195, cy: 52, r: 36 };  // bottom of hub = 88
const NODES = [
  {
    id: "blend", label: "Blend", apy: "USDC lending",
    cx: 110, cy: 192,    // center of node card
    left: 68, top: 158,  // card top-left (card ~68px tall, center at +34)
    logo: (
      <div className="w-5 h-5 overflow-hidden rounded-sm">
        <Image src="/logos/blend.svg" alt="Blend" width={20} height={20} />
      </div>
    ),
    bg: "#c3812418",
  },
  {
    id: "defindex", label: "DeFindex", apy: "USDC vault",
    cx: 280, cy: 192,
    left: 238, top: 158,
    logo: (
      <div className="w-5 h-5 overflow-hidden rounded-sm bg-black flex items-center justify-center text-white text-[9px] font-bold">
        D
      </div>
    ),
    bg: "#11111118",
  },
];

// SVG cubic-bezier paths: hub bottom → top edge of each node card
// End at n.top so lines stop exactly at the card border, never inside it
const NODE_PATHS = NODES.map((n) =>
  `M ${HUB.cx} ${HUB.cy + HUB.r} C ${HUB.cx} ${HUB.cy + HUB.r + 38} ${n.cx} ${n.top - 12} ${n.cx} ${n.top}`
);

function OptimizeScene({ playing }: { playing: boolean }) {
  const [seq, setSeq] = useState(0);
  const [apyIdx, setApyIdx] = useState(0);

  const blendApy = ["USDC lending", "USDC lending"];

  useEffect(() => {
    if (!playing) { setSeq(0); setApyIdx(0); return; }
    setSeq(0); setApyIdx(0);
    const t1 = setTimeout(() => setSeq(1), 500);
    const t2 = setTimeout(() => setSeq(2), 1000);
    let tick = 0;
    const iv = setInterval(() => {
      tick++;
      setApyIdx((p) => (p + 1) % blendApy.length);
      if (tick >= 6) clearInterval(iv);
    }, 900);
    return () => { clearInterval(iv); [t1, t2].forEach(clearTimeout); };
  }, [playing]);

  return (
    <div className="relative w-full select-none mx-auto" style={{ height: 310, maxWidth: 390 }}>
      {/* SVG for paths + dots — same coordinate space */}
      <svg
        viewBox="0 0 390 310"
        className="absolute inset-0 w-full h-full pointer-events-none"
        preserveAspectRatio="xMidYMid meet"
        style={{ overflow: "visible" }}
      >
        <defs>
          {NODE_PATHS.map((d, i) => (
            <path key={`def-${i}`} id={`npath-${i}`} d={d} fill="none" />
          ))}
        </defs>

        {/* Animated connector lines */}
        {NODE_PATHS.map((d, i) => (
          <motion.path
            key={`line-${i}`}
            d={d}
            fill="none"
            strokeWidth="1.5"
            stroke="rgba(0,0,0,0.13)"
            strokeDasharray="5 4"
            initial={{ pathLength: 0, opacity: 0 }}
            animate={{ pathLength: seq >= 1 ? 1 : 0, opacity: seq >= 1 ? 1 : 0 }}
            transition={{ duration: 0.55, delay: 0.1 + i * 0.12, ease: [0.23, 1, 0.32, 1] }}
          />
        ))}

        {/* Flowing signal dots along each path */}
        {seq >= 2 && NODE_PATHS.map((_, i) => (
          <g key={`dots-${i}`}>
            <circle r="3.5" fill="#111" opacity={0.75}>
              <animateMotion dur="1.6s" repeatCount="indefinite" begin={`${i * 0.45}s`}>
                <mpath href={`#npath-${i}`} />
              </animateMotion>
            </circle>
            <circle r="2" fill="#111" opacity={0.38}>
              <animateMotion dur="1.6s" repeatCount="indefinite" begin={`${i * 0.45 + 0.55}s`}>
                <mpath href={`#npath-${i}`} />
              </animateMotion>
            </circle>
          </g>
        ))}
      </svg>

      {/* Hub — Nester logo */}
      <motion.div
        initial={{ scale: 0, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        transition={{ type: "spring", damping: 14, stiffness: 200 }}
        className="absolute"
        style={{ left: HUB.cx - HUB.r, top: HUB.cy - HUB.r }}
      >
        <div
          className="w-[72px] h-[72px] rounded-2xl bg-black flex flex-col items-center justify-center shadow-xl overflow-hidden"
        >
          <Image src="/logo.png" alt="Nester" width={40} height={40} className="object-contain" />
          <motion.span
            animate={{ opacity: [0.4, 1, 0.4] }}
            transition={{ duration: 1.4, repeat: Infinity }}
            className="text-white/50 text-[7px] tracking-widest mt-0.5"
          >
            TESTNET
          </motion.span>
        </div>
      </motion.div>

      {/* Protocol nodes */}
      {NODES.map((n, i) => (
        <motion.div
          key={n.id}
          initial={{ scale: 0, opacity: 0 }}
          animate={{ scale: 1, opacity: 1 }}
          transition={{ type: "spring", damping: 14, stiffness: 160, delay: 0.3 + i * 0.15 }}
          className="absolute"
          style={{ left: n.left, top: n.top }}
        >
          <div
            className="rounded-xl border border-black/10 bg-white/85 shadow-sm px-3 py-2 flex flex-col items-center"
            style={{ minWidth: 80, background: n.bg }}
          >
            <div className="mb-1">{n.logo}</div>
            <span className="text-[10px] font-semibold text-black/75 leading-tight">{n.label}</span>
            <AnimatePresence mode="wait">
              <motion.span
                key={n.id === "blend" ? blendApy[apyIdx] : n.apy}
                initial={{ opacity: 0, y: -3 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: 3 }}
                transition={{ duration: 0.22 }}
                className="text-[11px] font-bold text-black tabular-nums mt-0.5"
              >
                {n.id === "blend" ? blendApy[apyIdx] : n.apy}
              </motion.span>
            </AnimatePresence>
          </div>
        </motion.div>
      ))}

      {/* Blended APY footer */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: seq >= 2 ? 1 : 0, y: seq >= 2 ? 0 : 12 }}
        transition={{ duration: 0.45 }}
        className="absolute bottom-0 left-0 right-0 flex items-center justify-between rounded-xl border border-black/10 bg-black/[0.03] px-4 py-3"
      >
        <span className="text-[9px] tracking-[0.18em] uppercase text-black/30 font-semibold">Realized 7-day rate</span>
        <motion.span
          animate={{ opacity: [0.7, 1, 0.7] }}
          transition={{ duration: 1.2, repeat: Infinity }}
          className="text-[18px] font-bold text-black tabular-nums"
        >
          Varies
        </motion.span>
      </motion.div>
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   PANEL 3 — SAFEGUARDS
   Uses inline JSX connectors between boxes — no broken SVG % paths.
───────────────────────────────────────────────────────────────── */
const SAFEGUARD_STEPS = [
  {
    label: "Venue Caps", sub: "Absolute and percentage cap per venue", value: "Capped",
    logoSlot: null as React.ReactNode,
  },
  {
    label: "Public Timelock", sub: "Risk-adding changes wait · 3 days by default", value: "Timelocked",
    logoSlot: null as React.ReactNode,
  },
  {
    label: "Locked Reserve", sub: "Covers saver losses only, via governance", value: "Reserve",
    logoSlot: null as React.ReactNode,
  },
];

/* Connector gap between boxes: a 20px tall strip with a dashed line + animated traveling dot */
function PipeConnector({ active, label }: { active: boolean; label?: string }) {
  return (
    <div className="relative flex items-center justify-center" style={{ height: 20 }}>
      {/* Dashed vertical line — just 1px wide, centered */}
      <motion.div
        className="w-px"
        style={{
          height: 20,
          transformOrigin: "top",
          background: "repeating-linear-gradient(to bottom, rgba(0,0,0,0.18) 0px, rgba(0,0,0,0.18) 4px, transparent 4px, transparent 8px)",
        }}
        initial={{ scaleY: 0, opacity: 0 }}
        animate={{ scaleY: active ? 1 : 0, opacity: active ? 1 : 0 }}
        transition={{ duration: 0.35, ease: [0.23, 1, 0.32, 1] }}
      />
      {/* Traveling dot — only within the 20px gap */}
      {active && (
        <motion.div
          className="absolute w-[6px] h-[6px] rounded-full bg-black/60"
          animate={{ y: [-10, 10] }}
          transition={{ duration: 0.65, repeat: Infinity, ease: "easeInOut", repeatType: "reverse" }}
        />
      )}
      {label && (
        <motion.span
          initial={{ opacity: 0 }}
          animate={{ opacity: active ? 0.4 : 0 }}
          transition={{ delay: 0.3 }}
          className="absolute left-[calc(50%+10px)] text-[8px] text-black/40 font-mono whitespace-nowrap"
        >
          {label}
        </motion.span>
      )}
    </div>
  );
}

function SafeguardsScene({ playing }: { playing: boolean }) {
  const [step, setStep] = useState(-1);
  const [done, setDone] = useState(false);

  useEffect(() => {
    if (!playing) { setStep(-1); setDone(false); return; }    setStep(-1); setDone(false);    const t0 = setTimeout(() => setStep(0), 400);
    const t1 = setTimeout(() => setStep(1), 1500);
    const t2 = setTimeout(() => setStep(2), 2800);
    const t3 = setTimeout(() => setDone(true), 3600);
    return () => [t0, t1, t2, t3].forEach(clearTimeout);
  }, [playing]);

  return (
    <div className="w-full select-none flex flex-col">
      {/* Pipeline: box → connector → box → connector → box */}
      {SAFEGUARD_STEPS.map((s, i) => (
        <React.Fragment key={s.label}>
          <motion.div
            initial={{ opacity: 0, x: -16 }}
            animate={{ opacity: step >= i ? 1 : 0.22, x: 0 }}
            transition={{ duration: 0.45 }}
            className={`rounded-xl ${step === i ? 'gradient-border-active' : ''}`}
          >
            <div
              className={`rounded-[10px] px-5 py-3.5 flex items-center justify-between transition-all duration-300 ${
                step === i ? "bg-[hsl(0,0%,90%)] shadow-md border border-transparent" :
                step > i   ? "border border-black/10 bg-white/65" :
                             "border border-black/[0.06] bg-white/40"
              }`}
            >
              <div className="flex items-center gap-3 flex-1 min-w-0">
                {s.logoSlot}
                <div>
                  <p className="text-[8px] font-bold tracking-[0.2em] uppercase text-black/28 mb-0.5">{s.label}</p>
                  <p className={`text-[1.15rem] font-bold leading-tight transition-colors duration-300 ${step >= i ? "text-black" : "text-black/28"}`}>{s.value}</p>
                  <p className="text-[9px] text-black/28 mt-0.5">{s.sub}</p>
                </div>
              </div>
              {/* Spinner while active */}
              {step === i && !done && (
                <motion.div
                  animate={{ rotate: 360 }}
                  transition={{ duration: 0.85, repeat: Infinity, ease: "linear" }}
                  className="w-5 h-5 rounded-full border-2 border-black/15 border-t-black/65 flex-shrink-0"
                />
              )}
              {/* Check when done */}
              {(step > i || (done && i === 2)) && (
                <motion.div
                  initial={{ scale: 0 }}
                  animate={{ scale: 1 }}
                  transition={{ type: "spring", damping: 10 }}
                  className="w-6 h-6 rounded-full bg-black/[0.07] border border-black/15 flex items-center justify-center flex-shrink-0"
                >
                  <svg width="10" height="8" viewBox="0 0 10 8" fill="none">
                    <path d="M1.5 4L4 6.5L8.5 1.5" stroke="#111" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round"/>
                  </svg>
                </motion.div>
              )}
            </div>
          </motion.div>

          {/* Connector between boxes — ONLY between, not after last */}
          {i < SAFEGUARD_STEPS.length - 1 && (
            <PipeConnector
              active={step > i}
              label={i === 0 ? "on-chain" : undefined}
            />
          )}
        </React.Fragment>
      ))}

      {/* Fee bar */}
      {done && (
        <motion.div
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.35 }}
          className="mt-2 flex items-center justify-between rounded-xl border border-black/[0.08] bg-white/50 px-4 py-2.5"
        >
          <span className="text-[9px] tracking-[0.15em] uppercase text-black/30 font-semibold">Guardian</span>
          <span className="text-[11px] font-bold text-black/60">can only reduce risk</span>
        </motion.div>
      )}
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   PANEL 4 — ARCHITECTURE  (layered infrastructure stack)
───────────────────────────────────────────────────────────────── */
const ARCH_LAYERS = [
  {
    label: "SHARES LAYER",
    desc: "USDC deposited for savings shares",
    detail: "USDC",
    color: "#6025f5",
    icon: (
      <svg width="15" height="15" viewBox="0 0 16 16" fill="none">
        <rect x="2" y="5" width="12" height="9" rx="1.5" stroke="currentColor" strokeWidth="1.3"/>
        <path d="M5 5V3.5a3 3 0 016 0V5" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round"/>
      </svg>
    ),
  },
  {
    label: "YIELD LAYER",
    desc: "Routed to Blend and DeFindex on Stellar",
    detail: "Realized",
    color: "#ff5555",
    icon: (
      <svg width="15" height="15" viewBox="0 0 16 16" fill="none">
        <path d="M2 13l4-5 3 3 5-8" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round"/>
      </svg>
    ),
  },
  {
    label: "SAFEGUARD LAYER",
    desc: "Caps, locked reserve and public timelock",
    detail: "Timelocked",
    color: "#facc15",
    icon: (
      <svg width="15" height="15" viewBox="0 0 16 16" fill="none">
        <path d="M8 1.5l5.5 2v4c0 3.2-2.3 5.6-5.5 7-3.2-1.4-5.5-3.8-5.5-7v-4L8 1.5z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round"/>
      </svg>
    ),
  },
];

const CONNECTOR_LABELS = ["funds allocate", "risk is bounded"];

function ArchitectureScene({ playing }: { playing: boolean }) {
  const [activeLayer, setActiveLayer] = useState(-1);
  const [flowActive, setFlowActive] = useState(false);

  useEffect(() => {
    if (!playing) { setActiveLayer(-1); setFlowActive(false); return; }    setActiveLayer(-1); setFlowActive(false);    const t0 = setTimeout(() => setActiveLayer(0), 500);
    const t1 = setTimeout(() => setActiveLayer(1), 1400);
    const t2 = setTimeout(() => setActiveLayer(2), 2300);
    const t3 = setTimeout(() => setFlowActive(true), 3200);
    return () => [t0, t1, t2, t3].forEach(clearTimeout);
  }, [playing]);

  return (
    <div className="w-full select-none flex flex-col">
      {ARCH_LAYERS.map((layer, i) => (
        <React.Fragment key={layer.label}>
          {/* Layer card */}
          <motion.div
            initial={{ opacity: 0, x: -16 }}
            animate={{ opacity: activeLayer >= i ? 1 : 0.15, x: activeLayer >= i ? 0 : -16 }}
            transition={{ duration: 0.5, ease: [0.23, 1, 0.32, 1] }}
            className={`rounded-xl px-4 py-3 flex items-center gap-3 transition-all duration-300 ${
              activeLayer === i
                ? "bg-[hsl(0,0%,90%)] shadow-md border border-black/[0.12]"
                : activeLayer > i
                  ? "border border-black/[0.08] bg-white/60"
                  : "border border-black/[0.04] bg-white/30"
            }`}
          >
            {/* Color icon */}
            <div
              className="w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0 transition-all duration-300"
              style={{
                backgroundColor: activeLayer >= i ? layer.color + "18" : "rgba(0,0,0,0.03)",
                color: activeLayer >= i ? layer.color : "rgba(0,0,0,0.2)",
              }}
            >
              {layer.icon}
            </div>

            {/* Text */}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-0.5">
                <span className={`text-[8px] font-bold tracking-[0.18em] uppercase transition-colors duration-300 ${
                  activeLayer >= i ? "text-black/50" : "text-black/15"
                }`}>
                  {layer.label}
                </span>
              </div>
              <p className={`text-[10.5px] leading-tight transition-colors duration-300 m-0 ${
                activeLayer >= i ? "text-black/55" : "text-black/12"
              }`}>
                {layer.desc}
              </p>
            </div>

            {/* Detail badge */}
            <motion.span
              animate={activeLayer === i ? { scale: [1, 1.05, 1] } : {}}
              transition={{ duration: 0.3 }}
              className={`text-[10px] font-bold tabular-nums flex-shrink-0 px-2.5 py-1 rounded-lg transition-all duration-300 ${
                activeLayer >= i
                  ? "text-black/60 bg-black/[0.05] border border-black/[0.08]"
                  : "text-black/10"
              }`}
            >
              {layer.detail}
            </motion.span>
          </motion.div>

          {/* Connector between layers */}
          {i < ARCH_LAYERS.length - 1 && (
            <div className="relative flex items-center justify-center" style={{ height: 16 }}>
              <motion.div
                className="w-px"
                style={{
                  height: 16,
                  transformOrigin: "top",
                  background: "repeating-linear-gradient(to bottom, rgba(0,0,0,0.18) 0px, rgba(0,0,0,0.18) 4px, transparent 4px, transparent 8px)",
                }}
                initial={{ scaleY: 0, opacity: 0 }}
                animate={{ scaleY: activeLayer > i ? 1 : 0, opacity: activeLayer > i ? 1 : 0 }}
                transition={{ duration: 0.35, ease: [0.23, 1, 0.32, 1] }}
              />
              {activeLayer > i && (
                <motion.div
                  className="absolute w-[5px] h-[5px] rounded-full bg-black/50"
                  animate={{ y: [-8, 8] }}
                  transition={{ duration: 0.6, repeat: Infinity, ease: "easeInOut", repeatType: "reverse" }}
                />
              )}
              {activeLayer > i && (
                <motion.span
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 0.35 }}
                  transition={{ delay: 0.2 }}
                  className="absolute left-[calc(50%+12px)] text-[7px] text-black/35 font-mono whitespace-nowrap"
                >
                  {CONNECTOR_LABELS[i]}
                </motion.span>
              )}
            </div>
          )}
        </React.Fragment>
      ))}

      {/* Risk controls strip — appears after all layers active */}
      {flowActive && (
        <motion.div
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.45 }}
          className="mt-3 rounded-xl border border-black/[0.08] bg-black/[0.03] px-4 py-3"
        >
          <div className="flex items-center justify-between mb-2">
            <motion.span
              animate={{ opacity: [0.5, 1, 0.5] }}
              transition={{ duration: 2, repeat: Infinity }}
              className="text-[8px] tracking-[0.2em] uppercase font-bold"
              style={{ color: "#6025f5", opacity: 0.6 }}
            >
              Risk Controls
            </motion.span>
            <span className="text-[8px] text-black/25 font-mono">all layers</span>
          </div>
          <div className="flex gap-1.5">
            {["Savings", "Yield", "Safeguards"].map((name, i) => (
              <motion.div
                key={name}
                initial={{ scaleX: 0 }}
                animate={{ scaleX: 1 }}
                transition={{ delay: i * 0.15, duration: 0.3 }}
                className="flex-1 h-[3px] rounded-full overflow-hidden origin-left"
              >
                <motion.div
                  className="h-full rounded-full"
                  style={{ background: `linear-gradient(90deg, ${ARCH_LAYERS[i].color}, ${ARCH_LAYERS[i].color}88)` }}
                  animate={{ opacity: [0.4, 1, 0.4] }}
                  transition={{ duration: 1.5, repeat: Infinity, delay: i * 0.3 }}
                />
              </motion.div>
            ))}
          </div>
        </motion.div>
      )}
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   PANEL ROUTER
───────────────────────────────────────────────────────────────── */
function ScenePanel({ stepId, playing }: { stepId: string; playing: boolean }) {
  switch (stepId) {
    case "deposit":       return <DepositScene       playing={playing} />;
    case "optimize":      return <OptimizeScene      playing={playing} />;
    case "safeguards":    return <SafeguardsScene    playing={playing} />;
    case "architecture":  return <ArchitectureScene  playing={playing} />;
    default:              return null;
  }
}

/* ─────────────────────────────────────────────────────────────────
   MAIN
───────────────────────────────────────────────────────────────── */
const STEP_DURATION = 6500;

export function HowItWorks() {
  const [active, setActive] = useState(0);
  const [playing, setPlaying] = useState(false);
  const sectionRef = useRef<HTMLDivElement>(null);
  const isInView = useInView(sectionRef, { once: true, margin: "-80px" });

  /* Trigger playing on mount/step change */
  useEffect(() => {
    setPlaying(false);    const t = setTimeout(() => setPlaying(true), 60);
    return () => clearTimeout(t);
  }, [active]);

  /* Auto-advance */
  useEffect(() => {
    if (!isInView) return;
    const id = setInterval(() => setActive((p) => (p + 1) % STEPS.length), STEP_DURATION);
    return () => clearInterval(id);
  }, [isInView]);

  const step = STEPS[active];

  return (
    <section
      ref={sectionRef}
      className="w-full py-24 md:py-36 overflow-hidden"
      style={{ background: "hsl(0,0%,85%)" }}
    >
      <div className="max-w-6xl mx-auto px-6 md:px-12">

        {/* ── Header ── */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          animate={isInView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.7, ease: [0.23, 1, 0.32, 1] }}
          className="mb-12 md:mb-16"
        >
          <p className="text-[10px] font-bold tracking-[0.25em] uppercase text-black/30 mb-5">
            How Nester Works
          </p>
          <h2
            className="text-[2rem] md:text-[2.8rem] lg:text-[3.4rem] font-light text-black leading-[1.1] tracking-tight m-0 max-w-2xl"
            style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
          >
            You bring the crypto.{" "}
            <span className="italic font-medium" style={{ fontFamily: "var(--font-cormorant, serif)" }}>
              We handle the rest.
            </span>
          </h2>
        </motion.div>

        {/* ── Card ── */}
        <motion.div
          initial={{ opacity: 0, y: 32 }}
          animate={isInView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.75, delay: 0.15, ease: [0.23, 1, 0.32, 1] }}
          className="rounded-t-3xl overflow-hidden"
          style={{
            background: "hsl(0,0%,91%)",
            boxShadow: "0 1px 2px rgba(0,0,0,0.05), 0 8px 40px rgba(0,0,0,0.06)",
          }}
        >
          {/* Browser chrome bar */}
          <div className="flex items-center gap-3 px-6 py-4 border-b border-black/[0.06]">
            <div className="flex gap-2">
              <div className="w-3 h-3 rounded-full bg-[#ff5f57]" />
              <div className="w-3 h-3 rounded-full bg-[#febc2e]" />
              <div className="w-3 h-3 rounded-full bg-[#28c840]" />
            </div>
            <div className="flex-1 flex items-center justify-center">
              <div className="flex items-center gap-1.5 bg-black/[0.05] border border-black/[0.07] rounded-lg px-4 py-1.5">
                <span className="text-[9px] tracking-wider text-black/25 uppercase font-medium">usenester.xyz</span>
              </div>
            </div>
            <AnimatePresence mode="wait">
              <motion.div
                key={active}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="flex items-center gap-1.5"
              >
                <motion.span
                  animate={{ opacity: [0.4, 1, 0.4] }}
                  transition={{ duration: 1.2, repeat: Infinity }}
                  className="w-1.5 h-1.5 rounded-full bg-black inline-block"
                />
                <span className="text-[9px] font-bold tracking-[0.15em] uppercase text-black/30">
                  {step.step}
                </span>
              </motion.div>
            </AnimatePresence>
          </div>

          {/* Body: left text + right scene */}
          <div className="flex flex-col lg:flex-row" style={{ minHeight: 480 }}>

            {/* Left */}
            <div className="flex flex-col justify-center px-8 py-10 md:px-12 lg:px-14 lg:w-[40%] shrink-0 lg:border-r border-black/[0.06]">
              <AnimatePresence mode="wait">
                <motion.div
                  key={active + "-text"}
                  initial={{ opacity: 0, x: -20 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: -12 }}
                  transition={{ duration: 0.4, ease: [0.23, 1, 0.32, 1] }}
                >
                  <p className="text-[9px] font-bold tracking-[0.22em] uppercase text-black/25 mb-5">
                    Step {step.step}
                  </p>
                  <h3
                    className="text-[1.65rem] md:text-[1.95rem] font-light text-black leading-[1.12] tracking-tight m-0 mb-5 whitespace-pre-line"
                    style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
                  >
                    {step.headline}
                  </h3>
                  <p
                    className="text-[13.5px] md:text-[14.5px] leading-[1.75] text-black/40 m-0 max-w-xs"
                    style={{ fontFamily: "var(--font-inter, sans-serif)" }}
                  >
                    {step.body}
                  </p>
                  <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    transition={{ delay: 0.4 }}
                    className="mt-8 flex items-center gap-2 group cursor-pointer"
                  >
                    <span className="text-[10px] font-bold tracking-[0.15em] uppercase text-black/30 group-hover:text-black/60 transition-colors duration-200">
                      Learn more
                    </span>
                    <svg width="12" height="12" viewBox="0 0 12 12" fill="none" className="text-black/25 group-hover:translate-x-0.5 transition-transform duration-200">
                      <path d="M2.5 6H9.5M6.5 3L9.5 6L6.5 9" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round"/>
                    </svg>
                  </motion.div>
                </motion.div>
              </AnimatePresence>
            </div>

            {/* Right — animated scene */}
            <div className="flex-1 flex items-center justify-center px-6 py-10 md:px-10 md:py-12 relative overflow-hidden">
              <div
                className="absolute inset-0 pointer-events-none"
                style={{ background: "radial-gradient(55% 55% at 50% 50%, rgba(255,255,255,0.55) 0%, transparent 100%)" }}
              />
              <div className="relative w-full max-w-[390px]">
                <AnimatePresence mode="wait">
                  <motion.div
                    key={active + "-scene"}
                    initial={{ opacity: 0, y: 18, scale: 0.98 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: -12, scale: 0.98 }}
                    transition={{ duration: 0.45, ease: [0.23, 1, 0.32, 1] }}
                  >
                    <ScenePanel stepId={step.id} playing={playing} />
                  </motion.div>
                </AnimatePresence>
              </div>
            </div>
          </div>

          {/* Auto-progress bar */}
          <div className="h-[2px] bg-black/[0.05]">
            <AnimatePresence mode="wait">
              <motion.div
                key={active + "-bar"}
                initial={{ scaleX: 0 }}
                animate={{ scaleX: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: STEP_DURATION / 1000, ease: "linear" }}
                className="h-full bg-black/35 origin-left"
              />
            </AnimatePresence>
          </div>
        </motion.div>

        {/* ── Tab strip ── */}
        <motion.div
          initial={{ opacity: 0, y: 14 }}
          animate={isInView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.55, delay: 0.3 }}
          className="grid grid-cols-3 lg:grid-cols-5 border border-t-0 border-black/[0.08] overflow-hidden"
          style={{ background: "hsl(0,0%,88%)" }}
        >
          {STEPS.map((s, i) => {
            const isActive = i === active;
            return (
              <button
                key={s.id}
                onClick={() => setActive(i)}
                className={`relative flex flex-col gap-2 px-6 py-6 text-left border-0 outline-none cursor-pointer transition-all duration-300 ${
                  i < STEPS.length - 1 ? "border-r border-black/[0.07]" : ""
                } ${i >= 3 ? "border-t border-black/[0.07] lg:border-t-0" : ""} ${
                  isActive ? "bg-white/50" : "hover:bg-white/25"
                }`}
              >
                {/* Gradient accent top line */}
                <motion.div
                  initial={false}
                  animate={{ scaleX: isActive ? 1 : 0, opacity: isActive ? 1 : 0 }}
                  transition={{ duration: 0.35, ease: [0.23, 1, 0.32, 1] }}
                  className="absolute top-0 left-0 right-0 h-[2.5px] origin-left"
                  style={{ background: "linear-gradient(90deg, #6025f5 0%, #ff5555 45%, #facc15 100%)" }}
                />
                <span
                  className={`text-[8px] font-bold tracking-[0.22em] uppercase transition-colors duration-200 ${
                    isActive ? "text-black/50" : "text-black/20"
                  }`}
                >
                  {s.step}
                </span>
                <p
                  className={`text-[13px] md:text-[14px] font-semibold m-0 leading-snug transition-colors duration-200 ${
                    isActive ? "text-black/85" : "text-black/25"
                  }`}
                  style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
                >
                  {s.label}
                </p>
                <p
                  className={`text-[11px] m-0 leading-relaxed hidden md:block transition-colors duration-200 ${
                    isActive ? "text-black/35" : "text-black/12"
                  }`}
                  style={{ fontFamily: "var(--font-inter, sans-serif)" }}
                >
                  {s.tab}
                </p>
              </button>
            );
          })}
        </motion.div>
      </div>
    </section>
  );
}
