import http from 'k6/http';
import { check } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

// Latency test for the public redirect endpoint: GET /:code -> 302.
// Runs at sustainable load (~80% of cluster capacity) to measure real p(95).
// Use read-heavy.js for stress testing at beyond-capacity load.
//
// Usage:
//   k6 run -e CODE=Ab3xY7q test/read-latency.js
//   k6 run -e CODE=Ab3xY7q -e NODE_IP=192.168.1.201 test/read-latency.js
//   k6 run -e CODES="Ab3xY7q,kQ9zL2p" test/read-latency.js
//   k6 run -e TARGET_RPS=1500 test/read-latency.js   # override sustained rate
const NODE_IP = __ENV.NODE_IP || '';
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

const host = BASE.split('//')[1].split('/')[0].split(':')[0];

// ~80% of observed 2530 req/s capacity → 2000 req/s sustained
const TARGET_RPS = parseInt(__ENV.TARGET_RPS || '2000', 10);

export const options = {
  ...(NODE_IP ? {
    hosts: { [host]: NODE_IP },
    insecureSkipTLSVerify: true,
  } : {}),
  thresholds: {
    http_req_failed:   ['rate<0.01'],   // <1% non-302
    http_req_duration: ['p(95)<200'],   // 95th pct under 200ms at sustainable load
  },
  scenarios: {
    sustained: {
      executor: 'constant-arrival-rate',
      rate: TARGET_RPS,
      timeUnit: '1s',
      duration: '60s',
      preAllocatedVUs: 200,
      maxVUs: 500,
    },
  },
};

export default function () {
  const code = codes[Math.floor(Math.random() * codes.length)];
  const res = http.get(`${BASE}/${code}`, { redirects: 0 });
  check(res, {
    'is 302 redirect':    (r) => r.status === 302,
    'has Location header': (r) => !!r.headers['Location'],
  });
}

export function handleSummary(data) {
  return { stdout: textSummary(data, { indent: ' ', enableColors: true }) };
}
