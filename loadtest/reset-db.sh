#!/usr/bin/env bash
# Kosongkan tabel users, products & products_audit_logs sebelum/sesudah load test.
# Pemakaian:
#   ./loadtest/reset-db.sh
#   ./loadtest/reset-db.sh && k6 run loadtest/product-flow.js
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
# shellcheck disable=SC1091
source .env
set +a

echo "Truncate products, products_audit_logs & users di DB '${DB_NAME}' (container learn-go-postgres) ..."
docker exec learn-go-postgres psql -v ON_ERROR_STOP=1 -U "$DB_USER" -d "$DB_NAME" -c \
  "TRUNCATE TABLE products_audit_logs, products, users RESTART IDENTITY CASCADE;"

echo "Successfully clean DB. You can continue develop"
echo "  k6 run loadtest/product-flow.js          # load test"
echo "  go run db/seed.go                        # isi ulang 250 produk dummy (opsional)"
