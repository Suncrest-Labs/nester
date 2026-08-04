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

Every delivery carries three headers:

- `X-Nester-Signature`: `t={unix_timestamp},v1={hex hmac}`
- `X-Nester-Delivery-Id`: a UUID, stable across retries of the same delivery
- `X-Nester-Event`: the event type, e.g. `goal.milestone.50`

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

## Delivery is at-least-once

A delivery may arrive more than once for the same event — retries, and
manual redelivery, both reuse the event but are still separate HTTP requests.
**Dedupe on `X-Nester-Delivery-Id`.** A manual redelivery (triggered by the
subscription owner from the delivery log) intentionally uses a *new* delivery
id, since it is a new attempt chain the owner asked for, not a retry of the
original — if you already processed the original, you are expected to still
process a manual redelivery as a fresh event.

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
