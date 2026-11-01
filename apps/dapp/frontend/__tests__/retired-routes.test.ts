import { describe, it, expect } from "vitest";
import nextConfig from "@/next.config";

async function redirectFor(source: string) {
  const redirects = (await nextConfig.redirects?.()) ?? [];
  return redirects.find((r) => r.source === source);
}

describe("retired routes", () => {
  it.each([
    "/vaults",
    "/vaults/:path*",
    "/dashboard/vaults/:path*",
  ])("vaults route redirects: %s -> /dashboard", async (source) => {
    const r = await redirectFor(source);
    expect(r).toMatchObject({ destination: "/dashboard", permanent: false });
  });

  it.each(["/portfolio", "/portfolio/:path*"])(
    "portfolio route redirects: %s -> /dashboard",
    async (source) => {
      const r = await redirectFor(source);
      expect(r).toMatchObject({ destination: "/dashboard", permanent: false });
    }
  );

  it.each(["/stocks", "/stocks/:path*"])(
    "stocks route redirects: %s -> /dashboard",
    async (source) => {
      const r = await redirectFor(source);
      expect(r).toMatchObject({ destination: "/dashboard", permanent: false });
    }
  );

  it.each([
    ["/dashboard/history", "/activity"],
    ["/dashboard/analytics", "/dashboard"],
  ])("moved route redirects: %s -> %s", async (source, destination) => {
    const r = await redirectFor(source);
    expect(r).toMatchObject({ destination, permanent: false });
  });
});
