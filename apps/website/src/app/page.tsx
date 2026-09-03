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
      "url": "https://nester.finance",
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
            "text": "Nester is a DeFi savings and yield platform that lets you deposit stablecoins into non-custodial smart vaults and automatically earn optimized returns across audited protocols like Aave and Blend on Stellar."
          }
        },
        {
          "@type": "Question",
          "name": "How does Nester generate yield?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Your deposits are automatically allocated across battle-tested DeFi protocols. Nester's optimization engine continuously monitors APYs and rebalances your funds to capture the best risk-adjusted returns."
          }
        },
        {
          "@type": "Question",
          "name": "How do withdrawals work?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Nester is fully non-custodial. You can withdraw at any time — your vault shares are burned on-chain and the underlying stablecoins are sent straight back to your own wallet."
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
