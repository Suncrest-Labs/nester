"use client";

import { useConsent } from "@/context/consent-context";

export function SiteFooter() {
  const { openPreferences } = useConsent();

  return (
    <footer className="border-t border-border bg-background/80 px-6 py-4 lg:px-10">
      <div className="mx-auto flex max-w-[1120px] flex-col items-center justify-between gap-2 sm:flex-row">
        <p className="text-xs text-muted-foreground">
          © {new Date().getFullYear()} Nester Finance. All rights reserved.
        </p>
        <button
          onClick={openPreferences}
          className="text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline transition-colors"
        >
          Manage Preferences
        </button>
      </div>
    </footer>
  );
}
