import { Metadata } from "next";
import { Navbar } from "@/components/navbar";
import { Hero } from "@/components/hero";
import { ImageCarousel } from "@/components/image-carousel";
import { Ecosystem } from "@/components/ecosystem";
import { HowItWorks } from "@/components/how-it-works";
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
            "text": "Nester is a non-custodial savings app on Stellar with one product, the Nester Savings Rate. You deposit USDC, receive shares, and the share price rises as Blend and DeFindex pay yield. Nester is live on testnet today; mainnet follows an external security audit."
          }
        },
        {
          "@type": "Question",
          "name": "How does Nester generate yield?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "Deposits are routed through adapter contracts to Blend and DeFindex, within an absolute cap and a percentage cap for each venue. The rate shown is the realized trailing 7-day rate of the share price. It is not guaranteed and it can go down. On testnet, balances and yield have no real value."
          }
        },
        {
          "@type": "Question",
          "name": "How do withdrawals work?",
          "acceptedAnswer": {
            "@type": "Answer",
            "text": "You can withdraw at any time while assets are liquid. Your shares are burned on-chain and USDC is paid at the current share price straight back to your own wallet. No role can block withdrawals; a guardian can only pause new deposits. If a venue loses funds, all savers share the loss pro-rata, partially offset by the fee reserve."
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
      {/* <FeaturesFloat /> */}
      <Ecosystem />
      <HowItWorks />
      <Faq />
      <Footer />
      {/* <Architecture /> */}
    </main>
  );
}
