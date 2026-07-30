# Notifications Dispatcher (#373)

In-process dispatcher that takes a domain event + a user ID and fans
out to the channels configured for that event, respecting per-user
opt-outs.

## What this package contains

- **`notifications.go`** — `Event` / `Channel` types; the event ↔
  channel routing matrix from the issue; `Preferences` with default
  opt-in semantics; `Dispatcher` service; `EmailChannel` and
  `WebSocketChannel` adapters with `MailSender` / `EmailLookup` /
  `WebSocketHub` seams for the real provider wiring; in-memory test
  doubles (`RecordingMailSender`, `RecordingHub`, `StaticEmailLookup`,
  `MemoryPreferences`).
- **`notifications_test.go`** — pins the issue's matrix, tests the
  per-user opt-out flow, the unknown-event guard, the persistence
  failure surface, the one-channel-failure-doesn't-block-others
  contract, and the default-allow-all preference behaviour.

## Acceptance criteria covered by this PR

- ✅ NotificationService interface with `Send(ctx, userID, event, ...)`
  (the `Dispatcher` struct).
- ✅ Email provider seam (`MailSender`) — pluggable for SMTP /
  SendGrid / Resend.
- ✅ WebSocket delivery seam (`WebSocketHub`) — ready to wire to the
  existing `internal/ws` hub.
- ✅ User preferences honoured before dispatching.
- ✅ Tests for notification dispatch logic (mock email provider).

## Deferred to a follow-up PR (intentional MVP scope)

The acceptance items below need the `notifications` table to exist
before they're meaningful. Splitting them out keeps this MVP shippable
and reviewable while the migration + handlers are worked on in parallel:

- 🟡 `notifications` Postgres table + migration `018`.
- 🟡 API endpoints — `GET / PATCH / PATCH read-all`.
- 🟡 Notifications page + nav-bar badge in the frontend.
- 🟡 Concrete SMTP `MailSender` (`net/smtp`) and a transactional
  provider implementation.
- 🟡 Wiring `WebSocketHub` to `internal/ws` and emitting the
  `notification` event over the existing socket.

Once the migration lands, the only swap needed is replacing
`NoopPersistenceStore` with a Postgres-backed implementation — the
dispatcher already calls `Save` so the wiring is exercised.

## Channel matrix (from the issue)

| Event                          | Channels                |
| ------------------------------ | ----------------------- |
| settlement_completed / failed  | email + websocket       |
| deposit_confirmed              | email + websocket       |
| vault_apy_drop                 | email                   |
| vault_paused                   | email + websocket       |
| rebalance_executed             | websocket only          |
| kyc_approved / rejected        | email                   |
