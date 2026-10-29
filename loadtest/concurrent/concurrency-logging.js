import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

// Log detail concurrency:
//  - setiap VU memakai user berbeda
//  - semua VU meng-update row produk yang SAMA
//  - log: user, row, waktu mulai, status, durasi
//    (durasi panjang = request menunggu row lock)
//
// Biasanya dijalankan lewat run-concurrency-logging.sh, yang juga
// menahan lock dan mencatat sesi yang waiting di PostgreSQL.
//
// Manual:
//   VUS=5 ITERATIONS=20 PRODUCT_ID=123 k6 run loadtest/concurrent/concurrency-logging.js

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const VUS = Number(__ENV.VUS || 5);
const ITERATIONS = Number(__ENV.ITERATIONS || 20);
const DELTA = Number(__ENV.DELTA || 1);
const PRODUCT_ID = __ENV.PRODUCT_ID ? Number(__ENV.PRODUCT_ID) : null;
const STOCK = Number(__ENV.STOCK || 1000);

const updatesOK = new Counter('updates_ok');
const updatesInsufficient = new Counter('updates_insufficient');
const updateDuration = new Trend('update_duration', true);

http.setResponseCallback(http.expectedStatuses(200, 201, 400, 404));

export const options = {
  scenarios: {
    concurrency_logging: {
      executor: 'shared-iterations',
      vus: VUS,
      iterations: ITERATIONS,
      maxDuration: '90s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
  },
};

function makeUsers(n) {
  const users = [];
  for (let i = 1; i <= n; i++) {
    const name = `conclog_${i}_${Date.now()}`;
    const phone = `08${String(Date.now()).slice(-12)}${i % 10}`;
    const password = 'Secret12345';

    const reg = http.post(
      `${BASE_URL}/register`,
      JSON.stringify({ name, phone_number: phone, password }),
      { headers: { 'Content-Type': 'application/json' } }
    );
    check(reg, { [`register user ${i} (201)`]: (r) => r.status === 201 });

    const login = http.post(
      `${BASE_URL}/login`,
      JSON.stringify({ name, password }),
      { headers: { 'Content-Type': 'application/json' } }
    );
    const token = login.json('data.access_token');
    if (typeof token !== 'string' || token.length === 0) {
      throw new Error(`login user ${i} gagal (status ${login.status})`);
    }
    users.push({ index: i, name, token });
  }
  return users;
}

function createProduct(token) {
  const res = http.post(
    `${BASE_URL}/products`,
    JSON.stringify({ name: `conclog product ${Date.now()}`, price: 1000, stock: STOCK }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` } }
  );
  const id = res.json('data.id');
  if (typeof id !== 'number') {
    throw new Error(`create product gagal (status ${res.status})`);
  }
  return id;
}

export function setup() {
  const users = makeUsers(VUS);
  const productId = PRODUCT_ID !== null ? PRODUCT_ID : createProduct(users[0].token);

  console.log(
    `[setup] row product_id=${productId} | users: ` +
      users.map((u) => `${u.index}=${u.name}`).join(', ')
  );
  return { users, productId };
}

export default function (data) {
  const vu = __VU;
  const user = data.users[(vu - 1) % data.users.length];
  const started = Date.now();

  console.log(
    `[VU ${vu} | user ${user.index} ${user.name}] START update row=${data.productId} delta=${DELTA}`
  );

  const res = http.patch(
    `${BASE_URL}/products/${data.productId}/update-stock`,
    JSON.stringify({ delta: DELTA }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${user.token}`,
      },
      tags: { user: user.name, row: String(data.productId) },
    }
  );

  const elapsed = Date.now() - started;
  updateDuration.add(elapsed, { user: user.name });

  let statusNote = 'ok';
  if (res.status === 200) {
    updatesOK.add(1, { user: user.name });
  } else if (res.status === 400) {
    updatesInsufficient.add(1, { user: user.name });
    statusNote = 'insufficient';
  } else {
    statusNote = `unexpected(${res.status})`;
  }

  const waitNote = elapsed >= 200 ? ' <-- MENUNGGU LOCK / ANTRE' : '';
  console.log(
    `[VU ${vu} | user ${user.index} ${user.name}] DONE  row=${data.productId} status=${res.status} (${statusNote}) duration=${elapsed}ms${waitNote}`
  );

  check(res, { 'update status 200/400': (r) => r.status === 200 || r.status === 400 });
}

export function teardown(data) {
  const product = http.get(`${BASE_URL}/products/${data.productId}`);
  const finalStock = product.json('data.stock');
  console.log(
    `[teardown] row=${data.productId} final_stock=${finalStock} ` +
      `(audit log bisa dicek di /products/${data.productId}/audit-logs)`
  );
}
