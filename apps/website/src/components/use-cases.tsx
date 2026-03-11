"use client";

import { useState, useRef, useEffect } from "react";
import { motion, useInView, AnimatePresence } from "framer-motion";
import Image from "next/image";

/* ─────────────────────────────────────────────────────────────────
   CONFIG
───────────────────────────────────────────────────────────────── */
interface Scenario {
  id: string;
  label: string;
  shortTag: string;
  subtitle: string;
}

const SCENARIOS: Scenario[] = [
  {
    id: "yield",
    label: "Yield Aggregation",
    shortTag: "Yield",
    subtitle: "Deposit once, earn across protocols.",
  },
  {
    id: "savings",
    label: "Savings Layer",
    shortTag: "Savings",
    subtitle: "Goal-based vaults earning platform fees.",
  },
  {
    id: "offramp",
    label: "Offramp Layer",
    shortTag: "Offramp",
    subtitle: "Smart LP routing for instant settlement.",
  },
  {
    id: "ai",
    label: "AI Portfolio",
    shortTag: "AI",
    subtitle: "Prometheus scans and optimizes.",
  },
];

const EASING = [0.23, 1, 0.32, 1] as const;
const STEP_DURATION = 16000;

/* ─────────────────────────────────────────────────────────────────
   HELPERS
───────────────────────────────────────────────────────────────── */

/** Typing animation for input fields */
function useTypedText(
  target: string,
  playing: boolean,
  startDelay: number,
  charDelay = 65,
) {
  const [text, setText] = useState("");

  useEffect(() => {
    if (!playing) {
      setText("");
      return;
    }

    let i = 0;
    const t = setTimeout(() => {
      const iv = setInterval(() => {
        setText(target.slice(0, ++i));
        if (i >= target.length) clearInterval(iv);
      }, charDelay);
      return () => clearInterval(iv);
    }, startDelay);

    return () => clearTimeout(t);
  }, [playing, target, startDelay, charDelay]);

  return text;
}

/** Counter animation */
function useCounter(
  playing: boolean,
  target: number,
  startDelay: number,
  step = 1,
  interval = 30,
) {
  const [value, setValue] = useState(0);

  useEffect(() => {
    if (!playing) {
      setValue(0);      return;
    }

    const t = setTimeout(() => {
      const iv = setInterval(() => {
        setValue((v) => {
          const next = v + step;
          return next >= target ? target : next;
        });
      }, interval);
      return () => clearInterval(iv);
    }, startDelay);

    return () => clearTimeout(t);
  }, [playing, target, startDelay, step, interval]);

  return value;
}

/* ─────────────────────────────────────────────────────────────────
   CURSOR
───────────────────────────────────────────────────────────────── */
function Cursor({
  x,
  y,
  clicking,
}: {
  x: number;
  y: number;
  clicking: boolean;
}) {
  return (
    <motion.div
      className="absolute pointer-events-none z-30"
      animate={{ left: x, top: y }}
      transition={{ duration: 0.55, ease: EASING }}
      style={{ left: x, top: y }}
    >
      <motion.svg
        width="20"
        height="24"
        viewBox="0 0 20 24"
        fill="none"
        animate={{ scale: clicking ? 0.82 : 1 }}
        transition={{ duration: 0.12 }}
      >
        <path
          d="M3 2L3 18L7.5 13.5L10.5 20L13 19L10 12.5L16 12.5L3 2Z"
          fill="black"
          stroke="white"
          strokeWidth="1.2"
        />
      </motion.svg>
    </motion.div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   BROWSER CHROME
───────────────────────────────────────────────────────────────── */
function BrowserChrome({ activeIdx }: { activeIdx: number }) {
  return (
    <div className="flex items-center gap-3 px-6 py-4 border-b border-black/[0.06]">
      <div className="flex gap-2">
        <div className="w-3 h-3 rounded-full bg-[#ff5f57]" />
        <div className="w-3 h-3 rounded-full bg-[#febc2e]" />
        <div className="w-3 h-3 rounded-full bg-[#28c840]" />
      </div>

      <div className="flex-1 flex items-center justify-center">
        <div className="flex items-center gap-1.5 bg-black/[0.05] border border-black/[0.07] rounded-lg px-4 py-1.5">
          <span className="text-[9px] tracking-wider text-black/25 font-medium">
            nester.com/app
          </span>
        </div>
      </div>

      <AnimatePresence mode="wait">
        <motion.div
          key={activeIdx}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="flex items-center gap-1.5"
        >
          <motion.span
            animate={{ opacity: [0.4, 1, 0.4] }}
            transition={{ duration: 1.2, repeat: Infinity }}
            className="w-1.5 h-1.5 rounded-full bg-black"
          />
          <span className="text-[9px] font-bold tracking-[0.15em] uppercase text-black/30">
            {String(activeIdx + 1).padStart(2, "0")} / 04
          </span>
        </motion.div>
      </AnimatePresence>
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   SCENARIO NAVIGATOR
───────────────────────────────────────────────────────────────── */
function ScenarioNavigator({
  activeIdx,
  onSelect,
}: {
  activeIdx: number;
  onSelect: (i: number) => void;
}) {
  return (
    <div className="flex flex-row lg:flex-col gap-0 p-0 lg:w-[240px] lg:shrink-0 border-b lg:border-b-0 lg:border-r border-black/[0.06] overflow-visible">
      {SCENARIOS.map((s, i) => {
        const isActive = i === activeIdx;
        return (
          <button
            key={s.id}
            onClick={() => onSelect(i)}
            className={`relative flex-1 lg:flex-none transition-all duration-300 ${isActive ? "gradient-border-active" : ""
              }`}
            style={{ borderRadius: 0 }}
          >
            <div
              className={`px-4 py-4 text-left transition-all duration-300 ${isActive
                  ? "bg-[hsl(0,0%,90%)]"
                  : "border-b border-black/[0.08] bg-white/60 hover:bg-white/80"
                }`}
              style={{ borderRadius: 0 }}
            >
              <p className="text-[9px] font-bold tracking-[0.22em] uppercase text-black/25 mb-2 m-0">
                {String(i + 1).padStart(2, "0")}
              </p>

              <p
                className={`text-[12px] font-semibold leading-snug m-0 hidden lg:block transition-colors duration-200 ${isActive ? "text-black/85" : "text-black/30"
                  }`}
                style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
              >
                {s.label}
              </p>

              <p
                className={`text-[10px] font-semibold m-0 lg:hidden transition-colors duration-200 ${isActive ? "text-black/85" : "text-black/30"
                  }`}
              >
                {s.shortTag}
              </p>

              {isActive && (
                <motion.p
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  className="text-[11px] text-black/35 leading-relaxed mt-1.5 m-0 hidden lg:block"
                  style={{ fontFamily: "var(--font-inter, sans-serif)" }}
                >
                  {s.subtitle}
                </motion.p>
              )}
            </div>
          </button>
        );
      })}
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   SCENE 1: YIELD AGGREGATION
───────────────────────────────────────────────────────────────── */
function YieldScene({ playing }: { playing: boolean }) {
  const [seq, setSeq] = useState(0);
  const [selectedStrategy, setSelectedStrategy] = useState(-1);
  const [selectedProtocol, setSelectedProtocol] = useState(-1);
  const depositTyped = useTypedText("25,000", playing, 2800, 80);
  const earningsCounter = useCounter(playing, 2125, 11000, 10, 25);

  useEffect(() => {
    if (!playing) {
      setSeq(0);      setSelectedStrategy(-1);
      setSelectedProtocol(-1);
      return;
    }

    setSeq(0);    setSelectedStrategy(-1);
    setSelectedProtocol(-1);

    const timers = [
      setTimeout(() => setSeq(1), 600),
      setTimeout(() => setSeq(2), 1400),
      setTimeout(() => setSelectedStrategy(1), 1400),
      setTimeout(() => setSeq(3), 2000),
      setTimeout(() => setSeq(4), 2800),
      setTimeout(() => setSeq(5), 4500),
      setTimeout(() => setSeq(6), 5200),
      setTimeout(() => setSeq(7), 6200),
      setTimeout(() => setSeq(8), 7500),
      setTimeout(() => {
        setSeq(9);
        setSelectedProtocol(0);
      }, 8800),
      setTimeout(() => setSeq(10), 10200),
      setTimeout(() => setSeq(11), 11800),
    ];

    return () => timers.forEach(clearTimeout);
  }, [playing]);

  const STRATEGIES = [
    { name: "Conservative", risk: "Low", apy: "5.2%" },
    { name: "Balanced", risk: "Moderate", apy: "9.8%" },
    { name: "Growth", risk: "High", apy: "13.4%" },
    { name: "Aggressive", risk: "Very High", apy: "16.8%" },
  ];

  const PROTOCOLS = [
    {
      name: "Aave",
      share: "$11,250",
      pct: 45,
      apy: "8.4%",
      util: "78%",
      logo: "/aave.jpg",
    },
    {
      name: "Blend",
      share: "$7,500",
      pct: 30,
      apy: "11.2%",
      util: "64%",
      logo: "/blend.svg",
    },
    {
      name: "Aquarius",
      share: "$3,750",
      pct: 15,
      apy: "13.8%",
      util: "41%",
      logo: "/aquirius.png",
    },
    {
      name: "Phoenix",
      share: "$2,500",
      pct: 10,
      apy: "9.6%",
      util: "55%",
      logo: "/phoenix.png",
    },
  ];

  const cursorPos = [
    { x: 320, y: 30 },
    { x: 70, y: 104 },
    { x: 70, y: 104 },
    { x: 140, y: 160 },
    { x: 140, y: 160 },
    { x: 340, y: 160 },
    { x: 340, y: 160 },
    { x: 150, y: 240 },
    { x: 150, y: 240 },
    { x: 370, y: 280 },
    { x: 370, y: 320 },
    { x: 370, y: 320 },
  ];

  const pos = cursorPos[Math.min(seq, cursorPos.length - 1)];

  return (
    <div
      className="relative w-full select-none overflow-hidden"
      style={{ height: 480 }}
    >
      <Cursor
        x={pos.x}
        y={pos.y}
        clicking={seq === 2 || seq === 6 || seq === 10}
      />

      <motion.div
        animate={{ y: seq >= 10 ? -240 : 0 }}
        transition={{ duration: 0.6, ease: EASING }}
        style={{ height: "100%" }}
      >
        {/* Strategy Pills */}
        <div className="mb-4">
          <p className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 mb-3 m-0">
            Choose Strategy
          </p>
          <div className="grid grid-cols-4 gap-2">
            {STRATEGIES.map((s, i) => (
              <motion.div
                key={s.name}
                initial={{ opacity: 0, y: -8 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: i * 0.05, duration: 0.3, ease: EASING }}
                className={`rounded-lg ${selectedStrategy === i ? "gradient-border-active" : ""}`}
              >
                <div
                  className={`rounded-[7px] px-3 py-3 text-center transition-all duration-300 text-[11px] ${selectedStrategy === i
                      ? "bg-[hsl(0,0%,90%)]"
                      : "border border-black/[0.08] bg-white/60"
                    }`}
                >
                  <p className="font-semibold text-black/70 m-0 mb-1">
                    {s.name}
                  </p>
                  <p className="text-[10px] text-black/35 m-0">{s.apy}</p>
                </div>
              </motion.div>
            ))}
          </div>
        </div>

        {/* Deposit Input */}
        {seq >= 3 && (
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.4, ease: EASING }}
            className="mb-4"
          >
            <label className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 block mb-2">
              Amount (USDC)
            </label>
            <div className="rounded-lg border border-black/[0.08] bg-white/70 px-4 py-3 font-mono text-[16px] text-black">
              {depositTyped}
              {seq === 4 && <span className="animate-pulse">|</span>}
            </div>
          </motion.div>
        )}

        {/* Deposit Button */}
        {seq >= 5 && (
          <motion.button
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.4, ease: EASING }}
            className="w-full py-3 rounded-lg bg-black text-white font-bold text-[12px] tracking-wider uppercase mb-4"
          >
            Deposit Now
          </motion.button>
        )}

        {/* Protocol Grid */}
        {seq >= 6 && (
          <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.4, ease: EASING }}
            className="grid grid-cols-2 gap-2 mb-2"
          >
            {PROTOCOLS.map((p, i) => (
              <motion.div
                key={p.name}
                initial={{ opacity: 0, scale: 0.9 }}
                animate={{
                  opacity: 1,
                }}
                transition={{ delay: i * 0.08, duration: 0.35, ease: EASING }}
                className={`rounded-lg border transition-all cursor-pointer ${selectedProtocol === i
                    ? "gradient-border-active border-transparent"
                    : "border-black/[0.08]"
                  } px-3 py-2 ${selectedProtocol === i ? "bg-white/90" : "bg-white/70"}`}
                onClick={() => setSelectedProtocol(i)}
              >
                <div className="flex flex-col items-center gap-2 mb-3 text-center">
                  <div className="w-12 h-12 relative flex-shrink-0 rounded-full border border-black/[0.08] bg-white/50 overflow-hidden">
                    <Image
                      src={p.logo}
                      alt={p.name}
                      fill
                      className="object-cover"
                    />
                  </div>
                  <span className="text-[10px] font-semibold text-black/70">
                    {p.name}
                  </span>
                </div>

                <p className="text-[10px] text-black/50 mb-2">
                  Your share:{" "}
                  <span className="font-bold text-black">{p.share}</span>
                </p>

                {/* Allocation bar */}
                <div className="w-full h-[2px] bg-black/[0.07] rounded-full overflow-hidden mb-2">
                  <motion.div
                    className="h-full rounded-full"
                    style={{
                      background:
                        "linear-gradient(90deg, #6025f5, #ff5555 50%, #facc15)",
                    }}
                    initial={{ width: 0 }}
                    animate={{ width: seq >= 7 ? `${p.pct}%` : 0 }}
                    transition={{ duration: 0.8, ease: EASING }}
                  />
                </div>
                <p className="text-[9px] text-black/40 mb-1">
                  {p.pct}% allocation
                </p>

                {/* APY & Utilization */}
                <div className="text-[10px] mb-2">
                  <div className="flex justify-between mb-1">
                    <span className="text-black/50">APY</span>
                    <span className="font-bold text-black">{p.apy}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-black/50">Util</span>
                    <span className="font-bold text-black">{p.util}</span>
                  </div>
                </div>

                {/* View Details Button */}
                <button className="w-full text-[9px] font-bold tracking-wider uppercase rounded px-2 py-1.5 border border-black/15 bg-black/[0.04] text-black/60 hover:bg-black/[0.08] transition-all">
                  View Details
                </button>
              </motion.div>
            ))}
          </motion.div>
        )}

        {/* Protocol Details Panel */}
        {selectedProtocol >= 0 && (
          <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={seq >= 10 ? { opacity: 1, y: 0 } : { opacity: 0, y: 12 }}
            transition={{ duration: 0.4, ease: EASING }}
            className="rounded-lg border border-black/[0.08] bg-white/80 px-4 py-4 mb-2"
          >
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <div className="w-8 h-8 relative rounded-full border border-black/[0.08] bg-white/50 overflow-hidden">
                  <Image
                    src={PROTOCOLS[selectedProtocol].logo}
                    alt={PROTOCOLS[selectedProtocol].name}
                    fill
                    className="object-cover"
                  />
                </div>
                <h4 className="text-[12px] font-bold text-black m-0">
                  {PROTOCOLS[selectedProtocol].name}
                </h4>
              </div>
              <button
                onClick={() => setSelectedProtocol(-1)}
                className="text-[16px] text-black/40 hover:text-black/60"
              >
                ✕
              </button>
            </div>

            <div className="space-y-3 text-[10px]">
              <div className="bg-black/[0.02] rounded px-3 py-2">
                <p className="text-black/40 m-0 mb-1">Total Value Locked</p>
                <p className="text-[14px] font-bold text-black m-0">
                  $1.2B TVL
                </p>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <div className="bg-black/[0.02] rounded px-2 py-2">
                  <p className="text-black/40 m-0 mb-1">Risk Level</p>
                  <p className="text-[12px] font-bold text-black m-0">Low</p>
                </div>
                <div className="bg-black/[0.02] rounded px-2 py-2">
                  <p className="text-black/40 m-0 mb-1">Est. Gas Fee</p>
                  <p className="text-[12px] font-bold text-black m-0">~$2.40</p>
                </div>
              </div>
              <p className="text-black/50 m-0 leading-relaxed">
                {PROTOCOLS[selectedProtocol].name} is a leading DeFi protocol
                with strong security audits and established liquidity pools.
              </p>
            </div>
          </motion.div>
        )}

        {/* Live Earnings */}
        {seq >= 11 && (
          <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.4, ease: EASING }}
            className="rounded-lg border border-black/[0.08] bg-white/80 px-4 py-3 flex items-center justify-between"
          >
            <div>
              <p className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 m-0 mb-1">
                Blended Return
              </p>
              <motion.p className="text-[18px] font-bold text-black m-0 tabular-nums">
                +${(earningsCounter / 100).toFixed(2)}
              </motion.p>
            </div>
            <div className="text-right">
              <p className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 m-0 mb-1">
                Total APY
              </p>
              <p className="text-[16px] font-bold text-black m-0">10.1%</p>
            </div>
          </motion.div>
        )}
      </motion.div>

      {/* Scrollbar visualization */}
      {seq >= 9 && (
        <motion.div
          className="absolute right-1 top-0 w-1 h-full bg-black/[0.08] rounded-full overflow-hidden"
          style={{ height: 480 }}
        >
          <motion.div
            className="w-full bg-black/40 rounded-full"
            animate={{
              top: seq >= 10 ? "60%" : "0%",
              height: seq >= 10 ? "30%" : "20%",
            }}
            transition={{ duration: 0.5, ease: EASING }}
            style={{ position: "absolute", left: 0 }}
          />
        </motion.div>
      )}
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   SCENE 2: SAVINGS LAYER
───────────────────────────────────────────────────────────────── */
function SavingsScene({ playing }: { playing: boolean }) {
  const [seq, setSeq] = useState(0);
  const [selectedGoal, setSelectedGoal] = useState(-1);
  const titleTyped = useTypedText("New MacBook Pro", playing, 6500, 65);
  const amountTyped = useTypedText("2,500 XLM", playing, 7800, 65);

  const GOALS = [
    {
      name: "My Tesla Model 3",
      saved: 4200,
      target: 25000,
      token: "NEST",
      earning: 8.4,
    },
    {
      name: "Birthday in Tokyo",
      saved: 890,
      target: 3000,
      token: "XLM",
      earning: 2.1,
    },
    {
      name: "Emergency Fund",
      saved: 9500,
      target: 10000,
      token: "NEST",
      earning: 19.8,
    },
    {
      name: "Holiday Gifts",
      saved: 340,
      target: 1200,
      token: "XLM",
      earning: 0.9,
    },
  ];

  useEffect(() => {
    if (!playing) {
      setSeq(0);      setSelectedGoal(-1);
      return;
    }

    setSeq(0);    setSelectedGoal(-1);

    const timers = [
      setTimeout(() => setSeq(1), 1200),
      setTimeout(() => setSeq(2), 2800),
      setTimeout(() => setSelectedGoal(0), 2800),
      setTimeout(() => setSeq(3), 3600),
      setTimeout(() => setSeq(4), 5000),
      setTimeout(() => setSeq(5), 5800),
      setTimeout(() => setSeq(6), 6500),
      setTimeout(() => setSeq(7), 7800),
      setTimeout(() => setSeq(8), 8800),
      setTimeout(() => setSeq(9), 9800),
      setTimeout(() => {
        setSeq(10);
        setSelectedGoal(0);
      }, 11200),
      setTimeout(() => setSeq(11), 12800),
    ];

    return () => timers.forEach(clearTimeout);
  }, [playing]);

  const cursorPos = [
    { x: 300, y: 20 },
    { x: 200, y: 60 },
    { x: 280, y: 60 },
    { x: 280, y: 90 },
    { x: 280, y: 140 },
    { x: 200, y: 180 },
    { x: 200, y: 220 },
    { x: 200, y: 250 },
    { x: 280, y: 280 },
    { x: 200, y: 320 },
    { x: 200, y: 360 },
    { x: 200, y: 360 },
  ];

  const pos = cursorPos[Math.min(seq, cursorPos.length - 1)];

  return (
    <div
      className="relative w-full select-none overflow-hidden"
      style={{ height: 480 }}
    >
      <Cursor x={pos.x} y={pos.y} clicking={seq === 8 || seq === 10} />

      <div style={{ height: "100%" }}>
        {/* Goals Accordion */}
        <div className="space-y-2 mb-4">
          {GOALS.slice(0, seq >= 1 ? 4 : 0).map((g, i) => (
            <motion.div
              key={g.name}
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.08, duration: 0.35, ease: EASING }}
              onClick={() => setSelectedGoal(selectedGoal === i ? -1 : i)}
              className={`rounded-lg border transition-all duration-300 cursor-pointer overflow-hidden ${selectedGoal === i
                  ? "gradient-border-active border-transparent"
                  : "border-black/[0.08]"
                }`}
            >
              {/* Tile Header (always visible) */}
              <div
                className={`rounded-[7px] px-4 py-3 transition-all duration-300 ${selectedGoal === i ? "bg-[hsl(0,0%,90%)]" : "bg-white/70"
                  }`}
              >
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-[11px] font-semibold text-black/70 m-0 mb-1">
                      {g.name}
                    </p>
                    <p className="text-[9px] text-black/40 m-0">
                      ${g.saved.toLocaleString()} / ${g.target.toLocaleString()}
                    </p>
                  </div>
                  <motion.div
                    animate={{ rotate: selectedGoal === i ? 180 : 0 }}
                    transition={{ duration: 0.3, ease: EASING }}
                    className="ml-3 flex-shrink-0 text-black/40"
                  >
                    ▼
                  </motion.div>
                </div>

                {/* Progress bar in header */}
                <div className="w-full h-[2px] bg-black/[0.07] rounded-full overflow-hidden mt-2">
                  <motion.div
                    className="h-full"
                    style={{
                      background:
                        "linear-gradient(90deg, #6025f5, #ff5555 50%, #facc15)",
                    }}
                    initial={{ width: 0 }}
                    animate={{ width: `${(g.saved / g.target) * 100}%` }}
                    transition={{
                      delay: i * 0.08 + 0.2,
                      duration: 0.8,
                      ease: EASING,
                    }}
                  />
                </div>
              </div>

              {/* Expanded Details */}
              <motion.div
                initial={{ height: 0, opacity: 0 }}
                animate={
                  selectedGoal === i
                    ? { height: "auto", opacity: 1 }
                    : { height: 0, opacity: 0 }
                }
                transition={{ duration: 0.3, ease: EASING }}
                className={`overflow-hidden ${selectedGoal === i ? "border-t border-black/[0.08]" : ""}`}
              >
                <div className="px-4 py-3 bg-white/60 space-y-3">
                  <div>
                    <p className="text-[9px] text-black/40 m-0 mb-1">
                      Target Amount
                    </p>
                    <p className="text-[13px] font-bold text-black m-0">
                      ${g.target.toLocaleString()} {g.token}
                    </p>
                  </div>

                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <p className="text-[9px] text-black/40 m-0 mb-1">
                        Time Remaining
                      </p>
                      <p className="text-[11px] font-bold text-black m-0">
                        ~8 months
                      </p>
                    </div>
                    <div>
                      <p className="text-[9px] text-black/40 m-0 mb-1">
                        Monthly Earn
                      </p>
                      <p className="text-[11px] font-bold text-black m-0">
                        +${g.earning.toFixed(2)}
                      </p>
                    </div>
                  </div>

                  <p className="text-[9px] text-black/50 m-0 leading-relaxed">
                    Saving towards {g.name}. Earn {g.token} tokens on your
                    savings while reaching your goal.
                  </p>

                  {seq >= 3 && selectedGoal === i && (
                    <motion.button
                      initial={{ opacity: 0 }}
                      animate={{ opacity: 1 }}
                      transition={{ duration: 0.3 }}
                      className="w-full text-[9px] font-bold tracking-wider uppercase rounded px-2 py-1.5 border border-black/15 bg-black/[0.04] text-black/60 hover:bg-black/[0.08] transition-all"
                    >
                      View Details
                    </motion.button>
                  )}
                </div>
              </motion.div>
            </motion.div>
          ))}
        </div>

        {/* Add Goal Button */}
        {seq >= 4 && (
          <motion.button
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.4, ease: EASING }}
            className="w-full py-2.5 rounded-lg bg-black text-white font-bold text-[11px] tracking-wider uppercase mb-4"
          >
            + Add Goal
          </motion.button>
        )}

        {/* New Goal Form */}
        {seq >= 5 && (
          <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.4, ease: EASING }}
            className="rounded-lg border border-black/[0.08] bg-white/70 px-4 py-3"
          >
            <label className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 block mb-2">
              Goal Title
            </label>
            <div className="border-b border-black/10 pb-2 mb-3 font-mono text-[13px] text-black">
              {titleTyped}
              {seq === 6 && <span className="animate-pulse">|</span>}
            </div>

            {seq >= 7 && (
              <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.3 }}
              >
                <label className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 block mb-2">
                  Target Amount
                </label>
                <div className="border-b border-black/10 pb-2 mb-3 font-mono text-[13px] text-black">
                  {amountTyped}
                  {seq === 7 && <span className="animate-pulse">|</span>}
                </div>
              </motion.div>
            )}

            {seq >= 8 && (
              <motion.button
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.3 }}
                className="w-full py-2 rounded-lg bg-black/5 border border-black/15 text-black font-bold text-[10px] tracking-wider uppercase"
              >
                Create Vault
              </motion.button>
            )}
          </motion.div>
        )}

        {/* New vault created */}
        {seq >= 9 && (
          <motion.div
            initial={{ opacity: 0, y: 12, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            transition={{ duration: 0.4, ease: EASING }}
            className="rounded-lg border border-black/[0.08] bg-white/70 px-3 py-3 mt-2"
          >
            <p className="text-[11px] font-semibold text-black/70 m-0 mb-1">
              New MacBook Pro
            </p>
            <p className="text-[9px] text-black/40 m-0 mb-2">$0 / 2,500 XLM</p>
            <div className="w-full h-[2px] bg-black/[0.07] rounded-full overflow-hidden">
              <motion.div
                className="h-full"
                style={{
                  background:
                    "linear-gradient(90deg, #6025f5, #ff5555 50%, #facc15)",
                }}
                initial={{ width: 0 }}
                animate={{ width: "2%" }}
                transition={{ duration: 0.6, ease: EASING }}
              />
            </div>
          </motion.div>
        )}
      </div>
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   SCENE 3: OFFRAMP LAYER
───────────────────────────────────────────────────────────────── */
// OfframpScene Layer Architecture

function OfframpScene({ playing }: { playing: boolean }) {
  const [seq, setSeq] = useState(0);

  useEffect(() => {
    if (!playing) {
      setSeq(0);      return;
    }
    setSeq(0);    const timings = [
      800,  // 1: Packet User -> Escrow, show Escrow
      1800, // 2: Packet Escrow -> Router, show Router & Oracle
      3200, // 3: Packets Router -> LPs, show LPs loading rings
      4800, // 4: Resolve LPs (GTBank matches)
      5800, // 5: Packet GTB -> API, show API
      6800, // 6: Packet API -> Settlement, show Settlement
      8000, // 7: Escrow atomic release (crypto -> GTB LP)
      11000 // 8: Reset
    ];
    const timers = timings.map((t, i) =>
      setTimeout(() => setSeq(i + 1), t),
    );
    return () => timers.forEach(clearTimeout);
  }, [playing]);

  const LPs = [
    { id: 'zenith', name: 'Zenith Operator', rate: '₦1,550/$', logo: '/zenith-logo.png', cx: 80, path: "M 250 193 C 250 230, 80 220, 80 260", failReason: 'Insufficient Balance' },
    { id: 'gtb', name: 'GTBank Node', rate: '₦1,557/$', logo: '/GTBank_logo.svg.png', cx: 250, path: "M 250 193 L 250 260", winner: true },
    { id: 'access', name: 'Access LP', rate: '₦1,552/$', logo: '/access-logo.png', cx: 420, path: "M 250 193 C 250 230, 420 220, 420 260", failReason: 'High Network Latency' },
  ];

  return (
    <div className="relative w-full select-none overflow-hidden rounded-b-3xl flex items-center justify-center" style={{ height: 600 }}>
      <div className="absolute left-1/2 -translate-x-1/2 w-[500px] h-[480px] scale-125 origin-top" style={{ top: '0px' }}>
        
        {/* SVG Paths Layer */}
        <svg className="absolute inset-0 w-full h-full pointer-events-none z-0" viewBox="0 0 500 480">
          
          {/* User -> Escrow */}
          <path d="M 250 54 L 250 80" stroke="#E5E7EB" strokeWidth="1.5" fill="none" />
          {seq >= 1 && (
             <motion.path d="M 250 54 L 250 80" stroke="#9CA3AF" strokeWidth="3" fill="none" strokeLinecap="round"
               initial={{ pathLength: 0.1, pathOffset: 0, opacity: 0 }} animate={{ pathOffset: 1, opacity: [0, 1, 0] }} transition={{ duration: 0.6, ease: "linear" }} />
          )}

          {/* Escrow -> Router */}
          <path d="M 250 124 L 250 143" stroke="#E5E7EB" strokeWidth="1.5" fill="none" />
          {seq >= 2 && (
             <motion.path d="M 250 124 L 250 143" stroke="#9CA3AF" strokeWidth="3" fill="none" strokeLinecap="round"
               initial={{ pathLength: 0.1, pathOffset: 0, opacity: 0 }} animate={{ pathOffset: 1, opacity: [0, 1, 0] }} transition={{ duration: 0.6, ease: "linear" }} />
          )}

          {/* Router -> Oracle */}
          <path d="M 335 168 L 365 168" stroke="#E5E7EB" strokeWidth="1.5" fill="none" />
          {seq === 2 && (
             <motion.path d="M 335 168 L 365 168" stroke="#9CA3AF" strokeWidth="3" fill="none" strokeLinecap="round"
               initial={{ pathLength: 0.2, pathOffset: 0, opacity: 0 }} animate={{ pathOffset: 1, opacity: [0, 1, 0] }} transition={{ duration: 0.5, delay: 0.6, ease: "linear" }} />
          )}

          {/* Router -> LPs */}
          {LPs.map((lp, i) => (
             <g key={`path-${lp.id}`}>
               <path d={lp.path} stroke="#E5E7EB" strokeWidth="1.5" fill="none" />
               {seq === 3 && (
                 <motion.path d={lp.path} stroke="#9CA3AF" strokeWidth="2.5" fill="none" strokeLinecap="round"
                   initial={{ pathLength: 0.15, pathOffset: 0, opacity: 0 }} animate={{ pathOffset: 1, opacity: [0, 1, 0] }} transition={{ duration: 0.8, delay: i * 0.15, ease: "easeInOut" }} />
               )}
             </g>
          ))}

          {/* GTB LP -> API */}
          <path d="M 250 320 L 250 340" stroke="#E5E7EB" strokeWidth="1.5" fill="none" />
          {seq >= 5 && (
             <motion.path d="M 250 320 L 250 340" stroke="#9CA3AF" strokeWidth="3" fill="none" strokeLinecap="round"
               initial={{ pathLength: 0.2, pathOffset: 0, opacity: 0 }} animate={{ pathOffset: 1, opacity: [0, 1, 0] }} transition={{ duration: 0.6, ease: "linear" }} />
          )}

          {/* API -> Settlement */}
          <path d="M 250 376 L 250 400" stroke="#E5E7EB" strokeWidth="1.5" fill="none" />
          {seq >= 6 && (
             <motion.path d="M 250 376 L 250 400" stroke="#9CA3AF" strokeWidth="4" fill="none" strokeLinecap="round"
               initial={{ pathLength: 0.15, pathOffset: 0, opacity: 0 }} animate={{ pathOffset: 1, opacity: [0, 1, 0] }} transition={{ duration: 0.6, ease: "linear" }} />
          )}

          {/* Escrow Crypto Release -> GTB LP (Atomic Release curve) */}
          {seq === 7 && (
             <motion.path d="M 150 102 C 100 150, 100 250, 180 290" stroke="#9CA3AF" strokeWidth="3" fill="none" strokeLinecap="round" strokeDasharray="4 4"
               initial={{ pathLength: 0.15, pathOffset: 0, opacity: 0 }} animate={{ pathOffset: 1, opacity: [0, 1, 0] }} transition={{ duration: 1.2, ease: "easeInOut" }} />
          )}
        </svg>

        {/* 1. USER REQUEST */}
        <div className="absolute top-[10px] left-[150px] w-[200px] h-[44px] bg-white rounded-xl shadow-sm border border-gray-200 flex items-center px-3 z-10 transition-all">
          <div className="w-6 h-6 rounded bg-gray-100 flex items-center justify-center shrink-0">
             <div className="w-1.5 h-1.5 rounded-full bg-gray-600" />
          </div>
          <div className="ml-2.5">
             <div className="text-[10px] text-gray-600 leading-tight">User Request</div>
             <div className="text-[8px] text-gray-500 mt-0.5 tracking-wide uppercase">Withdraw $1,000 to GTBank</div>
          </div>
        </div>

        {/* 2. ESCROW */}
        <motion.div
           initial={{ opacity: 0, scale: 0.95 }}
           animate={{ opacity: seq >= 1 ? 1 : 0, scale: seq >= 1 ? 1 : 0.95 }}
           transition={{ duration: 0.5, ease: EASING }}
           className={`absolute top-[80px] left-[150px] w-[200px] h-[44px] bg-white rounded-xl shadow-[0_4px_15px_rgba(0,0,0,0.03)] border transition-all duration-500 flex items-center px-3 z-10 ${seq >= 7 ? 'border-gray-400' : 'border-gray-200'}`}
        >
          <div className={`w-7 h-7 rounded-lg flex items-center justify-center text-[13px] shrink-0 transition-colors ${seq >= 7 ? 'bg-gray-200' : 'bg-gray-100'}`}>
            <div className={`w-2 h-2 rounded-sm ${seq >= 7 ? 'border-t-0 border-r-2 border-b-2 border-l-0 rotate-45 mb-0.5 border-gray-500' : 'bg-gray-400'}`} />
          </div>
          <div className="ml-2.5">
             <div className="text-[10px] text-gray-700 tracking-tight">Smart Contract Escrow</div>
             <div className={`text-[8px] mt-0.5 tracking-wide uppercase ${seq >= 7 ? 'text-gray-600' : 'text-gray-500'}`}>
               {seq >= 7 ? 'USDC Released to LP' : '1,000 USDC Locked'}
             </div>
          </div>
        </motion.div>

        {/* 3. ROUTING ENGINE */}
        <motion.div
           initial={{ opacity: 0, y: -10 }}
           animate={{ opacity: seq >= 2 ? 1 : 0, y: seq >= 2 ? 0 : -10 }}
           transition={{ duration: 0.4, ease: EASING }}
           className="absolute top-[143px] left-[165px] w-[170px] h-[50px] bg-white rounded-xl shadow-[0_4px_20px_rgba(0,0,0,0.04)] border border-gray-200 flex items-center px-3 z-10"
        >
           <div className="w-6 h-6 rounded bg-gray-50 flex items-center justify-center shrink-0 border border-gray-100">
             <div className="w-2 h-2 rounded-full flex items-center justify-center relative">
                {seq === 2 && <motion.div animate={{ scale: [1, 2], opacity: [1, 0]}} transition={{ repeat: Infinity, duration: 1 }} className="absolute inset-0 rounded-full border border-gray-400" />}
                <div className="w-1.5 h-1.5 rounded-full bg-gray-600" />
             </div>
           </div>
           <div className="ml-2.5 flex-1">
             <div className="text-[10px] text-gray-800 tracking-widest uppercase mb-0.5">Omni-Router</div>
             <div className="text-[7px] text-gray-500 uppercase tracking-wider">Algorithmic Match</div>
           </div>
           {seq === 2 && (
             <div className="flex gap-0.5 h-3 items-center">
                <motion.div animate={{ height: ['4px', '8px', '4px'] }} transition={{ repeat: Infinity, duration: 0.6, delay: 0 }} className="w-0.5 bg-gray-300 rounded-full" />
                <motion.div animate={{ height: ['4px', '12px', '4px'] }} transition={{ repeat: Infinity, duration: 0.6, delay: 0.2 }} className="w-0.5 bg-gray-400 rounded-full" />
                <motion.div animate={{ height: ['4px', '6px', '4px'] }} transition={{ repeat: Infinity, duration: 0.6, delay: 0.4 }} className="w-0.5 bg-gray-300 rounded-full" />
             </div>
           )}
        </motion.div>

        {/* 4. RATE ORACLE */}
        <motion.div
           initial={{ opacity: 0, x: -10 }}
           animate={{ opacity: seq >= 2 ? 1 : 0, x: seq >= 2 ? 0 : -10 }}
           transition={{ duration: 0.4, delay: 0.2, ease: EASING }}
           className="absolute top-[150px] left-[365px] w-[130px] h-[36px] bg-white rounded-lg shadow-sm border border-gray-200 flex items-center px-2 z-10"
        >
           <div className="w-1 h-3 bg-gray-200 rounded-sm ml-1" />
           <div className="w-1 h-4 bg-gray-400 rounded-sm mx-0.5" />
           <div className="w-1 h-2 bg-gray-300 rounded-sm mr-2" />
           <div className="ml-1">
             <div className="text-[8px] text-gray-600 uppercase">Rate Oracle</div>
             <div className="text-[7px] text-gray-400 mt-0.5 tracking-wide">Live Market Check</div>
           </div>
        </motion.div>

        {/* 5. LP NODES */}
        {LPs.map((lp, i) => (
          <motion.div
            key={lp.id}
            initial={{ opacity: 0, y: 15 }}
            animate={{ opacity: seq >= 3 ? (seq >= 4 && !lp.winner ? 0.4 : 1) : 0, y: seq >= 3 ? 0 : 15 }}
            transition={{ duration: 0.5, delay: i * 0.1, ease: EASING }}
            className={`absolute top-[260px] w-[140px] h-[60px] bg-white rounded-xl border flex items-center px-2 z-10 transition-all duration-500 border-gray-200 shadow-sm`}
            style={{ left: lp.cx - 70 }}
          >
            <div className="flex items-center gap-2 w-full">
              <div className="w-8 h-8 flex items-center justify-center shrink-0">
                <Image src={lp.logo} width={24} height={24} alt={lp.name} className="object-contain" />
              </div>
              <div className="flex-1">
                <div className="text-[10px] text-gray-600 leading-tight">{lp.name}</div>
                <div className="text-[8px] mt-0.5 tracking-wide text-gray-500">
                  {seq >= 4 && lp.winner ? 'Match + Rate' : (seq >= 4 ? lp.failReason : lp.rate)}
                </div>
              </div>
              
              <div className="w-4 h-4 relative shrink-0">
                {seq === 3 && (
                  <svg className="w-full h-full -rotate-90 overflow-visible">
                    <circle cx="8" cy="8" r="7" stroke="#E5E7EB" strokeWidth="2.5" fill="none" />
                    <motion.circle cx="8" cy="8" r="7" stroke="#9CA3AF" strokeWidth="2.5" fill="none"
                      initial={{ strokeDasharray: "44", strokeDashoffset: "44" }} animate={{ strokeDashoffset: "0" }}
                      transition={{ duration: 1.2, delay: i * 0.1, ease: "linear" }} strokeLinecap="round" />
                  </svg>
                )}
                {seq >= 7 && lp.winner && (
                   <div className="absolute inset-0 flex items-center justify-center">
                     <div className="w-2 h-2 rounded-sm border-t-0 border-r-2 border-b-2 border-l-0 rotate-45 mb-0.5 border-gray-500" />
                   </div>
                )}
              </div>
            </div>
          </motion.div>
        ))}

        {/* 6. BANKING API */}
        <motion.div
           initial={{ opacity: 0, y: -10 }}
           animate={{ opacity: seq >= 5 ? 1 : 0, y: seq >= 5 ? 0 : -10 }}
           transition={{ duration: 0.4, ease: EASING }}
           className="absolute top-[340px] left-[170px] w-[160px] h-[36px] bg-white rounded-full shadow-sm border border-gray-200 flex items-center justify-center gap-2 z-10"
        >
           <span className="text-[12px]">⚙️</span>
           <span className="text-[8px] text-gray-500 uppercase tracking-widest">GTBank Direct API</span>
        </motion.div>

        {/* 7. SETTLEMENT NODE */}
        <motion.div
          initial={{ scale: 0.95, opacity: 0, y: 15 }}
          animate={{ scale: seq >= 6 ? 1 : 0.95, opacity: seq >= 6 ? 1 : 0, y: seq >= 6 ? 0 : 15 }}
          transition={{ duration: 0.6, ease: EASING }}
          className="absolute top-[400px] left-[130px] w-[240px] h-[64px] rounded-xl shadow-[0_8px_30px_rgba(0,0,0,0.06)] z-10 overflow-hidden p-[2px]"
        >
          {/* Animated Gradient border equivalent */}
          <motion.div 
             animate={{ backgroundPosition: ['0% 50%', '100% 50%', '0% 50%'] }} 
             transition={{ duration: 4, ease: "linear", repeat: Infinity }}
             className="absolute inset-0 z-0" 
             style={{ backgroundSize: '200% auto', backgroundImage: 'linear-gradient(90deg, #6025f5 0%, #ff5555 45%, #facc15 100%, #6025f5)' }}
          />

          <div className="relative w-full h-full bg-white rounded-[10px] flex items-center justify-between px-3 z-10">
            <div className="flex items-center gap-3 w-full">
              <div className="w-9 h-9 rounded-full bg-gray-100 flex items-center justify-center shrink-0 border border-gray-200">
                <div className="w-4 h-4 rounded-sm border-t-0 border-r-2 border-b-2 border-l-0 rotate-45 mb-1 border-gray-800" />
              </div>
              <div className="flex-1">
                <div className="text-[11px] text-gray-800 tracking-wide mb-0.5">GTBank Account</div>
                <div className="text-[8px] text-gray-500 tracking-widest uppercase mb-0.5" style={{ fontFamily: "var(--font-space-grotesk, sans-serif)", letterSpacing: '0.15em' }}>012****899</div>
                <div className="text-[7px] text-gray-400 tracking-wider flex items-center gap-1 uppercase">
                  T+0 Finality
                </div>
              </div>
              <div className="text-right flex flex-col items-end">
                <div className="text-[13px] text-gray-800 tracking-tight mb-0.5 tabular-nums">
                  +₦1,557,350
                </div>
                <div className="text-[7px] text-gray-500 uppercase tracking-wider bg-gray-50 border border-gray-100 px-1.5 py-0.5 rounded shadow-sm">Settled Instantly</div>
              </div>
            </div>
          </div>
        </motion.div>

      </div>
    </div>
  );
}

/* ─────────────────────────────────────────────────────────────────
   SCENE 4: AI PORTFOLIO
───────────────────────────────────────────────────────────────── */

function AiScene({ playing }: { playing: boolean }) {
  const [seq, setSeq] = useState(0);
  const riskScore = useCounter(playing, 68, 4000, 2, 25);

  useEffect(() => {
    if (!playing) {
      setSeq(0);      return;
    }

    setSeq(0);    const timers = [
      setTimeout(() => setSeq(1), 800),
      setTimeout(() => setSeq(2), 2000),
      setTimeout(() => setSeq(3), 2800),
      setTimeout(() => setSeq(4), 4000),
      setTimeout(() => setSeq(5), 5200),
      setTimeout(() => setSeq(6), 6400),
      setTimeout(() => setSeq(7), 7400),
      setTimeout(() => setSeq(8), 8500),
    ];

    return () => timers.forEach(clearTimeout);
  }, [playing]);

  const holdings = [
    {
      asset: "Aave (USDC)",
      balance: "15,200",
      value: "$15,200",
      change: "+0.23%",
      apy: "8.4%",
    },
    {
      asset: "Blend (USDC)",
      balance: "8,400",
      value: "$8,400",
      change: "+0.18%",
      apy: "11.2%",
    },
    {
      asset: "Cash (USDC)",
      balance: "3,200",
      value: "$3,200",
      change: "0.00%",
      apy: "0.0%",
    },
  ];

  const recommendations = [
    {
      priority: "HIGH",
      title: "Rebalance $5K from Blend",
      impact: "-12% volatility · +0.4% APY",
    },
    {
      priority: "MEDIUM",
      title: "Increase Cash Reserve",
      impact: "+6% liquidity · Better opportunities",
    },
    {
      priority: "LOW",
      title: "Lock Yield in Conservative",
      impact: "+0.2% stability · Reduced risk",
    },
  ];

  return (
    <div
      className="relative w-full select-none overflow-hidden"
      style={{ height: 480 }}
    >
      {/* Header */}
      <motion.div
        initial={{ opacity: 0, y: -8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.4, ease: EASING }}
        className="rounded-lg border border-black/[0.08] bg-white/80 px-4 py-3 mb-4"
      >
        <div className="flex items-start justify-between mb-3">
          <div>
            <p className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 m-0 mb-1">
              Portfolio Total
            </p>
            <p
              className="text-[22px] font-bold text-black m-0"
              style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
            >
              $26,800
            </p>
          </div>
          <div className="flex items-center gap-1.5 rounded-lg border border-black/[0.08] bg-black/[0.03] px-3 py-2">
            <motion.span
              animate={{ opacity: [0.4, 1, 0.4] }}
              transition={{ duration: 1.4, repeat: Infinity }}
              className="w-1.5 h-1.5 rounded-full bg-black"
            />
            <span className="text-[8px] font-bold tracking-[0.15em] uppercase text-black/35">
              Prometheus
            </span>
          </div>
        </div>

        {/* Holdings Table */}
        <div className="space-y-2 text-[10px] border-t border-black/[0.06] pt-3">
          {holdings.map((h, i) => (
            <motion.div
              key={h.asset}
              initial={{ opacity: 0, x: -8 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{
                delay: i * 0.1,
                duration: 0.35,
                ease: EASING,
              }}
              className="flex items-center justify-between"
            >
              <div>
                <p className="font-semibold text-black/70 m-0">{h.asset}</p>
                <p className="text-black/40 m-0">{h.balance}</p>
              </div>
              <div className="text-right">
                <p className="font-bold text-black tabular-nums m-0">
                  {h.value}
                </p>
                <p className="text-black/40 tabular-nums m-0">{h.change}</p>
              </div>
            </motion.div>
          ))}
        </div>
      </motion.div>

      {/* Allocation Bar */}
      {seq >= 1 && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.4, ease: EASING }}
          className="h-2 bg-black/[0.07] rounded-full overflow-hidden mb-4 flex"
        >
          <motion.div
            className="flex"
            initial={{ width: 0 }}
            animate={{ width: "56.7%" }}
            transition={{ duration: 0.8, ease: EASING }}
            style={{
              background:
                "linear-gradient(90deg, #6025f5, #ff5555 50%, #facc15)",
            }}
          />
          <div className="flex-1 bg-black/[0.1]" />
        </motion.div>
      )}

      {/* Scan Button */}
      {seq >= 2 && (
        <motion.button
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, ease: EASING }}
          className="w-full py-2 rounded-lg border border-black/[0.08] bg-white/60 font-bold text-[11px] tracking-wider uppercase text-black/70 mb-4"
        >
          Scan with Prometheus
        </motion.button>
      )}

      {/* Scanning Overlay */}
      {seq === 3 && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="absolute inset-0 flex items-center justify-center bg-white/50 rounded-lg z-10"
        >
          <motion.span
            animate={{ rotate: 360 }}
            transition={{ repeat: Infinity, duration: 1.2, ease: "linear" }}
            className="text-2xl"
          >
            ⚙️
          </motion.span>
        </motion.div>
      )}

      {/* Risk Score */}
      {seq >= 4 && (
        <motion.div
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, ease: EASING }}
          className="rounded-lg border border-black/[0.08] bg-white/70 px-4 py-3 mb-4"
        >
          <p className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 m-0 mb-2">
            Risk Assessment
          </p>
          <div className="flex items-end gap-3">
            <div>
              <p className="text-[20px] font-bold text-black m-0 tabular-nums">
                {riskScore}
              </p>
              <p className="text-[9px] text-black/50 m-0">/ 100</p>
            </div>
            <div className="flex-1">
              <div className="w-full h-[3px] bg-black/[0.07] rounded-full overflow-hidden">
                <motion.div
                  className="h-full rounded-full"
                  style={{
                    background:
                      "linear-gradient(90deg, #6025f5, #ff5555 50%, #facc15)",
                  }}
                  initial={{ width: 0 }}
                  animate={{ width: "68%" }}
                  transition={{ duration: 1.0, ease: EASING }}
                />
              </div>
              <p className="text-[8px] text-black/40 mt-1">MODERATE RISK</p>
            </div>
          </div>
        </motion.div>
      )}

      {/* Recommendations */}
      {seq >= 5 && (
        <div className="space-y-2">
          <p className="text-[9px] font-bold tracking-[0.2em] uppercase text-black/25 m-0 mb-2">
            Recommendations
          </p>
          {recommendations
            .slice(0, seq >= 6 ? (seq >= 7 ? 3 : 2) : 1)
            .map((rec, i) => (
              <motion.div
                key={i}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.4, ease: EASING }}
                className="rounded-lg border border-black/[0.08] bg-white/70 px-3 py-2.5"
              >
                <div className="flex items-start gap-2">
                  <span
                    className={`text-[8px] font-bold tracking-[0.12em] uppercase rounded px-1.5 py-0.5 flex-shrink-0 ${rec.priority === "HIGH"
                        ? "bg-black/8 text-black/80"
                        : rec.priority === "MEDIUM"
                          ? "bg-black/4 text-black/60"
                          : "bg-black/3 text-black/50"
                      }`}
                  >
                    {rec.priority}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-[11px] font-semibold text-black/75 m-0 mb-0.5">
                      {rec.title}
                    </p>
                    <p className="text-[9px] text-black/40 m-0">{rec.impact}</p>
                  </div>
                </div>
              </motion.div>
            ))}
        </div>
      )}

      {/* Apply All Button */}
      {seq >= 8 && (
        <motion.button
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, ease: EASING }}
          className="w-full py-3 rounded-lg bg-black text-white font-bold text-[11px] tracking-wider uppercase mt-4"
        >
          Apply All Recommendations
        </motion.button>
      )}

      {/* Scrollbar visualization */}
      {seq >= 7 && (
        <motion.div
          className="absolute right-1 top-0 w-1 h-full bg-black/[0.08] rounded-full overflow-hidden"
          style={{ height: 480 }}
        >
          <motion.div
            className="w-full bg-black/40 rounded-full"
            animate={{
              top: seq >= 8 ? "60%" : "0%",
              height: seq >= 8 ? "30%" : "20%",
            }}
            transition={{ duration: 0.5, ease: EASING }}
            style={{ position: "absolute", left: 0 }}
          />
        </motion.div>
      )}
    </div>
  );
}


/* ─────────────────────────────────────────────────────────────────
   SCENE ROUTER
───────────────────────────────────────────────────────────────── */
function SceneRouter({
  activeIdx,
  playing,
}: {
  activeIdx: number;
  playing: boolean;
}) {
  return (
    <>
      {activeIdx === 0 && <YieldScene playing={playing} />}
      {activeIdx === 1 && <SavingsScene playing={playing} />}
      {activeIdx === 2 && <OfframpScene playing={playing} />}
      {activeIdx === 3 && <AiScene playing={playing} />}
    </>
  );
}

/* ─────────────────────────────────────────────────────────────────
   MAIN EXPORT
───────────────────────────────────────────────────────────────── */
export function UseCases() {
  const [activeIdx, setActiveIdx] = useState(0);
  const [playing, setPlaying] = useState(false);
  const sectionRef = useRef<HTMLElement>(null);
  const isInView = useInView(sectionRef, { once: true, margin: "-80px" });

  useEffect(() => {
    setPlaying(false);    const t = setTimeout(() => setPlaying(true), 60);
    return () => clearTimeout(t);
  }, [activeIdx]);

  useEffect(() => {
    if (!isInView) return;
    const id = setInterval(() => {
      setActiveIdx((p) => (p + 1) % SCENARIOS.length);
    }, STEP_DURATION);
    return () => clearInterval(id);
  }, [isInView, activeIdx]);

  return (
    <section
      ref={sectionRef}
      className="w-full py-24 md:py-36 overflow-hidden"
      style={{ background: "#ffffff" }}
    >
      <div className="max-w-7xl mx-auto px-6 md:px-12">
        {/* Header */}
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, margin: "-80px" }}
          transition={{ duration: 0.7, ease: EASING }}
          className="mb-20"
        >
          <div className="flex items-center gap-2 mb-6">
            <span className="w-6 h-[1px] bg-black/20" />
            <p
              className="text-[10px] font-bold tracking-[0.25em] uppercase text-black/30 m-0"
              style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
            >
              How It All Works
            </p>
          </div>

          <h2
            className="text-[2rem] md:text-[2.8rem] lg:text-[3.4rem] font-light text-black leading-[1.1] tracking-tight m-0 max-w-3xl"
            style={{ fontFamily: "var(--font-space-grotesk, sans-serif)" }}
          >
            Every feature in motion.{" "}
            <span
              className="italic font-medium"
              style={{ fontFamily: "var(--font-cormorant, serif)" }}
            >
              Watch it work.
            </span>
          </h2>
        </motion.div>

        {/* Main Card */}
        <motion.div
          initial={{ opacity: 0, y: 32 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          transition={{ duration: 0.7, delay: 0.1, ease: EASING }}
          className="rounded-t-3xl overflow-hidden"
          style={{
            background: "hsl(0,0%,91%)",
            boxShadow:
              "0 1px 2px rgba(0,0,0,0.05), 0 8px 40px rgba(0,0,0,0.06)",
          }}
        >
          <BrowserChrome activeIdx={activeIdx} />

          <div className="flex flex-col lg:flex-row" style={{ minHeight: 600 }}>
            <ScenarioNavigator activeIdx={activeIdx} onSelect={setActiveIdx} />

            <div className="flex-1 relative overflow-hidden">
              <div
                className="absolute inset-0 pointer-events-none"
                style={{
                  background:
                    "radial-gradient(55% 55% at 50% 50%, rgba(255,255,255,0.55) 0%, transparent 100%)",
                }}
              />

              <div className="relative w-full h-full px-8 py-12 md:px-12">
                <AnimatePresence mode="wait">
                  <motion.div
                    key={activeIdx + "-scene"}
                    initial={{ opacity: 0, y: 18, scale: 0.98 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: -12, scale: 0.98 }}
                    transition={{ duration: 0.45, ease: EASING }}
                  >
                    <SceneRouter activeIdx={activeIdx} playing={playing} />
                  </motion.div>
                </AnimatePresence>
              </div>
            </div>
          </div>

          {/* Progress Bar */}
          <div className="h-[2px] bg-black/[0.05]">
            <AnimatePresence mode="wait">
              <motion.div
                key={activeIdx + "-bar"}
                initial={{ scaleX: 0 }}
                animate={{ scaleX: 1 }}
                exit={{ opacity: 0 }}
                transition={{
                  duration: STEP_DURATION / 1000,
                  ease: "linear",
                }}
                className="h-full origin-left"
                style={{
                  background:
                    "linear-gradient(90deg, #6025f5 0%, #ff5555 45%, #facc15 100%)",
                }}
              />
            </AnimatePresence>
          </div>
        </motion.div>
      </div>
    </section>
  );
}
