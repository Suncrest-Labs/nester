"use client";

import { Moon, Sun } from "lucide-react";
import { useSettings } from "@/context/settings-context";

export function ThemeToggle() {
  const { isDarkMode, setTheme } = useSettings();

  const toggle = () => {
    setTheme(isDarkMode ? "light" : "dark");
  };

  return (
    <button
      onClick={toggle}
      aria-label={isDarkMode ? "Switch to light mode" : "Switch to dark mode"}
      title={isDarkMode ? "Light mode" : "Dark mode"}
      className="flex h-9 w-9 items-center justify-center rounded-full border text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
    >
      {isDarkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </button>
  );
}
