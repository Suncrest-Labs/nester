"use client";

import { notFound } from "next/navigation";

/**
 * Diagnostics route that throws during render, used to verify that the error
 * boundaries actually catch a render-time exception rather than only a failed
 * fetch. It 404s in production so it can never be reached by a real user.
 */
export default function RenderErrorDiagnostic() {
  if (process.env.NODE_ENV === "production") notFound();

  throw new Error("Diagnostic render failure");
}
