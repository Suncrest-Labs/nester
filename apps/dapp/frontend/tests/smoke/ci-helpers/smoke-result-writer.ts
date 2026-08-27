/**
 * smoke-result-writer.ts
 *
 * Generates machine-parsable smoke test result artifacts (smoke-result.json)
 * for CI pipeline failure reporting and observability.
 *
 * Each step is recorded with:
 * - name: canonical step identifier (register, connect-wallet, deposit, etc.)
 * - status: PASS or FAIL
 * - message: human-readable context or error
 * - durationMs: milliseconds to complete the step
 * - txHash: optional Stellar transaction hash for deposit/withdraw steps
 */

export interface SmokeStep {
  name: string;
  status: "PASS" | "FAIL";
  message: string;
  durationMs: number;
  txHash?: string;
}

export interface SmokeResult {
  runId: string;
  startedAt: string;
  completedAt: string;
  steps: SmokeStep[];
  summary: {
    passed: number;
    failed: number;
    durationMs: number;
  };
}

/**
 * Generate a unique run ID for this smoke test execution.
 */
function generateRunId(): string {
  return `smoke-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

/**
 * Generate smoke test result artifact.
 *
 * @param steps - Array of recorded smoke test steps
 * @param startTimeMs - Test start time (from Date.now())
 * @returns SmokeResult with structured summary and artifacts
 */
export function generateSmokeTestResult(steps: SmokeStep[], startTimeMs: number): SmokeResult {
  const endTimeMs = Date.now();
  const durationMs = endTimeMs - startTimeMs;

  const passed = steps.filter((s) => s.status === "PASS").length;
  const failed = steps.filter((s) => s.status === "FAIL").length;

  const result: SmokeResult = {
    runId: generateRunId(),
    startedAt: new Date(startTimeMs).toISOString(),
    completedAt: new Date(endTimeMs).toISOString(),
    steps,
    summary: {
      passed,
      failed,
      durationMs,
    },
  };

  return result;
}

/**
 * Format smoke test result for workflow summary output.
 * Returns a one-line summary suitable for GitHub Actions job summary.
 *
 * Example: "SMOKE: PASS (6/6 steps, 125s)"
 * Example: "SMOKE: FAIL at deposit (5/6 steps, 85s)"
 */
export function formatSmokeTestSummary(result: SmokeResult): string {
  if (result.summary.failed === 0) {
    const steps = result.steps.length;
    const seconds = Math.round(result.summary.durationMs / 1000);
    return `SMOKE: PASS (${steps}/${steps} steps, ${seconds}s)`;
  }

  const failedStep = result.steps.find((s) => s.status === "FAIL");
  const totalSteps = result.steps.length;
  const passedSteps = result.summary.passed;
  const seconds = Math.round(result.summary.durationMs / 1000);

  return `SMOKE: FAIL at ${failedStep?.name || "unknown"} (${passedSteps}/${totalSteps} steps, ${seconds}s)`;
}

/**
 * Write smoke test result to stdout in formats suitable for CI pipelines.
 * 
 * This function outputs:
 * 1. Machine-parsable JSON (can be captured and parsed by CI scripts)
 * 2. Human-readable summary line
 * 3. Per-step status lines
 */
export function reportSmokeTestResult(result: SmokeResult): void {
  const summary = formatSmokeTestSummary(result);

  // Human-readable summary
  console.log("\n" + "=".repeat(60));
  console.log(summary);
  console.log("=".repeat(60));

  // Per-step details
  console.log("\nSmoke Test Results:");
  result.steps.forEach((step) => {
    const icon = step.status === "PASS" ? "✓" : "✗";
    const duration = `${step.durationMs}ms`;
    const txInfo = step.txHash ? ` [${step.txHash.slice(0, 8)}...]` : "";
    console.log(`  ${icon} ${step.name.padEnd(16)} ${step.status.padEnd(4)} ${duration.padEnd(8)} ${step.message}${txInfo}`);
  });

  // Run metrics
  console.log(`\nRun ID: ${result.runId}`);
  console.log(`Started: ${result.startedAt}`);
  console.log(`Completed: ${result.completedAt}`);
  console.log(`Total Duration: ${result.summary.durationMs}ms`);

  // Machine-parsable JSON output (can be captured to file)
  console.log("\nMachine-Parsable Result:");
  console.log(JSON.stringify(result, null, 2));
}

/**
 * Check if smoke test result indicates success (all steps passed).
 */
export function isSmokeTestSuccess(result: SmokeResult): boolean {
  return result.summary.failed === 0 && result.steps.length > 0;
}

/**
 * Get the first failing step, if any.
 */
export function getFirstFailingStep(result: SmokeResult): SmokeStep | undefined {
  return result.steps.find((s) => s.status === "FAIL");
}
