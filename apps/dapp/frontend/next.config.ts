import type { NextConfig } from "next";
import path from "path";

// Environment variable validation during build
if (!process.env.NEXT_PUBLIC_STELLAR_NETWORK && process.env.NODE_ENV !== "development") {
  console.warn("⚠️ Warning: NEXT_PUBLIC_STELLAR_NETWORK is not defined in environment variables");
}

const nextConfig: NextConfig = {
  reactStrictMode: true,
  turbopack: {
    root: path.resolve(__dirname, "../../"),
  },
  async rewrites() {
    const intelligenceUrl =
      process.env.INTELLIGENCE_SERVICE_URL ?? "http://localhost:8000";
    const apiUrl = process.env.NEXT_PUBLIC_API_URL
      ? process.env.NEXT_PUBLIC_API_URL.replace(/\/api\/v1\/?$/, "")
      : "http://localhost:8080";
    return [
      // Go backend — all /api/v1/* calls
      {
        source: "/api/v1/:path*",
        destination: `${apiUrl}/api/v1/:path*`,
      },
      // Intelligence / AI service
      {
        source: "/api/intelligence/:path*",
        destination: `${intelligenceUrl}/:path*`,
      },
    ];
  },
};

export default nextConfig;
