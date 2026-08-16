import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { RecoverableError } from "@/components/recoverable-error";

describe("RecoverableError", () => {
  it("renders the error heading and message", () => {
    const error = new Error("Test error");

    render(
      <RecoverableError
        error={error}
        reset={vi.fn()}
        route="/dashboard"
      />,
    );

    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    expect(
      screen.getByText(/we couldn't load this page/i),
    ).toBeInTheDocument();
  });

  it("renders a 'Try again' button that calls reset", () => {
    const reset = vi.fn();
    const error = new Error("Test error");

    render(
      <RecoverableError
        error={error}
        reset={reset}
        route="/vaults"
      />,
    );

    const tryAgainButton = screen.getByRole("button", { name: /try again/i });
    expect(tryAgainButton).toBeInTheDocument();
    tryAgainButton.click();
    expect(reset).toHaveBeenCalledTimes(1);
  });

  it("renders a 'Dashboard' link", () => {
    const error = new Error("Test error");

    render(
      <RecoverableError
        error={error}
        reset={vi.fn()}
        route="/vaults"
      />,
    );

    const dashboardLink = screen.getByRole("link", { name: /dashboard/i });
    expect(dashboardLink).toBeInTheDocument();
    expect(dashboardLink).toHaveAttribute("href", "/dashboard");
  });

  it("renders a custom heading and message when provided", () => {
    const error = new Error("Custom error");

    render(
      <RecoverableError
        error={error}
        reset={vi.fn()}
        route="/portfolio"
        heading="Custom Error Title"
        message="This is a custom error message for testing."
      />,
    );

    expect(screen.getByText("Custom Error Title")).toBeInTheDocument();
    expect(
      screen.getByText("This is a custom error message for testing."),
    ).toBeInTheDocument();
  });

  it("shows error digest when present", () => {
    const error = new Error("Digest error");
    error.digest = "abc123";

    render(
      <RecoverableError
        error={error}
        reset={vi.fn()}
        route="/"
      />,
    );

    expect(screen.getByText(/Error ID: abc123/i)).toBeInTheDocument();
  });

  it("does not show digest when absent", () => {
    const error = new Error("No digest");

    render(
      <RecoverableError
        error={error}
        reset={vi.fn()}
        route="/"
      />,
    );

    expect(screen.queryByText(/Error ID:/i)).not.toBeInTheDocument();
  });
});