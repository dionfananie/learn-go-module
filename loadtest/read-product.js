import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

const listDuration = new Trend('product_list_duration', true);
const detailDuration = new Trend('product_detail_duration', true);
const notFoundDuration = new Trend('product_not_found_duration', true);
const serverErrorDuration = new Trend('product_server_error_duration', true);
const notFoundRate = new Rate('product_not_found');
const serverErrorRate = new Rate('product_server_error');

export const options = {
  scenarios: {
    read_product: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2s', target: 5 },
        { duration: '30s', target: 200 },
        { duration: '5s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500'],
    product_list_duration: ['p(95)<500'],
    product_detail_duration: ['p(95)<500'],
    product_not_found_duration: ['p(95)<500'],
    product_server_error_duration: ['p(95)<500'],
  },
};

export function setup() {
  const res = http.get(`${BASE_URL}/products`);
  const ids = [];
  let maxId = 0;
  if (res.status === 200) {
    try {
      const products = res.json();
      products.forEach((p) => ids.push(p.id));
      maxId = products.length > 0 ? Math.max(...products.map((p) => p.id)) : 0;
    } catch (e) {
      console.error('Failed to parse product list in setup', e);
    }
  } else {
    console.warn(`Setup GET /products returned ${res.status}`);
  }
  return { ids, maxId };
}

export default function (data) {
  const resList = http.get(`${BASE_URL}/products`);
  check(resList, {
    'list product status is 200': (r) => r.status === 200,
  });
  listDuration.add(resList.timings.duration);

  if (data.ids.length > 0) {
    const id = data.ids[Math.floor(Math.random() * data.ids.length)];
    const resDetail = http.get(`${BASE_URL}/products/${id}`);
    check(resDetail, {
      'detail product status is 200': (r) => r.status === 200,
    });
    notFoundRate.add(resDetail.status === 404);
    detailDuration.add(resDetail.timings.duration);
  }

  const nonexistentId = data.maxId + Math.floor(Math.random() * 10000) + 1;
  const resNotFound = http.get(`${BASE_URL}/products/${nonexistentId}`);
  check(resNotFound, {
    'missing product returns 404': (r) => r.status === 404,
  });
  notFoundRate.add(resNotFound.status === 404);
  notFoundDuration.add(resNotFound.timings.duration);

  const invalidIds = ['abc', '-1', '0', '9999999999999999999999'];
  const invalidId = invalidIds[Math.floor(Math.random() * invalidIds.length)];
  const resInvalid = http.get(`${BASE_URL}/products/${invalidId}`);
  check(resInvalid, {
    'invalid id returns error (404 or 500)': (r) => r.status === 404 || r.status === 500,
  });
  serverErrorRate.add(resInvalid.status === 500);
  serverErrorDuration.add(resInvalid.timings.duration);

  sleep(1);
}
