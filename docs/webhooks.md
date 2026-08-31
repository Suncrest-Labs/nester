# Outbound webhooks

Subscribe an HTTPS endpoint you control to receive Nester events —
`POST /api/v1/webhooks` with `{"url": "...", "event_types": ["goal.milestone.50"]}`.
An empty `event_types` list subscribes to every event. The response includes a
`secret` field — this is the only time it is ever returned; store it.

Target URLs must be HTTPS and resolve to a public address. Private, loopback,
link-local (including the `169.254.169.254` cloud metadata address), and
multicast ranges are rejected, both when you register the subscription and
again immediately before every delivery — a hostname that resolved publicly
at registration time can be repointed at an internal address later (DNS
rebinding), so the second check is the one that actually matters.

## Verifying a delivery

Every delivery carries these headers:

- `X-Nester-Signature`: `t={unix_timestamp},v1={hex hmac}`
- `X-Nester-Delivery-Id`: a UUID, stable across retries of the same delivery
- `X-Nester-Event`: the event type, e.g. `goal.milestone.50`
- `X-Nester-Dedupe-Key`: the originating event's dedupe key. Present on
  deliveries produced by a domain event; absent on a manual redelivery. Also
  included in the JSON body as `dedupe_key`, so a consumer that queues the
  raw body and processes it later — with the headers long gone — can still
  dedupe.

The signature is `HMAC-SHA256(secret, "{timestamp}.{raw_body}")`, hex-encoded.
Recompute it yourself and compare in constant time — do not compare with `==`.
Reject requests whose timestamp is too far from your own clock (a few minutes
of tolerance is reasonable); this is what makes a captured signature
unreplayable rather than the signature alone.

```
mac = HMAC_SHA256(secret, timestamp + "." + raw_request_body)
expected = "t=" + timestamp + ",v1=" + hex(mac)
if expected != request.header("X-Nester-Signature"): reject
if abs(now() - timestamp) > tolerance: reject
```

The `v1=` prefix is a scheme version, not a secret version — it lets a future
signing scheme change add a new prefix without breaking existing
integrations.

## Delivery is at-least-once — your handler must be idempotent

**This is a requirement, not a caveat.** A delivery may arrive more than
once for the same event: a retry after a timeout, a redelivery after our
process restarted mid-dispatch, or a manual redelivery. If your handler
treats a webhook as a trigger for something with an effect — a payout, a
ledger entry, an email — and is not idempotent, it *will* eventually do that
thing twice. We cannot prevent that from our side; only your handler can.

**Dedupe on `X-Nester-Delivery-Id`** (equivalently, on `dedupe_key` in the
body if you prefer to work from the payload alone). Both are stable across
every redelivery of the same logical event, in this process and in the one
that picks the event up after a restart — they are derived from the event,
not generated per attempt. Record the id when you finish processing, and
discard a delivery whose id you have already recorded.

A manual redelivery (triggered by the subscription owner from the delivery
log) intentionally uses a *new* delivery id, since it is a new attempt chain
the owner asked for, not a retry of the original — if you already processed
the original, you are expected to still process a manual redelivery as a
fresh event.

## Ordering

Ordering is guaranteed **per aggregate** — per savings goal, per vault — and
is explicitly **not** guaranteed globally. Events about one goal arrive in
the order they happened (25% before 50% before 75%); events about different
goals, or about different users, race freely and may arrive in any order.

Do not infer ordering across aggregates, and do not assume a gap means an
event was dropped: an event for another aggregate simply overtook it.

If an event for one aggregate cannot be delivered at all — your endpoint
rejects it permanently — it is dead-lettered after a bounded number of
attempts and the events behind it for that aggregate resume. A permanently
broken event stalls its own goal's stream briefly, never anyone else's.

## Retry, dead-lettering, and suspension

A non-2xx response or a timeout retries with exponential backoff. After
exhausting retries, the delivery is dead-lettered and logged. After enough
consecutive dead-lettered deliveries, the subscription is automatically
suspended and you are notified — a permanently-broken endpoint is not retried
forever. Re-registering (as a new subscription) resumes delivery once your
endpoint is fixed.

## Delivery log and manual redelivery

`GET /api/v1/webhooks/{id}/deliveries` returns your subscription's recent
delivery attempts — outcome, HTTP status, latency, and a snippet of the
response body — for debugging your integration.

`POST /api/v1/webhooks/deliveries/{deliveryId}/redeliver` re-sends a past
delivery's event and payload under a fresh delivery id (see the dedup note
above). Redelivery is rejected for a suspended subscription; re-register
first.
