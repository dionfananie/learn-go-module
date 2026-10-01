import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SMOKE = Boolean(__ENV.SMOKE); // SMOKE=1 utk tes cepat 1-2 VU

const registerDuration = new Trend('register_duration', true);
const loginDuration = new Trend('login_duration', true);
const createDuration = new Trend('product_create_duration', true);
const getDuration = new Trend('product_get_duration', true);
const flowFailureRate = new Rate('flow_failures');

export const options = {
  scenarios: {
    product_flow: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: SMOKE
        ? [{ duration: '5s', target: 2 }]
        : [
          { duration: '15s', target: 100 }, // ramp-up
          { duration: '10s', target: 500 }, // steady
          { duration: '10s', target: 200 }, // ramp-down
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
  },
};

// State per-VU (init context dijalankan terpisah untuk setiap VU).
// Register+login cukup sekali per VU, sisanya iterasi create+get product.
let auth = null;

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

export default function () {
  const a = ensureAuth();
  if (a === null) {
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
  const payload = JSON.stringify({
    name: `Loadtest Product ${Date.now()}`,
    price: Math.floor(Math.random() * 100000) + 500, // > 0
    stock: Math.floor(Math.random() * 500), // >= 0
  });
  const createRes = http.post(`${BASE_URL}/products`, payload, authHeaders);
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
