import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ThemeToggle } from "@/components/theme-toggle";
import { SettingsProvider } from "@/context/settings-context";

describe("ThemeToggle", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.classList.remove("dark");
  });

  it("renders in the top bar with accessible label", () => {
    render(
      <SettingsProvider>
        <ThemeToggle />
      </SettingsProvider>
    );
    expect(screen.getByRole("button", { name: /switch to light mode/i })).toBeInTheDocument();
  });

  it("toggles theme without page reload", () => {
    render(
      <SettingsProvider>
        <ThemeToggle />
      </SettingsProvider>
    );
    const btn = screen.getByRole("button", { name: /switch to light mode/i });
    fireEvent.click(btn);
    expect(localStorage.getItem("nester-theme")).toBe("light");
    expect(screen.getByRole("button", { name: /switch to dark mode/i })).toBeInTheDocument();
  });
});
