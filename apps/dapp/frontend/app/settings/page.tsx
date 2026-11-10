"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ExternalLink } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { SessionsSection } from "@/components/settings/sessions-section";
import {
  BUTTON_SECONDARY,
  INPUT,
  PageHeader,
  Panel,
  Section,
  SegmentedControl,
} from "@/components/ui/page-kit";
import { useNetwork } from "@/hooks/useNetwork";
import { useSettings, type Theme } from "@/context/settings-context";
import { useLocale, useTranslations } from "@/context/locale-context";
import { SUPPORTED_LOCALES, LOCALE_LABELS, type Locale } from "@/lib/i18n/config";
import { config } from "@/lib/config";
import { cn } from "@/lib/utils";

const THEMES: { id: Theme; label: string }[] = [
  { id: "light", label: "Light" },
  { id: "dark", label: "Dark" },
  { id: "system", label: "System" },
];

function WalletPanel({ address, onDisconnect }: { address: string; onDisconnect: () => void }) {
  const { currentNetwork } = useNetwork();
  const [copied, setCopied] = useState(false);

  const copy = () => {
    navigator.clipboard.writeText(address);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <Panel>
      <p className="break-all font-heading text-xl font-light text-foreground sm:text-2xl">{address}</p>
      <dl className="mt-5 text-sm">
        <dt className="text-muted-foreground">Network</dt>
        <dd className="mt-0.5 text-foreground">{currentNetwork.name}</dd>
      </dl>
      <div className="mt-6 flex flex-wrap gap-3">
        <button type="button" onClick={copy} className={BUTTON_SECONDARY}>
          {copied ? "Copied" : "Copy address"}
        </button>
        <a
          href={`${config.explorerUrl}/account/${address}`}
          target="_blank"
          rel="noopener noreferrer"
          className={BUTTON_SECONDARY}
        >
          View on explorer
          <ExternalLink className="h-3 w-3" />
        </a>
        <button
          type="button"
          onClick={onDisconnect}
          className={cn(BUTTON_SECONDARY, "text-red-600 dark:text-red-400")}
        >
          Disconnect
        </button>
      </div>
    </Panel>
  );
}

export default function SettingsPage() {
  const { isConnected, address, disconnect } = useWallet();
  const router = useRouter();
  const { theme, setTheme } = useSettings();
  const { locale, setLocale } = useLocale();
  const t = useTranslations();

  useEffect(() => {
    if (!isConnected) router.push("/");
  }, [isConnected, router]);

  if (!isConnected || !address) return null;

  return (
    <AppShell>
      <PageHeader title="Settings" />

      <Section id="settings-wallet" title="Wallet" className="mt-8">
        <WalletPanel address={address} onDisconnect={() => disconnect()} />
      </Section>

      <Section id="settings-appearance" title="Appearance" lede="System follows your device setting.">
        <Panel>
          <SegmentedControl label="Theme" options={THEMES} value={theme} onChange={setTheme} />
        </Panel>
      </Section>

      <Section id="settings-language" title={t("settings.language")}>
        <Panel>
          <label htmlFor="locale-select" className="sr-only">
            {t("settings.language")}
          </label>
          <select
            id="locale-select"
            value={locale}
            onChange={(e) => setLocale(e.target.value as Locale)}
            className={cn(INPUT, "max-w-sm")}
          >
            {SUPPORTED_LOCALES.map((l) => (
              <option key={l} value={l}>
                {LOCALE_LABELS[l]}
              </option>
            ))}
          </select>
          <p className="mt-2 text-sm text-muted-foreground">{t("settings.languageDescription")}</p>
        </Panel>
      </Section>

      <Section id="settings-sessions" title="Sessions">
        <SessionsSection />
      </Section>
    </AppShell>
  );
}
