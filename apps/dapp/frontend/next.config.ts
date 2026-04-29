import type { NextConfig } from "next";

// Environment variable validation during build
if (!process.env.NEXT_PUBLIC_STELLAR_NETWORK && process.env.NODE_ENV !== "development") {
  console.warn("⚠️ Warning: NEXT_PUBLIC_STELLAR_NETWORK is not defined in environment variables");
}

const nextConfig: NextConfig = {
  reactStrictMode: true,
  async rewrites() {
    const intelligenceUrl =
      process.env.INTELLIGENCE_SERVICE_URL ?? "http://localhost:8000";
    return [
      {
        source: "/api/v1/:path*",
        destination: `${intelligenceUrl}/:path*`,
      },
    ];
  },
};

export default nextConfig;
