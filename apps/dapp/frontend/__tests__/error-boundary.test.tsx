import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import {
  ErrorBoundary,
  WidgetErrorBoundary,
} from "@/components/ui/error-boundary/error-boundary";
import { resetErrorReportDedupe } from "@/lib/observability/report-error";

const reportError = vi.fn();
vi.mock("@/lib/observability/report-error", async () => {
  const actual = await vi.importActual<
    typeof import("@/lib/observability/report-error")
  >("@/lib/observability/report-error");
  return {
    ...actual,
    reportError: (...args: Parameters<typeof actual.reportError>) =>
      reportError(...args),
  };
});

function Boom(): never {
  throw new Error("component exploded");
}

describe("ErrorBoundary", () => {
  beforeEach(() => {
    resetErrorReportDedupe();
    reportError.mockClear();
    // React logs the caught error to console.error too; keep test output clean.
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("reports the caught error to the observability pipeline by default", () => {
    render(
      <ErrorBoundary level="page">
        <Boom />
      </ErrorBoundary>
    );

    expect(reportError).toHaveBeenCalledTimes(1);
    const [input] = reportError.mock.calls[0];
    expect(input.error).toBeInstanceOf(Error);
    expect(input.error.message).toBe("component exploded");
    expect(input.boundary).toBe("page");
  });

  it("uses 'widget' as the boundary name for a widget-level boundary", () => {
    render(
      <WidgetErrorBoundary>
        <Boom />
      </WidgetErrorBoundary>
    );

    expect(reportError).toHaveBeenCalledTimes(1);
    expect(reportError.mock.calls[0][0].boundary).toBe("widget");
  });

  it("still invokes an explicit onError handler in addition to the default reporting", () => {
    const onError = vi.fn();
    render(
      <ErrorBoundary level="widget" onError={onError}>
        <Boom />
      </ErrorBoundary>
    );

    expect(reportError).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError.mock.calls[0][0]).toBeInstanceOf(Error);
  });

  it("renders the fallback UI instead of crashing the tree", () => {
    render(
      <ErrorBoundary level="page">
        <Boom />
      </ErrorBoundary>
    );

    expect(screen.getByRole("heading", { name: /unexpected error/i })).toBeInTheDocument();
  });
});
