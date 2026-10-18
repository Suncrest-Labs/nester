import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = path.resolve(__dirname, "../../../..");

describe("Stage 1 Launch Scope Freeze & Change-Control Rule (#1554)", () => {
  it("documents the Stage 1 freeze date, audit-candidate-1 tag, and contract change-control rule in LAUNCH_GO_NO_GO.md", () => {
    const docPath = path.join(REPO_ROOT, "docs/LAUNCH_GO_NO_GO.md");
    const content = fs.readFileSync(docPath, "utf8");

    expect(content).toContain("Stage 1 Launch Scope Freeze & Change-Control Rule (#1554)");
    expect(content).toContain("audit-candidate-1");
    expect(content).toContain("packages/contracts/");
    expect(content).toContain("[audit-fix]");
    expect(content).toContain("scripts/check-contract-freeze.sh");
  });

  it("provides scripts/check-contract-freeze.sh to enforce post-tag contract change control", () => {
    const scriptPath = path.join(REPO_ROOT, "scripts/check-contract-freeze.sh");
    const scriptContent = fs.readFileSync(scriptPath, "utf8");

    expect(scriptContent).toContain("audit-candidate-1");
    expect(scriptContent).toContain("packages/contracts/");
    expect(scriptContent).toContain("(\\[audit-fix\\]|AUDIT-[0-9]+|\\[p0-fix\\]|\\[p1-fix\\]|INC-[0-9]+)");
  });
});
