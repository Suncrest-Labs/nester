import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiRequest, ApiError } from "@/lib/api/client";
import { setAccessToken, getAccessToken } from "@/lib/auth/token-store";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("apiFetch transparent refresh", () => {
  beforeEach(() => {
    window.localStorage.clear();
    setAccessToken("expired-access-token");
  });

  it("refreshes once on 401 and retries the original request", async () => {
    let refreshCalls = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();
      const auth = (init?.headers as Record<string, string> | undefined)?.["Authorization"];

      if (url.endsWith("/auth/refresh")) {
        refreshCalls++;
        return jsonResponse(200, {
          success: true,
          data: { access_token: "new-access-token", expires_in: 300, token_type: "Bearer" },
        });
      }

      if (auth === "Bearer expired-access-token") {
        return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "token expired" } });
      }

      if (auth === "Bearer new-access-token") {
        return jsonResponse(200, { success: true, data: { ok: true } });
      }

      return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "unexpected auth header" } });
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await apiRequest<{ ok: boolean }>("/vaults/123");

    expect(result).toEqual({ ok: true });
    expect(refreshCalls).toBe(1);
    expect(getAccessToken()).toBe("new-access-token");
    // The refresh call must rely on the httpOnly cookie, not a JS-readable
    // token — the client never sends a refresh_token field.
    const refreshCall = fetchMock.mock.calls.find(([input]) =>
      (typeof input === "string" ? input : input.toString()).endsWith("/auth/refresh")
    );
    expect(refreshCall?.[1]).toMatchObject({ credentials: "include" });
    expect(JSON.parse((refreshCall?.[1] as RequestInit).body as string)).not.toHaveProperty("refresh_token");

    vi.unstubAllGlobals();
  });

  it("shares a single in-flight refresh across concurrent 401s", async () => {
    let refreshCalls = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();
      const auth = (init?.headers as Record<string, string> | undefined)?.["Authorization"];

      if (url.endsWith("/auth/refresh")) {
        refreshCalls++;
        // Simulate network latency so both callers' 401s land before either
        // refresh resolves — the scenario the single-flight guard exists for.
        await new Promise((r) => setTimeout(r, 10));
        return jsonResponse(200, {
          success: true,
          data: { access_token: "new-access-token", expires_in: 300, token_type: "Bearer" },
        });
      }

      if (auth === "Bearer expired-access-token") {
        return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "token expired" } });
      }

      return jsonResponse(200, { success: true, data: { ok: true } });
    });
    vi.stubGlobal("fetch", fetchMock);

    const [a, b] = await Promise.all([
      apiRequest<{ ok: boolean }>("/vaults/1"),
      apiRequest<{ ok: boolean }>("/vaults/2"),
    ]);

    expect(a).toEqual({ ok: true });
    expect(b).toEqual({ ok: true });
    expect(refreshCalls).toBe(1);

    vi.unstubAllGlobals();
  });

  it("clears the local session when the refresh cookie itself is rejected (401/403)", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/auth/refresh")) {
        return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "refresh token is invalid or expired" } });
      }
      return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "token expired" } });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiRequest("/vaults/123")).rejects.toBeInstanceOf(ApiError);
    expect(getAccessToken()).toBe("");

    vi.unstubAllGlobals();
  });

  it("preserves the local session on a transient refresh failure (5xx)", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/auth/refresh")) {
        return jsonResponse(503, { success: false, error: { code: "UNAVAILABLE", message: "database unreachable" } });
      }
      return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "token expired" } });
    });
    vi.stubGlobal("fetch", fetchMock);

    let caught: unknown;
    try {
      await apiRequest("/vaults/123");
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(ApiError);
    expect((caught as ApiError).status).toBe(503);
    // Unlike a genuine 401/403 rejection, a transient server error must not
    // wipe the still-possibly-valid session.
    expect(getAccessToken()).toBe("expired-access-token");

    vi.unstubAllGlobals();
  });

  it("preserves the local session when the refresh request throws (network error)", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/auth/refresh")) {
        throw new TypeError("Failed to fetch");
      }
      return jsonResponse(401, { success: false, error: { code: "UNAUTHORIZED", message: "token expired" } });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(apiRequest("/vaults/123")).rejects.toBeInstanceOf(ApiError);
    expect(getAccessToken()).toBe("expired-access-token");

    vi.unstubAllGlobals();
  });
});
