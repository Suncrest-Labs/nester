import { z } from "zod";

/**
 * Runtime schemas mirroring the TypeScript `interface` response shapes in
 * this directory (Issue #1130). A hand-written TS interface only checks
 * shape at compile time against whatever the developer typed — it says
 * nothing about whether the real backend still returns that shape. These
 * schemas are the first piece a contract-test runner needs: validate a
 * captured (or live) real response against the schema and fail loudly on
 * drift, instead of a hand-written fixture silently diverging from reality.
 *
 * This file currently covers one endpoint (`GET /vaults/:id/apy-history`)
 * as a concrete starting point. Extending to every core screen, wiring a
 * CI step that runs these against live/recorded real responses, and
 * generating this file's schemas from the OpenAPI spec (#1058) instead of
 * by hand are the real remaining scope of #1130.
 */

export const apyHistoryPointSchema = z.object({
  timestamp: z.string(),
  apy: z.number(),
});

export const apyHistoryResponseSchema = z.object({
  vault_id: z.string(),
  period: z.enum(["7d", "30d", "90d"]),
  points: z.array(apyHistoryPointSchema),
});

/**
 * Validate `data` against `apyHistoryResponseSchema`, throwing a
 * descriptive error on mismatch instead of letting a shape drift surface
 * later as a confusing runtime bug in a chart or calculation.
 */
export function assertApyHistoryResponseShape(data: unknown): void {
  const result = apyHistoryResponseSchema.safeParse(data);
  if (!result.success) {
    throw new Error(
      `APYHistoryResponse shape mismatch: ${formatIssues(result.error.issues)}`
    );
  }
}

/**
 * Render each issue as `path: message`. The path is the useful half when a
 * backend response drifts — "expected string, received undefined" alone does
 * not say which field went missing.
 */
function formatIssues(issues: z.core.$ZodIssue[]): string {
  return issues
    .map((issue) => {
      const path = issue.path.join(".");
      return path ? `${path}: ${issue.message}` : issue.message;
    })
    .join(", ");
}
