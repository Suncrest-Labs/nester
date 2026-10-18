import { NextResponse, type NextRequest } from "next/server";

export interface CspBuildOptions {
  nonce: string;
  network?: string;
  apiUrl?: string;
  rpcUrl?: string;
  horizonUrl?: string;
  sentryDsn?: string;
  isDev?: boolean;
}

const TESTNET_HOSTS = [
  "https://soroban-testnet.stellar.org",
  "https://horizon-testnet.stellar.org",
  "https://friendbot.stellar.org",
];

const MAINNET_HOSTS = [
  "https://soroban-rpc.mainnet.stellar.org",
  "https://horizon.stellar.org",
];

function extractOrigin(rawUrl: string | undefined): string | null {
  if (!rawUrl) return null;
  try {
    const parsed = new URL(rawUrl.trim());
    return parsed.origin;
  } catch {
    return null;
  }
}

function toWebSocketOrigin(httpOrigin: string): string | null {
  if (httpOrigin.startsWith("https://")) {
    return `wss://${httpOrigin.slice("https://".length)}`;
  }
  if (httpOrigin.startsWith("http://")) {
    return `ws://${httpOrigin.slice("http://".length)}`;
  }
  return null;
}

/**
 * Build the strict Content-Security-Policy header with a per-request nonce
 * and network-scoped `connect-src` hosts (#1504).
 */
export function buildCspHeader(options: CspBuildOptions): string {
  const {
    nonce,
    network = process.env.NEXT_PUBLIC_STELLAR_NETWORK ||
      process.env.NEXT_PUBLIC_NETWORK ||
      "testnet",
    apiUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080",
    rpcUrl = process.env.NEXT_PUBLIC_STELLAR_RPC_URL,
    horizonUrl = process.env.NEXT_PUBLIC_HORIZON_URL,
    sentryDsn = process.env.NEXT_PUBLIC_SENTRY_DSN || process.env.SENTRY_DSN,
    isDev = process.env.NODE_ENV === "development",
  } = options;

  const isMainnet = network.toLowerCase() === "mainnet";
  const connectSources = new Set<string>(["'self'"]);

  for (const host of isMainnet ? MAINNET_HOSTS : TESTNET_HOSTS) {
    connectSources.add(host);
  }

  const apiOrigin = extractOrigin(apiUrl);
  if (apiOrigin) {
    connectSources.add(apiOrigin);
    const wsOrigin = toWebSocketOrigin(apiOrigin);
    if (wsOrigin) connectSources.add(wsOrigin);
  }

  const customRpcOrigin = extractOrigin(rpcUrl);
  if (customRpcOrigin) connectSources.add(customRpcOrigin);

  const customHorizonOrigin = extractOrigin(horizonUrl);
  if (customHorizonOrigin) connectSources.add(customHorizonOrigin);

  const sentryOrigin = extractOrigin(sentryDsn);
  if (sentryOrigin) {
    connectSources.add(sentryOrigin);
  }

  const scriptSrc = [
    "'self'",
    `'nonce-${nonce}'`,
    "'strict-dynamic'",
    ...(isDev ? ["'unsafe-eval'"] : []),
  ].join(" ");

  const directives = [
    "default-src 'self'",
    `script-src ${scriptSrc}`,
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' blob: data: https:",
    "font-src 'self' data:",
    `connect-src ${Array.from(connectSources).join(" ")}`,
    "frame-ancestors 'none'",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
  ];

  return directives.join("; ");
}

export function getSecurityHeaders(options: CspBuildOptions): Record<string, string> {
  return {
    "Content-Security-Policy": buildCspHeader(options),
    "Strict-Transport-Security": "max-age=63072000; includeSubDomains; preload",
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "strict-origin-when-cross-origin",
    "Permissions-Policy": "camera=(), microphone=(), geolocation=(), browsing-topics=()",
    "X-Frame-Options": "DENY",
  };
}

function generateNonce(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  let binary = "";
  for (const b of bytes) {
    binary += String.fromCharCode(b);
  }
  return btoa(binary);
}

export function middleware(request: NextRequest) {
  const nonce = generateNonce();
  const headers = getSecurityHeaders({ nonce });

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", headers["Content-Security-Policy"]);

  const response = NextResponse.next({
    request: {
      headers: requestHeaders,
    },
  });

  for (const [key, value] of Object.entries(headers)) {
    response.headers.set(key, value);
  }
  response.headers.set("x-nonce", nonce);

  return response;
}

export const config = {
  matcher: [
    {
      source: "/((?!_next/static|_next/image|favicon.ico).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
