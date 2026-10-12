-- Index for the login flow (UsersRepository.LoginUser):
--   SELECT id, name, password, created_at FROM users WHERE name = $1
-- Without this index every login triggers a Seq Scan (PostgreSQL reads
-- the whole users table) which gets slower as the user table grows.
CREATE INDEX IF NOT EXISTS idx_users_name ON users (name);
