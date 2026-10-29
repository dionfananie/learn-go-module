import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

// Test ini membutuhkan trigger sementara yang dibuat oleh
// run-rollback-audit-error.sh. Trigger sengaja menggagalkan INSERT audit.
// Tujuannya memastikan UPDATE stok ikut di-ROLLBACK.

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const FAILED_UPDATES = new Counter('intentional_audit_failures');

http.setResponseCallback(http.expectedStatuses(200, 201, 500));

export const options = {
  scenarios: {
    rollback_audit_error: {
      executor: 'shared-iterations',
      vus: 5,
      iterations: 5,
      maxDuration: '30s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    intentional_audit_failures: ['count==5'],
  },
};

function setupUser() {
  const suffix = `${Date.now()}${Math.floor(Math.random() * 1000)}`;
  const user = {
    name: `rollback_${suffix}`,
    phone_number: `08${String(Date.now()).slice(-12)}${Math.floor(Math.random() * 10)}`,
    password: 'Secret12345',
  };

  const register = http.post(`${BASE_URL}/register`, JSON.stringify(user), {
    headers: { 'Content-Type': 'application/json' },
  });
  check(register, { 'register succeeds': (r) => r.status === 201 });

  const login = http.post(`${BASE_URL}/login`, JSON.stringify({ name: user.name, password: user.password }), {
    headers: { 'Content-Type': 'application/json' },
  });
  let token;
  const loginOK = check(login, {
    'login succeeds': (r) => r.status === 200,
    'login returns token': (r) => {
      try {
        token = r.json('data.access_token');
        return typeof token === 'string' && token.length > 0;
      } catch (e) {
        return false;
      }
    },
  });

  if (!loginOK || !token) throw new Error('setup user gagal');
  return token;
}

function setupProduct(token) {
  const response = http.post(
    `${BASE_URL}/products`,
    JSON.stringify({ name: `Rollback Test ${Date.now()}`, price: 1000, stock: 100 }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` } }
  );

  let id;
  const ok = check(response, {
    'create product succeeds': (r) => r.status === 201,
    'create returns id': (r) => {
      try {
        id = r.json('data.id');
        return typeof id === 'number';
      } catch (e) {
        return false;
      }
    },
  });
  if (!ok || id === undefined) throw new Error('setup product gagal');
  return id;
}

export function setup() {
  const token = setupUser();
  const productID = setupProduct(token);
  return { token, productID, initialStock: 100 };
}

export default function (data) {
  const response = http.patch(
    `${BASE_URL}/products/${data.productID}/update-stock`,
    JSON.stringify({ delta: 10 }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${data.token}` } }
  );

  const failedAsExpected = check(response, {
    'audit failure returns 500': (r) => r.status === 500,
  });
  if (failedAsExpected) FAILED_UPDATES.add(1);
}

export function teardown(data) {
  const product = http.get(`${BASE_URL}/products/${data.productID}`);
  const audit = http.get(`${BASE_URL}/products/${data.productID}/audit-logs`);
  const finalStock = product.json('data.stock');
  const auditLogs = audit.json('data') || [];

  console.log('\n=== VERIFIKASI ROLLBACK AUDIT ERROR ===');
  console.log(`stok awal       : ${data.initialStock}`);
  console.log(`stok akhir      : ${finalStock}`);
  console.log(`audit log count : ${auditLogs.length}`);

  if (finalStock !== data.initialStock || auditLogs.length !== 0) {
    throw new Error(`ROLLBACK GAGAL: stock=${finalStock}, audit=${auditLogs.length}`);
  }
  console.log('=== PASS: UPDATE stok dan audit sama-sama rollback ===');
}
