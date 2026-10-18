import { describe, it, expect } from "vitest";
import { buildCspHeader, getSecurityHeaders } from "@/middleware";

describe("Content-Security-Policy and security headers (#1504)", () => {
  it("builds strict CSP with nonce and testnet connect-src hosts", () => {
    const csp = buildCspHeader({
      nonce: "test-nonce-123",
      network: "testnet",
      apiUrl: "https://api-testnet.nester.fi/api/v1",
      sentryDsn: "",
      isDev: false,
    });

    expect(csp).toContain("default-src 'self'");
    expect(csp).toContain("script-src 'self' 'nonce-test-nonce-123' 'strict-dynamic'");
    expect(csp).not.toContain("'unsafe-eval'");
    expect(csp).toContain("frame-ancestors 'none'");
    expect(csp).toContain("object-src 'none'");
    expect(csp).toContain("base-uri 'self'");

    // Testnet RPC + Horizon + API
    expect(csp).toContain("https://soroban-testnet.stellar.org");
    expect(csp).toContain("https://horizon-testnet.stellar.org");
    expect(csp).toContain("https://api-testnet.nester.fi");
    expect(csp).toContain("wss://api-testnet.nester.fi");
    expect(csp).not.toContain("https://soroban-rpc.mainnet.stellar.org");
    expect(csp).not.toContain("https://horizon.stellar.org");
  });

  it("uses mainnet RPC and Horizon hosts when network is mainnet", () => {
    const csp = buildCspHeader({
      nonce: "mainnet-nonce-456",
      network: "mainnet",
      apiUrl: "https://api.nester.fi/api/v1",
      sentryDsn: "https://public@o12345.ingest.sentry.io/67890",
      isDev: false,
    });

    expect(csp).toContain("https://soroban-rpc.mainnet.stellar.org");
    expect(csp).toContain("https://horizon.stellar.org");
    expect(csp).toContain("https://api.nester.fi");
    expect(csp).toContain("https://o12345.ingest.sentry.io");
    expect(csp).not.toContain("https://soroban-testnet.stellar.org");
    expect(csp).not.toContain("https://horizon-testnet.stellar.org");
  });

  it("includes HSTS, X-Content-Type-Options, Referrer-Policy, and Permissions-Policy", () => {
    const headers = getSecurityHeaders({
      nonce: "nonce-xyz",
      network: "testnet",
      isDev: false,
    });

    expect(headers["Content-Security-Policy"]).toContain("default-src 'self'");
    expect(headers["Strict-Transport-Security"]).toBe(
      "max-age=63072000; includeSubDomains; preload"
    );
    expect(headers["X-Content-Type-Options"]).toBe("nosniff");
    expect(headers["Referrer-Policy"]).toBe("strict-origin-when-cross-origin");
    expect(headers["Permissions-Policy"]).toContain("camera=()");
    expect(headers["X-Frame-Options"]).toBe("DENY");
  });
});
