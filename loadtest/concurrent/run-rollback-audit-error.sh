#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

cleanup() {
  docker exec learn-go-postgres psql -v ON_ERROR_STOP=1 -U postgres -d learn_go -c \
    "DROP TRIGGER IF EXISTS test_fail_audit_insert ON products_audit_logs; DROP FUNCTION IF EXISTS test_fail_audit_insert();" >/dev/null
}
trap cleanup EXIT

docker exec -i learn-go-postgres psql -v ON_ERROR_STOP=1 -U postgres -d learn_go <<'SQL'
CREATE OR REPLACE FUNCTION test_fail_audit_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'intentional audit insert failure for rollback test';
END;
$$;

DROP TRIGGER IF EXISTS test_fail_audit_insert ON products_audit_logs;
CREATE TRIGGER test_fail_audit_insert
BEFORE INSERT ON products_audit_logs
FOR EACH ROW EXECUTE FUNCTION test_fail_audit_insert();
SQL

k6 run loadtest/concurrent/rollback-audit-error.js
