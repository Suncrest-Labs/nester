"use client"

import { useState, useEffect, useRef } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Flame } from "lucide-react";

const CAPABILITIES = [
  {
    id: "vault",
    tag: "VAULT STRATEGY",
    title: "Smart vault selection",
    desc: "AI analyzes your timeline, risk profile, and market conditions to recommend the optimal vault.",
    question: "I want to save $5,000 over the next year without much risk. Which vault?",
    answer:
      "Given your 12-month timeline and low risk tolerance, the Balanced Vault is your best fit. It's currently yielding 9.4% APY across Aave and Blend, with no exposure to newer, unaudited protocols. You'd end the year with approximately $5,470 — without touching anything.",
  },
  {
    id: "risk",
    tag: "RISK ANALYSIS",
    title: "Portfolio health scoring",
    desc: "Continuous monitoring of your allocation, volatility exposure, and protocol health in real time.",
    question: "How risky is my current portfolio?",
    answer:
      "Risk score: 3.2 / 10. Your 70% stablecoin base is solid. The XLM position adds healthy upside without overconcentration. One flag: 18% of your stablecoin allocation is on a protocol that saw a TVL dip this week — I'd suggest shifting that 18% to Aave until it stabilizes.",
  },
  {
    id: "market",
    tag: "MARKET INTEL",
    title: "Weekly ecosystem signals",
    desc: "Nester tracks DeFi protocols, on-chain activity, and sentiment so you never have to.",
    question: "Is now a good time to add XLM exposure?",
    answer:
      "Moderate buy signal. XLM DEX volume is up 35% this week and sentiment across Stellar forums is bullish. Given your current 85% stablecoin base, adding 10–15% XLM looks favorable. Suggested entry: $900 USDC equivalent. I can execute whenever you're ready.",
  },
  {
    id: "offramp",
    tag: "INSTANT OFF-RAMP",
    title: "Withdraw in seconds",
    desc: "AI routes your fiat request to the optimal liquidity node — same-bank transfers settle in 3 seconds.",
    question: "My rent is due tomorrow. Can I withdraw ₦650,000?",
    answer:
      "That's $415 USDC at today's rate. A GTBank liquidity node is active with full reserves. Estimated settlement: 3 seconds after confirmation. Your remaining $5,300 stays fully invested and earning. Want me to initiate the withdrawal now?",
  },
];

function ChatPanel({ cap }: { cap: typeof CAPABILITIES[0] }) {
  const [phase, setPhase] = useState<"typing-q" | "clicking" | "thinking" | "answering" | "done">("typing-q");
  
  const [qDisplayed, setQDisplayed] = useState("");
  const [aDisplayed, setADisplayed] = useState("");
  const endRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setPhase("typing-q");
    setQDisplayed("");
    setADisplayed("");
  }, [cap.id]);

  useEffect(() => {
    if (phase !== "typing-q") return;
    let i = 0;
    let interval: NodeJS.Timeout;

    // Wait 400ms before starting to type, so the smooth scroll has time to get there
    const startDelay = setTimeout(() => {
      interval = setInterval(() => {
        setQDisplayed(prev => cap.question.slice(0, prev.length + 1));
        i++;
        if (i >= cap.question.length) {
          clearInterval(interval);
          setTimeout(() => setPhase("clicking"), 400);
        }
      }, 25);
    }, 400);

    return () => {
      clearTimeout(startDelay);
      if (interval) clearInterval(interval);
    };
  }, [phase, cap.question]);

  useEffect(() => {
    if (phase !== "clicking") return;
    const timer = setTimeout(() => {
       setPhase("thinking");
    }, 900);
    return () => clearTimeout(timer);
  }, [phase]);

  useEffect(() => {
    if (phase !== "thinking") return;
    const timer = setTimeout(() => {
      setPhase("answering");
    }, 1500);
    return () => clearTimeout(timer);
  }, [phase]);

  useEffect(() => {
    if (phase !== "answering") return;
    let i = 0;
    const interval = setInterval(() => {
      setADisplayed(prev => cap.answer.slice(0, prev.length + 1));
      i++;
      if (i >= cap.answer.length) {
        clearInterval(interval);
        setTimeout(() => setPhase("done"), 100);
      }
    }, 15);
    return () => clearInterval(interval);
  }, [phase, cap.answer]);

  useEffect(() => {
    // Scroll continuously down to the panel as it expands to keep up with action
    setTimeout(() => {
      endRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }, 50);
  }, [phase, qDisplayed, aDisplayed]);

  return (
    <motion.div 
      key={cap.id}
      initial={{ opacity: 0, height: 0, scale: 0.98 }}
      animate={{ opacity: 1, height: "auto", scale: 1 }}
      exit={{ opacity: 0, height: 0, scale: 0.98 }}
      transition={{ duration: 0.5, ease: [0.23, 1, 0.32, 1] }}
      className="border-t border-black/[0.08] pt-10 mt-6 lg:mt-8 overflow-hidden"
    >
      <div className="flex flex-col gap-6 max-w-[720px] mb-8">
        {/* User bubble only shows after clicking send */}
        <AnimatePresence>
        {phase !== "typing-q" && phase !== "clicking" && (
          <motion.div 
            initial={{ opacity: 0, scale: 0.95, y: 10 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            className="flex justify-end"
          >
            <div className="bg-[#111] rounded-2xl rounded-tr-sm px-5 py-3.5 max-w-[85%] md:max-w-[72%] shadow-[0_4px_12px_rgba(0,0,0,0.06)]">
              <p style={{ fontFamily: "'DM Sans', sans-serif" }} className="m-0 text-white/[0.88] text-[13.5px] leading-relaxed">
                {cap.question}
              </p>
            </div>
          </motion.div>
        )}
        </AnimatePresence>

        {/* AI bubble */}
        <AnimatePresence>
        {phase !== "typing-q" && phase !== "clicking" && (
          <motion.div 
            initial={{ opacity: 0, x: -10 }}
            animate={{ opacity: 1, x: 0 }}
            className="flex gap-3 md:gap-4 items-start"
          >
            <div style={{ fontFamily: "'DM Sans', sans-serif" }} className="w-8 h-8 rounded-full bg-[#111] flex items-center justify-center flex-shrink-0 text-[11.5px] font-semibold text-[#f5f5f0] shadow-sm">
              <Flame size={14} strokeWidth={2.5} className="text-[#f5f5f0]" />
            </div>
            <div className="bg-white border border-black/[0.07] rounded-3xl rounded-tl-sm px-6 py-5 flex-1 shadow-sm relative overflow-hidden">
              <div className="flex items-center gap-2 mb-3">
                <motion.div animate={{ opacity: [1, 0.5, 1] }} transition={{ duration: 2, repeat: Infinity }} className="w-1.5 h-1.5 rounded-full bg-[#111]" />
                <span style={{ fontFamily: "'DM Mono', monospace", letterSpacing: "0.15em" }} className="text-[9px] text-black/40 uppercase">
                  PROMETHEUS AI · {cap.tag}
                </span>
              </div>
              
              {phase === "thinking" ? (
                <div className="flex items-center h-[24px] gap-1.5 px-1 py-1">
                  <motion.span animate={{ opacity: [0.3, 1, 0.3], y: [0, -2, 0] }} transition={{ duration: 0.9, repeat: Infinity, delay: 0 }} className="w-[5px] h-[5px] bg-black/40 rounded-full" />
                  <motion.span animate={{ opacity: [0.3, 1, 0.3], y: [0, -2, 0] }} transition={{ duration: 0.9, repeat: Infinity, delay: 0.15 }} className="w-[5px] h-[5px] bg-black/40 rounded-full" />
                  <motion.span animate={{ opacity: [0.3, 1, 0.3], y: [0, -2, 0] }} transition={{ duration: 0.9, repeat: Infinity, delay: 0.3 }} className="w-[5px] h-[5px] bg-black/40 rounded-full" />
                </div>
              ) : (
                <p style={{ fontFamily: "'DM Sans', sans-serif" }} className="m-0 text-black/[0.65] text-[14.5px] leading-[1.7] min-h-[48px]">
                  {aDisplayed}
                  {phase === "answering" && (
                    <motion.span 
                      animate={{ opacity: [1, 0, 1] }}
                      transition={{ duration: 0.8, repeat: Infinity }}
                      className="inline-block w-[1.5px] h-[14px] bg-[#111] ml-1 align-middle"
                    />
                  )}
                </p>
              )}
            </div>
          </motion.div>
        )}
        </AnimatePresence>
      </div>

      {/* Input Box Area */}
      <div className="max-w-[720px] bg-white border border-black/[0.08] p-1.5 px-5 rounded-full flex items-center justify-between shadow-sm relative z-10 w-full mb-2">
         <div className="flex-1 text-[14px] text-black/80 font-sans h-10 flex items-center overflow-hidden mr-4">
            {(phase === "typing-q" || phase === "clicking") ? (
               <div className="flex items-center w-full">
                  <span className="truncate">{qDisplayed}</span>
                  {phase === "typing-q" && <span className="w-[1.5px] h-[16px] bg-black/40 ml-[2px] animate-pulse flex-shrink-0" />}
               </div>
            ) : (
               <span className="text-black/30">Ask Prometheus anything...</span>
            )}
         </div>

         <div 
           className="w-8 h-8 rounded-full flex items-center justify-center relative flex-shrink-0" 
           style={{ backgroundColor: (phase === "clicking" || phase === "thinking" || phase === "answering" || phase === "done") ? "#111" : "#f0f0f0", transition: "all 0.2s" }}
         >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke={(phase === "clicking" || phase === "thinking" || phase === "answering" || phase === "done") ? "white" : "black"} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: (phase === "typing-q") ? 0.3 : 1 }}>
               <line x1="12" y1="19" x2="12" y2="5"></line>
               <polyline points="5 12 12 5 19 12"></polyline>
            </svg>

            {/* Simulated Mouse cursor arrow */}
            {phase === "clicking" && (
              <motion.div
                initial={{ opacity: 0, x: 50, y: 50 }}
                animate={{ 
                  opacity: [0, 1, 1, 0, 0], 
                  x: [60, 4, 4, 4, 4], 
                  y: [80, 8, 8, 8, 8],
                  scale: [1, 1, 0.75, 0.75, 1]
                }}
                transition={{ duration: 0.8, times: [0, 0.4, 0.55, 0.8, 1], ease: "easeInOut" }}
                className="absolute right-0 bottom-0 pointer-events-none z-50 text-black origin-top-left drop-shadow-md"
              >
                <svg width="22" height="22" viewBox="0 0 24 24" fill="black" stroke="white" strokeWidth="1.5">
                  <path d="M4 2L20 10L13 13L16 22L13 24L10 16L4 20V2Z" />
                </svg>
              </motion.div>
            )}
         </div>
      </div>
      <div ref={endRef} className="h-2" />
    </motion.div>
  );
}

export function AiLayer() {
  const [active, setActive] = useState<string | null>(null);

  const handleSelect = (id: string) => {
    setActive(active === id ? null : id);
  };

  const activeCap = CAPABILITIES.find(c => c.id === active);

  return (
    <section className="bg-[#f5f5f0] w-full relative z-20 py-24 md:py-32 overflow-hidden px-6 md:px-12">
      <style dangerouslySetInnerHTML={{ __html: `
        @import url('https://fonts.googleapis.com/css2?family=DM+Mono:wght@400;500&family=DM+Sans:ital,opsz,wght@0,9..40,400;0,9..40,500;1,9..40,400&family=Playfair+Display:ital,wght@0,400;0,600;1,400;1,600&display=swap');
      `}} />
      
      <div className="max-w-6xl mx-auto w-full">
        {/* Header */}
        <motion.div 
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, margin: "-100px" }}
          transition={{ duration: 0.7, ease: "easeOut" }}
          className="mb-14 md:mb-20"
        >
          <div className="flex items-center gap-2 mb-4 md:mb-6">
            <span className="w-6 h-[1px] bg-black/20" />
            <p style={{ fontFamily: "'DM Mono', monospace", letterSpacing: "0.2em" }} className="text-[10px] text-black/40 uppercase m-0">
              AI Intelligence
            </p>
          </div>

          <h2 style={{ fontFamily: "'Playfair Display', serif" }} className="text-[2rem] sm:text-[2.6rem] md:text-[3.6rem] lg:text-[4.2rem] font-normal text-[#111] leading-[1.05] tracking-tight m-0 mb-5 max-w-2xl">
            Your money, guided by <span className="italic font-semibold block sm:inline mt-1 sm:mt-0">AI intelligence.</span>
          </h2>

          <p style={{ fontFamily: "'DM Sans', sans-serif" }} className="text-black/40 text-[15px] md:text-[17px] leading-[1.65] max-w-lg m-0">
            Nester doesn&apos;t just give you tools — it pairs you with Prometheus, an AI financial advisor that never sleeps, never misses a signal, and always puts your interests first.
          </p>
        </motion.div>

        {/* Cards grid */}
        <div className={`grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3 lg:gap-4 transition-all duration-500 ${activeCap ? 'mb-8 md:mb-12' : 'mb-0'}`}>
          {CAPABILITIES.map((cap, i) => {
            const isActive = active === cap.id;
            return (
              <div key={cap.id} className="flex flex-col">
                <motion.div
                  initial={{ opacity: 0, y: 20 }}
                  whileInView={{ opacity: 1, y: 0 }}
                  viewport={{ once: true, margin: "-50px" }}
                  transition={{ duration: 0.6, delay: i * 0.1, ease: "easeOut" }}
                  onClick={() => handleSelect(cap.id)}
                  className={`relative cursor-pointer transition-all duration-300 ease-[cubic-bezier(0.25,1,0.5,1)] rounded-2xl p-6 md:p-7 border bg-white/[0.02]
                    ${isActive
                      ? 'gradient-border-active gradient-border-sm shadow-sm bg-black/[0.04] translate-y-[-2px]'
                      : 'border-black/[0.07] hover:border-black/[0.18] hover:translate-y-[-4px] hover:shadow-[0_12px_32px_rgba(0,0,0,0.04)] hover:bg-white/[0.3]'
                    }
                  `}
                >
                  <div className="flex flex-col h-full">
                    <p style={{ fontFamily: "'DM Mono', monospace", letterSpacing: "0.18em" }} className="text-[9px] text-black/[0.28] uppercase m-0 mb-4">
                      {cap.tag}
                    </p>

                    <h3 style={{ fontFamily: "'Playfair Display', serif" }} className="text-[17px] md:text-[19px] font-normal text-[#111] m-0 mb-2.5 leading-[1.2] tracking-tight">
                      {cap.title}
                    </h3>

                    <p style={{ fontFamily: "'DM Sans', sans-serif" }} className="text-[13px] text-black/40 m-0 mb-6 leading-[1.6] flex-1">
                      {cap.desc}
                    </p>

                    <div className="mt-auto pt-4 border-t border-black/[0.04] flex items-center justify-between">
                      <span style={{ fontFamily: "'DM Mono', monospace", letterSpacing: "0.1em" }} className={`text-[9.5px] uppercase transition-colors duration-200 ${isActive ? 'text-[#111]' : 'text-black/[0.22] group-hover:text-black/40'}`}>
                        {isActive ? 'CLOSE' : 'ASK PROMETHEUS'}
                      </span>
                      <span style={{ fontFamily: "'DM Mono', monospace" }} className={`text-[10px] transition-transform duration-300 ${isActive ? 'text-[#111] rotate-180' : 'text-black/[0.22] rotate-0'}`}>
                        ↓
                      </span>
                    </div>
                  </div>
                </motion.div>

                {/* Inline chat panel — visible on mobile/tablet only */}
                <div className="sm:hidden">
                  <AnimatePresence mode="wait">
                    {isActive && (
                      <ChatPanel key={cap.id} cap={cap} />
                    )}
                  </AnimatePresence>
                </div>
              </div>
            );
          })}
        </div>

        {/* Chat Panel — visible on sm+ screens */}
        <div className="hidden sm:block">
          <AnimatePresence mode="wait">
            {activeCap && (
              <ChatPanel key={activeCap.id} cap={activeCap} />
            )}
          </AnimatePresence>
        </div>

      </div>
    </section>
  );
}
