import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SMOKE = Boolean(__ENV.SMOKE);

const createDuration = new Trend('product_create_duration', true);
const adjustDuration = new Trend('stock_adjust_duration', true);
const transferDuration = new Trend('stock_transfer_duration', true);
const getDuration = new Trend('product_get_duration', true);
const flowFailureRate = new Rate('flow_failures');

// 404 pada negative path memang disengaja
http.setResponseCallback(http.expectedStatuses(200, 201, 404));

export const options = {
    scenarios: {
        stock_event_flow: {
            executor: 'ramping-vus',
            startVUs: 1,
            stages: SMOKE
                ? [{ duration: '5s', target: 2 }]
                : [
                    { duration: '15s', target: 10 },
                    { duration: '1m', target: 10 },
                    { duration: '10s', target: 0 },
                ],
            gracefulRampDown: '10s',
        },
    },
    thresholds: {
        http_req_failed: ['rate<0.01'],
        checks: ['rate>0.99'],
        flow_failures: ['rate<0.01'],
        product_create_duration: ['p(95)<300'],
        stock_adjust_duration: ['p(95)<300'],
        stock_transfer_duration: ['p(95)<400'],
        product_get_duration: ['p(95)<200'],
    },
};

let auth = null;

function ensureAuth() {
    if (auth !== null) return auth;
    const user = {
        name: `mqtest_${__VU}_${Date.now()}`,
        phone_number: `08${Date.now()}${__VU % 10}`,
        password: 'Secret12345',
    };

    http.post(`${BASE_URL}/register`, JSON.stringify(user), {
        headers: { 'Content-Type': 'application/json' },
    });

    const loginRes = http.post(
        `${BASE_URL}/login`,
        JSON.stringify({ name: user.name, password: user.password }),
        { headers: { 'Content-Type': 'application/json' } }
    );
    let token = null;
    check(loginRes, {
        'login returns access_token': (r) => {
            try {
                token = r.json('data.access_token');
                return typeof token === 'string' && token.length > 0;
            } catch (e) {
                return false;
            }
        },
    });
    if (token === null) return null;
    auth = { token };
    return auth;
}

function createProduct(token, stock) {
    const res = http.post(
        `${BASE_URL}/products`,
        JSON.stringify({ name: `MQ Flow ${Date.now()}`, price: 1000, stock }),
        { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` } }
    );
    createDuration.add(res.timings.duration);
    let id = null;
    const ok = check(res, {
        'create product is 201': (r) => r.status === 201,
        'create returns id': (r) => {
            try {
                id = r.json('data.id');
                return typeof id === 'number';
            } catch (e) {
                return false;
            }
        },
    });
    return { ok, id };
}

export default function () {
    const a = ensureAuth();
    if (a === null) {
        flowFailureRate.add(true);
        sleep(1);
        return;
    }
    const authHeaders = {
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${a.token}` },
    };
    let ok = true;

    const A = createProduct(a.token, 1000);
    const B = createProduct(a.token, 1000);
    ok = ok && A.ok && A.id !== null && B.ok && B.id !== null;
    if (A.id === null || B.id === null) {
        flowFailureRate.add(true);
        sleep(1);
        return;
    }

    // 1) adjust A: 1000 -> 995 (publish 1 event "subtract")
    const adj = http.patch(
        `${BASE_URL}/products/${A.id}/update-stock`,
        JSON.stringify({ delta: 5 }),
        authHeaders
    );
    adjustDuration.add(adj.timings.duration);
    ok = check(adj, {
        'adjust stock is 200': (r) => r.status === 200,
        'adjust returns audit_id': (r) => {
            try {
                const aid = r.json('data.audit_id');
                return typeof aid === 'number' || typeof aid === 'string';
            } catch (e) {
                return false;
            }
        },
    }) && ok;

    // 2) transfer A->B amount 3 (publish 2 event: transfer_out + transfer_in)
    const trf = http.post(
        `${BASE_URL}/products/transfer-stock`,
        JSON.stringify({ from_id: A.id, to_id: B.id, amount: 3 }),
        authHeaders
    );
    transferDuration.add(trf.timings.duration);
    ok = check(trf, { 'transfer stock is 200': (r) => r.status === 200 }) && ok;

    // 3) verifikasi stok konsisten: A = 995-3 = 992, B = 1000+3 = 1003
    const gA = http.get(`${BASE_URL}/products/${A.id}`);
    const gB = http.get(`${BASE_URL}/products/${B.id}`);
    getDuration.add(gA.timings.duration);
    getDuration.add(gB.timings.duration);
    ok = check(gA, {
        'stock A == 992': (r) => { try { return r.json('data.stock') === 992; } catch (e) { return false; } },
    }) && ok;
    ok = check(gB, {
        'stock B == 1003': (r) => { try { return r.json('data.stock') === 1003; } catch (e) { return false; } },
    }) && ok;

    // 4) negative: adjust produk tidak ada -> 404
    const miss = http.patch(
        `${BASE_URL}/products/999999999/update-stock`,
        JSON.stringify({ delta: 1 }),
        authHeaders
    );
    ok = check(miss, { 'adjust missing product is 404': (r) => r.status === 404 }) && ok;

    flowFailureRate.add(!ok);
    sleep(1);
}