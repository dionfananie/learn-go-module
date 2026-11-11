import http from 'k6/http';
import { check } from 'k6';
import { Trend, Rate } from 'k6/metrics';

// Load test cache stampede:
//   - 1 VU invalidator memanggil PATCH /products/:id/update-stock tiap detik
//     (handler Transaction menghapus key cache setelah commit)
//   - banyak VU reader membaca produk yang sama pada saat bersamaan
//
// Tanpa proteksi (singleflight/lock), setiap invalidasi membuat semua reader
// miss serentak dan menghantam DB. Lihat cache_read_duration p95/p99 dan
// bandingkan dengan MODE=hot.
//
//   k6 run -e BASE_URL=http://localhost:8080 -e VUS=100 -e DURATION=30s loadtest/cache/stampede.js

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const VUS = Number(__ENV.VUS || 100);
const DURATION = __ENV.DURATION || '30s';
const INVALIDATE_RPS = Number(__ENV.INVALIDATE_RPS || 1);
const STOCK = Number(__ENV.STOCK || 1000000);
const SMOKE = Boolean(__ENV.SMOKE);

const readDuration = new Trend('cache_read_duration', true);
const invalidateDuration = new Trend('cache_invalidate_duration', true);
const readOk = new Rate('cache_read_ok');

export const options = {
  scenarios: {
    readers: {
      executor: 'constant-vus',
      exec: 'readHotKey',
      vus: SMOKE ? 2 : VUS,
      duration: SMOKE ? '5s' : DURATION,
    },
    invalidator: {
      executor: 'constant-arrival-rate',
      exec: 'invalidate',
      rate: SMOKE ? 1 : INVALIDATE_RPS,
      timeUnit: '1s',
      duration: SMOKE ? '5s' : DURATION,
      preAllocatedVUs: 2,
      maxVUs: 5,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    cache_read_ok: ['rate>0.99'],
    cache_read_duration: ['p(95)<200', 'p(99)<400'],
    cache_invalidate_duration: ['p(95)<300'],
  },
};

function dummyUser() {
  const suffix = `${Date.now()}${Math.floor(Math.random() * 100)}`;
  return {
    name: `stampede_${suffix}`,
    phone_number: `08${suffix}`.slice(0, 16),
    password: 'Secret12345',
  };
}

function registerAndLogin() {
  const user = dummyUser();

  const reg = http.post(`${BASE_URL}/register`, JSON.stringify(user), {
    headers: { 'Content-Type': 'application/json' },
  });
  if (reg.status !== 201) {
    throw new Error(`register gagal: ${reg.status} ${reg.body}`);
  }

  const login = http.post(
    `${BASE_URL}/login`,
    JSON.stringify({ name: user.name, password: user.password }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (login.status !== 200) {
    throw new Error(`login gagal: ${login.status} ${login.body}`);
  }

  const token = login.json('data.access_token');
  if (!token) {
    throw new Error('login tidak mengembalikan access_token');
  }
  return token;
}

export function setup() {
  const token = registerAndLogin();

  const create = http.post(
    `${BASE_URL}/products`,
    JSON.stringify({
      name: `stampede-hot-${Date.now()}`,
      price: 1000,
      stock: STOCK,
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
    }
  );
  if (create.status !== 201) {
    throw new Error(`create product gagal: ${create.status} ${create.body}`);
  }

  const id = create.json('data.id');
  // Warmup supaya reader pertama tidak dihitung sebagai cold start.
  http.get(`${BASE_URL}/products/${id}`);

  return { token, id };
}

export function readHotKey(data) {
  const res = http.get(`${BASE_URL}/products/${data.id}`, {
    tags: { name: 'GET /products/:id' },
  });
  readOk.add(check(res, { 'status 200': (r) => r.status === 200 }));
  readDuration.add(res.timings.duration);
}

export function invalidate(data) {
  const res = http.patch(
    `${BASE_URL}/products/${data.id}/update-stock`,
    JSON.stringify({ delta: 1 }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${data.token}`,
      },
      tags: { name: 'PATCH /products/:id/update-stock' },
    }
  );
  check(res, { 'invalidate status 200': (r) => r.status === 200 });
  invalidateDuration.add(res.timings.duration);
}
