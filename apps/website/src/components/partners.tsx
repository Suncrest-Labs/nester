"use client"

const partners = [
    { name: "MoneyGram", logo: "/logos/moneygram.svg" },
    { name: "Circle", logo: "/logos/circle.svg" },
    { name: "Franklin Templeton", logo: "/logos/franklin.svg" },
    { name: "Fireblocks", logo: "/logos/fireblocks.svg" },
    { name: "WisdomTree", logo: "/logos/wisdomtree.svg" },
    { name: "Bitso", logo: "/logos/bitso.svg" },
    { name: "Flutterwave", logo: "/logos/flutterwave.svg" },
    { name: "Anchorage Digital", logo: "/logos/anchorage.svg" },
]

export function Partners() {
    return (
        <section className="w-full py-4 overflow-hidden bg-white border-b border-gray-100 relative">
            <div className="relative z-10 flex w-full">
                <div className="flex animate-marquee whitespace-nowrap items-center">
                    {partners.map((partner, i) => (
                        <div key={i} className="mx-12 md:mx-24 flex items-center justify-center grayscale hover:grayscale-0 transition-all duration-300 opacity-70 hover:opacity-100">
                            <img
                                src={partner.logo}
                                alt={partner.name}
                                className="h-10 md:h-14 w-auto"
                                style={{ minWidth: "80px", maxWidth: "160px" }}
                            />
                        </div>
                    ))}
                </div>
                {/* Duplicate for seamless scrolling */}
                <div className="flex absolute top-0 animate-marquee2 whitespace-nowrap items-center">
                    {partners.map((partner, i) => (
                        <div key={`dup-${i}`} className="mx-12 md:mx-24 flex items-center justify-center grayscale hover:grayscale-0 transition-all duration-300 opacity-70 hover:opacity-100">
                            <img
                                src={partner.logo}
                                alt={partner.name}
                                className="h-10 md:h-14 w-auto"
                                style={{ minWidth: "80px", maxWidth: "160px" }}
                            />
                        </div>
                    ))}
                </div>
            </div>

            {/* Smoke White Effect - Left */}
            <div className="absolute top-0 left-0 h-full w-32 bg-gradient-to-r from-white via-white/80 to-transparent z-20 pointer-events-none" />

            {/* Smoke White Effect - Right */}
            <div className="absolute top-0 right-0 h-full w-32 bg-gradient-to-l from-white via-white/80 to-transparent z-20 pointer-events-none" />
        </section>
    )
}