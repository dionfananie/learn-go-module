import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

// Uji concurrency untuk flow FOR UPDATE di UpdateProduct:
// banyak VU mengurangi stok SATU produk yang sama secara bersamaan,
// lalu teardown memverifikasi tidak ada lost update / oversell.
//
// Cara pakai:
//   k6 run loadtest/for-update-concurrency.js                 # full: 10 VU, 300 req, stok 200
//   SMOKE=1 k6 run loadtest/for-update-concurrency.js         # smoke: 1 VU, 20 req
//   VUS=50 ITERATIONS=1000 TARGET_STOCK=500 k6 run loadtest/for-update-concurrency.js

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SMOKE = Boolean(__ENV.SMOKE);
const VUS = Number(__ENV.VUS || 10);
const ITERATIONS = SMOKE ? 20 : Number(__ENV.ITERATIONS || 300);
const TARGET_STOCK = Number(__ENV.TARGET_STOCK || 200);

const successfulUpdates = new Counter('successful_updates');
const insufficientRejects = new Counter('insufficient_rejects');
const updateDuration = new Trend('update_duration', true);

// 200 = sukses, 201 = register/create, 400 = stok habis (ditest hingga oversell), 404 = produk hilang
http.setResponseCallback(http.expectedStatuses(200, 201, 400, 404));

export const options = {
  scenarios: {
    concurrent_for_update: {
      executor: 'shared-iterations', // total ITERATIONS dibagi rata ke VUS VU
      vus: SMOKE ? 1 : VUS,
      iterations: ITERATIONS,
      maxDuration: '90s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    // Dengan FOR UPDATE, jumlah sukses TIDAK BOLEH melebihi stok awal.
    // Kalau oversell terjadi (stok bisa negatif), count > TARGET_STOCK -> threshold gagal.
    successful_updates: [`count<=${TARGET_STOCK}`],
    update_duration: ['p(95)<1000'],
  },
};

function makeUser(tag) {
  const name = `forupdate_${tag}_${Date.now()}`;
  const phone = `08${String(Date.now()).slice(-12)}${Math.floor(Math.random() * 10)}`;
  const password = 'Secret12345';

  const regRes = http.post(
    `${BASE_URL}/register`,
    JSON.stringify({ name, phone_number: phone, password }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(regRes, { 'register 201': (r) => r.status === 201 });

  const loginRes = http.post(
    `${BASE_URL}/login`,
    JSON.stringify({ name, password }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  let token = null;
  check(loginRes, {
    'login 200': (r) => r.status === 200,
    'login returns token': (r) => {
      try {
        token = r.json('data.access_token');
        return typeof token === 'string' && token.length > 0;
      } catch (e) {
        return false;
      }
    },
  });

  if (token === null) throw new Error(`setup user ${tag} gagal`);
  return token;
}

function createProduct(token, stock) {
  const res = http.post(
    `${BASE_URL}/products`,
    JSON.stringify({ name: `ForUpdate ${Date.now()}`, price: 1000, stock }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` } }
  );
  let id = null;
  check(res, {
    'create product 201': (r) => r.status === 201,
    'create returns id': (r) => {
      try {
        id = r.json('data.id');
        return typeof id === 'number';
      } catch (e) {
        return false;
      }
    },
  });
  if (id === null) throw new Error('setup create product gagal');
  return id;
}

export function setup() {
  const token = makeUser('FU');
  const id = createProduct(token, TARGET_STOCK);
  return { token, id, stock: TARGET_STOCK };
}

export default function (data) {
  const res = http.patch(
    `${BASE_URL}/products/${data.id}/update-stock`,
    JSON.stringify({ delta: 1 }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${data.token}` } }
  );

  updateDuration.add(res.timings.duration);
  if (res.status === 200) successfulUpdates.add(1);
  else if (res.status === 400) insufficientRejects.add(1);

  check(res, { 'update is 200/400': (r) => r.status === 200 || r.status === 400 });
}

export function teardown(data) {
  const prodRes = http.get(`${BASE_URL}/products/${data.id}`);
  const logsRes = http.get(`${BASE_URL}/products/${data.id}/audit-logs`);

  const finalStock = prodRes.json('data.stock');
  const logs = logsRes.json('data') || [];

  const auditCount = logs.length;
  const expected = data.stock - auditCount;

  // Rantai audit: urutkan by id (urutan commit), pastikan old->new tersambung.
  // Contoh benar: 200->199, 199->198, 198->197 ...
  const asc = [...logs].sort((a, b) => a.id - b.id);
  let chainOk = true;
  for (let i = 0; i < asc.length - 1; i++) {
    if (asc[i].new_stock !== asc[i + 1].old_stock) {
      chainOk = false;
      break;
    }
  }

  console.log('\n=== VERIFIKASI KONSISTENSI (FOR UPDATE) ===');
  console.log(`produk id        : ${data.id}`);
  console.log(`stok awal        : ${data.stock}`);
  console.log(`stok akhir       : ${finalStock}`);
  console.log(`audit log count  : ${auditCount}`);
  console.log(`expected akhir   : ${expected}`);
  console.log(`tidak negatif    : ${finalStock >= 0}`);
  console.log(`konsisten        : ${finalStock === expected}`);
  console.log(`rantai audit ok  : ${chainOk}`);

  if (finalStock !== expected || finalStock < 0 || !chainOk) {
    throw new Error(
      `INKONSISTEN: stok=${finalStock} expected=${expected} audit=${auditCount} chain=${chainOk}`
    );
  }
  console.log('=== PASS: tidak ada lost update / oversell ===');
}
