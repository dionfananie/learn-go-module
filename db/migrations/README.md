# Database Migrations

Migrations for this project are managed with [golang-migrate](https://github.com/golang-migrate/migrate) and live in `db/migrations/`.

## Structure

Each migration consists of a pair of files:

```
000001_create_users.up.sql     # applied when migrating up
000001_create_users.down.sql   # applied when rolling back
```

Naming convention: `<6-digit-sequence>_<name>.up.sql` / `<6-digit-sequence>_<name>.down.sql`.

Current migrations:

| Version | Name             | Description                  |
|---------|------------------|------------------------------|
| 000001  | `create_users`   | Creates the `users` table    |
| 000002  | `create_products`| Creates the `products` table |

## Prerequisites

1. **PostgreSQL** — start the local database with Docker Compose (see `compose.yaml` for the default local settings):

   ```bash
   docker compose up -d
   ```

2. **migrate CLI** — install it with one of the following:

   ```bash
   # macOS
   brew install golang-migrate

   # Linux
   curl -L https://packagecloud.io/golang-migrate/migrate/gpgkey | sudo apt-key add -
   # ...or download a binary from the releases page:
   # https://github.com/golang-migrate/migrate/releases

   # Via Go
   go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
   ```

## Environment

Migrations connect using the same `DATABASE_URL` variable the application uses. Create a `.env` file in the project root (never commit real credentials — `.env` is already git-ignored):

```env
DATABASE_URL=postgres://<user>:<password>@<host>:<port>/<database>?sslmode=disable
```

> Replace `<user>`, `<password>`, `<host>`, `<port>`, and `<database>` with your own values.
> Note: the `migrate` CLI expects a full `postgres://` URL, while the app DSN is read by `src/database/connection.go`.

Export it so the commands below can use it:

```bash
set -a; source .env; set +a
```

## Creating a new migration

```bash
migrate create -ext sql -dir db/migrations -seq add_orders_table
```

This generates an empty up/down pair with the next sequence number. Edit both files:

- `*.up.sql` — the change to apply (e.g. `CREATE TABLE ...`)
- `*.down.sql` — the inverse, so the migration can be reverted (e.g. `DROP TABLE ...`)

## Running migrations

All commands use the same flags: `-path` for the migrations directory and `-database` for the connection URL.

```bash
# Apply all pending migrations
migrate -path db/migrations -database "$DATABASE_URL" up

# Apply only the next N migrations
migrate -path db/migrations -database "$DATABASE_URL" up 1

# Roll back the most recent migration
migrate -path db/migrations -database "$DATABASE_URL" down 1

# Show the current migration version
migrate -path db/migrations -database "$DATABASE_URL" version
```

Applied migrations are tracked in the `schema_migrations` table, created automatically on first run.

## Fixing a dirty state

If a migration fails halfway, the database is marked as *dirty* and further commands are rejected. Inspect the error, fix the underlying issue, then force the version back to the last known-good state:

```bash
migrate -path db/migrations -database "$DATABASE_URL" force <last-good-version>
```

## Conventions

- Never edit a migration that has already been applied — always create a new one.
- Every `up` must have a matching, safe `down`.
- Keep migrations small and focused; one logical change per version.

## Seeding (optional)

After migrating, you can fill the `products` table with sample data:

```bash
go run db/seed.go
```
