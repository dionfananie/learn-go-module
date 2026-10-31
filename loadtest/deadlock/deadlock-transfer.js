import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

// Reproduksi DEADLOCK di level database lewat transfer stok antar dua produk.
//
// Endpoint POST /products/transfer-stock mengunci dua baris dalam SATU transaksi
// dengan urutan TETAP: source dulu, lalu destination (SELECT ... FOR UPDATE).
// Saat dua transfer berlawanan arah (A->B dan B->A) berjalan bersamaan:
//   tx1: kunci A, lalu menunggu B
//   tx2: kunci B, lalu menunggu A
//   => circular wait => PostgreSQL membatalkan salah satu (SQLSTATE 40P01)
//      => endpoint membalas HTTP 409.
//
// Test ini menghitung berapa deadlock yang terdeteksi, mencatat SETIAP deadlock
// ke log ([VU n] DEADLOCK: ...), dan teardown memastikan TOTAL stok tetap terjaga
// (transfer hanya memindahkan, tidak menciptakan/menghilangkan stok) — jadi
// deadlock TIDAK menyebabkan lost update.
//
// Cara pakai:
//   k6 run loadtest/deadlock/deadlock-transfer.js               # 10 VU, 300 iter
//   SMOKE=1 k6 run loadtest/deadlock/deadlock-transfer.js       # 2 VU, 40 iter (cepat)
//   VUS=20 ITERATIONS=600 STOCK=50000 k6 run loadtest/deadlock/deadlock-transfer.js

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SMOKE = Boolean(__ENV.SMOKE);
const VUS = Number(__ENV.VUS || 10);
const ITERATIONS = SMOKE ? 40 : Number(__ENV.ITERATIONS || 300);
const STOCK = Number(__ENV.STOCK || 10000);
const AMOUNT = Number(__ENV.AMOUNT || 1);

const transfersOK = new Counter('transfers_ok'); // 200 -> transfer sukses
const deadlocks = new Counter('deadlocks'); // 409 -> jadi korban deadlock (ini yang kita amati)
const insufficient = new Counter('insufficient'); // 400 -> stok source habis
const transferDuration = new Trend('transfer_duration', true);

// 200 = sukses, 201 = register/create, 400 = stok source kurang,
// 404 = produk hilang, 409 = deadlock (ekspektasi skenario ini).
http.setResponseCallback(http.expectedStatuses(200, 201, 400, 404, 409));

export const options = {
  scenarios: {
    deadlock_transfer: {
      executor: 'shared-iterations', // total ITERATIONS dibagi rata ke VUS VU
      vus: SMOKE ? 2 : VUS,
      iterations: ITERATIONS,
      maxDuration: '120s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    // Deadlock baru terdeteksi setelah deadlock_timeout PostgreSQL (±1 detik),
    // lalu transaksi pemenang tetap harus menunggu antrean lock. Jadi p(95)
    // wajar berada di kisaran beberapa detik.
    transfer_duration: ['p(95)<15000'],
    // Sanity: jumlah sukses tidak boleh melebihi total iterasi.
    transfers_ok: [`count<=${ITERATIONS}`],
    // Full run WAJIB menghasilkan minimal satu deadlock (bukti skenario jalan).
    // Smoke run tidak diwajibkan — concurrency rendah bisa 0 deadlock.
    ...(SMOKE ? {} : { deadlocks: ['count>0'] }),
  },
};

function makeUser() {
  const name = `deadlock_${Date.now()}`;
  const phone = `08${String(Date.now()).slice(-12)}${Math.floor(Math.random() * 10)}`;
  const password = 'Secret12345';

  const reg = http.post(
    `${BASE_URL}/register`,
    JSON.stringify({ name, phone_number: phone, password }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(reg, { 'register 201': (r) => r.status === 201 });

  const login = http.post(
    `${BASE_URL}/login`,
    JSON.stringify({ name, password }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  let token = null;
  check(login, {
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
  if (token === null) throw new Error('setup user gagal');
  return token;
}

function createProduct(token, tag) {
  const res = http.post(
    `${BASE_URL}/products`,
    JSON.stringify({ name: `Deadlock ${tag} ${Date.now()}`, price: 1000, stock: STOCK }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` } }
  );
  let id = null;
  check(res, {
    [`create ${tag} 201`]: (r) => r.status === 201,
    [`create ${tag} returns id`]: (r) => {
      try {
        id = r.json('data.id');
        return typeof id === 'number';
      } catch (e) {
        return false;
      }
    },
  });
  if (id === null) throw new Error(`setup create ${tag} gagal`);
  return id;
}

export function setup() {
  const token = makeUser();
  const a = createProduct(token, 'A');
  const b = createProduct(token, 'B');
  console.log(`[setup] transfer A(id=${a}) <-> B(id=${b}) | stok awal masing-masing ${STOCK}`);
  return { token, a, b, initialA: STOCK, initialB: STOCK };
}

export default function (data) {
  // Setengah VU transfer A->B, setengahnya B->A (dua aliran berlawanan arah).
  const forward = __VU % 2 === 0;
  const fromId = forward ? data.a : data.b;
  const toId = forward ? data.b : data.a;

  const res = http.post(
    `${BASE_URL}/products/transfer-stock`,
    JSON.stringify({ from_id: fromId, to_id: toId, amount: AMOUNT }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${data.token}` } }
  );

  transferDuration.add(res.timings.duration);

  if (res.status === 200) {
    transfersOK.add(1);
  } else if (res.status === 409) {
    deadlocks.add(1);
    // Log SETIAP deadlock: VU, arah transfer, durasi, dan pesan error dari API.
    // PostgreSQL membatalkan satu transaksi dengan SQLSTATE 40P01 (deadlock_detected).
    console.log(
      `[VU ${__VU}] DEADLOCK: transfer ${fromId} -> ${toId} amount=${AMOUNT} ` +
        `status=${res.status} durasi=${res.timings.duration}ms | ${res.body}`
    );
  } else if (res.status === 400) {
    insufficient.add(1);
  }

  check(res, {
    'transfer is 200/409/400': (r) => r.status === 200 || r.status === 409 || r.status === 400,
  });
}

function auditDelta(productID) {
  const res = http.get(`${BASE_URL}/products/${productID}/audit-logs`);
  const logs = res.json('data') || [];
  let delta = 0;
  for (const l of logs) {
    delta += l.new_stock - l.old_stock;
  }
  return { count: logs.length, delta: delta };
}

export function teardown(data) {
  const a = http.get(`${BASE_URL}/products/${data.a}`);
  const b = http.get(`${BASE_URL}/products/${data.b}`);
  const stockA = a.json('data.stock');
  const stockB = b.json('data.stock');
  const total = stockA + stockB;
  const expected = data.initialA + data.initialB;

  // Rekonsiliasi audit trail: jumlah (new_stock - old_stock) per produk HARUS
  // sama dengan perubahan stok produk tsb. Hanya transaksi yang commit yang
  // menulis audit; korban deadlock di-rollback penuh.
  const audA = auditDelta(data.a);
  const audB = auditDelta(data.b);
  const deltaA = stockA - data.initialA;
  const deltaB = stockB - data.initialB;
  const auditOk = deltaA === audA.delta && deltaB === audB.delta;

  console.log('\n=== VERIFIKASI DEADLOCK TRANSFER ===');
  console.log(`produk A (id ${data.a}): stok ${data.initialA} -> ${stockA} (delta ${deltaA})`);
  console.log(`produk B (id ${data.b}): stok ${data.initialB} -> ${stockB} (delta ${deltaB})`);
  console.log(`total stok  : awal ${expected} / akhir ${total}`);
  console.log(`audit A     : ${audA.count} log, delta ${audA.delta}`);
  console.log(`audit B     : ${audB.count} log, delta ${audB.delta}`);
  console.log('catatan     : cari baris "[VU n] DEADLOCK:" di log untuk tiap deadlock,');
  console.log('              dan counter "deadlocks" (HTTP 409) di ringkasan k6.');

  if (total !== expected) {
    throw new Error(`INKONSISTEN: total stok ${total} != ${expected} (lost update?)`);
  }
  if (!auditOk) {
    throw new Error(
      `INKONSISTEN: audit tidak rekonsiliasi (A: delta ${deltaA} vs audit ${audA.delta}, ` +
        `B: delta ${deltaB} vs audit ${audB.delta})`
    );
  }
  console.log('=== PASS: total stok konsisten & audit trail rekonsiliasi ===');
}
