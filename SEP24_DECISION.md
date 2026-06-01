# SEP-24 Anchor Evaluation: Cowrie vs. MoneyGram

## Background
The current settlement service creates DB records but never executes actual fiat transfers. Direct NIBBS API access and Paystack/Flutterwave Transfer (payout) APIs both require CBN business registration — blocked until Nester registers as a licensed financial institution. 

This document outlines the findings of investigating two Stellar-based SEP-24 anchors to serve as an NGN offramp execution layer, bypassing direct CBN licensing requirements.

## 1. Cowrie Exchange (Stellar SEP-24)
Cowrie is a CBN-licensed Nigerian fintech that operates as a Stellar anchor for NGN, issuing NGNT (Nigerian Naira Token).

- **Implementation**: Follows standard Stellar protocols (SEP-10 for Authentication, SEP-24 for Interactive Withdrawal).
- **Testnet Flow**: To test the USDC -> NGN flow, developers can use the standard Stellar SEP-24 interactions. Real testnet access for NGNT specifically may require onboarding through Cowrie's developer channel.
- **Integration Requirements**:
  - Implement SEP-10 JWT flow.
  - Implement SEP-24 interactive withdrawal flow to get the Cowrie-hosted web view URL.
  - Poll the transaction status via the SEP-24 `/transaction` endpoint.
  - Nester needs to build a frontend webview or redirect the user to complete KYC/bank details on Cowrie's hosted page.

## 2. MoneyGram on Stellar
MoneyGram allows users to offramp USDC directly to cash or bank accounts via their API (MoneyGram Ramps).

- **Implementation**: Also utilizes SEP-10 and SEP-24.
- **Testnet Flow**: Fully available on the Stellar Testnet. Requires generating Stellar keypairs, establishing a `stellar.toml` file, and registering on the MoneyGram Developer Portal.
- **Integration Requirements**:
  - Wallet domain allowlisting via the MoneyGram Developer Portal.
  - Implementing SEP-10 and SEP-24, similarly to Cowrie.
  - Requires adhering to MoneyGram's specific compliance standards, which might be more demanding for a Nigerian operation without local entity registration.

## Recommendation
**Cowrie via Stellar SEP-24** is recommended as the primary integration path. 

**Rationale:**
1. **Local Expertise**: Cowrie specifically targets the Nigerian market and issues NGNT, which inherently implies better optimization for NGN bank payouts.
2. **Standard SEP-24**: The code to integrate Cowrie is almost identical to any other Stellar anchor, meaning Nester can build a generic `sep24_resolver.go` that can be re-used for MoneyGram or other anchors later if needed.
3. **Regulatory**: Cowrie handles the Nigerian fiat compliance directly. Nester's users will interact with Cowrie's UI for the fiat offramp, keeping Nester out of the regulatory crosshairs.

## Next Steps
Proceed with a full implementation of the `sep24_resolver.go` in the settlement service, using generic SEP-24 models. File a new issue for the complete specification.
