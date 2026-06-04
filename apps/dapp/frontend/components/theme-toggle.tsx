"use client";

import { Moon, Sun } from "lucide-react";
import { useSettings } from "@/context/settings-context";
import { cn } from "@/lib/utils";

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
      className={cn(
        "flex h-9 w-9 items-center justify-center rounded-xl border transition-colors",
        "border-black/[0.08] dark:border-white/10",
        "text-black/50 dark:text-white/60",
        "hover:border-black/20 hover:text-black dark:hover:border-white/20 dark:hover:text-white",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
      )}
    >
      {isDarkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </button>
  );
}
