import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 100,           // 100 user ảo (connections)
  duration: '30s',    // Bắn liên tục trong 30 giây
};

export default function () {
  const payload = JSON.stringify({
    url: `https://example.com/promo`, // Tạo URL unique để tránh cache DB
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': 'dev-key-1',
    },
  };

  const res = http.post('http://localhost:8080/api/links', payload, params);
  
  check(res, {
    'is status 200/201': (r) => r.status === 200 || r.status === 201,
  });
}