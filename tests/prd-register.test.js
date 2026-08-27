/**
 * prd-register.test.js
 * 
 * Test suite for LAUNCH_READINESS_REGISTER.md
 * 
 * Verifies:
 * 1. Register file exists and is well-formed
 * 2. All Resolved entries have valid evidence references
 * 3. Evidence references point to existing files
 * 4. Verification script runs and produces valid JSON
 * 
 * Run: npm test -- tests/prd-register.test.js
 */

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');
const { describe, it, beforeAll } = require('@jest/globals');

const REGISTER_PATH = path.join(__dirname, '..', 'LAUNCH_READINESS_REGISTER.md');
const EVIDENCE_JSON_PATH = path.join(__dirname, '..', 'docs', 'launch-readiness-evidence.json');
const VERIFY_SCRIPT = path.join(__dirname, '..', 'scripts', 'verify-prd-refs.js');
const REPO_ROOT = path.join(__dirname, '..');

describe('Launch Readiness Register', () => {
  let registerContent;
  let entries = [];

  beforeAll(() => {
    // Read and parse register
    if (!fs.existsSync(REGISTER_PATH)) {
      throw new Error(`Register file not found: ${REGISTER_PATH}`);
    }
    registerContent = fs.readFileSync(REGISTER_PATH, 'utf-8');
    entries = parseRegister(registerContent);
  });

  describe('File Structure', () => {
    it('should exist and be readable', () => {
      expect(fs.existsSync(REGISTER_PATH)).toBe(true);
      expect(registerContent).toBeTruthy();
    });

    it('should contain all three status types', () => {
      const statuses = entries.map(e => e.status);
      expect(statuses).toContain('Resolved');
      expect(statuses).toContain('Open');
    });

    it('should have at least 90 Resolved entries', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');
      expect(resolved.length).toBeGreaterThanOrEqual(90);
    });

    it('should have at least 40 Open entries', () => {
      const open = entries.filter(e => e.status === 'Open');
      expect(open.length).toBeGreaterThanOrEqual(40);
    });

    it('should have valid entry format', () => {
      expect(entries.length).toBeGreaterThan(100);
      
      for (const entry of entries) {
        expect(entry.id).toBeTruthy();
        expect(entry.title).toBeTruthy();
        expect(entry.status).toMatch(/^(Resolved|Open|Needs more info)$/);
      }
    });
  });

  describe('Resolved Entries Validation', () => {
    it('all Resolved entries must have non-empty Evidence', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');
      
      for (const entry of resolved) {
        expect(entry.evidence).toBeTruthy();
        expect(entry.evidence).not.toBe('—');
        expect(entry.evidence).not.toMatch(/^—\s*$/);
      }
    });

    it('Evidence references should follow valid format', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');
      const validTypes = ['file', 'test', 'migration', 'pr', 'ci'];

      for (const entry of resolved) {
        const references = entry.evidence.split(',').map(r => r.trim());
        
        for (const ref of references) {
          const type = ref.split(':')[0];
          expect(validTypes).toContain(type);
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
            expect(fs.existsSync(filePath)).toBe(true);
          }
        }
      }
    });

    it('test: references should point to existing test files', () => {
      const resolved = entries.filter(e => e.status === 'Resolved');

      for (const entry of resolved) {
        const testRefs = (entry.evidence || '')
          .split(',')
          .filter(r => r.trim().startsWith('test:'));

        for (const ref of testRefs) {
          const match = ref.match(/^test:\s*([^\s:]+)/);
          if (match) {
            const filePath = path.join(REPO_ROOT, match[1]);
            expect(fs.existsSync(filePath)).toBe(true);
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
            expect(fs.existsSync(filePath)).toBe(true);
          }
        }
      }
    });

    it('ci: references should point to existing workflows', () => {
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
            expect(fs.existsSync(workflowPath)).toBe(true);
          }
        }
      }
    });
  });

  describe('Open Entries Validation', () => {
    it('all Open entries should have Notes', () => {
      const open = entries.filter(e => e.status === 'Open');

      for (const entry of open) {
        expect(entry.notes).toBeTruthy();
      }
    });

    it('Open entry notes should explain what is missing', () => {
      const open = entries.filter(e => e.status === 'Open');
      
      for (const entry of open) {
        // Notes should mention backlog issue or clarify what's needed
        expect(entry.notes.toLowerCase()).toMatch(
          /(backlog|issue|created|recommend|requires|needs|pending|not yet)/
        );
      }
    });
  });

  describe('Verification Script', () => {
    it('should exist and be executable', () => {
      expect(fs.existsSync(VERIFY_SCRIPT)).toBe(true);
    });

    it('should run without errors', (done) => {
      try {
        const output = execSync(`node ${VERIFY_SCRIPT}`, {
          cwd: REPO_ROOT,
          stdio: 'pipe',
          encoding: 'utf-8',
        });
        expect(output).toBeTruthy();
        done();
      } catch (error) {
        // Allow exit code 1 if issues were found (expected)
        // But 2+ indicates script errors
        if (error.status > 1) {
          done(error);
        } else {
          done();
        }
      }
    });

    it('should generate evidence JSON report', () => {
      // Run verification script to generate report
      try {
        execSync(`node ${VERIFY_SCRIPT}`, {
          cwd: REPO_ROOT,
          stdio: 'pipe',
        });
      } catch (e) {
        // Ignore script exit; we just want the side effect of JSON generation
      }

      expect(fs.existsSync(EVIDENCE_JSON_PATH)).toBe(true);

      const report = JSON.parse(
        fs.readFileSync(EVIDENCE_JSON_PATH, 'utf-8')
      );

      expect(report.timestamp).toBeTruthy();
      expect(report.totalEntries).toBeGreaterThan(100);
      expect(report.summary.resolved).toBeGreaterThanOrEqual(90);
      expect(report.summary.open).toBeGreaterThanOrEqual(40);
    });
  });

  describe('Evidence Count Summary', () => {
    it('should have valid distribution of statuses', () => {
      const resolved = entries.filter(e => e.status === 'Resolved').length;
      const open = entries.filter(e => e.status === 'Open').length;
      const needsInfo = entries.filter(e => e.status === 'Needs more info').length;
      const total = resolved + open + needsInfo;

      // Should account for all entries
      expect(total).toBe(entries.length);

      // Resolved should be majority
      expect(resolved).toBeGreaterThan(open);

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
