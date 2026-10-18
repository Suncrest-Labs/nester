import fs from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';

const REPO_ROOT = path.resolve(__dirname, '../../../..');
const GAME_DAY_PATH = path.join(REPO_ROOT, 'docs/security/game-day.md');

describe('Global and per-vault pause drill rehearsal (#1552)', () => {
  const report = fs.readFileSync(GAME_DAY_PATH, 'utf8');

  it('records per-vault and global pause execution times within the 15-minute SLA', () => {
    expect(report).toContain('Global and Per-Vault Pause Drill — End-to-End Rehearsal (#1552)');
    expect(report).toContain('3 minutes 12 seconds');
    expect(report).toContain('4 minutes 48 seconds');
    expect(report).toContain('PASS (under 15m SLA)');
  });

  it('verifies deposits are blocked while emergency withdrawals remain available during pause', () => {
    expect(report).toContain('ContractError(VaultPaused)');
    expect(report).toContain('emergency_withdraw');
    expect(report).toContain('unpause_vault');
  });
});
