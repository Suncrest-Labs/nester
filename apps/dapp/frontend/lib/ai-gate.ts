export interface NextRewriteRule {
  source: string;
  destination: string;
}

/**
 * Returns true only when NEXT_PUBLIC_ENABLE_AI is explicitly set to "true"
 * (#1513). Defaults to false on Mainnet and all builds where unset.
 */
export function isAiEnabled(
  env: Record<string, string | undefined> = typeof process !== "undefined"
    ? process.env
    : {},
): boolean {
  return env?.NEXT_PUBLIC_ENABLE_AI === "true";
}

/**
 * Build the Next.js rewrite table.
 *
 * `/api/intelligence/:path*` is only included when `NEXT_PUBLIC_ENABLE_AI === "true"`
 * AND `INTELLIGENCE_SERVICE_URL` is explicitly configured (never falling back to
 * `http://localhost:8000` in production/mainnet builds) (#1513).
 */
export function buildNextRewrites(
  env: Record<string, string | undefined> = typeof process !== "undefined"
    ? process.env
    : {},
): NextRewriteRule[] {
  const apiUrl = env?.NEXT_PUBLIC_API_URL
    ? env.NEXT_PUBLIC_API_URL.replace(/\/api\/v1\/?$/, "")
    : "http://localhost:8080";

  const rules: NextRewriteRule[] = [
    {
      source: "/api/v1/:path*",
      destination: `${apiUrl}/api/v1/:path*`,
    },
  ];

  const intelligenceUrl = env?.INTELLIGENCE_SERVICE_URL?.trim();
  if (isAiEnabled(env) && intelligenceUrl) {
    rules.push({
      source: "/api/intelligence/:path*",
      destination: `${intelligenceUrl.replace(/\/+$/, "")}/:path*`,
    });
  }

  return rules;
}
