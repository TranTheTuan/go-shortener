import http from 'k6/http';
import { check } from 'k6';

// Bearer tokens, one per real user, so each VU authenticates as a distinct
// Keycloak user (own quota + dedup). Pass a comma-separated list:
//   k6 run -e TOKENS="$T1,$T2,$T3" test/write-heavy.js
// A single token also works (-e TOKEN=...) but then it's one user (10/day quota).
const TOKENS = (__ENV.TOKENS || __ENV.TOKEN || '').split(',').filter(Boolean);
if (TOKENS.length === 0) {
  throw new Error('Set -e TOKENS="tok1,tok2,..." (or -e TOKEN=...) with Keycloak access token(s)');
}

export const options = {
  vus: 10,           // 100 user ảo (connections)
  duration: '10s',    // Bắn liên tục trong 30 giây
};

export default function () {
  const payload = JSON.stringify({
    // Unique per virtual-user (__VU) + iteration (__ITER) so every request
    // creates a NEW link (no dedup hit) — real write load, spread across "users".
    url: `example/promo/u${__VU}-${__ITER}`,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      // Spread VUs across the token pool → each acts as a different user.
      'Authorization': `Bearer ${TOKENS[0]}`,
    },
  };

  const res = http.post('https://go-short.tonytran.xyz/api/links', payload, params);

  check(res, {
    'is status 200/201': (r) => r.status === 200 || r.status === 201,
  });
}