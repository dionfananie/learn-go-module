import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SMOKE = Boolean(__ENV.SMOKE); // SMOKE=1 utk tes cepat 1 VU

const createDuration = new Trend('product_create_duration', true);
const deleteOwnDuration = new Trend('delete_own_duration', true);
const deleteForbiddenDuration = new Trend('delete_forbidden_duration', true);
const deleteMissingDuration = new Trend('delete_missing_duration', true);
const flowFailureRate = new Rate('flow_failures');

// 4xx pada skenario ini memang disengaja (403/404); jangan dihitung sbg kegagalan
http.setResponseCallback(http.expectedStatuses(200, 201, 403, 404));

export const options = {
  scenarios: {
    delete_flow: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: SMOKE
        ? [{ duration: '5s', target: 1 }]
        : [
            { duration: '10s', target: 3 },
            { duration: '30s', target: 3 },
            { duration: '5s', target: 0 },
          ],
      gracefulRampDown: '5s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    flow_failures: ['rate<0.01'],
    product_create_duration: ['p(95)<300'],
    delete_own_duration: ['p(95)<200'],
    delete_forbidden_duration: ['p(95)<200'],
    delete_missing_duration: ['p(95)<200'],
  },
};

function dummyUser(tag) {
  const name = `deltest_${tag}_${Date.now()}`;
  const phone = `08${Date.now()}${Math.floor(Math.random() * 10)}`;
  return { name: name, phone_number: phone, password: 'Secret12345' };
}

// Register + login satu user, kembalikan token-nya
function makeUser(tag) {
  const user = dummyUser(tag);

  const regRes = http.post(`${BASE_URL}/register`, JSON.stringify(user), {
    headers: { 'Content-Type': 'application/json' },
  });
  const regOk = check(regRes, {
    [`register ${tag} status is 201`]: (r) => r.status === 201,
  });

  const loginRes = http.post(
    `${BASE_URL}/login`,
    JSON.stringify({ name: user.name, password: user.password }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  let token = null;
  const loginOk = check(loginRes, {
    [`login ${tag} status is 200`]: (r) => r.status === 200,
    [`login ${tag} returns access_token`]: (r) => {
      try {
        token = r.json('data.access_token');
        return typeof token === 'string' && token.length > 0;
      } catch (e) {
        return false;
      }
    },
  });

  if (!regOk || !loginOk || token === null) {
    throw new Error(`setup user ${tag} gagal: register=${regRes.status} login=${loginRes.status}`);
  }
  return { name: user.name, token: token };
}

export function setup() {
  // User A = pemilik produk, User B = user lain (bukan pemilik)
  const userA = makeUser('A');
  const userB = makeUser('B');
  return { a: userA, b: userB };
}

function createProduct(token) {
  const res = http.post(
    `${BASE_URL}/products`,
    JSON.stringify({
      name: `Delete Flow ${Date.now()}`,
      price: Math.floor(Math.random() * 100000) + 500,
      stock: Math.floor(Math.random() * 500),
    }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` } }
  );
  createDuration.add(res.timings.duration);
  let id = null;
  const ok = check(res, {
    'create product status is 201': (r) => r.status === 201,
    'create product returns id': (r) => {
      try {
        id = r.json('data.id');
        return typeof id === 'number';
      } catch (e) {
        return false;
      }
    },
  });
  return { ok: ok, id: id };
}

export default function (data) {
  const authA = { headers: { Authorization: `Bearer ${data.a.token}` } };
  const authB = { headers: { Authorization: `Bearer ${data.b.token}` } };
  let ok = true;

  // Skenario 1: User A create product (created_by = A di database)
  const p1 = createProduct(data.a.token);
  ok = ok && p1.ok && p1.id !== null;
  if (p1.id === null) {
    flowFailureRate.add(true);
    sleep(1);
    return;
  }

  // Skenario 2: User A delete product miliknya -> 200
  const delOwn = http.del(`${BASE_URL}/products/${p1.id}`, null, authA);
  deleteOwnDuration.add(delOwn.timings.duration);
  ok =
    check(delOwn, {
      'A delete own product is 200': (r) => r.status === 200,
      'A delete own returns deleted id': (r) => {
        try {
          return r.json('data') === p1.id;
        } catch (e) {
          return false;
        }
      },
    }) && ok;

  // Skenario 3: User B delete product milik A -> 403
  // (butuh produk yang MASIH ADA, jadi dibuat produk kedua)
  const p2 = createProduct(data.a.token);
  ok = ok && p2.ok && p2.id !== null;
  if (p2.id !== null) {
    const delForeign = http.del(`${BASE_URL}/products/${p2.id}`, null, authB);
    deleteForbiddenDuration.add(delForeign.timings.duration);
    ok =
      check(delForeign, {
        'B delete product of A is 403': (r) => r.status === 403,
      }) && ok;

    // bereskan: produk kedua dihapus pemiliknya supaya tidak menumpuk
    http.del(`${BASE_URL}/products/${p2.id}`, null, authA);
  }

  // Skenario 4: delete product yang tidak pernah ada -> 404
  const delMissing = http.del(`${BASE_URL}/products/999999999`, null, authA);
  deleteMissingDuration.add(delMissing.timings.duration);
  ok =
    check(delMissing, {
      'delete missing product is 404': (r) => r.status === 404,
    }) && ok;

  flowFailureRate.add(!ok);
  sleep(1);
}
