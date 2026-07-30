import React from "react";
import { render, type RenderOptions } from "@testing-library/react";
import { SettingsProvider } from "@/context/settings-context";
import { ConsentProvider } from "@/context/consent-context";

function AllProviders({ children }: { children: React.ReactNode }) {
  return (
    <ConsentProvider>
      <SettingsProvider>{children}</SettingsProvider>
    </ConsentProvider>
  );
}

export function renderWithProviders(ui: React.ReactElement, options?: RenderOptions) {
  return render(ui, { wrapper: AllProviders, ...options });
}
