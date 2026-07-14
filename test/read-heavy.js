import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

// Read-load test for the public redirect endpoint: GET /:code -> 302.
// Provide short codes one of three ways (checked in this order):
//   -e CODE=Ab3xY7q
//   -e CODES="Ab3xY7q,kQ9zL2p,..."
//   -e CODES_FILE=test/codes.txt        (one code per line)
//
// To bypass Cloudflare and hit the cluster directly:
//   -e NODE_IP=<any-k3s-node-public-ip>
const NODE_IP = __ENV.NODE_IP || '';
// When hitting cluster directly (NODE_IP set), Cloudflare is bypassed so TLS
// terminates at Cloudflare only — use http:// to reach Traefik on port 80.
const BASE = __ENV.BASE || (NODE_IP ? 'http://go-short.tonytran.xyz' : 'https://go-short.tonytran.xyz');

const codes = (function () {
  if (__ENV.CODES_FILE) {
    return open(__ENV.CODES_FILE).split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
  }
  return (__ENV.CODES || __ENV.CODE || '').split(',').map((s) => s.trim()).filter(Boolean);
})();
if (codes.length === 0) {
  throw new Error('Set -e CODE=abc123, -e CODES="a,b,c", or -e CODES_FILE=codes.txt');
}

// errors tagged by HTTP status + API error code (registered as submetrics below).
const errors = new Counter('errors');

const host = BASE.split('//')[1].split('/')[0].split(':')[0];

export const options = {
  ...(NODE_IP ? {
    hosts: { [host]: NODE_IP },
    insecureSkipTLSVerify: true,  // origin cert may not match when bypassing Cloudflare
  } : {}),
  thresholds: {
    // Stress test: only gate on error rate, not latency.
    // At 12000 iter/s the cluster is overloaded by design — latency will be high.
    // Use read-latency.js to measure p(95) at sustainable load.
    http_req_failed: ['rate<0.01'],
  },
  scenarios: {
    ramp: {
      executor: 'ramping-arrival-rate',
      startRate: 100, timeUnit: '1s',
      preAllocatedVUs: 200, maxVUs: 2000,
      stages: [
        { target: 2000, duration: '20s' },
        { target: 8000, duration: '30s' },
        { target: 12000, duration: '30s' },
      ],
    },
  },
};

export default function () {
  const code = codes[Math.floor(Math.random() * codes.length)]; // spread across the pool

  // redirects: 0 → do NOT follow the 302 to the external URL; we test OUR 302,
  // not the destination site (and avoid hammering third parties).
  const res = http.get(`${BASE}/${code}`, { redirects: 0 });

  const ok = check(res, {
    'is 302 redirect': (r) => r.status === 302,
    'has Location header': (r) => !!r.headers['Location'],
  });

  if (res.status !== 302) {
    let errCode = 'NONE';
    try {
      errCode = res.json('error.code') || 'NONE'; // API envelope: {error:{code,message}}
    } catch (_) {
      /* non-JSON body → NONE */
    }
    errors.add(1, { status: String(res.status), code: errCode });
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
    : '(no errors — all 302)\n';
  return out;
}
