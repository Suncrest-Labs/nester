# Mainnet Risk Disclosures

Depositing into Nester vaults on **Stellar Mainnet** involves real funds (`USDC`, `XLM`, and other supported assets). Please review the following risks before signing any Mainnet transaction:

1. **Real Funds & Irreversibility**
   - Mainnet transactions move real economic value on the Stellar public network.
   - Once signed by your wallet and confirmed on-chain, transactions are **irreversible**. Neither Suncrest Labs nor any third party can reverse a signed transaction.

2. **Smart Contract & Adapter Risk**
   - Funds deposited into Nester vaults are governed by Soroban smart contracts and allocated to downstream yield strategies (such as Blend lending pools and liquidity adapters).
   - Despite security reviews and testing, smart contracts and external DeFi protocols carry inherent technical, economic, and oracle risks.

3. **How to Read the Vault Deposit Cap**
   - During staged Mainnet rollouts, each vault enforces an on-chain deposit cap (`MaxDeposit`).
   - Any deposit that would cause total vault assets to exceed the active cap is rejected closed by the contract (`DepositCapExceeded`). Check the vault details panel in the dapp for current TVL and remaining capacity before depositing.

4. **Security & Audit Documentation**
   - Review our security architecture and audit scope at [`/security`](/security).
   - Usage of the Nester interface and smart contracts is subject to our [Terms of Service](/terms).
