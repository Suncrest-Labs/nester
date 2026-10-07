/**
 * prd-register.test.js
 * 
 * Test suite for docs/launch-readiness-register.md
 * 
 * Verifies:
 * 1. Register file exists and is well-formed
 * 2. All Resolved entries have valid evidence references
 * 3. Evidence references point to existing files
 * 4. Verification script runs and produces valid JSON
 * 
 * Run: node --test tests/prd-register.test.js
 */

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');
const { describe, it, before } = require('node:test');
const assert = require('node:assert/strict');

const REGISTER_PATH = path.join(__dirname, '..', 'docs', 'launch-readiness-register.md');
const EVIDENCE_JSON_PATH = path.join(__dirname, '..', 'docs', 'launch-readiness-evidence.json');
const VERIFY_SCRIPT = path.join(__dirname, '..', 'scripts', 'verify-prd-refs.js');
const REPO_ROOT = path.join(__dirname, '..');

describe('Launch Readiness Register', () => {
  let registerContent;
  let entries = [];

  before(() => {
    // Read and parse register
    if (!fs.existsSync(REGISTER_PATH)) {
      throw new Error(`Register file not found: ${REGISTER_PATH}`);
    }
    registerContent = fs.readFileSync(REGISTER_PATH, 'utf-8');
    entries = parseRegister(registerContent);
  });

  describe('File Structure', () => {
    it('should exist and be readable', () => {
      assert.strictEqual(fs.existsSync(REGISTER_PATH), true);
      assert.ok(registerContent);
    });

    it('should contain all valid status types', () => {
      const statuses = entries.map(e => e.status);
      assert.ok(statuses.includes('Resolved'));
      assert.ok(statuses.includes('Open'));
    });

    it('should have at least 40 Resolved entries', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');
      assert.ok(resolved.length >= 40);
    });

    it('should have Open entries tracked in backlog', () => {
      const open = entries.filter(e => e.status === 'Open');
      assert.ok(open.length >= 2);
    });

    it('should have valid entry format', () => {
      assert.ok(entries.length >= 40);
      
      for (const entry of entries) {
        assert.ok(entry.id);
        assert.ok(entry.title);
        assert.match(entry.status, /^(Resolved|Open|Needs more info)$/);
      }
    });
  });

  describe('Resolved Entries Validation', () => {
    it('all Resolved entries must have non-empty Evidence', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');
      
      for (const entry of resolved) {
        assert.ok(entry.evidence);
        assert.notStrictEqual(entry.evidence, '—');
        assert.ok(!/^—\s*$/.test(entry.evidence));
      }
    });

    it('Evidence references should follow valid format', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');
      const validTypes = ['file', 'test', 'migration', 'pr', 'ci'];

      for (const entry of resolved) {
        const references = entry.evidence.split(',').map(r => r.trim());
        
        for (const ref of references) {
          const type = ref.split(':')[0];
          assert.ok(validTypes.includes(type), `Invalid evidence type ${type} in ${entry.id}`);
        }
      }
    });

    it('file: references should point to existing files', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');

      for (const entry of resolved) {
        const fileRefs = (entry.evidence || '')
          .split(',')
          .filter(r => r.trim().startsWith('file:'));

        for (const ref of fileRefs) {
          const match = ref.match(/^file:\s*([^\s#]+)/);
          if (match) {
            const filePath = path.join(REPO_ROOT, match[1]);
            assert.ok(fs.existsSync(filePath), `File does not exist: ${filePath} in ${entry.id}`);
          }
        }
      }
    });

    it('test: references should point to existing test files and tests', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');

      for (const entry of resolved) {
        const testRefs = (entry.evidence || '')
          .split(',')
          .filter(r => r.trim().startsWith('test:'));

        for (const ref of testRefs) {
          const match = ref.match(/^test:\s*([^\s:]+)::(.+)$/);
          if (match) {
            const filePath = path.join(REPO_ROOT, match[1]);
            assert.ok(fs.existsSync(filePath), `Test file does not exist: ${filePath} in ${entry.id}`);
            const content = fs.readFileSync(filePath, 'utf-8');
            assert.ok(content.includes(match[2]), `Test name ${match[2]} not found in ${filePath} for ${entry.id}`);
          }
        }
      }
    });

    it('migration: references should point to existing migrations', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');

      for (const entry of resolved) {
        const migRefs = (entry.evidence || '')
          .split(',')
          .filter(r => r.trim().startsWith('migration:'));

        for (const ref of migRefs) {
          const match = ref.match(/^migration:\s*([^\s]+)$/);
          if (match) {
            const filePath = path.join(REPO_ROOT, match[1]);
            assert.ok(fs.existsSync(filePath), `Migration file does not exist: ${filePath} in ${entry.id}`);
          }
        }
      }
    });

    it('ci: references should point to existing workflows and jobs', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');

      for (const entry of resolved) {
        const ciRefs = (entry.evidence || '')
          .split(',')
          .filter(r => r.trim().startsWith('ci:'));

        for (const ref of ciRefs) {
          const match = ref.match(/^ci:\s*([^\s:]+)(?:::|\s+job:\s*)([^\s]+)$/);
          if (match) {
            const workflowPath = path.join(
              REPO_ROOT,
              '.github',
              'workflows',
              `${match[1]}.yml`
            );
            assert.ok(fs.existsSync(workflowPath), `Workflow does not exist: ${workflowPath} in ${entry.id}`);
            const content = fs.readFileSync(workflowPath, 'utf-8');
            assert.ok(content.includes(match[2]), `Job ${match[2]} not found in workflow ${match[1]} for ${entry.id}`);
          }
        }
      }
    });
  });

  describe('Open Entries Validation', () => {
    it('all Open entries should have Notes', () => {
      const open = entries.filter(e => e.status === 'Open');

      for (const entry of open) {
        assert.ok(entry.notes);
      }
    });

    it('Open entry notes should explain what is missing', () => {
      const open = entries.filter(e => e.status === 'Open');
      
      for (const entry of open) {
        assert.match(
          entry.notes.toLowerCase(),
          /(backlog|issue|created|recommend|requires|needs|pending|not yet|prior to)/
        );
      }
    });
  });

  describe('Verification Script', () => {
    it('should exist and be executable', () => {
      assert.ok(fs.existsSync(VERIFY_SCRIPT));
    });

    it('should run without errors and exit 0', () => {
      const output = execSync(`node ${VERIFY_SCRIPT}`, {
        cwd: REPO_ROOT,
        stdio: 'pipe',
        encoding: 'utf-8',
      });
      assert.ok(output);
      assert.match(output, /All entries validated successfully/);
    });

    it('should generate evidence JSON report matching register counts', () => {
      execSync(`node ${VERIFY_SCRIPT}`, {
        cwd: REPO_ROOT,
        stdio: 'pipe',
      });

      assert.ok(fs.existsSync(EVIDENCE_JSON_PATH));

      const report = JSON.parse(
        fs.readFileSync(EVIDENCE_JSON_PATH, 'utf-8')
      );

      assert.ok(report.timestamp);
      assert.strictEqual(report.totalEntries, entries.length);
      assert.strictEqual(report.summary.resolved, entries.filter(e => e.status === 'Resolved').length);
      assert.strictEqual(report.summary.open, entries.filter(e => e.status === 'Open').length);
    });
  });

  describe('Evidence Count Summary', () => {
    it('should have valid distribution of statuses', () => {
      const resolved = entries.filter(e => e.status === 'Resolved').length;
      const open = entries.filter(e => e.status === 'Open').length;
      const needsInfo = entries.filter(e => e.status === 'Needs more info').length;
      const total = resolved + open + needsInfo;

      assert.strictEqual(total, entries.length);
      assert.ok(resolved > open);

      console.log(`\n📊 Register Summary:`);
      console.log(`   Resolved: ${resolved}`);
      console.log(`   Open: ${open}`);
      console.log(`   Needs more info: ${needsInfo}`);
      console.log(`   Total: ${total}`);
    });
  });
});

// ============================================================================
// Parser (matches verify-prd-refs.js)
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

  if (currentEntry) {
    entries.push(currentEntry);
  }

  return entries;
}
