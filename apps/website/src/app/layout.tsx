import type { Metadata } from "next";
import { Space_Grotesk, Inter, Cormorant } from "next/font/google";
import { CookieConsent } from "@/components/cookie-consent";
import { SmoothScroll } from "@/components/smooth-scroll";
import "./globals.css";

const spaceGrotesk = Space_Grotesk({ subsets: ["latin"], variable: "--font-space-grotesk" });
const inter = Inter({ subsets: ["latin"], variable: "--font-inter" });
const cormorant = Cormorant({ subsets: ["latin"], weight: ["300", "400"], style: ["normal", "italic"], variable: "--font-cormorant" });

export const metadata: Metadata = {
  metadataBase: new URL("https://nester.finance"),
  title: {
    default: "Nester | Decentralized Savings & Liquidity",
    template: "%s | Nester",
  },
  description:
    "Optimize crypto yield and settle to fiat instantly through a decentralized liquidity network built for emerging markets.",
  keywords: ["DeFi", "Stellar", "Soroban", "Savings", "Liquidity", "Crypto", "Fiat Settlement", "Yield Optimization"],
  authors: [{ name: "Nester Protocol" }],
  creator: "Nester Protocol",
  publisher: "Nester Protocol",
  alternates: {
    canonical: "/",
  },
  icons: {
    icon: "/logo.png",
    apple: "/logo.png",
  },
  openGraph: {
    type: "website",
    locale: "en_US",
    url: "https://nester.finance",
    siteName: "Nester",
    title: "Nester | Decentralized Savings & Liquidity",
    description: "Optimize crypto yield and settle to fiat instantly through a decentralized liquidity network built for emerging markets.",
    images: [
      {
        url: "/og-image.png",
        width: 1200,
        height: 630,
        alt: "Nester - Decentralized Savings & Liquidity",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "Nester | Decentralized Savings & Liquidity",
    description: "Optimize crypto yield and settle to fiat instantly through a decentralized liquidity network built for emerging markets.",
    images: ["/og-image.png"],
    site: "@NesterProtocol",
  },
};

const jsonLd = {
  "@context": "https://schema.org",
  "@type": "Organization",
  "name": "Nester",
  "url": "https://nester.finance",
  "logo": "https://nester.finance/logo.png",
  "sameAs": [
    "https://twitter.com/NesterProtocol",
    "https://github.com/Suncrest-Labs/nester"
  ],
  "description": "Nester is a decentralized savings and liquidity protocol built on Stellar/Soroban that automates crypto savings."
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className="scroll-smooth">
      <head>
        <script
          defer
          data-domain="nester.finance"
          src="https://plausible.io/js/script.js"
        />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
        />
      </head>
      <body
        suppressHydrationWarning
        className={`${spaceGrotesk.variable} ${inter.variable} ${cormorant.variable} antialiased bg-background text-foreground selection:bg-primary selection:text-primary-foreground font-sans`}
      >
        <SmoothScroll>
          {children}
          <CookieConsent />
        </SmoothScroll>
      </body>
    </html>
  );
}
