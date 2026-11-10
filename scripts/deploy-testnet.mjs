#!/usr/bin/env node
// Provisions the public testnet stack end to end and is safe to re-run:
// Neon Postgres -> Render API (+ api.usenester.xyz) -> Vercel env, DNS and
// production deploy -> UptimeRobot keep-alive -> live checks.
//
// Tokens come from ~/.nester/deploy.env (never committed, never printed):
//   NEON_API_KEY, RENDER_API_KEY, VERCEL_TOKEN, UPTIMEROBOT_API_KEY
// Optional overrides: DEPLOY_BRANCH, APP_DOMAIN, API_DOMAIN, ROOT_DOMAIN.
//
// Usage: node scripts/deploy-testnet.mjs [--only=neon,render,vercel,uptime,verify]

import { readFileSync, existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const TOKEN_FILE = join(homedir(), ".nester", "deploy.env");

function loadTokens() {
  if (!existsSync(TOKEN_FILE)) fail(`missing ${TOKEN_FILE}`);
  const env = {};
  for (const line of readFileSync(TOKEN_FILE, "utf8").split(/\r?\n/)) {
    const m = line.match(/^\s*([A-Z0-9_]+)\s*=\s*(.*?)\s*$/);
    if (m && m[2] !== "") env[m[1]] = m[2].replace(/^["']|["']$/g, "");
  }
  return { ...env, ...process.env };
}

const E = loadTokens();
const BRANCH = E.DEPLOY_BRANCH || "feat/savings-rate";
const ROOT_DOMAIN = E.ROOT_DOMAIN || "usenester.xyz";
const APP_DOMAIN = E.APP_DOMAIN || `app.${ROOT_DOMAIN}`;
const API_DOMAIN = E.API_DOMAIN || `api.${ROOT_DOMAIN}`;
const SERVICE_NAME = "nester-api-testnet";
const NEON_PROJECT = "nester-testnet";

const SAVINGS = {
  core: "CAWDB2TAO5MKNULROETJ3BRAT2DNTLI3S7BOHOVM2EDUQUY5FN6DUJ4Q",
  reserve: "CAYEFS7LBHU3XGT2U7ZC55WXMOQ7BPIAEHIWMR7YSQQL5Z3VZYLB3B7P",
  blend: "CCJZEW3FJ4NPPNMKPYVPFJOGYDZKV5DHFHP2NYLNYCKOTFUENI3HQ2YL",
  defindex: "CCNFZP235C52SJUHX3JBMC3UJJATUSKJJESDBXK5OPQRZUWSXXUZIPU7",
  usdcSac: "CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU",
  usdcIssuer: "GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56",
};
const PASSPHRASE = "Test SDF Network ; September 2015";
const RPC = "https://soroban-testnet.stellar.org";
const HORIZON = "https://horizon-testnet.stellar.org";

const only = (process.argv.find((a) => a.startsWith("--only=")) || "").slice(7).split(",").filter(Boolean);
const want = (step) => only.length === 0 || only.includes(step);

function fail(msg) {
  console.error(`\n✗ ${msg}`);
  process.exit(1);
}
function need(...keys) {
  const missing = keys.filter((k) => !E[k]);
  if (missing.length) fail(`set ${missing.join(", ")} in ${TOKEN_FILE}`);
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function http(method, url, { token, body, form, headers = {}, ok = [] } = {}) {
  const init = { method, headers: { Accept: "application/json", ...headers } };
  if (token) init.headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  if (form) {
    init.headers["Content-Type"] = "application/x-www-form-urlencoded";
    init.body = new URLSearchParams(form).toString();
  }
  const res = await fetch(url, init);
  const text = await res.text();
  let data;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text;
  }
  if (!res.ok && !ok.includes(res.status)) {
    const safeUrl = url.replace(/api_key=[^&]+/, "api_key=***");
    fail(`${method} ${safeUrl} -> ${res.status}: ${typeof data === "string" ? data.slice(0, 400) : JSON.stringify(data).slice(0, 400)}`);
  }
  return { status: res.status, data };
}

// ── Neon ─────────────────────────────────────────────────────────────────────

async function neon() {
  need("NEON_API_KEY");
  const api = "https://console.neon.tech/api/v2";
  const t = E.NEON_API_KEY;
  const { data: list } = await http("GET", `${api}/projects?search=${NEON_PROJECT}`, { token: t });
  let project = (list.projects || []).find((p) => p.name === NEON_PROJECT);
  if (!project) {
    console.log(`  creating Neon project ${NEON_PROJECT} (Frankfurt)`);
    const { data } = await http("POST", `${api}/projects`, {
      token: t,
      body: { project: { name: NEON_PROJECT, region_id: "aws-eu-central-1", pg_version: 16 } },
    });
    project = data.project;
  } else {
    console.log(`  Neon project ${NEON_PROJECT} exists`);
  }
  const { data: branches } = await http("GET", `${api}/projects/${project.id}/branches`, { token: t });
  const branch = branches.branches.find((b) => b.default) || branches.branches[0];
  const { data: roles } = await http("GET", `${api}/projects/${project.id}/branches/${branch.id}/roles`, { token: t });
  const role = roles.roles.find((r) => !r.protected) || roles.roles[0];
  const { data: dbs } = await http("GET", `${api}/projects/${project.id}/branches/${branch.id}/databases`, { token: t });
  const db = dbs.databases[0];
  const q = new URLSearchParams({ branch_id: branch.id, database_name: db.name, role_name: role.name, pooled: "false" });
  const { data: conn } = await http("GET", `${api}/projects/${project.id}/connection_uri?${q}`, { token: t });
  const dsn = conn.uri.includes("sslmode=") ? conn.uri : `${conn.uri}${conn.uri.includes("?") ? "&" : "?"}sslmode=require`;
  console.log(`✓ Neon ready (direct connection, database ${db.name})`);
  return dsn;
}

// ── Vercel (discovered early: Render deploys from the same repo) ─────────────

async function vercelProject() {
  need("VERCEL_TOKEN");
  const t = E.VERCEL_TOKEN;
  const { data: teams } = await http("GET", "https://api.vercel.com/v2/teams?limit=100", { token: t });
  const scopes = [null, ...(teams.teams || []).map((x) => x.id)];
  for (const teamId of scopes) {
    const qs = teamId ? `?teamId=${teamId}` : "";
    const { status, data } = await http("GET", `https://api.vercel.com/v9/projects/${APP_DOMAIN}${qs}`, { token: t, ok: [404, 403] });
    if (status === 200) return { teamId, project: data };
    const { data: projects } = await http("GET", `https://api.vercel.com/v9/projects${qs}${qs ? "&" : "?"}limit=100`, { token: t, ok: [403] });
    for (const p of projects?.projects || []) {
      const sep = qs ? "&" : "?";
      const { data: doms } = await http("GET", `https://api.vercel.com/v9/projects/${p.id}/domains${qs}${sep}limit=100`, { token: t, ok: [403, 404] });
      if ((doms?.domains || []).some((d) => d.name === APP_DOMAIN)) {
        const { data: full } = await http("GET", `https://api.vercel.com/v9/projects/${p.id}${qs}`, { token: t });
        return { teamId, project: full };
      }
    }
  }
  fail(`no Vercel project serving ${APP_DOMAIN} is visible to this token`);
}

function repoUrl(project) {
  const link = project.link || {};
  if (link.type !== "github") fail(`Vercel project ${project.name} is not linked to GitHub`);
  return `https://github.com/${link.org}/${link.repo}`;
}

// ── Render ───────────────────────────────────────────────────────────────────

async function render(dsn, repo) {
  need("RENDER_API_KEY");
  const api = "https://api.render.com/v1";
  const t = E.RENDER_API_KEY;
  const { data: owners } = await http("GET", `${api}/owners?limit=20`, { token: t });
  const owner = owners[0]?.owner;
  if (!owner) fail("Render account has no workspace");

  const { data: found } = await http("GET", `${api}/services?name=${SERVICE_NAME}&ownerId=${owner.id}`, { token: t });
  let service = (found || []).map((x) => x.service).find((s) => s.name === SERVICE_NAME);

  if (!service) {
    if (!dsn) fail("Neon DSN required to create the Render service (run without --only or include neon)");
    console.log(`  creating Render service ${SERVICE_NAME} from ${repo}@${BRANCH}`);
    const { data } = await http("POST", `${api}/services`, {
      token: t,
      body: {
        type: "web_service",
        name: SERVICE_NAME,
        ownerId: owner.id,
        repo,
        branch: BRANCH,
        autoDeployTrigger: "commit",
        envVars: [
          { key: "APP_ENV", value: "staging" },
          { key: "SERVER_PORT", value: "8080" },
          { key: "PORT", value: "8080" },
          { key: "RUN_MIGRATIONS", value: "true" },
          { key: "DATABASE_DSN", value: dsn },
          { key: "AUTH_JWT_SECRET", generateValue: true },
          { key: "ALLOWED_ORIGINS", value: `https://${APP_DOMAIN},https://localhost:3002` },
          { key: "STELLAR_NETWORK_PASSPHRASE", value: PASSPHRASE },
          { key: "STELLAR_RPC_URL", value: RPC },
          { key: "STELLAR_HORIZON_URL", value: HORIZON },
          { key: "SAVINGS_CORE_CONTRACT_ID", value: SAVINGS.core },
          { key: "SAVINGS_ADAPTER_NAMES", value: `${SAVINGS.blend}=Blend,${SAVINGS.defindex}=DeFindex` },
          { key: "LOG_FORMAT", value: "json" },
        ],
        serviceDetails: {
          runtime: "docker",
          plan: "free",
          region: "frankfurt",
          numInstances: 1,
          healthCheckPath: "/healthz",
          envSpecificDetails: { dockerfilePath: "./apps/api/Dockerfile", dockerContext: "./apps/api" },
        },
      },
    });
    service = data.service;
  } else {
    console.log(`  Render service ${SERVICE_NAME} exists`);
    if (dsn) await http("PUT", `${api}/services/${service.id}/env-vars/DATABASE_DSN`, { token: t, body: { value: dsn } });
  }

  const { data: domains } = await http("GET", `${api}/services/${service.id}/custom-domains`, { token: t });
  if (!(domains || []).some((d) => (d.customDomain || d).name === API_DOMAIN)) {
    await http("POST", `${api}/services/${service.id}/custom-domains`, { token: t, body: { name: API_DOMAIN } });
    console.log(`  added custom domain ${API_DOMAIN}`);
  }

  console.log("  waiting for the Render deploy (first build takes a few minutes)");
  for (let i = 0; i < 90; i++) {
    const { data: deploys } = await http("GET", `${api}/services/${service.id}/deploys?limit=1`, { token: t });
    const d = deploys[0]?.deploy;
    if (d?.status === "live") break;
    if (["build_failed", "update_failed", "canceled", "pre_deploy_failed"].includes(d?.status)) {
      fail(`Render deploy ${d.id} ${d.status}: ${service.dashboardUrl}`);
    }
    await sleep(20_000);
  }
  const host = new URL(service.serviceDetails.url).host;
  console.log(`✓ Render live at https://${host}`);
  return host;
}

// ── Vercel env, DNS, deploy ──────────────────────────────────────────────────

async function vercel({ teamId, project }, renderHost) {
  const t = E.VERCEL_TOKEN;
  const qs = teamId ? `teamId=${teamId}` : "";
  const env = {
    NEXT_PUBLIC_API_URL: `https://${API_DOMAIN}/api/v1`,
    NEXT_PUBLIC_NETWORK: "testnet",
    NEXT_PUBLIC_STELLAR_NETWORK: PASSPHRASE,
    NEXT_PUBLIC_STELLAR_RPC_URL: RPC,
    NEXT_PUBLIC_STELLAR_HORIZON_URL: HORIZON,
    NEXT_PUBLIC_SAVINGS_CORE_CONTRACT_ID: SAVINGS.core,
    NEXT_PUBLIC_SAVINGS_RESERVE_CONTRACT_ID: SAVINGS.reserve,
    NEXT_PUBLIC_ADAPTER_BLEND_CONTRACT_ID: SAVINGS.blend,
    NEXT_PUBLIC_ADAPTER_DEFINDEX_CONTRACT_ID: SAVINGS.defindex,
    NEXT_PUBLIC_USDC_CONTRACT_ID: SAVINGS.usdcSac,
    NEXT_PUBLIC_USDC_ISSUER: SAVINGS.usdcIssuer,
  };
  await http("POST", `https://api.vercel.com/v10/projects/${project.id}/env?upsert=true${qs ? "&" + qs : ""}`, {
    token: t,
    body: Object.entries(env).map(([key, value]) => ({ key, value, type: "plain", target: ["production", "preview"] })),
  });
  console.log(`  set ${Object.keys(env).length} env vars on ${project.name}`);

  if (renderHost) {
    const sub = API_DOMAIN.replace(`.${ROOT_DOMAIN}`, "");
    const { data: recs } = await http("GET", `https://api.vercel.com/v4/domains/${ROOT_DOMAIN}/records?limit=100${qs ? "&" + qs : ""}`, { token: t });
    const existing = (recs.records || []).find((r) => r.name === sub);
    if (existing && existing.value !== renderHost) {
      await http("DELETE", `https://api.vercel.com/v2/domains/${ROOT_DOMAIN}/records/${existing.id}${qs ? "?" + qs : ""}`, { token: t });
    }
    if (!existing || existing.value !== renderHost) {
      await http("POST", `https://api.vercel.com/v2/domains/${ROOT_DOMAIN}/records${qs ? "?" + qs : ""}`, {
        token: t,
        body: { name: sub, type: "CNAME", value: renderHost, ttl: 60 },
      });
      console.log(`  DNS ${API_DOMAIN} CNAME ${renderHost}`);
    }
  }

  const { data: dep } = await http("POST", `https://api.vercel.com/v13/deployments?forceNew=1${qs ? "&" + qs : ""}`, {
    token: t,
    body: {
      name: project.name,
      project: project.id,
      target: "production",
      gitSource: { type: "github", repoId: project.link.repoId, ref: BRANCH },
    },
  });
  console.log(`  Vercel production deploy of ${BRANCH} started`);
  for (let i = 0; i < 90; i++) {
    const { data: d } = await http("GET", `https://api.vercel.com/v13/deployments/${dep.id}${qs ? "?" + qs : ""}`, { token: t });
    if (d.readyState === "READY") break;
    if (["ERROR", "CANCELED"].includes(d.readyState)) fail(`Vercel deploy ${d.readyState}: https://${d.url}`);
    await sleep(15_000);
  }
  console.log(`✓ Vercel live at https://${APP_DOMAIN}`);
}

// ── UptimeRobot ──────────────────────────────────────────────────────────────

async function uptime() {
  need("UPTIMEROBOT_API_KEY");
  const url = `https://${API_DOMAIN}/healthz`;
  const base = { api_key: E.UPTIMEROBOT_API_KEY, format: "json" };
  const { data } = await http("POST", "https://api.uptimerobot.com/v2/getMonitors", { form: { ...base, search: API_DOMAIN } });
  if (data.stat !== "ok") fail(`UptimeRobot: ${JSON.stringify(data.error || data)}`);
  if ((data.monitors || []).some((m) => m.url === url)) {
    console.log("✓ UptimeRobot monitor exists");
    return;
  }
  const { data: made } = await http("POST", "https://api.uptimerobot.com/v2/newMonitor", {
    form: { ...base, type: "1", url, friendly_name: "Nester API (testnet)", interval: "300" },
  });
  if (made.stat !== "ok") fail(`UptimeRobot: ${JSON.stringify(made.error || made)}`);
  console.log(`✓ UptimeRobot pinging ${url} every 5 minutes`);
}

// ── Verify ───────────────────────────────────────────────────────────────────

async function verify() {
  const check = async (label, url, test) => {
    for (let i = 0; i < 30; i++) {
      try {
        const res = await fetch(url, { headers: { Origin: `https://${APP_DOMAIN}` } });
        const body = await res.text();
        if (res.ok && test(body, res)) return console.log(`✓ ${label}`);
      } catch {
        // DNS and TLS for a new custom domain can take a few minutes.
      }
      await sleep(20_000);
    }
    fail(`${label} not passing: ${url}`);
  };
  await check("API health", `https://${API_DOMAIN}/healthz`, () => true);
  await check("API savings data", `https://${API_DOMAIN}/api/v1/savings`, (b) => b.includes(SAVINGS.blend));
  await check("API CORS for the dapp", `https://${API_DOMAIN}/api/v1/savings`, (_, r) => r.headers.get("access-control-allow-origin") === `https://${APP_DOMAIN}`);
  await check("Dapp", `https://${APP_DOMAIN}/dashboard`, () => true);
}

// ── Run ──────────────────────────────────────────────────────────────────────

let dsn = null;
let renderHost = null;
let vp = null;
if (want("neon")) dsn = await neon();
if (want("render") || want("vercel")) vp = await vercelProject();
if (want("render")) renderHost = await render(dsn, repoUrl(vp.project));
if (want("vercel")) await vercel(vp, renderHost);
if (want("uptime")) await uptime();
if (want("verify")) await verify();
console.log("\nDone. Testers: https://" + APP_DOMAIN);
