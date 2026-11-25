# Mainnet protocol integration rehearsal

`Mainnet protocol integration` (`.github/workflows/mainnet-protocol-integration.yml`,
nester#1569) is a manual workflow that runs the launch contract set against a
local network configured like mainnet, instead of relying on testnet or the
Soroban SDK's defaults.

Testnet and mainnet can run different protocol versions and different network
settings. Size limits, fees, TTL bounds and resource limits are ledger settings,
not code constants, so a suite that passes on testnet defaults can still have
mainnet reject the same calls. This workflow removes that gap for the launch
contract set.

## What it does

1. Reads the protocol version and the Soroban `ConfigUpgradeSet` currently active
   on mainnet through a read-only RPC call.
2. Starts `stellar/quickstart` in local mode, pinned to that protocol version.
3. Applies mainnet's settings verbatim through quickstart's `--limits <file>`
   mechanism, after removing only the two entries that describe mainnet ledger
   state rather than limits:
   - `frozen_ledger_keys`
   - `freeze_bypass_txs`

   Those entries reference specific mainnet ledger keys and transactions, and
   stellar-core rejects them on a fresh network. Every limit, fee, TTL and
   resource setting mainnet publishes is applied.
4. Fails unless the local RPC and core report the pinned protocol version and
   `stellar network settings` reports the same values as mainnet.
5. Deploys `deploy/mainnet/launch-manifest.toml` to the local network, then
   initialises and verifies every contract in it. The manifest mirrors the launch
   deploy order and uses a locally issued USDC asset plus the native XLM SAC.
   A WASM larger than mainnet's `contract_max_size_bytes` fails before any deploy
   transaction, naming the contract and both sizes rather than surfacing as an
   opaque `TxSorobanInvalid` later. That red result is a real launch blocker, not
   a workflow defect.
6. Runs the contract integration suite and, when enabled, the dapp mainnet e2e
   smoke suite.
7. Uploads `.mainnet-protocol/` as an artifact and writes the same report to the
   workflow job summary.

## Running it

Actions → **Mainnet protocol integration** → **Run workflow**.

| Input | Default | Purpose |
| --- | --- | --- |
| `protocol_version` | empty | Pins quickstart to a protocol version. Empty reads the version mainnet currently reports. |
| `quickstart_image` | `stellar/quickstart:latest` | Override when a protocol version needs a newer image. |
| `mainnet_rpc_url` | `https://mainnet.sorobanrpc.com` | Read-only source for the protocol version and settings. |
| `run_e2e` | `true` | Run the dapp mainnet e2e smoke suite. |

The workflow never submits a transaction to a public network. The only mainnet
traffic is the read-only `getNetwork` RPC call and the settings query.

## Run report

The artifact `mainnet-protocol-run-<run-id>` contains:

| File | Contents |
| --- | --- |
| `run-report.md` | Protocol version, quickstart image, settings applied, mainnet limits table, per-stage results, deployed contract IDs, verification transcript and log tails. |
| `mainnet-settings.json` | Raw mainnet `ConfigUpgradeSet`, including the two excluded entries. |
| `quickstart-config-settings/mainnet.json` | The exact settings file quickstart applied. |
| `local-settings.json` | Settings the local network reported after startup. |
| `deployed-local.env` | Contract IDs and SAC addresses from the local deploy. |
| `deployed-local.verification.txt` | Read-only calls proving the deployed contracts were initialised. |
| `deploy.log`, `integration.log`, `e2e.log`, `quickstart.log` | Full logs. |

The report records the protocol version and the settings used, so a passing run
can be compared against a later mainnet protocol upgrade.

## Scope and limits

- This is a launch-set rehearsal, not the audited mainnet deploy record. The
  manifest lives under `deploy/` and is not the audited contract list governed by
  the contracts freeze (nester#1501).
- The contract integration suite is the repository's in-process Soroban test
  suite. The protocol-sensitive live coverage in this workflow is the manifest
  deploy and its read-only verification calls against the local network that has
  mainnet's limits applied.
- The dapp mainnet e2e suite is the same mocked, read-only smoke suite CI runs.
  It validates the Mainnet production build; it does not submit chain
  transactions.
