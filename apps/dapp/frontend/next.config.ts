import type { NextConfig } from "next";
import path from "path";
import { withSentryConfig } from "@sentry/nextjs/config";
import { buildNextRewrites } from "./lib/ai-gate";

// Environment variable validation during build
if (!process.env.NEXT_PUBLIC_STELLAR_NETWORK && process.env.NODE_ENV !== "development") {
  console.warn("⚠️ Warning: NEXT_PUBLIC_STELLAR_NETWORK is not defined in environment variables");
}

const nextConfig: NextConfig = {
  reactStrictMode: true,
  turbopack: {
    // Monorepo root (where pnpm-lock.yaml lives) so Turbopack resolves the
    // workspace correctly under pnpm's hoisted node_modules.
    root: path.resolve(__dirname, "../../../"),
  },
  async rewrites() {
    return buildNextRewrites(process.env);
  },
};

// Sentry is opt-in end to end (nester#791): wrapping withSentryConfig only
// happens when a DSN is present, so a build with no Sentry env vars produces
// byte-identical output to nextConfig above — no source-map upload attempt,
// no build-time warnings, nothing for local dev or a Sentry-less CI run to
// notice.
const sentryDsnConfigured = Boolean(
  process.env.NEXT_PUBLIC_SENTRY_DSN || process.env.SENTRY_DSN
);

export default sentryDsnConfigured
  ? withSentryConfig(nextConfig, {
      org: process.env.SENTRY_ORG,
      project: process.env.SENTRY_PROJECT,
      // No SENTRY_AUTH_TOKEN in most environments (it's a CI/release secret,
      // not something local dev or every deploy target sets) — the plugin
      // skips source-map upload gracefully without one, so this is left
      // unset here rather than hard-required.
      authToken: process.env.SENTRY_AUTH_TOKEN,
      silent: true,
      widenClientFileUpload: true,
      telemetry: false,
    })
  : nextConfig;
