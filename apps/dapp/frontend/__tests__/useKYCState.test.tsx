import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import { useKYCState } from "@/app/settings/page";

function jsonResponse(data: unknown, ok = true) {
  return {
    ok,
    statusText: ok ? "OK" : "Error",
    json: async () => ({ data }),
  } as Response;
}

describe("useKYCState", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("loads the initial KYC status", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        status: "pending",
        submitted_at: "2026-08-01T00:00:00Z",
      }),
    );

    const { result } = renderHook(() => useKYCState());

    expect(result.current.isLoading).toBe(true);

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(fetchMock).toHaveBeenCalledWith("/api/v1/users/me/kyc");
    expect(result.current.status).toBe("pending");
    expect(result.current.submittedAt).toBe("2026-08-01T00:00:00Z");
    expect(result.current.loadError).toBeNull();
  });

  it("surfaces a load failure and recovers on retry", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockRejectedValueOnce(new Error("network down"));

    const { result } = renderHook(() => useKYCState());

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.loadError).toBe("network down");

    fetchMock.mockResolvedValueOnce(jsonResponse({ status: "unverified" }));
    await act(async () => {
      await result.current.refresh();
    });

    expect(result.current.loadError).toBeNull();
    expect(result.current.status).toBe("unverified");
  });

  it("submits successfully and refreshes the status", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse({ status: "unverified" }));
    const { result } = renderHook(() => useKYCState());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    fetchMock.mockResolvedValueOnce(jsonResponse({ status: "pending" }));
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        status: "pending",
        submitted_at: "2026-08-28T00:00:00Z",
      }),
    );

    const formData = new FormData();
    await act(async () => {
      await result.current.submitKYC(formData);
    });

    expect(fetchMock).toHaveBeenCalledWith("/api/v1/users/me/kyc", {
      method: "POST",
      body: formData,
    });
    expect(result.current.status).toBe("pending");
    expect(result.current.submitError).toBeNull();
    expect(result.current.refreshError).toBeNull();
    expect(result.current.isSubmitting).toBe(false);
  });

  it("does not report a submit error when only the post-submit refresh fails", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse({ status: "unverified" }));
    const { result } = renderHook(() => useKYCState());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    fetchMock.mockResolvedValueOnce(jsonResponse({ status: "pending" }));
    fetchMock.mockRejectedValueOnce(new Error("refresh failed"));

    const formData = new FormData();
    await act(async () => {
      await result.current.submitKYC(formData);
    });

    // Submission itself succeeded — must not be reported as a submit
    // error (that would incorrectly reopen the form).
    expect(result.current.submitError).toBeNull();
    expect(result.current.refreshError).toContain("refresh failed");
    expect(result.current.isSubmitting).toBe(false);
  });

  it("reports a submit error and does not touch refreshError when submission itself fails", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse({ status: "unverified" }));
    const { result } = renderHook(() => useKYCState());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    fetchMock.mockResolvedValueOnce({
      ok: false,
      statusText: "Error",
      json: async () => ({ message: "submit rejected" }),
    } as Response);

    const formData = new FormData();
    await act(async () => {
      await expect(result.current.submitKYC(formData)).rejects.toThrow(
        "submit rejected",
      );
    });

    expect(result.current.submitError).toBe("submit rejected");
    expect(result.current.refreshError).toBeNull();
    expect(result.current.isSubmitting).toBe(false);
    // fetch should only have been called for the initial load and the
    // failed submit — the refresh path is never reached.
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
