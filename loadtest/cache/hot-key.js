import http from 'k6/http';
import { check } from 'k6';
import { Trend, Rate } from 'k6/metrics';

// Load test cache HIT: semua VU membaca beberapa produk yang sama terus-menerus.
// Setelah warmup di setup(), request seharusnya dilayani dari Redis.
//
//   k6 run -e BASE_URL=http://localhost:8080 -e VUS=100 -e DURATION=30s loadtest/cache/hot-key.js
//
// Bandingkan hasilnya dengan MODE=stampede (cache selalu di-invalidate).

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const VUS = Number(__ENV.VUS || 100);
const DURATION = __ENV.DURATION || '30s';
const HOT_KEYS = Number(__ENV.HOT_KEYS || 5);
const SMOKE = Boolean(__ENV.SMOKE);

const readDuration = new Trend('cache_read_duration', true);
const readOk = new Rate('cache_read_ok');

export const options = {
  scenarios: {
    hot_key_read: {
      executor: 'constant-vus',
      vus: SMOKE ? 2 : VUS,
      duration: SMOKE ? '5s' : DURATION,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    checks: ['rate>0.99'],
    // Cache hit seharusnya jauh di bawah 10 ms di lokal.
    cache_read_duration: ['p(95)<50', 'p(99)<100'],
  },
};

export function setup() {
  const res = http.get(`${BASE_URL}/products`);
  if (res.status !== 200) {
    throw new Error(`GET /products gagal: ${res.status}`);
  }

  const products = res.json('data') || [];
  if (products.length === 0) {
    throw new Error('tidak ada produk; jalankan seed dulu (go run db/seed.go)');
  }

  const ids = products.slice(0, HOT_KEYS).map((p) => p.id);

  // Warmup: isi cache sekali supaya pengukuran dimulai dari kondisi hit.
  ids.forEach((id) => http.get(`${BASE_URL}/products/${id}`));

  return { ids };
}

export default function (data) {
  const id = data.ids[Math.floor(Math.random() * data.ids.length)];
  const res = http.get(`${BASE_URL}/products/${id}`, {
    tags: { name: 'GET /products/:id' },
  });

  readOk.add(check(res, { 'status 200': (r) => r.status === 200 }));
  readDuration.add(res.timings.duration);
}
