"use client"

import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Network, BrainCircuit, Landmark, Coins, ArrowRightLeft, ShieldCheck, Zap } from "lucide-react";

const LAYERS = [
  {
    id: "ai",
    title: "Prometheus AI Layer",
    subtitle: "The Brain of Nester",
    icon: <BrainCircuit strokeWidth={1.5} size={28} />,
    color: "bg-[#111]",
    textColor: "text-[#f5f5f0]",
    borderColor: "border-[#333]",
    description: "An intelligent routing engine that analyzes your financial goals in real-time. Prometheus continuously monitors market volatility, protocol TVL, and yield opportunities across the Stellar ecosystem to autonomously rebalance your portfolio.",
    features: [
      { icon: <ShieldCheck size={16} />, text: "Real-time risk scoring & rebalancing" },
      { icon: <Zap size={16} />, text: "Automated yield hunting" },
      { icon: <ArrowRightLeft size={16} />, text: "Conversational interface execution" }
    ]
  },
  {
    id: "defi",
    title: "Smart Yield Layer",
    subtitle: "The DeFi Engine",
    icon: <Coins strokeWidth={1.5} size={28} />,
    color: "bg-[#f5f5f0]",
    textColor: "text-[#111]",
    borderColor: "border-black/[0.08]",
    description: "The underlying smart contract architecture that actually puts your money to work. It aggregates fragmented liquidity and distributes it into institutional-grade lending protocols to secure the highest safe APY.",
    features: [
      { icon: <Landmark size={16} />, text: "Aave & Blend integrations" },
      { icon: <ShieldCheck size={16} />, text: "Auto-compounding smart vaults" },
      { icon: <Zap size={16} />, text: "Zero collateral lockup strategies" }
    ]
  },
  {
    id: "liquidity",
    title: "Distributed Off-Ramp Network",
    subtitle: "The Global Bridge",
    icon: <Network strokeWidth={1.5} size={28} />,
    color: "bg-white",
    textColor: "text-[#111]",
    borderColor: "border-black/[0.08]",
    description: "A decentralized mesh of Liquidity Providers (LPs) acting as specialized fiat nodes. When you request a withdrawal, the network routes it to the most optimal P2P node for instant, sub-3-second settlement to your local bank.",
    features: [
      { icon: <Landmark size={16} />, text: "Over-collateralized verified LPs" },
      { icon: <Zap size={16} />, text: "Instant fiat settlement" },
      { icon: <ArrowRightLeft size={16} />, text: "Zero exchange swap latency" }
    ]
  }
];

export function Architecture() {
  const [activeLayer, setActiveLayer] = useState(LAYERS[0].id);

  return (
    <section className="bg-[#fafaf8] py-24 md:py-32 border-t border-black/[0.04] overflow-hidden">
       <div className="max-w-6xl mx-auto px-6 md:px-12">
          {/* Header */}
          <motion.div 
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.6 }}
            className="mb-16 md:mb-24 text-center md:text-left flex flex-col md:flex-row md:items-end justify-between gap-8"
          >
             <div className="max-w-xl">
                <p style={{ fontFamily: "'DM Mono', monospace", letterSpacing: "0.2em" }} className="text-[10px] text-black/40 uppercase m-0 mb-4">
                  What we are building
                </p>
                <h2 style={{ fontFamily: "'Playfair Display', serif" }} className="text-[2.2rem] md:text-[3.4rem] font-normal text-[#111] leading-[1.1] tracking-tight m-0">
                  The <span className="italic font-semibold">Stack.</span>
                </h2>
             </div>
             <p style={{ fontFamily: "'DM Sans', sans-serif" }} className="text-black/40 text-[15px] md:text-[16px] leading-[1.6] max-w-sm m-0 md:pb-2">
                A vertically integrated architecture designed to make global decentralized finance feel like a local savings account.
             </p>
          </motion.div>

          <div className="flex flex-col lg:flex-row gap-12 lg:gap-20 items-start">
            
            {/* Visual Stack Representation (Left) */}
            <div className="w-full lg:w-1/2 relative min-h-[400px] flex flex-col items-center justify-center perspective-[1000px]">
               <div className="relative w-full max-w-[400px] mx-auto perspective-1000">
                 {LAYERS.map((layer, idx) => {
                   const isActive = activeLayer === layer.id;

                   return (
                     <motion.div
                       key={layer.id}
                       onClick={() => setActiveLayer(layer.id)}
                       animate={{
                         y: (idx - 1) * 90 + (isActive ? 0 : 0),
                         scale: isActive ? 1.05 : 0.95,
                         rotateX: isActive ? 5 : 25,
                         opacity: isActive ? 1 : 0.6,
                         zIndex: isActive ? 30 : (20 - idx),
                       }}
                       transition={{ duration: 0.6, ease: [0.23, 1, 0.32, 1] }}
                       className={`absolute w-full left-0 cursor-pointer p-6 md:p-8 rounded-2xl shadow-xl border ${layer.borderColor} ${layer.color} ${layer.textColor} flex items-center gap-6`}
                       style={{ transformOrigin: "center bottom" }}
                     >
                       <div className={`w-14 h-14 rounded-full flex items-center justify-center flex-shrink-0 ${layer.id === 'ai' ? 'bg-[#222]' : 'bg-black/[0.04]'}`}>
                         {layer.icon}
                       </div>
                       <div>
                         <p style={{ fontFamily: "'DM Mono', monospace", letterSpacing: "0.15em" }} className="text-[9px] opacity-60 uppercase m-0 mb-1">
                           Layer {idx + 1}
                         </p>
                         <h3 style={{ fontFamily: "'Playfair Display', serif" }} className="text-[20px] m-0 leading-tight">
                           {layer.title}
                         </h3>
                       </div>
                     </motion.div>
                   );
                 })}
               </div>
            </div>

            {/* Content Details (Right) */}
            <div className="w-full lg:w-1/2 h-[400px] flex items-center">
               <AnimatePresence mode="wait">
                 {LAYERS.map((layer) => {
                   if (layer.id !== activeLayer) return null;
                   return (
                     <motion.div
                       key={layer.id}
                       initial={{ opacity: 0, y: 20 }}
                       animate={{ opacity: 1, y: 0 }}
                       exit={{ opacity: 0, y: -20 }}
                       transition={{ duration: 0.4 }}
                       className="w-full"
                     >
                        <div className="mb-6">
                           <span style={{ fontFamily: "'DM Mono', monospace", letterSpacing: "0.15em" }} className="inline-block px-3 py-1 bg-black/[0.04] rounded-full text-[10px] text-black/60 uppercase mb-4">
                             {layer.subtitle}
                           </span>
                           <h3 style={{ fontFamily: "'Playfair Display', serif" }} className="text-[2rem] md:text-[2.6rem] font-normal text-[#111] leading-[1.1] tracking-tight m-0 mb-6">
                             {layer.title}
                           </h3>
                           <p style={{ fontFamily: "'DM Sans', sans-serif" }} className="text-black/60 text-[15px] md:text-[16px] leading-[1.7] m-0">
                             {layer.description}
                           </p>
                        </div>

                        <div className="space-y-4 pt-6 border-t border-black/[0.08]">
                           {layer.features.map((feat, i) => (
                             <div key={i} className="flex items-center gap-3">
                                <div className="w-6 h-6 rounded-full bg-black/[0.03] flex items-center justify-center text-black/60">
                                   {feat.icon}
                                </div>
                                <span style={{ fontFamily: "'DM Sans', sans-serif" }} className="text-[14px] text-black/80 font-medium">
                                   {feat.text}
                                </span>
                             </div>
                           ))}
                        </div>
                     </motion.div>
                   )
                 })}
               </AnimatePresence>
            </div>
          </div>
       </div>
    </section>
  );
}
