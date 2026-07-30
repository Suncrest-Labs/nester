import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { CookieConsentBanner } from "@/components/cookie-consent-banner";
import { ConsentProvider } from "@/context/consent-context";
import { CONSENT_STORAGE_KEY } from "@/lib/consent";

describe("CookieConsentBanner", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("appears on first visit with proper ARIA labels", () => {
    render(
      <ConsentProvider>
        <CookieConsentBanner />
      </ConsentProvider>
    );
    expect(screen.getByRole("dialog", { name: /we value your privacy/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/dismiss cookie banner/i)).toBeInTheDocument();
  });

  it("persists consent to localStorage on accept", async () => {
    render(
      <ConsentProvider>
        <CookieConsentBanner />
      </ConsentProvider>
    );
    fireEvent.click(screen.getByRole("button", { name: /accept all/i }));
    await waitFor(() => {
      const stored = JSON.parse(localStorage.getItem(CONSENT_STORAGE_KEY)!);
      expect(stored.decided).toBe(true);
      expect(stored.analytics).toBe(true);
    });
  });

  it("dismisses with Escape key", async () => {
    render(
      <ConsentProvider>
        <CookieConsentBanner />
      </ConsentProvider>
    );
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: /we value your privacy/i })).not.toBeInTheDocument();
    });
  });
});
