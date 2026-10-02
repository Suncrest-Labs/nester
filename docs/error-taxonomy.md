# API error taxonomy

Tracks issue #1048. Defines the closed set of error kinds every API handler
should return, the response envelope, and the migration path from the
current ad-hoc error handling to it.

## The taxonomy

`apps/api/pkg/apperror` defines eight kinds, each mapping to exactly one
HTTP status and a fixed retry semantic (`apperror.Kind.HTTPStatus()` /
`apperror.Kind.Retryable()`, asserted by `TestKindHTTPStatusMapping` and
`TestKindRetryable` in `apps/api/pkg/apperror/error_test.go`):

| Kind                   | HTTP | Retryable | Example                          |
| ---------------------- | ---- | --------- | --------------------------------- |
| `validation`           | 400  | no        | malformed amount                  |
| `unauthenticated`      | 401  | no        | missing or expired token          |
| `forbidden`            | 403  | no        | not the owner of this vault       |
| `not_found`            | 404  | no        | vault id does not exist           |
| `conflict`             | 409  | no        | goal already completed            |
| `quota_exceeded`       | 429  | yes       | rate or quota limit               |
| `upstream_unavailable` | 503  | yes       | Soroban RPC down                  |
| `internal`             | 500  | no        | unexpected                        |

This is a closed set deliberately. `Kind` and its HTTP/Retryable mapping are
part of the public API contract — adding, removing, or remapping a kind is a
breaking change, not a refactor, and must be versioned accordingly (see
Migration below).

## Constructing an error

```go
import "github.com/suncrestlabs/nester/apps/api/pkg/apperror"

// Simple case
return apperror.NewNotFound("VAULT_NOT_FOUND", "vault does not exist")

// Validation with field-level detail
return apperror.NewValidationWithDetails(
    "VALIDATION_FAILED",
    "request has invalid fields",
    []apperror.FieldDetail{
        {Field: "amount", Message: "must be positive"},
    },
)

// Wrapping an underlying cause for server-side logging, WITHOUT leaking it
// to the client — Message is what the client sees; WithCause is for your
// logger only.
return apperror.NewInternal("DB_ERROR", "internal server error").
    WithCause(err)
```

`Code` is a stable, machine-readable string scoped within its `Kind` (e.g.
`VAULT_NOT_FOUND`) — safe to switch on in a client. `Message` is safe to
display to an end user. Never put a driver error, SQL fragment, or stack
trace in `Message` — that is exactly the leak this taxonomy exists to close;
put it in `WithCause` instead, which only your logger reads.

## The response envelope

Every error response has this shape, built by
`response.FromAppError(appErr, requestID)`:

```json
{
  "success": false,
  "error": {
    "code": "VAULT_NOT_FOUND",
    "message": "vault does not exist",
    "request_id": "a1b2c3d4",
    "retryable": false,
    "details": null
  }
}
```

`retryable` and `details` are derived from the `AppError`'s `Kind` — the
handler does not set them directly, so they can never drift out of sync
with the taxonomy table above.

## Wiring a handler up

Handlers using the `AppHandler` signature (`func(w, r) error`) are wrapped
with `middleware.ErrorHandler(...)`, which translates any returned error
into the envelope above via the error's `apperror.Kind` (falling back to
`internal` for anything that is not an `*apperror.AppError`, so a raw driver
error escaping a handler can never leak its message to the client — see
`TestErrorHandler_NeverLeaksDriverOrSQLText`):

```go
mux.Handle("GET /api/v1/vaults/{id}", middleware.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
    vault, err := svc.GetVault(r.Context(), id)
    if err != nil {
        return apperror.NewNotFound("VAULT_NOT_FOUND", "vault does not exist").WithCause(err)
    }
    response.WriteJSON(w, http.StatusOK, response.OK(vault))
    return nil
}))
```

## Existence-leak decision (403 vs. 404)

PRD B-05 records that `PATCH /settlements/{id}` returns 403 in a way that
confirms the resource exists for a non-owner — an enumeration oracle: an
attacker can distinguish "exists, not yours" (403) from "does not exist"
(404) without ever being authorized to see the resource.

**Decision:** for any resource scoped to a specific caller (an owner, a
merchant, a vault holder), a lookup by a caller who is not authorized for
that specific resource returns `KindNotFound` (404), not `KindForbidden`
(403), when the *only* thing distinguishing the two cases is the resource's
existence. Use `KindForbidden` only when the caller is unambiguously aware
the resource exists (e.g. they created it, then a subsequent permission
change revoked their access) and telling them so is not itself a leak.

This must be applied uniformly, not decided per-handler — a single handler
that returns 403 for a not-mine-but-exists resource undoes the guarantee for
every other handler that gets it right, since ownership can then be probed
via whichever endpoint got it wrong.

## Migration status and plan

**What exists today (this change):** the taxonomy (`apperror.Kind` and its
eight values), the `AppError` type and its constructors, the envelope
(`response.FromAppError`), and `middleware.ErrorHandler`'s dispatch on all
eight kinds. All of the above have tests, including the enforcement tests
issue #1048 calls for (`TestKindHTTPStatusMapping`,
`TestErrorHandler_NeverLeaksDriverOrSQLText`).

**Domain error retrofit (nester#1341, done):** `internal/domain/ledger` and
`internal/domain/vault`'s (including `vault`'s `deactivation.go`) exported
sentinel errors are now `*apperror.AppError` values instead of plain
`errors.New` strings, each carrying a stable `Kind`/`Code` reachable via
`errors.As`. Every sentinel was converted *in place* — `var ErrVaultNotFound
error = apperror.NewNotFound(...)` rather than a new variable — so it is the
exact same error value it always was: every existing
`errors.Is(err, vault.ErrVaultNotFound)` call site across the codebase
(including `vault_handler.go`'s own `writeDomainError` dispatch) keeps
compiling and behaving identically with no migration of its own required.
This is a strictly additive capability (a caller *can* now read `Kind`/`Code`
via `errors.As`), not a behavior change.

**What is deliberately deferred, and why:**

- **Handler migration.** `apperror`/`middleware.ErrorHandler` are not yet
  wired into any route. Every handler under `apps/api/internal/handler/`
  currently constructs its own `response.Response` inline and calls
  `response.WriteJSON` directly (see e.g. `watchlist_handler.go`), rather
  than returning an `AppError` for the middleware to translate. This stays
  true even after the domain retrofit above: `writeDomainError` still
  dispatches on `errors.Is` against each named sentinel and writes its own
  `response.WriteJSON` call per branch, exactly as before — it does not yet
  read the now-available `Kind`/`Code` off the error. Migrating every
  handler to `AppHandler`/`ErrorHandler` is the "22 packages" scope the
  issue names — a large, mechanical, but non-trivial change (every handler's
  function signature and every route registration changes) that deserves
  its own PR, reviewed independently of the taxonomy's design. This document
  exists so that PR has a spec to migrate *to* rather than inventing one
  along the way.
- **A third, separate error-response system.** `apps/api/internal/api/response.go`
  defines its own `ValidationError`/`FieldError` types, independent of both
  `apperror` and `apps/api/pkg/response`. Reconciling three parallel
  systems is out of scope for defining the taxonomy itself; it is called
  out here so it is not lost, and should be resolved as part of (or
  immediately before) the handler migration above.
- **Enforcement lint.** The issue asks for "a test or lint pass that fails
  when a handler returns a bare error rather than a typed one." This is only
  meaningful once handlers are actually returning errors through
  `AppHandler`/`ErrorHandler` (see above) — before that migration, every
  handler already bypasses the middleware entirely by writing its own
  response, so a lint rule today would have nothing to check. Add it in the
  same PR as the handler migration.
- **Version header / transition period.** The issue asks that the new
  envelope not break the DApp in the same release. Since the envelope is
  additive (`retryable` and `details` are new fields; `code`, `message`, and
  `request_id` are unchanged from the current `ErrorBody` shape already
  shipped), no existing DApp code should observe a breaking change from
  *this* PR alone. A version header or transition period becomes relevant
  once handler migration starts changing which `code`/`message` values
  handlers actually emit — track that decision in the handler-migration PR,
  not here.

## Adding a new error kind

Do not. The taxonomy is closed. If a genuinely new category of failure
emerges that does not fit the eight kinds above, that is a proposal for a
new major API version, not an addition to this list — discuss it as such.
