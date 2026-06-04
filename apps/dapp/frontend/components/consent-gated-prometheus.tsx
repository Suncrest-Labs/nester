"use client";

import { PrometheusChatbot } from "@/components/ai/prometheusChatbot";
import { useConsent } from "@/context/consent-context";

/** Loads Prometheus chatbot only after analytics consent is granted. */
export function ConsentGatedPrometheus() {
  const { consent } = useConsent();
  if (!consent.analytics) return null;
  return <PrometheusChatbot />;
}
