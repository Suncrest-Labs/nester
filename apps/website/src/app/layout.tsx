import type { Metadata } from "next";
import { Space_Grotesk, Inter, Cormorant } from "next/font/google";
import { CookieConsent } from "@/components/cookie-consent";
import "./globals.css";

const spaceGrotesk = Space_Grotesk({ subsets: ["latin"], variable: "--font-space-grotesk" });
const inter = Inter({ subsets: ["latin"], variable: "--font-inter" });
const cormorant = Cormorant({ subsets: ["latin"], weight: ["300", "400"], style: ["normal", "italic"], variable: "--font-cormorant" });

export const metadata: Metadata = {
  title: "Nester | Decentralized Savings & Liquidity",
  description:
    "Optimize crypto yield and settle to fiat instantly through a decentralized liquidity network built for emerging markets.",
  icons: {
    icon: "/logo.png",
    apple: "/logo.png",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className="scroll-smooth">
      <body
        suppressHydrationWarning
        className={`${spaceGrotesk.variable} ${inter.variable} ${cormorant.variable} antialiased bg-background text-foreground selection:bg-primary selection:text-primary-foreground font-sans`}
      >
        {children}
        <CookieConsent />
      </body>
    </html>
  );
}
