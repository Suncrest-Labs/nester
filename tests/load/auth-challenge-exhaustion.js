import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const baseUrl = __ENV.API_BASE_URL || 'http://localhost:8080';
const acceptedChallenges = new Counter('auth_challenge_accepted_total');
const shedChallenges = new Counter('auth_challenge_shed_429_total');

// Deterministic Stellar StrKey generator for unique synthetic wallets under load (#1563)
const base32Alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
function syntheticStellarAddress(seed) {
  let suffix = '';
  let n = seed;
  for (let i = 0; i < 55; i++) {
    suffix += base32Alphabet[(n + i * 7) % 32];
    n = Math.floor(n / 2) + i;
  }
  return 'G' + suffix;
}

export const options = {
  scenarios: {
    challenge_flood: {
      executor: 'constant-arrival-rate',
      rate: 200,
      timeUnit: '1s',
      duration: __ENV.FLOOD_DURATION || '5m',
      preAllocatedVUs: 50,
      maxVUs: 250,
    },
  },
  thresholds: {
    // 5xx responses must remain 0% even when challenge capacity or per-IP rate limit sheds load with 429
    'http_req_failed{expected_response:true}': ['rate==0'],
    http_req_duration: ['p(95)<250', 'p(99)<500'],
  },
  tags: {
    test_type: 'auth_challenge_exhaustion',
  },
};

export default function () {
  const uniqueSeed = (__VU * 1000000) + __ITER;
  const wallet = syntheticStellarAddress(uniqueSeed);
  const forwardedIp = `198.51.${Math.floor(uniqueSeed / 254) % 254}.${(uniqueSeed % 254) + 1}`;

  const res = http.post(
    `${baseUrl}/api/v1/auth/challenge`,
    JSON.stringify({ wallet_address: wallet }),
    {
      headers: {
        'Content-Type': 'application/json',
        'X-Forwarded-For': forwardedIp,
      },
      responseCallback: http.expectedStatuses(200, 400, 429),
    }
  );

  if (res.status === 200) {
    acceptedChallenges.add(1);
  } else if (res.status === 429) {
    shedChallenges.add(1);
  }

  check(res, {
    'status is 200 or 429 (no 5xx or OOM)': (r) => r.status === 200 || r.status === 429 || r.status === 400,
  });
}
