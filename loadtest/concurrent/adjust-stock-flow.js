import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SMOKE = Boolean(__ENV.SMOKE); // SMOKE=1 utk tes cepat 1 VU

const adjustDuration = new Trend('stock_adjust_duration', true);
const adjustInsufficientDuration = new Trend('stock_adjust_insufficient_duration', true);
const adjustMissingDuration = new Trend('stock_adjust_missing_duration', true);
const flowFailureRate = new Rate('flow_failures');

// 400/404 pada negative path memang disengaja; jangan dihitung sbg kegagalan
http.setResponseCallback(http.expectedStatuses(200, 201, 400, 404));

export const options = {
  scenarios: {
    adjust_stock_flow: {
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
    stock_adjust_duration: ['p(95)<300'],
    stock_adjust_insufficient_duration: ['p(95)<300'],
    stock_adjust_missing_duration: ['p(95)<300'],
  },
};

function makeUser(tag) {
  const name = `stocktest_${tag}_${Date.now()}`;
  const phone = `08${Date.now()}${Math.floor(Math.random() * 10)}`;
  const user = { name: name, phone_number: phone, password: 'Secret12345' };

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

// Buat produk dengan stok awal yang dikontrol, kembalikan {id, stock}
function createProduct(token, stock) {
  const res = http.post(
    `${BASE_URL}/products`,
    JSON.stringify({
      name: `Stock Flow ${Date.now()}`,
      price: Math.floor(Math.random() * 100000) + 500,
      stock: stock,
    }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` } }
  );
  let id = null;
  const ok = check(res, {
    'setup: create product is 201': (r) => r.status === 201,
    'setup: create product returns id': (r) => {
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

export function setup() {
  const user = makeUser('U');
  return { user: user };
}

export default function (data) {
  const auth = { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${data.user.token}` } };
  let ok = true;

  // Skenario 1 (happy path): produk stok 100, kurangi 5 -> 200 + audit_id
  const p = createProduct(data.user.token, 100);
  ok = ok && p.ok && p.id !== null;
  if (p.id !== null) {
    const res = http.patch(`${BASE_URL}/products/${p.id}/update-stock`, JSON.stringify({ delta: 5 }), auth);
    adjustDuration.add(res.timings.duration);
    ok =
      check(res, {
        'adjust stock (delta 5) is 200': (r) => r.status === 200,
        'adjust returns audit_id': (r) => {
          try {
            const aid = r.json('data.audit_id');
            return typeof aid === 'number' || typeof aid === 'string';
          } catch (e) {
            return false;
          }
        },
      }) && ok;

    // Skenario 2 (audit trail): setelah adjust, stok produk berubah jadi 95
    const checkRes = http.get(`${BASE_URL}/products/${p.id}`);
    ok =
      check(checkRes, {
        'stock reduced to 95 after adjust': (r) => {
          try {
            return r.json('data.stock') === 95;
          } catch (e) {
            return false;
          }
        },
      }) && ok;

    // Skenario 3 (stok kurang): minta delta lebih besar dari sisa stok (95) -> ditolak
    const insRes = http.patch(
      `${BASE_URL}/products/${p.id}/update-stock`,
      JSON.stringify({ delta: 99999 }),
      auth
    );
    adjustInsufficientDuration.add(insRes.timings.duration);
    ok =
      check(insRes, {
        'adjust beyond stock is 400 (insufficient)': (r) => r.status === 400,
      }) && ok;

    // pastikan stok TIDAK berubah setelah penolakan (transaksi rollback)
    const checkRes2 = http.get(`${BASE_URL}/products/${p.id}`);
    ok =
      check(checkRes2, {
        'stock unchanged after rejected adjust': (r) => {
          try {
            return r.json('data.stock') === 95;
          } catch (e) {
            return false;
          }
        },
      }) && ok;
  }

  // Skenario 4: adjust produk yang tidak ada -> 404 (beda dari stok kurang -> 400)
  const missRes = http.patch(`${BASE_URL}/products/999999999/update-stock`, JSON.stringify({ delta: 1 }), auth);
  adjustMissingDuration.add(missRes.timings.duration);
  ok =
    check(missRes, {
      'adjust missing product is 404': (r) => r.status === 404,
    }) && ok;

  flowFailureRate.add(!ok);
  sleep(1);
}
