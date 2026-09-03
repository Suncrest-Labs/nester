import React from "react";
import { render, type RenderOptions } from "@testing-library/react";
import { SettingsProvider } from "@/context/settings-context";

function AllProviders({ children }: { children: React.ReactNode }) {
  return (
    <SettingsProvider>{children}</SettingsProvider>
  );
}

export function renderWithProviders(ui: React.ReactElement, options?: RenderOptions) {
  return render(ui, { wrapper: AllProviders, ...options });
}
