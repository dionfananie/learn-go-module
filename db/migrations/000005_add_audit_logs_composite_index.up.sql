-- Composite index for audit-history queries, e.g.:
--   SELECT ... FROM products_audit_logs
--   WHERE product_id = $1 ORDER BY created_at DESC;
-- Sorted by (product_id, created_at), so one index serves both the
-- WHERE (leftmost column) and the ORDER BY (no separate sort needed).
CREATE INDEX IF NOT EXISTS idx_audit_logs_product_created
    ON products_audit_logs (product_id, created_at);
