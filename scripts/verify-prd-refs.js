#!/usr/bin/env node

/**
 * verify-prd-refs.js
 * 
 * Validates docs/launch-readiness-register.md by checking that:
 * 1. All "Resolved" entries have non-empty Evidence fields
 * 2. All Evidence references point to existing files/tests/PRs
 * 3. Evidence follows consistent formatting
 * 4. Generates a JSON report: docs/launch-readiness-evidence.json
 * 
 * Usage:
 *   node scripts/verify-prd-refs.js
 * 
 * Exit codes:
 *   0 = All checks pass
 *   1 = One or more checks failed
 */

const fs = require('fs');
const path = require('path');

// ============================================================================
// Configuration
// ============================================================================

const REGISTER_PATH = path.join(__dirname, '..', 'docs', 'launch-readiness-register.md');
const EVIDENCE_JSON_PATH = path.join(__dirname, '..', 'docs', 'launch-readiness-evidence.json');
const REPO_ROOT = path.join(__dirname, '..');

// Evidence type validators
const EVIDENCE_VALIDATORS = {
  file: (ref) => {
    // Format: file: path/to/file.ext#L1-L50 or file: path/to/file.ext
    const match = ref.match(/^file:\s*([^\s#]+)(?:#L\d+(?:-L\d+)?)?$/);
    if (!match) return { valid: false, error: 'Invalid file format' };
    
    const filePath = match[1];
    const fullPath = path.join(REPO_ROOT, filePath);
    
    if (!fs.existsSync(fullPath)) {
      return { valid: false, error: `File not found: ${filePath}` };
    }
    
    return { valid: true, path: filePath };
  },

  test: (ref) => {
    // Format: test: path/to/file.test.ts::testName
    const match = ref.match(/^test:\s*([^\s:]+)::(.+)$/);
    if (!match) return { valid: false, error: 'Invalid test format' };
    
    const [, filePath, testName] = match;
    const fullPath = path.join(REPO_ROOT, filePath);
    
    if (!fs.existsSync(fullPath)) {
      return { valid: false, error: `Test file not found: ${filePath}` };
    }
    
    // Read file and check if test name exists (basic check)
    const content = fs.readFileSync(fullPath, 'utf-8');
    if (!content.includes(testName)) {
      return { valid: false, error: `Test name not found in ${filePath}: ${testName}` };
    }
    
    return { valid: true, path: filePath, testName };
  },

  migration: (ref) => {
    // Format: migration: path/to/migration/001_*.sql or migration: apps/api/migrations/013_create_sessions_table.sql
    const match = ref.match(/^migration:\s*([^\s]+)$/);
    if (!match) return { valid: false, error: 'Invalid migration format' };
    
    const filePath = match[1];
    const fullPath = path.join(REPO_ROOT, filePath);
    
    if (!fs.existsSync(fullPath)) {
      return { valid: false, error: `Migration file not found: ${filePath}` };
    }
    
    return { valid: true, path: filePath };
  },

  pr: (ref) => {
    // Format: pr: #123 or pr: #123 (merged) commit: abc1234
    const match = ref.match(/^pr:\s*#(\d+)(?:\s*\(merged\))?(?:\s+commit:\s*([a-f0-9]+))?$/);
    if (!match) return { valid: false, error: 'Invalid PR format' };
    
    // Note: We can't validate PR numbers without GitHub API access
    // For now, just verify format
    return { valid: true, prNumber: match[1], commit: match[2] };
  },

  ci: (ref) => {
    // Format: ci: workflow_name::job_name or ci: workflow_name job: job_name
    const match = ref.match(/^ci:\s*([^\s:]+)(?:::|\s+job:\s*)([^\s]+)$/);
    if (!match) return { valid: false, error: 'Invalid CI format' };
    
    const workflowName = match[1];
    const jobName = match[2];
    
    // Check if workflow file exists
    const workflowPath = path.join(REPO_ROOT, '.github', 'workflows', `${workflowName}.yml`);
    if (!fs.existsSync(workflowPath)) {
      return { valid: false, error: `Workflow file not found: .github/workflows/${workflowName}.yml` };
    }
    
    // Basic check: job name appears in workflow file
    const content = fs.readFileSync(workflowPath, 'utf-8');
    if (!content.includes(jobName)) {
      return { valid: false, error: `Job name not found in workflow ${workflowName}: ${jobName}` };
    }
    
    return { valid: true, workflow: workflowName, job: jobName };
  },
};

// ============================================================================
// Parser
// ============================================================================

function parseRegister(content) {
  const entries = [];
  const lines = content.split('\n');
  
  let currentEntry = null;
  
  for (const line of lines) {
    // Match entry header: #### [ID] Description
    const headerMatch = line.match(/^####\s+\[([^\]]+)\]\s+(.+)$/);
    if (headerMatch) {
      if (currentEntry) {
        entries.push(currentEntry);
      }
      currentEntry = {
        id: headerMatch[1],
        title: headerMatch[2],
        status: null,
        evidence: null,
        notes: null,
      };
      continue;
    }
    
    if (!currentEntry) continue;
    
    // Match status line: - **Status:** Resolved (may have spaces like "Needs more info")
    const statusMatch = line.match(/^- \*\*Status:\*\*\s+(.+?)(?:\s+|$)/);
    if (statusMatch) {
      currentEntry.status = statusMatch[1].trim();
      continue;
    }
    
    // Match evidence line: - **Evidence:** test: ...
    const evidenceMatch = line.match(/^- \*\*Evidence:\*\*\s+(.+)$/);
    if (evidenceMatch) {
      currentEntry.evidence = evidenceMatch[1].trim();
      continue;
    }
    
    // Match notes line: - **Notes:** ...
    const notesMatch = line.match(/^- \*\*Notes:\*\*\s+(.+)$/);
    if (notesMatch) {
      currentEntry.notes = notesMatch[1].trim();
    }
  }
  
  // Don't forget the last entry
  if (currentEntry) {
    entries.push(currentEntry);
  }
  
  return entries;
}

// ============================================================================
// Validation
// ============================================================================

function validateEntry(entry) {
  const issues = [];
  
  // Check status is valid
  const validStatuses = ['Resolved', 'Open', 'Needs more info'];
  if (!validStatuses.includes(entry.status)) {
    issues.push(`Invalid status: ${entry.status}`);
  }
  
  // If Resolved, Evidence must be present and non-empty
  if (entry.status === 'Resolved') {
    if (!entry.evidence || entry.evidence === '—') {
      issues.push(`Resolved entry must have Evidence`);
      return issues;
    }
    
    // Parse and validate evidence
    const result = validateEvidence(entry.evidence);
    if (!result.valid) {
      issues.push(`Invalid evidence: ${result.error}`);
    }
  }
  
  // If Open or Needs more info, Notes should mention issue/ticket
  if (entry.status === 'Open' || entry.status === 'Needs more info') {
    if (!entry.notes) {
      issues.push(`${entry.status} entry should have explanatory Notes`);
    }
  }
  
  return issues;
}

function validateEvidence(evidenceStr) {
  // Evidence can be a single reference or comma-separated list
  const references = evidenceStr.split(',').map(r => r.trim()).filter(r => r && r !== '—');
  
  if (references.length === 0) {
    return { valid: false, error: 'No evidence references found' };
  }
  
  const results = [];
  
  for (const ref of references) {
    // Determine evidence type
    const type = ref.split(':')[0];
    const validator = EVIDENCE_VALIDATORS[type];
    
    if (!validator) {
      results.push({ valid: false, error: `Unknown evidence type: ${type}`, reference: ref });
      continue;
    }
    
    const result = validator(ref);
    results.push({ ...result, reference: ref });
  }
  
  // All references must be valid
  const allValid = results.every(r => r.valid);
  
  return {
    valid: allValid,
    references: results,
    error: allValid ? null : results.find(r => !r.valid)?.error,
  };
}

// ============================================================================
// Reporting
// ============================================================================

function generateReport(entries, validationResults) {
  const report = {
    timestamp: new Date().toISOString(),
    totalEntries: entries.length,
    summary: {
      resolved: entries.filter(e => e.status === 'Resolved').length,
      open: entries.filter(e => e.status === 'Open').length,
      needsMoreInfo: entries.filter(e => e.status === 'Needs more info').length,
    },
    issues: [],
    resolvedWithEvidence: [],
  };
  
  for (let i = 0; i < entries.length; i++) {
    const entry = entries[i];
    const validation = validationResults[i];
    
    if (entry.status === 'Resolved') {
      report.resolvedWithEvidence.push({
        id: entry.id,
        title: entry.title,
        evidence: entry.evidence,
      });
    }
    
    if (validation.issues.length > 0) {
      report.issues.push({
        id: entry.id,
        title: entry.title,
        status: entry.status,
        issues: validation.issues,
      });
    }
  }
  
  return report;
}

// ============================================================================
// Main
// ============================================================================

function main() {
  let exitCode = 0;
  
  console.log('🔍 Verifying docs/launch-readiness-register.md...\n');
  
  // Read register file
  if (!fs.existsSync(REGISTER_PATH)) {
    console.error(`❌ Register file not found: ${REGISTER_PATH}`);
    process.exit(1);
  }
  
  const registerContent = fs.readFileSync(REGISTER_PATH, 'utf-8');
  const entries = parseRegister(registerContent);
  
  console.log(`📋 Found ${entries.length} entries in register\n`);
  
  // Validate each entry
  const validationResults = entries.map(entry => ({
    issues: validateEntry(entry),
  }));
  
  // Generate report
  const report = generateReport(entries, validationResults);
  
  // Print summary
  console.log('📊 Summary:');
  console.log(`  ✓ Resolved:       ${report.summary.resolved}`);
  console.log(`  ⊘ Open:           ${report.summary.open}`);
  console.log(`  ? Needs more info: ${report.summary.needsMoreInfo}`);
  console.log();
  
  // Print issues if any
  if (report.issues.length > 0) {
    console.log('⚠️  Validation Issues:');
    for (const issue of report.issues) {
      console.log(`\n  [${issue.id}] ${issue.title}`);
      console.log(`    Status: ${issue.status}`);
      for (const msg of issue.issues) {
        console.log(`    - ${msg}`);
      }
    }
    console.log();
    exitCode = 1;
  } else {
    console.log('✅ All entries validated successfully!\n');
  }
  
  // Write JSON report
  const docsDir = path.dirname(EVIDENCE_JSON_PATH);
  if (!fs.existsSync(docsDir)) {
    fs.mkdirSync(docsDir, { recursive: true });
  }
  
  fs.writeFileSync(EVIDENCE_JSON_PATH, JSON.stringify(report, null, 2));
  console.log(`📄 Evidence report written to: docs/launch-readiness-evidence.json\n`);
  
  // Print evidence summary
  console.log(`📌 Resolved entries with evidence: ${report.resolvedWithEvidence.length}`);
  if (report.resolvedWithEvidence.length > 0) {
    console.log('\nSample resolved entries:');
    for (const item of report.resolvedWithEvidence.slice(0, 5)) {
      console.log(`  [${item.id}] ${item.title}`);
      console.log(`         → ${item.evidence}`);
    }
    if (report.resolvedWithEvidence.length > 5) {
      console.log(`  ... and ${report.resolvedWithEvidence.length - 5} more`);
    }
  }
  
  console.log();
  process.exit(exitCode);
}

main();
