import { Metadata } from "next";
import { Navbar } from "@/components/navbar";
import { Hero } from "@/components/hero";
import { ImageCarousel } from "@/components/image-carousel";
import { Ecosystem } from "@/components/ecosystem";
import { HowItWorks } from "@/components/how-it-works";
import { UseCases } from "@/components/use-cases";
import { Faq } from "@/components/faq";
import { Footer } from "@/components/footer";
// import { FeaturesFloat } from "@/components/features-float";
// import { Architecture } from "@/components/architecture";

export const metadata: Metadata = {
  alternates: {
    canonical: "/",
  },
};

const jsonLd = {
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "WebApplication",
      "name": "Nester",
      "url": "https://usenester.xyz",
      "applicationCategory": "FinanceApplication",
      "operatingSystem": "All",
      "description": "Decentralized savings and yield protocol built on Stellar/Soroban.",
      "offers": {
        "@type": "Offer",
        "price": "0",
        "priceCurrency": "USD"
      }
    },
    {
      "@type": "FAQPage",
      "mainEntity": [
        {
          "@type": "Question",
          "name": "What is Nester?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Nester is a non-custodial savings app on Stellar. You set savings goals, automate recurring deposits, and hold stablecoins in smart vaults that route deposits to Stellar yield protocols. Nester is live on testnet today; mainnet follows an external security audit."
          }
        },
        {
          "@type": "Question",
          "name": "How does Nester generate yield?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Vault deposits are routed through adapter contracts to integrated Stellar yield protocols, within per-protocol limits set by each vault's risk parameters. Rebalancing is triggered by a keeper and rate-limited on-chain. On testnet, the yield integrations are still being connected, so vaults do not earn live yield yet."
          }
        },
        {
          "@type": "Question",
          "name": "How do withdrawals work?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "You can withdraw at any time. Your vault shares are burned on-chain and the underlying stablecoins are paid from the vault's available liquidity straight back to your own wallet. Withdrawing within one day of depositing incurs a 0.1% early-withdrawal fee; locked positions charge a higher fee if exited early."
          }
        }
      ]
    }
  ]
};

export default function Home() {
  return (
    <main className="min-h-screen bg-background text-foreground overflow-hidden">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
      />

      <div className="bg-[#fafafa] pb-8">
        <Navbar />
        <div className="min-h-[100vh] flex flex-col pt-[100px] justify-between">
          <div className="flex-1 flex items-center justify-center">
              <Hero />
          </div>
          <div className="mb-4">
              <ImageCarousel />
          </div>
        </div>
      </div>
      <UseCases />
      {/* <FeaturesFloat /> */}
      <Ecosystem />
      <HowItWorks />
      <Faq />
      <Footer />
      {/* <Architecture /> */}
    </main>
  );
}
