# KMS / HSM Mainnet Signer Key Custody Architecture

> **Scope:** Non-exportable KMS/HSM Ed25519 key custody for the Nester Mainnet isolated transaction signer (`apps/api/cmd/signer`, `apps/api/internal/stellar/kms_signer.go`).
>
> **Related Issue:** [#1531](https://github.com/Suncrest-Labs/nester/issues/1531)

---

## 1. Threat Model & Motivation

When `STELLAR_OPERATOR_SECRET` (`S...`) is passed via environment variable, the raw 32-byte Ed25519 seed resides in the signer container's process environment (`/proc/self/environ`) and heap memory. A container breakout, memory dump, or accidental environment leak would expose the permanent private key.

By moving the Mainnet operator key into a Cloud KMS or HSM with `Non-Exportable` key material (`KMSKeyProvider` / `KMSTransactionSigner`):
1. **No Private Key in Process Memory or Env:** `SIGNER_REQUIRE_KMS=true` rejects startup if `STELLAR_OPERATOR_SECRET` is present in the environment.
2. **Policy Enforcement Before KMS Call:** The signer service still validates the kill switch, intent structure, contract/operation allowlist, per-call and daily caps, and replay guard *before* constructing the transaction and sending its 32-byte Stellar network transaction hash (`network.HashTransactionInEnvelope`) to KMS/HSM.
3. **Cryptographic Post-Sign Verification:** Before returning the signed XDR envelope, `KMSTransactionSigner` verifies the 64-byte signature returned by KMS against `SIGNER_KMS_PUBLIC_ADDRESS` (`G...`) using `keypair.FromAddress.Verify`.
4. **Independent Cloud Audit Trail:** Every `AsymmetricSign` / `Sign` call is logged in Cloud Audit Logs / AWS CloudTrail independently of the application database and container logs, with alerts on anomalous call rates.

---

## 2. Supported KMS / HSM Providers

| Provider | Key Spec / Algorithm | Protection Level |
|----------|----------------------|------------------|
| **GCP Cloud KMS / Cloud HSM** | `EC_SIGN_ED25519` (`PureEd25519`) | `HSM` (FIPS 140-2 Level 3) |
| **AWS KMS** | `ECC_NIST_EDWARDS25519` (`ED25519_SHA_512`) | `HSM` |
| **HashiCorp Vault Transit** | `ed25519` (`exportable=false`) | Vault Enterprise HSM / Seal |

---

## 3. Signer Environment Configuration (Mainnet)

```bash
# Enforce KMS/HSM custody and forbid STELLAR_OPERATOR_SECRET in env
SIGNER_REQUIRE_KMS=true

# Non-exportable KMS key identifier and corresponding Stellar G... public address
SIGNER_KMS_KEY_ID="gcpkms://projects/nester-mainnet/locations/us-east1/keyRings/stellar-signer/cryptoKeys/operator-ed25519/cryptoKeyVersions/1"
SIGNER_KMS_PUBLIC_ADDRESS="G..."
SIGNER_KMS_ENDPOINT_URL="http://127.0.0.1:8200/v1/kms/sign"
```

---

## 4. IAM & Cloud Audit Alerting

- **Workload Identity Binding:** Only the `nester-signer` Kubernetes ServiceAccount (`system:serviceaccount:nester-prod:nester-signer`) holds `roles/cloudkms.signerVerifier` (`kms:Sign`) on the operator key resource. No human user or CI role has `Sign` permission in production.
- **Key Export Prohibition:** Key material is generated inside the HSM with `ImportOnly=false, Exportable=false`.
- **Cloud Audit Alerting:** Any `AsymmetricSign` invocation from an unexpected principal or exceeding 30 calls/hour pages on-call immediately.
