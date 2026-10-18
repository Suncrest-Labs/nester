# Phishing & Lookalike Domain Takedown Runbook

> **Scope:** Operational procedure for detecting, triaging, warning users, and executing registrar/hosting/wallet-blocklist takedowns of phishing domains and impersonation accounts targeting Nester (`nester.finance`).
>
> **Related Issue:** [#1535](https://github.com/Suncrest-Labs/nester/issues/1535)

---

## 1. Ownership & Escalation Roles

| Role | Primary Owner | Backup Owner | Responsibilities |
|------|---------------|--------------|------------------|
| **Takedown Lead** | `@0xDeon` (Security Lead) | `@nester-infra-oncall` | Evidence preservation, registrar/hosting abuse filing, wallet blocklist submissions |
| **Comms Lead** | `@0xDeon` | `@nester-ops-lead` | Publishes user warnings on official X (`@NesterFinance`), Discord (`discord.gg/nester`), Telegram (`t.me/NesterFinance`), and Status Page (`status.nester.finance`) |

---

## 2. Automated Detection (`dnstwist`)

1. `scripts/monitor-phishing-domains.sh` runs weekly (and on-demand during launch week) against `nester.finance`.
2. Any resolving permutation outside the defensive allowlist (`nester.finance`, `nester.xyz`, `nester.app`, `nester.io`, `nester.dev`, `nesterfi.com`, `nester-finance.com`) triggers a critical alert to `security@nester.dev` and the on-call webhook.
3. Community reports arriving at `security@nester.dev` with subject `[PHISHING]` follow the exact same workflow below.

---

## 3. Step-by-Step Takedown Procedure

### Step 1 — Evidence Preservation (< 15 minutes)

Before contacting the attacker's infrastructure providers, capture immutable evidence:

```bash
SUSPECT_DOMAIN="example-nester-claim.com"
mkdir -p "/tmp/phishing-evidence-${SUSPECT_DOMAIN}"
dig +noall +answer "${SUSPECT_DOMAIN}" A AAAA CNAME NS TXT MX > "/tmp/phishing-evidence-${SUSPECT_DOMAIN}/dns.txt"
whois "${SUSPECT_DOMAIN}" > "/tmp/phishing-evidence-${SUSPECT_DOMAIN}/whois.txt"
curl -sSL -D "/tmp/phishing-evidence-${SUSPECT_DOMAIN}/headers.txt" "https://${SUSPECT_DOMAIN}" -o "/tmp/phishing-evidence-${SUSPECT_DOMAIN}/index.html" || true
```

Also submit the URL to `https://urlscan.io` and record the permanent scan UUID and screenshot.

### Step 2 — Blocklist & Wallet Warning Submissions (< 30 minutes)

Immediately protect Stellar ecosystem users by submitting the malicious domain and any attacker destination address (`G...` / `C...`) to:

1. **Stellar Ecosystem & Wallet Blocklists:**
   - **Freighter / SDF Security Team:** File a phishing report with the urlscan.io permalink and attacker contract/account IDs.
   - **Lobstr / xBull / Scopuly:** Submit domain and malicious asset/contract report.
   - **Stellar Expert Directory:** Flag any malicious Stellar account or contract address (`C...` / `G...`) with the `phishing` / `unsafe` directory tag.
2. **Browser Safe Browsing Feeds:**
   - **Google Safe Browsing:** `https://safebrowsing.google.com/safebrowsing/report_phish/`
   - **Microsoft SmartScreen:** `https://www.microsoft.com/en-us/wdsi/support/report-unsafe-site-guest`
   - **Cloudflare Radar / PhishTank / Netcraft:** Submit URL with evidence that it impersonates `https://nester.finance`.

### Step 3 — Public Community Warning (< 30 minutes)

Comms Lead posts an alert across X (`@NesterFinance`), Discord `#announcements`, and Telegram (`t.me/NesterFinance`):

> **SECURITY ALERT — Phishing Site Detected**
> Do NOT interact with `example-nester-claim[.]com`. It is NOT affiliated with Nester.
> Nester's ONLY official web app is `https://app.nester.finance` and marketing site is `https://nester.finance`.
> Never share your recovery phrase or sign unrecognized transactions.

### Step 4 — Registrar, Hosting & CDN Abuse Takedown (< 2 hours)

Using the `whois.txt` and `dns.txt` outputs from Step 1, Takedown Lead (`@0xDeon`) sends formal abuse notices to:
1. **Domain Registrar Abuse Contact** (`Registrar Abuse Contact Email` in WHOIS): Request immediate `clientHold` suspension for trademark/brand impersonation and cryptocurrency wallet draining.
2. **CDN / Reverse Proxy** (e.g., Cloudflare Abuse `https://cloudflare.com/abuse`, Vercel `abuse@vercel.com`, Netlify `fraud@netlify.com`): Provide the urlscan.io link and original `https://nester.finance` / `https://app.nester.finance` URLs being cloned.

### Step 5 — Post-Takedown Verification

Keep the domain in daily monitoring for 14 days to confirm the `clientHold` / NXDOMAIN state persists and no A/AAAA records are re-pointed.
