# Public testnet deployment

Anyone can test Nester on Stellar testnet at https://app.usenester.xyz. It costs nothing to host.

| Piece | Host | Plan |
|---|---|---|
| Dapp | Vercel, existing project at app.usenester.xyz | Hobby |
| API | Render web service from `render.yaml` | Free |
| Postgres | Neon | Free |
| Contracts | Stellar testnet (`packages/contracts/scripts/deployed-testnet.env`) | — |

Redis is optional and isn't used.

## Automated deploy

Put one token per service in `~/.nester/deploy.env` (`NEON_API_KEY`, `RENDER_API_KEY`, `VERCEL_TOKEN`, `UPTIMEROBOT_API_KEY`), then run:

```
node scripts/deploy-testnet.mjs            # everything, safe to re-run
node scripts/deploy-testnet.mjs --only=verify
```

It creates or reuses the Neon project, the Render service and its `api` domain, the Vercel env vars and DNS record, a production deploy of `DEPLOY_BRANCH` (default `feat/savings-rate`, which must be pushed), and the UptimeRobot monitor, then checks the live API and dapp. The manual steps below describe the same thing.

## One-time setup (manual equivalent)

### 1. Database (Neon)

1. Sign in at https://neon.tech with GitHub.
2. Create a project named `nester-testnet` in region `AWS Europe Central (Frankfurt)`.
3. Copy the **direct** connection string. In the connection dialog, turn off "Connection pooling". It looks like this:
   `postgresql://USER:PASSWORD@ep-xxx.eu-central-1.aws.neon.tech/neondb?sslmode=require`

   Migrations take advisory locks. The pooled `-pooler` host breaks those locks, so don't use it.

### 2. API (Render)

1. Sign in at https://render.com with GitHub. Grant it access to the `nester` repository.
2. Go to **New → Blueprint**, pick the repository, then pick the branch that holds `render.yaml`.
3. When Render prompts for `DATABASE_DSN`, paste the Neon string. Render generates `AUTH_JWT_SECRET` itself.
4. Deploy. On first boot the service runs every migration, then serves `https://nester-api-testnet.onrender.com/healthz`.
5. Add the custom domain:
   1. In Render, go to **Settings → Custom domains** and add `api.usenester.xyz`.
   2. In Vercel, go to **Domains → usenester.xyz → DNS** and add a `CNAME` record, `api` → `nester-api-testnet.onrender.com`.

Free services sleep after 15 minutes without traffic. That would stall the hourly rate snapshots, so keep it awake:

1. Create a free monitor at https://uptimerobot.com: HTTP(s), URL `https://api.usenester.xyz/healthz`, checked every 5 minutes.
2. One always-on service stays inside Render's 750 free hours a month.

### 3. Dapp (Vercel)

In the `app.usenester.xyz` project, go to **Settings → Environment Variables**. Set these for Production and Preview:

```
NEXT_PUBLIC_API_URL=https://api.usenester.xyz/api/v1
NEXT_PUBLIC_NETWORK=testnet
NEXT_PUBLIC_STELLAR_NETWORK=Test SDF Network ; September 2015
NEXT_PUBLIC_STELLAR_RPC_URL=https://soroban-testnet.stellar.org
NEXT_PUBLIC_STELLAR_HORIZON_URL=https://horizon-testnet.stellar.org
NEXT_PUBLIC_SAVINGS_CORE_CONTRACT_ID=CAWDB2TAO5MKNULROETJ3BRAT2DNTLI3S7BOHOVM2EDUQUY5FN6DUJ4Q
NEXT_PUBLIC_SAVINGS_RESERVE_CONTRACT_ID=CAYEFS7LBHU3XGT2U7ZC55WXMOQ7BPIAEHIWMR7YSQQL5Z3VZYLB3B7P
NEXT_PUBLIC_ADAPTER_BLEND_CONTRACT_ID=CCJZEW3FJ4NPPNMKPYVPFJOGYDZKV5DHFHP2NYLNYCKOTFUENI3HQ2YL
NEXT_PUBLIC_ADAPTER_DEFINDEX_CONTRACT_ID=CCNFZP235C52SJUHX3JBMC3UJJATUSKJJESDBXK5OPQRZUWSXXUZIPU7
NEXT_PUBLIC_USDC_CONTRACT_ID=CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU
NEXT_PUBLIC_USDC_ISSUER=GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56
```

Then redeploy the branch you want live. Until `feat/savings-rate` is merged, deploy that branch as Production.

## Tester instructions

1. Install Freighter (https://freighter.app) and switch it to **Testnet**.
2. Fund the account with test XLM. Freighter offers Friendbot, or use https://friendbot.stellar.org/?addr=YOUR_ADDRESS.
3. Open https://testnet.blend.capital, connect, and click the faucet banner. One signature adds the USDC trustline and sends 1,000 test USDC.
4. Open https://app.usenester.xyz, connect, and sign in. Then deposit and withdraw on **Savings**, check **Activity**, compare rates on **Markets**, and set a **Goal**.

## Verifying a deploy

```
curl https://api.usenester.xyz/healthz
curl https://api.usenester.xyz/api/v1/savings   # totalAssets, adapters Blend + DeFindex
```

The first request after a sleep can take about 50 seconds while Render wakes the service. With the uptime monitor running, that should not happen.
