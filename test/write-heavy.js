import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

// Bearer tokens, one per real user, so each VU authenticates as a distinct
// Keycloak user (own quota + dedup). Pass a comma-separated list:
//   k6 run -e TOKENS="$T1,$T2,$T3" test/write-heavy.js
// A single token also works (-e TOKEN=...) but then it's one user (10/day quota).
const TOKENS = (__ENV.TOKENS || __ENV.TOKEN || '').split(',').filter(Boolean);
if (TOKENS.length === 0) {
  throw new Error('Set -e TOKENS="tok1,tok2,..." (or -e TOKEN=...) with Keycloak access token(s)');
}

// errors is tagged by HTTP status + the API error code. k6 only aggregates a
// tagged metric per-tag in the summary via submetrics, which are created by
// declaring them as thresholds below — one per known error type.
const errors = new Counter('errors');

// Known (status, code) pairs the API returns, plus NONE for non-JSON bodies
// (gateway/network). Each becomes a summary submetric.
const ERROR_TYPES = [
  ['400', 'BAD_REQUEST'],
  ['401', 'UNAUTHORIZED'],
  ['404', 'NOT_FOUND'],
  ['409', 'CONFLICT'],
  ['410', 'GONE'],
  ['410', 'DISABLED'],
  ['422', 'UNPROCESSABLE'],
  ['429', 'QUOTA_EXCEEDED'],
  ['500', 'INTERNAL'],
  ['0', 'NONE'],   // network error / timeout (no response)
  ['502', 'NONE'],
  ['503', 'NONE'],
  ['504', 'NONE'],
];

export const options = {
  vus: 10,
  duration: '10s',
  // `count>=0` always passes — it just registers the per-type submetric so it
  // shows up in handleSummary().
  thresholds: Object.fromEntries(
    ERROR_TYPES.map(([s, c]) => [`errors{status:${s},code:${c}}`, ['count>=0']]),
  ),
};

export default function () {
  const payload = JSON.stringify({
    // Unique per virtual-user (__VU) + iteration (__ITER) so every request
    // creates a NEW link (no dedup hit) — real write load, spread across "users".
    url: `https://example.com/promo/u${__VU}-${__ITER}`,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      // Spread VUs across the token pool → each acts as a different user.
      'Authorization': `Bearer ${TOKENS[(__VU - 1) % TOKENS.length]}`,
    },
  };

  const res = http.post('https://go-short.tonytran.xyz/api/links', payload, params);

  const ok = check(res, {
    'is status 200/201': (r) => r.status === 200 || r.status === 201,
  });

  if (!ok) {
    let code = 'NONE';
    try {
      code = res.json('error.code') || 'NONE'; // API envelope: {error:{code,message}}
    } catch (_) {
      /* non-JSON body → NONE */
    }
    errors.add(1, { status: String(res.status), code });
  }
}

// handleSummary keeps k6's default summary and appends an errors-by-type block.
export function handleSummary(data) {
  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }) + errorsByType(data),
  };
}

// errorsByType renders the `errors{status,code}` submetrics as one line each.
function errorsByType(data) {
  const rows = [];
  for (const [key, met] of Object.entries(data.metrics)) {
    if (!key.startsWith('errors{') || !(met.values.count > 0)) continue;
    const status = (key.match(/status:([^,}]+)/) || [])[1] || '?';
    const code = (key.match(/code:([^,}]+)/) || [])[1] || '?';
    rows.push({ status, code, n: met.values.count });
  }
  rows.sort((a, b) => b.n - a.n);

  let out = '\n=== Errors by type ===\n';
  out += rows.length
    ? rows.map((e) => `HTTP ${e.status} ${e.code} — Quantity: ${e.n}`).join('\n') + '\n'
    : '(no errors)\n';
  return out;
}
