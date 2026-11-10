import type { Metadata } from "next";
import { Space_Grotesk, Inter, Cormorant } from "next/font/google";
import { CookieConsent } from "@/components/cookie-consent";
import { Analytics } from "@/components/analytics";
import { SmoothScroll } from "@/components/smooth-scroll";
import "./globals.css";

const spaceGrotesk = Space_Grotesk({ subsets: ["latin"], variable: "--font-space-grotesk" });
const inter = Inter({ subsets: ["latin"], variable: "--font-inter" });
const cormorant = Cormorant({ subsets: ["latin"], weight: ["300", "400"], style: ["normal", "italic"], variable: "--font-cormorant" });

export const metadata: Metadata = {
  metadataBase: new URL("https://usenester.xyz"),
  title: {
    default: "Nester | The USDC Savings Rate for Stellar",
    template: "%s | Nester",
  },
  description:
    "Deposit USDC into the Nester Savings Rate, a non-custodial savings product on Stellar. The rate is the realized 7-day rate paid by Blend and DeFindex; it is not guaranteed.",
  keywords: ["DeFi", "Stellar", "Soroban", "Savings", "Liquidity", "Crypto", "USDC", "Savings Rate", "Blend", "DeFindex"],
  authors: [{ name: "Nester Protocol" }],
  creator: "Nester Protocol",
  publisher: "Nester Protocol",
  icons: {
    icon: "/logo.png",
    apple: "/logo.png",
  },
  openGraph: {
    type: "website",
    locale: "en_US",
    siteName: "Nester",
    title: "Nester | The USDC Savings Rate for Stellar",
    description: "Deposit USDC into the Nester Savings Rate, a non-custodial savings product on Stellar. The rate is the realized 7-day rate paid by Blend and DeFindex; it is not guaranteed.",
    images: [
      {
        url: "/og-image.png",
        width: 1200,
        height: 630,
        alt: "Nester - The USDC Savings Rate for Stellar",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "Nester | The USDC Savings Rate for Stellar",
    description: "Deposit USDC into the Nester Savings Rate, a non-custodial savings product on Stellar. The rate is the realized 7-day rate paid by Blend and DeFindex; it is not guaranteed.",
    images: ["/og-image.png"],
    site: "@NesterProtocol",
  },
};

const jsonLd = {
  "@context": "https://schema.org",
  "@type": "Organization",
  "name": "Nester",
  "url": "https://usenester.xyz",
  "logo": "https://usenester.xyz/logo.png",
  "sameAs": [
    "https://twitter.com/NesterProtocol",
    "https://github.com/Suncrest-Labs/nester"
  ],
  "description": "Nester is a decentralized savings and yield protocol built on Stellar/Soroban that automates crypto savings."
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
          <Analytics />
        </SmoothScroll>
      </body>
    </html>
  );
}
