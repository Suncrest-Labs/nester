# API Versioning and Deprecation Policy

Nester's HTTP API uses **URL-path versioning**: every endpoint lives under a
version prefix (`/v1/...`, `/v2/...`). The scheme is applied uniformly — no
endpoint uses header-based or query-parameter versioning.

## What is a breaking change

A new major version is required for any change that can break an existing
consumer:

- removing or renaming a response field
- changing a field's type or semantics
- adding a **required** request field
- changing an endpoint's status codes or error contract
- changing authentication or pagination behaviour

Non-breaking changes ship inside the current version:

- adding an **optional** request field
- adding a new response field
- adding a new endpoint

## Multiple active versions

When `v2` of an endpoint ships, `v1` keeps working until its announced
retirement date. Version-specific code is confined to the transport edge —
handlers and DTO mapping (`internal/server/versioning.go` mounts the
versioned groups). Shared business logic lives in the services and is never
duplicated per version.

## Deprecation lifecycle

```
active → deprecated → sunset announced → usage monitored → retired
```

1. **Deprecated** versions return a `Deprecation: true` header, a `Sunset`
   header carrying the retirement date (RFC 8594), and a
   `Link: </vN>; rel="successor-version"` header on **every** response.
2. **Usage is monitored** per version (metrics counter) so retirement
   decisions are data-driven — a version still serving a major consumer is
   not retired.
3. **Retired** versions return `410 Gone` with a JSON body pointing at the
   current version — never a silent 404.

## Default version

Requests without a version prefix route to a **pinned default** version.
The default is *not* "latest": it changes only deliberately, with a code
change and an announcement. Consumers — including the Nester frontend
(`apps/dapp/frontend/lib/api/client.ts`) — should still pin an explicit
version rather than relying on the default.

## Consumer guidance

- Pin an explicit version in every integration.
- Watch for the `Deprecation` and `Sunset` headers; plan migration when they
  appear.
- A `410 Gone` means the version is retired; the response body names the
  version to move to.
