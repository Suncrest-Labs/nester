import type { NextConfig } from "next";

// Environment variable validation during build
if (!process.env.NEXT_PUBLIC_STELLAR_NETWORK && process.env.NODE_ENV !== "development") {
  console.warn("⚠️ Warning: NEXT_PUBLIC_STELLAR_NETWORK is not defined in environment variables");
}

const nextConfig: NextConfig = {
  reactStrictMode: true,
};

export default nextConfig;
