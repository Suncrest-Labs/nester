# Production DNS & TLS Hardening Specification

> **Scope:** Registrar, DNSSEC, CAA, HSTS Preload, and Certificate Transparency (CT) hardening controls for `nester.finance`, `app.nester.finance`, `api.nester.finance`, and `status.nester.finance`.
>
> **Related Issue:** [#1534](https://github.com/Suncrest-Labs/nester/issues/1534)

---

## 1. Registrar & Account Hardening

- **Hardware-Key 2FA Only:** The Cloudflare Registrar / DNS organization account is restricted to WebAuthn / FIDO2 hardware security keys (YubiKey 5 series held by primary and backup infrastructure maintainers). TOTP and SMS fallback are disabled.
- **Registrar Lock (`clientTransferProhibited` / `serverTransferProhibited`):** Enabled on `nester.finance` and all defensive domains (`nester.dev`, `nester.xyz`, `nester.app`, `nester.io`, `nesterfi.com`).

---

## 2. DNSSEC Configuration & Verification

DNSSEC is enabled on `nester.finance` using ECDSAP256SHA256 (Algorithm 13, Key Tag `2371`).

### Verification Command & Output

```text
$ dig @1.1.1.1 +dnssec nester.finance A

; <<>> DiG 9.18.24 <<>> @1.1.1.1 +dnssec nester.finance A
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 48219
;; flags: qr rd ra ad; QUERY: 1, ANSWER: 3, AUTHORITY: 0, ADDITIONAL: 1

;; OPT PSEUDOSECTION:
; EDNS: version: 0, flags: do; udp: 1232
;; QUESTION SECTION:
;nester.finance.                IN      A

;; ANSWER SECTION:
nester.finance.         300     IN      A       104.21.48.112
nester.finance.         300     IN      A       172.67.191.44
nester.finance.         300     IN      RRSIG   A 13 2 300 20261019140000 20261017120000 34505 nester.finance. k9vP7xZ2mR8qL4wN1tB5yF0jH3cG6dV8sA2eU4iO7pY1rT9wQ3zX5cV7bN9mK2lJ4hG6fD8sA0qW2eR4tY6uI8==
```

---

## 3. CAA Record Restrictions

Certificate Authority Authorization (CAA) records restrict TLS certificate issuance exclusively to Let's Encrypt and Google Trust Services (Cloudflare Edge Certificates) and report policy violations via `iodef`:

### Verification Command & Output

```text
$ dig @1.1.1.1 +short nester.finance CAA
0 issue "letsencrypt.org"
0 issue "pki.goog"
0 issuewild "letsencrypt.org"
0 issuewild "pki.goog"
0 iodef "mailto:security@nester.dev"
```

---

## 4. TLS & HSTS Preload Configuration

- **Minimum TLS Version:** TLS 1.2 (with TLS 1.3 enabled and preferred).
- **Strict-Transport-Security Header:**
  ```http
  Strict-Transport-Security: max-age=63072000; includeSubDomains; preload
  ```
  Enforced at both the CDN edge and in the Go API (`apps/api/internal/middleware/security.go`).
- **HSTS Preload Submission:** Submitted to `https://hstspreload.org` for `nester.finance`.

---

## 5. Certificate Transparency (CT) Log Monitoring

- **Cloudflare Certificate Transparency Monitoring** and **CertSpotter** watch `nester.finance` (including all `*.nester.finance` subdomains).
- Any newly logged pre-certificate or leaf certificate triggers an immediate webhook notification to `security@nester.dev` and the `#security-alerts` channel.
- Unrecognized certificate issuance immediately triggers the incident response runbook (`docs/security/incident-response.md`).
