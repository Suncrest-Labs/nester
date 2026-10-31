"use client"

import { motion } from "framer-motion"
import { ArrowRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Container } from "@/components/container"
import Image from "next/image"
import Link from "next/link"

const DAPP_URL = process.env.NEXT_PUBLIC_DAPP_URL ?? "http://localhost:3001"

export function Hero() {
    return (
        <section className="relative py-4 md:py-8 flex flex-col items-center justify-center overflow-hidden text-center w-full">
            <Container className="flex flex-col items-center justify-center gap-6 md:gap-8 z-20">
                <motion.h1
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.8, ease: "easeOut" }}
                    className="font-heading font-light leading-[1.15] text-foreground tracking-tight text-[2rem] sm:text-[2.6rem] md:text-[3.4rem] lg:text-[4.2rem] xl:text-[5rem]"
                >
                    <span className="font-montserrat">The</span>{" "}
                    <span
                        className="hidden md:inline-flex items-center justify-center align-middle mx-1 lg:mx-2 rounded-2xl lg:rounded-3xl overflow-hidden shadow-sm"
                        style={{
                            width: "clamp(80px, 10vw, 150px)",
                            height: "clamp(54px, 6.7vw, 100px)",
                            verticalAlign: "middle",
                            transform: "rotate(10deg)"
                        }}
                    >
                        <Image src="/logo.png" alt="Nester" width={150} height={100} className="object-cover w-full h-full" />
                    </span>{" "}
                    <span className="font-alpina italic font-medium">Savings</span>{" "}
                    <span className="font-montserrat">Layer</span><br />
                    <span className="font-montserrat">for</span>{" "}
                    <span
                        className="hidden md:inline-flex items-center justify-center align-middle mx-1 lg:mx-2 rounded-2xl lg:rounded-3xl overflow-hidden shadow-sm"
                        style={{
                            width: "clamp(80px, 10vw, 150px)",
                            height: "clamp(54px, 6.7vw, 100px)",
                            verticalAlign: "middle",
                            backgroundColor: "#c8c8c8"
                        }}
                    >
                        <Image src="/logos/Stellar.png" alt="Stellar" width={100} height={100} className="object-contain p-2" />
                    </span>{" "}
                    <span className="font-alpina italic font-medium">Stellar</span>
                </motion.h1>

                <motion.p
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.8, delay: 0.2, ease: "easeOut" }}
                    className="text-muted-foreground text-sm sm:text-base md:text-lg lg:text-xl max-w-sm sm:max-w-lg md:max-w-2xl leading-relaxed font-sans"
                >
                    Set savings goals, automate recurring deposits, and grow your stablecoins in non-custodial smart vaults on Stellar. Live on testnet.
                </motion.p>

                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.8, delay: 0.3, ease: "easeOut" }}
                    className="flex flex-col sm:flex-row items-center justify-center gap-3 md:gap-4 mt-2 md:mt-4"
                >
                    <div className="p-[3px] rounded-full border border-black/15 shadow-xl bg-white">
                        <Button
                            asChild
                            className="h-10 md:h-12 px-6 md:px-8 my-[0.1rem] mx-[0.1rem] bg-gradient-to-r from-[#0a0a0a] to-[#1a1a2e] border border-white/10 hover:from-[#1a1a2e] hover:to-[#0a0a0a] text-white transition-all rounded-full font-medium text-sm md:text-base"
                        >
                            <a href={DAPP_URL} target="_blank" rel="noopener noreferrer">
                                Launch Testnet App
                                <ArrowRight className="ml-2 w-4 h-4" />
                            </a>
                        </Button>
                    </div>
                    <div className="p-[3px] rounded-full border border-black/15 shadow-xl bg-white">
                        <Button
                            asChild
                            className="h-10 md:h-12 px-6 md:px-8 my-[0.1rem] mx-[0.1rem] bg-gradient-to-r from-[#0a0a0a] to-[#1a1a2e] border border-white/10 hover:from-[#1a1a2e] hover:to-[#0a0a0a] text-white transition-all rounded-full font-medium text-sm md:text-base"
                        >
                            <Link href="/docs">
                                Read the Docs
                                <ArrowRight className="ml-2 w-4 h-4" />
                            </Link>
                        </Button>
                    </div>
                </motion.div>
            </Container>

            
        </section>
    )
}
