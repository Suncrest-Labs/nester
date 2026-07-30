import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { PrometheusPanel } from "@/components/prometheus-panel";

vi.mock("@/components/portfolio-provider", () => ({
  usePortfolio: () => ({
    positions: [{ vaultName: "USDC Vault", currentValue: 1000 }],
  }),
}));

describe("PrometheusPanel", () => {
  beforeEach(() => {
    vi.stubGlobal("crypto", {
      ...globalThis.crypto,
      randomUUID: () => "test-session-id",
    });
  });

  it("renders message input and send button", () => {
    render(<PrometheusPanel />);
    fireEvent.click(screen.getAllByRole("button")[0]);
    expect(screen.getByPlaceholderText(/type your message/i)).toBeInTheDocument();
  });

  it("sends user message and shows error state when API unavailable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(new Error("network error"))
    );

    render(<PrometheusPanel />);
    fireEvent.click(screen.getAllByRole("button")[0]);

    const input = screen.getByPlaceholderText(/type your message/i);
    fireEvent.change(input, { target: { value: "What is my APY?" } });

    const buttons = screen.getAllByRole("button");
    const sendBtn = buttons[buttons.length - 1];
    fireEvent.click(sendBtn);

    await waitFor(() => {
      expect(screen.getByText("What is my APY?")).toBeInTheDocument();
    });

    await waitFor(() => {
      expect(screen.getByText(/encountered an error/i)).toBeInTheDocument();
    });
  });
});
