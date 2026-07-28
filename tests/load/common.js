import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';

// URLs and credentials deliberately come only from the environment. Never point
// load tests at production or place real tokens/keys in this repository.
export const apiBase = (__ENV.API_BASE_URL || 'http://localhost:8080/api/v1').replace(/\/$/, '');
export const intelligenceBase = (__ENV.INTELLIGENCE_BASE_URL || 'http://localhost:8000/intelligence').replace(/\/$/, '');
export const wsURL = __ENV.WS_URL || apiBase.replace(/^http/, 'ws').replace(/\/api\/v1$/, '/ws');
export const authHeaders = __ENV.AUTH_TOKEN ? { Authorization: `Bearer ${__ENV.AUTH_TOKEN}` } : {};
export const vaultID = __ENV.VAULT_ID || '00000000-0000-0000-0000-000000000000';
export const portfolioURL = __ENV.PORTFOLIO_URL || `${apiBase}/portfolio/summary`;
// The API's deposit route is vault-scoped. Override only when a deployment exposes
// a compatibility /transactions/deposit route.
export const depositURL = __ENV.DEPOSIT_URL || `${apiBase}/vaults/${vaultID}/deposit`;

const jsonHeaders = { 'Content-Type': 'application/json', ...authHeaders };
const expected = (response, name) => check(response, {
  [`${name}: successful response`]: r => r.status >= 200 && r.status < 300,
});

export function requestFor(name) {
  switch (name) {
    case 'vaults': return expected(http.get(`${apiBase}/vaults`, { headers: authHeaders, tags: { endpoint: 'vaults' } }), name);
    case 'portfolio': return expected(http.get(portfolioURL, { headers: authHeaders, tags: { endpoint: 'portfolio' } }), name);
    case 'challenge': {
      // Supply a comma-separated pool of valid Stellar addresses to exercise
      // distinct Redis keys. The default is a valid testnet-format address.
      const wallets = (__ENV.CHALLENGE_WALLETS || 'GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN').split(',');
      const wallet = wallets[(__VU + __ITER) % wallets.length].trim();
      return expected(http.post(`${apiBase}/auth/challenge`, JSON.stringify({ wallet_address: wallet }), { headers: jsonHeaders, tags: { endpoint: 'challenge' } }), name);
    }
    case 'deposit': return expected(http.post(depositURL, JSON.stringify({ amount: __ENV.DEPOSIT_AMOUNT || '1', asset: __ENV.DEPOSIT_ASSET || 'USDC' }), { headers: jsonHeaders, tags: { endpoint: 'deposit' } }), name);
    case 'chat': return expected(http.post(`${intelligenceBase}/chat`, JSON.stringify({ message: __ENV.CHAT_MESSAGE || 'Load-test health check' }), { headers: jsonHeaders, timeout: '60s', tags: { endpoint: 'chat' } }), name);
    default: throw new Error(`unknown load-test endpoint: ${name}`);
  }
}

export function websocket() {
  const response = ws.connect(wsURL, { headers: authHeaders, tags: { endpoint: 'websocket' } }, socket => {
    socket.on('open', () => {
      if (__ENV.WS_CHANNEL) socket.send(JSON.stringify({ type: 'subscribe', channel: __ENV.WS_CHANNEL }));
    });
    socket.setTimeout(() => socket.close(), Number(__ENV.WS_HOLD_MS || 30000));
    socket.on('error', error => console.error(`websocket error: ${error.error()}`));
  });
  check(response, { 'websocket: connected (101)': r => r && r.status === 101 });
  sleep(0.1);
}

export const endpointTargets = { deposit: 50, vaults: 500, portfolio: 200, chat: 20, challenge: 200 };
export const readThresholds = {
  'http_req_duration{endpoint:vaults}': ['p(95)<500'],
  'http_req_duration{endpoint:portfolio}': ['p(95)<500'],
  'http_req_failed': ['rate<0.01'],
  'checks': ['rate>0.99'],
  'ws_connecting': ['p(95)<1000'],
};
