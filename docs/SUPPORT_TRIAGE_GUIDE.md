# Support triage guide

For support engineers without codebase access, covering the four most
likely launch reports. Uses the money-path lookup added in #1141:
`GET /api/v1/admin/users/{id}/money-path`.

**Status**: first draft, based on how the system is supposed to behave.
Issue #1142 asks for this to be reviewed against real reports after the
first week of launch and corrected — that review hasn't happened yet since
there's no launch traffic yet to review against.

## 1. "My deposit isn't showing"

**What to ask:**
- Wallet address used for the deposit.
- Approximate time of the deposit.
- Transaction hash, if the user has one (from their wallet's history).

**What to check:**
- Look up the user (by wallet address) and open their money-path view
  (`/api/v1/admin/users/{id}/money-path`).
- Check `pending_submissions` — if the transaction is there, it's still
  processing.
- Check `recent_errors` — if it's there, the deposit failed on-chain or
  during indexing; the error field explains why.
- Not in either list? Check the transaction hash directly against
  Stellar Horizon/a block explorer to confirm it actually landed on-chain
  at all — if it didn't, this isn't a Nester issue.

**What to do:**
- Pending and under 5 minutes old: reassure the user, ask them to check
  again shortly.
- Pending and older than expected (see indexer poll interval in
  `docs/ENVIRONMENT.md` if present): escalate — the indexer may be stuck.
- In `recent_errors`: relay the error reason; if it's a contract-level
  rejection, escalate to engineering with the transaction hash.
- Confirmed on-chain but not in Nester's view at all: escalate
  immediately — likely an indexer gap.

**When to escalate:** transaction confirmed on-chain (verified via
Horizon/explorer) but absent or stuck longer than expected in Nester's own
view.

## 2. "My withdrawal is stuck"

**What to ask:**
- Wallet address and approximate withdrawal time.
- What the UI currently shows (a specific status, a spinner, an error).

**What to check:**
- Money-path view: look at `recent_transactions` for the withdrawal's
  status.
- If `pending`, check how long — withdrawals typically clear within [fill
  in expected duration once known from real operation].
- If `failed`, read the recorded error.

**What to do:**
- Pending within expected duration: reassure, ask them to wait.
- Pending beyond expected duration: escalate with the transaction id from
  the money-path view.
- Failed: relay the reason; if funds should have been returned to the
  vault and weren't, escalate immediately (this is a fund-safety issue,
  not a UX one).

**When to escalate:** any withdrawal failure that doesn't cleanly return
funds, or any pending withdrawal beyond the expected clearing window.

## 3. "My balance looks wrong"

**What to ask:**
- What the user expected vs. what they see.
- Whether they've deposited/withdrawn/harvested recently.

**What to check:**
- Money-path view's `positions` section for their actual recorded
  position per vault.
- `recent_transactions` for anything that would explain the discrepancy
  (a harvest that changed share price, a pending withdrawal already
  deducted from the displayed balance, etc).

**What to do:**
- If the recorded position matches what they should have given their
  transaction history: explain the calculation (e.g. "your share price
  changed after the last harvest").
- If it genuinely doesn't reconcile: escalate with the money-path view's
  output attached — this needs engineering to check the vault's on-chain
  state against what's indexed.

**When to escalate:** the position doesn't reconcile against the user's
own transaction history, or the discrepancy involves an amount the user
disputes as a loss.

## 4. "My wallet won't connect"

**What to ask:**
- Which wallet (Freighter, Albedo, Lobstr, etc.) and browser.
- Exact error message or behavior (nothing happens vs. an explicit error).
- Whether this is their first time connecting or it previously worked.

**What to check:**
- This is almost always client-side (extension not installed/unlocked,
  browser blocking a popup, wrong network selected in the wallet).
- Not something the money-path view or any backend lookup will show —
  there's no server-side state for "wallet didn't connect."

**What to do:**
- Confirm the wallet extension is installed, unlocked, and set to the
  correct network (testnet/mainnet matching the app).
- Ask them to try a different browser/incognito to rule out an extension
  conflict.
- If it still fails and other users aren't reporting it: likely
  wallet-specific — no escalation needed, just document it.
- If multiple users report it at once: escalate immediately — likely a
  frontend regression or a wallet provider's own outage.

**When to escalate:** multiple simultaneous reports (signals a systemic
issue, not a one-off client problem).

## Links

- Support tooling: `GET /api/v1/admin/users/{id}/money-path` (#1141)
- In-app problem report (attaches context automatically): #1143
