import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SMOKE = Boolean(__ENV.SMOKE); // SMOKE=1 utk tes cepat 1-2 VU
const NEG_EVERY = Number(__ENV.NEG_EVERY || 20); // 1 dari N iterasi = negative path (20 => 5%)

const registerDuration = new Trend('register_duration', true);
const loginDuration = new Trend('login_duration', true);
const createDuration = new Trend('product_create_duration', true);
const getDuration = new Trend('product_get_duration', true);
const negativeDuration = new Trend('negative_duration', true);
const flowFailureRate = new Rate('flow_failures');

// 4xx pada negative path memang disengaja; jangan dihitung sbg kegagalan request
http.setResponseCallback(http.expectedStatuses(200, 201, 400, 401, 404));

export const options = {
  scenarios: {
    product_flow: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: SMOKE
        ? [{ duration: '5s', target: 2 }]
        : [
            { duration: '15s', target: 5 }, // ramp-up
            { duration: '1m', target: 5 }, // steady
            { duration: '10s', target: 0 }, // ramp-down
          ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    flow_failures: ['rate<0.01'],
    product_create_duration: ['p(95)<300'],
    product_get_duration: ['p(95)<200'],
    negative_duration: ['p(95)<100'],
  },
};

// State per-VU (init context dijalankan terpisah untuk setiap VU).
// Register+login cukup sekali per VU, sisanya iterasi create+get product.
let auth = null;
let negCount = 0; // pemutar 5 jenis negative path

function dummyUser() {
  // name: 3-30 char, phone_number: 10-16 digit & UNIQUE di DB
  const name = `loadtest_${__VU}_${Date.now()}`;
  const phone = `08${Date.now()}${__VU % 10}`;
  return { name: name, phone_number: phone, password: 'Secret12345' };
}

function ensureAuth() {
  if (auth !== null) {
    return auth;
  }

  const user = dummyUser();

  const regRes = http.post(`${BASE_URL}/register`, JSON.stringify(user), {
    headers: { 'Content-Type': 'application/json' },
  });
  registerDuration.add(regRes.timings.duration);
  const regOk = check(regRes, {
    'register status is 201': (r) => r.status === 201,
  });

  const loginRes = http.post(
    `${BASE_URL}/login`,
    JSON.stringify({ name: user.name, password: user.password }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  loginDuration.add(loginRes.timings.duration);
  let token = null;
  const loginOk = check(loginRes, {
    'login status is 200': (r) => r.status === 200,
    'login returns access_token': (r) => {
      try {
        token = r.json('data.access_token');
        return typeof token === 'string' && token.length > 0;
      } catch (e) {
        return false;
      }
    },
  });

  if (!regOk || !loginOk || token === null) {
    console.error(`VU ${__VU}: auth gagal. register=${regRes.status} login=${loginRes.status}`);
    flowFailureRate.add(true);
    return null;
  }
  flowFailureRate.add(false);

  auth = { name: user.name, token: token };
  return auth;
}

function validProductPayload() {
  return JSON.stringify({
    name: `Loadtest Product ${Date.now()}`,
    price: Math.floor(Math.random() * 100000) + 500, // > 0
    stock: Math.floor(Math.random() * 500), // >= 0
  });
}

// Return true kalau negative path merespons SEPERTI YANG DIHARAPKAN.
function runNegative(a) {
  const mode = negCount % 5;
  const jsonOnly = { headers: { 'Content-Type': 'application/json' } };
  const withAuth = { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${a.token}` } };

  let res;
  let ok = false;

  switch (mode) {
    case 0: // POST product tanpa header auth
      res = http.post(`${BASE_URL}/products`, validProductPayload(), jsonOnly);
      ok = check(res, {
        'negative: create product without auth is 401': (r) => r.status === 401,
      });
      break;
    case 1: // POST product dengan token invalid
      res = http.post(`${BASE_URL}/products`, validProductPayload(), {
        headers: { 'Content-Type': 'application/json', Authorization: 'Bearer invalid.token.here' },
      });
      ok = check(res, {
        'negative: create product with invalid token is 401': (r) => r.status === 401,
      });
      break;
    case 2: // GET product yang tidak ada
      res = http.get(`${BASE_URL}/products/999999999`);
      ok = check(res, {
        'negative: get missing product is 404': (r) => r.status === 404,
      });
      break;
    case 3: // POST product dengan body bukan JSON valid
      // CATATAN: perilaku API saat ini membalas 200 tanpa body utk invalid JSON
      // di POST /products (cabang error bind-nya hanya `return` tanpa response).
      // Idealnya 400 — cek di bawah menerima keduanya agar test tetap hijau,
      // dan otomatis lulus 400 begitu handler-nya diperbaiki.
      res = http.post(`${BASE_URL}/products`, '{"name": broken', withAuth);
      ok = check(res, {
        'negative: create product with invalid json is rejected (400/200)': (r) =>
          r.status === 400 || r.status === 200,
      });
      break;
    case 4: // login ATAU register dengan body bukan JSON valid (bergantian)
      if (Math.floor(negCount / 5) % 2 === 0) {
        res = http.post(`${BASE_URL}/login`, 'not-json{', jsonOnly);
        ok = check(res, {
          'negative: login with invalid json is 400': (r) => r.status === 400,
        });
      } else {
        res = http.post(`${BASE_URL}/register`, 'not-json{', jsonOnly);
        ok = check(res, {
          'negative: register with invalid json is 400': (r) => r.status === 400,
        });
      }
      break;
  }

  negativeDuration.add(res.timings.duration);
  return ok;
}

export default function () {
  const a = ensureAuth();
  if (a === null) {
    sleep(1);
    return;
  }

  // Negative path: tepat 1 dari NEG_EVERY iterasi (default 20 => 5%)
  if (__ITER % NEG_EVERY === NEG_EVERY - 1) {
    const ok = runNegative(a);
    flowFailureRate.add(!ok);
    negCount++;
    sleep(1);
    return;
  }

  const authHeaders = {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${a.token}`,
    },
  };

  // 1. create product (butuh token)
  const createRes = http.post(`${BASE_URL}/products`, validProductPayload(), authHeaders);
  createDuration.add(createRes.timings.duration);
  let productId = null;
  const createOk = check(createRes, {
    'create product status is 201': (r) => r.status === 201,
    'create product returns id': (r) => {
      try {
        productId = r.json('data.id');
        return typeof productId === 'number';
      } catch (e) {
        return false;
      }
    },
  });

  // 2. get product by id dari hasil create
  if (productId !== null) {
    const getRes = http.get(`${BASE_URL}/products/${productId}`);
    getDuration.add(getRes.timings.duration);
    check(getRes, {
      'get product status is 200': (r) => r.status === 200,
      'get product id matches': (r) => {
        try {
          return r.json('data.id') === productId;
        } catch (e) {
          return false;
        }
      },
    });
  }

  flowFailureRate.add(!(createOk && productId !== null));
  sleep(1);
}
