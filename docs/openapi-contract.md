# OpenAPI Contract Generation

Nester's Go API had no machine-readable contract: the API's shape lived in
handler code, the DApp's assumptions lived in hand-written TypeScript, and
nothing checked that the two agreed (issue #1058). This document covers what
exists today and what is deliberately deferred.

## What exists

- `apps/api/internal/openapi/spec.go` generates an OpenAPI 3.1 specification
  from the real Go request/response types via
  [`swaggest/openapi-go`](https://github.com/swaggest/openapi-go)'s struct
  reflection, rather than from hand-written annotations that can drift from
  the implementation.
- `apps/api/cmd/openapi-gen` is the CLI: `go run ./cmd/openapi-gen >
  openapi.yaml` regenerates the spec; `-check openapi.yaml` compares the
  freshly generated spec against the committed file and fails if they
  differ.
- `apps/api/openapi.yaml` is the committed spec. CI's "Check OpenAPI spec is
  up to date" step (`.github/workflows/ci.yml`, `api` job) runs the `-check`
  form on every PR that touches `apps/api` or shared code — this is what
  gives the whole exercise teeth: a handler change that isn't reflected in
  the spec fails the build, and the PR diff of `openapi.yaml` is the
  mechanism by which a human sees a breaking change during review, per the
  issue's own stated goal.

## Scope of this first slice

Only the vault lifecycle routes are covered:

- `POST /api/v1/vaults` (create)
- `GET /api/v1/vaults/{id}` (get)
- `POST /api/v1/vaults/{id}/deposit`
- `POST /api/v1/vaults/{id}/withdraw`

These were chosen because they have the cleanest request/response shapes and
include a genuine optional-vs-nullable example
(`vault.Vault.SoftCapacity *decimal.Decimal` alongside its plain
`decimal.Decimal` siblings) worth proving the generator gets right — see
`fixPointerNullability`'s doc comment in `spec.go` for a real bug this
uncovered in the underlying reflection library, confirmed by isolated
reproduction and covered by
`TestNewReflector_PointerFieldsAreNullable_NonPointerFieldsAreNot`.

The remaining ~25 handlers (`apps/api/internal/handler/*.go`) are not yet
covered. Extending coverage means adding an `addX` function per operation in
`spec.go` following the existing pattern — mechanical, but real work per
handler, not something to rush across two dozen handlers with the risk of
misrepresenting a contract nobody then checks. See the PR that introduced
this file for the disclosure of what stayed out of scope and why.

## Deliberately deferred

Per issue #1058's acceptance criteria, not attempted in this first slice:

- **Error taxonomy.** #1058 itself says the spec should capture "the final
  error shape" from #1048's typed error taxonomy, which is still open as of
  this writing. Error responses here use the existing, genuinely-used
  `response.ErrorBody` shape (`pkg/response/response.go`) with the real HTTP
  status codes each route can return (read directly from each handler's
  `writeDomainError` switch) — not a new, invented code taxonomy that would
  likely conflict with #1048's actual design once it lands.
- **Full endpoint coverage.** ~25 handlers remain undocumented; see above.
- **TypeScript client generation.** No generated client replaces the DApp's
  hand-written fetch code yet.
- **Breaking-change detection with label override.** The CI check here
  catches spec staleness (the spec no longer matches the code), not whether
  a change to the spec itself is breaking relative to the base branch. That
  is a genuinely separate tool (diffing two spec versions, classifying the
  diff, and gating on a PR label) and real follow-up work.
- **Contract tests validating live responses.** Generation proves the
  annotations are self-consistent; only running a real handler and
  validating its actual response against the schema proves the handler
  matches what's documented. This needs a schema-validating HTTP client
  (e.g. `kin-openapi`'s `openapi3filter`, or similar) wired against mocked
  handler dependencies, and was left for follow-up rather than added as a
  rushed, thin stub.

## Versioning and deprecation policy

Already documented and implemented independently of this issue:
`docs/api-versioning.md` (policy) and `apps/api/internal/server/versioning.go`
(the `Deprecation`/`Sunset`/`Link` header implementation). This satisfies
#1058's "Versioning and deprecation policy documented" acceptance criterion
without new work here.
