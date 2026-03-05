"use client"

import Image from "next/image"

const baseItems = [
    { src: "/logos/blend.svg",     alt: "Blend",     bg: "#dcdcdc" },
    { src: "/logos/moneygram.png", alt: "MoneyGram", bg: "#dcdcdc" },
    { src: "/logos/Stellar.png",   alt: "Stellar",   bg: "#dcdcdc" },
    { src: "/logos/usdc.svg",      alt: "USDC",      bg: "#dcdcdc" },
    { src: "/logos/aquarius.svg",  alt: "Aquarius",  bg: "#dcdcdc" },
    { src: "/logos/freighter.png", alt: "Freighter", bg: "#dcdcdc" },
]

// Repeat 4× so the track is long enough for a seamless loop on any screen
const carouselItems = [...baseItems, ...baseItems, ...baseItems, ...baseItems]

// Duplicate for seamless infinite loop (translateX -50% lands back at start)
const track = [...carouselItems, ...carouselItems]

export function ImageCarousel() {
    return (
        <section className="relative w-full pt-4 pb-16 overflow-hidden">
            {/* Left smoke fade */}
            <div
                className="pointer-events-none absolute left-0 top-0 h-full w-16 sm:w-24 md:w-40 lg:w-48 z-10"
                style={{ background: "linear-gradient(to right, hsl(0,0%,85%) 0%, transparent 100%)" }}
            />
            {/* Right smoke fade */}
            <div
                className="pointer-events-none absolute right-0 top-0 h-full w-16 sm:w-24 md:w-40 lg:w-48 z-10"
                style={{ background: "linear-gradient(to left, hsl(0,0%,85%) 0%, transparent 100%)" }}
            />

            {/* Scrolling track — contains two identical sets for seamless loop */}
            <div className="animate-scroll-left flex w-max gap-3 md:gap-4">
                {track.map((item, i) => (
                    <div
                        key={i}
                        className="flex-shrink-0 w-44 h-60 sm:w-52 sm:h-72 md:w-60 md:h-80 lg:w-72 lg:h-96 rounded-2xl lg:rounded-3xl overflow-hidden flex items-center justify-center shadow-sm"
                        style={{ backgroundColor: item.bg }}
                    >
                        <div className="relative w-28 h-28 sm:w-36 sm:h-36 md:w-44 md:h-44 lg:w-52 lg:h-52">
                            <Image
                                src={item.src}
                                alt={item.alt}
                                fill
                                className="object-contain"
                                draggable={false}
                            />
                        </div>
                    </div>
                ))}
            </div>
        </section>
    )
}
