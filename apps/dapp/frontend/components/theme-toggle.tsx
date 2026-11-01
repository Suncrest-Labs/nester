"use client";

import { Moon, Sun } from "lucide-react";
import { useSettings } from "@/context/settings-context";
import { cn } from "@/lib/utils";

export function ThemeToggle() {
  const { isDarkMode, setTheme } = useSettings();

  return (
    <button
      type="button"
      role="switch"
      aria-checked={isDarkMode}
      onClick={() => setTheme(isDarkMode ? "light" : "dark")}
      aria-label={isDarkMode ? "Switch to light mode" : "Switch to dark mode"}
      title={isDarkMode ? "Light mode" : "Dark mode"}
      className="relative inline-flex h-8 w-[60px] shrink-0 items-center rounded-full border bg-muted p-[3px] transition-colors duration-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground focus-visible:ring-offset-2 focus-visible:ring-offset-background motion-reduce:transition-none"
    >
      <Sun
        aria-hidden
        className={cn(
          "absolute left-2 h-3.5 w-3.5 text-muted-foreground transition-opacity duration-300",
          isDarkMode ? "opacity-60" : "opacity-0"
        )}
      />
      <Moon
        aria-hidden
        className={cn(
          "absolute right-2 h-3.5 w-3.5 text-muted-foreground transition-opacity duration-300",
          isDarkMode ? "opacity-0" : "opacity-60"
        )}
      />
      <span
        aria-hidden
        className={cn(
          "relative z-10 flex h-6 w-6 items-center justify-center rounded-full bg-card text-foreground shadow-[0_1px_2px_rgba(0,0,0,0.12),0_0_0_1px_hsl(var(--border))] transition-transform duration-300 ease-[cubic-bezier(0.34,1.4,0.64,1)] motion-reduce:transition-none",
          isDarkMode ? "translate-x-[28px]" : "translate-x-0"
        )}
      >
        <Sun
          className={cn(
            "absolute h-3.5 w-3.5 transition-all duration-300 motion-reduce:transition-none",
            isDarkMode ? "-rotate-90 scale-50 opacity-0" : "rotate-0 scale-100 opacity-100"
          )}
        />
        <Moon
          className={cn(
            "absolute h-3.5 w-3.5 transition-all duration-300 motion-reduce:transition-none",
            isDarkMode ? "rotate-0 scale-100 opacity-100" : "rotate-90 scale-50 opacity-0"
          )}
        />
      </span>
    </button>
  );
}
