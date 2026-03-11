"use client";

import { useRef, useState, useEffect } from "react";
import { motion, useInView } from "framer-motion";
import Image from "next/image";
import Link from "next/link";

const NAV_COMMANDS = [
  {
    cmd: "ls ./protocol",
    items: [
      { name: "Vaults", href: "#" },
      { name: "Off-Ramp", href: "#" },
      { name: "Governance", href: "#" },
      { name: "Tokenomics", href: "#" },
    ],
  },
  {
    cmd: "ls ./developers",
    items: [
      { name: "Documentation", href: "#" },
      { name: "GitHub", href: "#" },
      { name: "API Reference", href: "#" },
      { name: "Bug Bounty", href: "#" },
    ],
  },
  {
    cmd: "ls ./community",
    items: [
      { name: "Twitter / X", href: "#" },
      { name: "Discord", href: "#" },
      { name: "Blog", href: "#" },
      { name: "Brand Kit", href: "#" },
    ],
  },
];

const SOCIAL_COMMANDS = [
  { cmd: "connect --twitter", label: "Twitter / X", href: "#" },
  { cmd: "connect --discord", label: "Discord", href: "#" },
  { cmd: "connect --github", label: "GitHub", href: "#" },
];

/* Typing animation hook */
function useTypedText(text: string, active: boolean, speed = 35) {
  const [displayed, setDisplayed] = useState("");
  const [done, setDone] = useState(false);

  useEffect(() => {
    if (!active) {
      setDisplayed("");
      setDone(false);
      return;
    }
    let i = 0;
    setDisplayed("");
    setDone(false);
    const iv = setInterval(() => {
      i++;
      setDisplayed(text.slice(0, i));
      if (i >= text.length) {
        clearInterval(iv);
        setDone(true);
      }
    }, speed);
    return () => clearInterval(iv);
  }, [active, text, speed]);

  return { displayed, done };
}

/* A single terminal command block */
function CommandBlock({
  command,
  children,
  delay,
  isInView,
}: {
  command: string;
  children: React.ReactNode;
  delay: number;
  isInView: boolean;
}) {
  const [active, setActive] = useState(false);
  const { displayed, done } = useTypedText(command, active, 30);

  useEffect(() => {
    if (!isInView) return;
    const t = setTimeout(() => setActive(true), delay);
    return () => clearTimeout(t);
  }, [isInView, delay]);

  return (
    <div className="mb-5">
      {/* Command line */}
      <div className="flex items-center gap-2 mb-2">
        <span className="text-emerald-400/70 text-[12px] font-mono flex-shrink-0 select-none">
          $
        </span>
        <span className="text-[12px] font-mono text-white/60">
          {displayed}
          {active && !done && (
            <motion.span
              animate={{ opacity: [1, 0, 1] }}
              transition={{ duration: 0.8, repeat: Infinity }}
              className="inline-block w-[6px] h-[14px] bg-white/50 ml-0.5 align-middle"
            />
          )}
        </span>
      </div>

      {/* Output */}
      {done && (
        <motion.div
          initial={{ opacity: 0, y: 4 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, ease: "easeOut" }}
          className="pl-5"
        >
          {children}
        </motion.div>
      )}
    </div>
  );
}

export function Footer() {
  const footerRef = useRef<HTMLElement>(null);
  const isInView = useInView(footerRef, { once: true, margin: "-60px" });

  return (
    <footer
      ref={footerRef}
      className="relative w-full" style={{ background: "#f5f5f0" }}
    >
      <div className="relative max-w-6xl mx-auto px-6 md:px-12">
      {/* Terminal window */}
      <div className="pt-20 md:pt-28 pb-12 md:pb-16">
        {/* Terminal chrome */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={isInView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.6, ease: [0.23, 1, 0.32, 1] }}
          className="rounded-2xl border border-white/[0.06] bg-[#111111] overflow-hidden"
        >
          {/* Title bar */}
          <div className="flex items-center gap-3 px-5 py-3.5 border-b border-white/[0.06] bg-white/[0.02]">
            <div className="flex gap-2">
              <span className="w-3 h-3 rounded-full bg-[#ff5f57]" />
              <span className="w-3 h-3 rounded-full bg-[#febc2e]" />
              <span className="w-3 h-3 rounded-full bg-[#28c840]" />
            </div>
            <div className="flex-1 flex justify-center">
              <span className="text-[10px] font-mono tracking-[0.15em] uppercase text-white/20">
                nester@protocol ~ /home
              </span>
            </div>
            <div className="flex items-center gap-1.5">
              <motion.span
                animate={{ opacity: [0.3, 0.8, 0.3] }}
                transition={{ duration: 2, repeat: Infinity }}
                className="w-1.5 h-1.5 rounded-full bg-emerald-400/80 inline-block"
              />
              <span className="text-[9px] font-mono text-white/20">live</span>
            </div>
          </div>

          {/* Terminal body */}
          <div className="px-5 md:px-8 py-6 md:py-8">
            {/* Startup message */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={isInView ? { opacity: 1 } : {}}
              transition={{ duration: 0.4, delay: 0.2 }}
              className="mb-6"
            >
              <div className="flex items-center gap-3 mb-1">
                <Image
                  src="/logo.png"
                  alt="Nester"
                  width={18}
                  height={18}
                  className="rounded-md opacity-60"
                />
                <span className="text-[11px] font-mono text-white/25">
                  Nester Protocol v1.0.0 — Decentralized yield &amp; instant
                  fiat settlement
                </span>
              </div>
              <span className="text-[11px] font-mono text-white/15 pl-[30px]">
                Type a command or click a link to navigate.
              </span>
            </motion.div>

            <div className="h-px bg-white/[0.04] mb-6" />

            {/* Command blocks — staggered on scroll */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-x-8 gap-y-2">
              {NAV_COMMANDS.map((group, gi) => (
                <CommandBlock
                  key={group.cmd}
                  command={group.cmd}
                  delay={300 + gi * 600}
                  isInView={isInView}
                >
                  <div className="flex flex-col gap-1.5">
                    {group.items.map((item) => (
                      <Link
                        key={item.name}
                        href={item.href}
                        className="group flex items-center gap-2 text-[12px] font-mono text-white/40 hover:text-white/80 transition-colors duration-200 w-fit"
                      >
                        <span className="text-white/15 group-hover:text-emerald-400/60 transition-colors duration-200 select-none">
                          &gt;
                        </span>
                        {item.name}
                      </Link>
                    ))}
                  </div>
                </CommandBlock>
              ))}
            </div>

            <div className="h-px bg-white/[0.04] my-6" />

            {/* Social connect commands */}
            <CommandBlock
              command="nester --socials"
              delay={300 + NAV_COMMANDS.length * 600}
              isInView={isInView}
            >
              <div className="flex flex-wrap gap-x-6 gap-y-2">
                {SOCIAL_COMMANDS.map((s) => (
                  <Link
                    key={s.cmd}
                    href={s.href}
                    className="group flex items-center gap-2 text-[12px] font-mono text-white/40 hover:text-white/80 transition-colors duration-200"
                  >
                    <span className="text-emerald-400/40 group-hover:text-emerald-400/80 transition-colors duration-200 select-none">
                      $
                    </span>
                    <span className="text-white/20 group-hover:text-white/50 transition-colors duration-200">
                      {s.cmd}
                    </span>
                  </Link>
                ))}
              </div>
            </CommandBlock>

            {/* Blinking cursor at the bottom */}
            <div className="flex items-center gap-2 mt-4">
              <span className="text-emerald-400/70 text-[12px] font-mono select-none">
                $
              </span>
              <motion.span
                animate={{ opacity: [1, 0, 1] }}
                transition={{ duration: 1, repeat: Infinity }}
                className="inline-block w-[7px] h-[15px] bg-white/40"
              />
            </div>
          </div>
        </motion.div>
      </div>

      {/* Bottom bar — outside the terminal */}
      <motion.div
        initial={{ opacity: 0 }}
        animate={isInView ? { opacity: 1 } : {}}
        transition={{ duration: 0.5, delay: 0.6 }}
        className="flex flex-col md:flex-row items-center justify-between gap-4 pb-8"
      >
        <div className="flex items-center gap-3">
          <Image
            src="/logo.png"
            alt="Nester"
            width={20}
            height={20}
            className="rounded-lg opacity-30"
          />
          <p className="text-[11px] font-mono tracking-[0.08em] text-black/25">
            &copy; {new Date().getFullYear()} Nester Protocol
          </p>
        </div>

        <div className="flex items-center gap-4">
          <Link
            href="#"
            className="text-[11px] font-mono text-black/20 hover:text-black/50 transition-colors duration-200"
          >
            Terms
          </Link>
          <span className="w-px h-3 bg-black/[0.12]" />
          <Link
            href="#"
            className="text-[11px] font-mono text-black/20 hover:text-black/50 transition-colors duration-200"
          >
            Privacy
          </Link>
          <span className="w-px h-3 bg-black/[0.12]" />
          <Link
            href="#"
            className="text-[11px] font-mono text-black/20 hover:text-black/50 transition-colors duration-200"
          >
            Risk
          </Link>
        </div>
      </motion.div>
      </div>
    </footer>
  );
}
