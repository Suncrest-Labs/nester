import { Navbar } from "@/components/navbar";
import { Hero } from "@/components/hero";
import { ImageCarousel } from "@/components/image-carousel";
import { Ecosystem } from "@/components/ecosystem";
import { AiLayer } from "@/components/ai-layer";
import { HowItWorks } from "@/components/how-it-works";
import { UseCases } from "@/components/use-cases";
import { Faq } from "@/components/faq";
import { Footer } from "@/components/footer";
// import { FeaturesFloat } from "@/components/features-float";
// import { Architecture } from "@/components/architecture";

export default function Home() {
  return (
    <main className="min-h-screen bg-background text-foreground overflow-hidden">
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
      <AiLayer />
      <HowItWorks />
      <Faq />
      <Footer />
      {/* <Architecture /> */}
    </main>
  );
}
