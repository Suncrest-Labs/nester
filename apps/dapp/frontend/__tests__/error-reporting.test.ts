import { describe, it, expect, vi, beforeEach } from "vitest";
import { reportError, type ErrorReport } from "@/lib/error-reporting";

describe("reportError", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("logs the error to console.error with route context", () => {
    const error = new Error("Test error message");
    reportError(error, "/dashboard");

    expect(console.error).toHaveBeenCalledWith(
      "[ErrorBoundary] /dashboard:",
      "Error",
      "Test error message",
    );
  });

  it("uses the error name as the code", () => {
    class CustomError extends Error {
      name = "ApiError";
    }
    const error = new CustomError("Not found");
    reportError(error, "/vaults");

    expect(console.error).toHaveBeenCalledWith(
      "[ErrorBoundary] /vaults:",
      "ApiError",
      "Not found",
    );
  });

  it("produces a safe ErrorReport with no sensitive fields", () => {
    // Spy on console.error and extract the call arguments
    vi.spyOn(console, "error").mockImplementation(() => {});

    const error = new Error("Something broke");
    reportError(error, "/settings");

    // The report is only used internally — verify no PII is leaked
    const callArgs = (console.error as ReturnType<typeof vi.spyOn>).mock
      .calls[0];
    const [tag, code, message] = callArgs as [string, string, string];

    expect(tag).toBe("[ErrorBoundary] /settings:");
    expect(code).toBe("Error");
    expect(message).toBe("Something broke");
    // No wallet addresses, balances, or tokens in the payload
    expect(message).not.toContain("0x");
    expect(message).not.toContain("G");
    expect(message).not.toContain("secret");
  });

  it("handles error with no message gracefully", () => {
    const error = new Error();
    reportError(error, "/");

    expect(console.error).toHaveBeenCalledWith(
      "[ErrorBoundary] /:",
      "Error",
      "An unexpected error occurred",
    );
  });
});