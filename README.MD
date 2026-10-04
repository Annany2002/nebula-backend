<p align="center">
  <img src="docs/logo/nebula-mark.png" alt="Nebula logo" width="96" height="96" />
</p>

<h1 align="center">Nebula Backend</h1>

Open-source Backend-as-a-Service built with Go, Gin, and SQLite. Provision user databases, define schemas, and access data through REST. [Nebula frontend](https://github.com/Annany2002/nebula-frontend) provides the web interface; the [TypeScript SDK](https://github.com/Annany2002/nebula-sdk-ts) provides a client library.

## Features

| Area             | Available today                                                                                                                                     |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Database storage | One SQLite file per user database, a separate platform metadata database, WAL mode, and foreign-key enforcement                                     |
| Authentication   | Account signup/login, bcrypt password hashing, JWT sessions, and database-scoped API keys                                                           |
| API keys         | Generate, rotate, inspect prefix/creation metadata, and revoke; one active key per database, hashed at rest                                         |
| Schemas          | Create tables with typed columns and foreign keys; add/rename/drop columns and rename tables, including transactional batches of schema alterations |
| Records          | Create/read/update/delete records, column equality filters, pagination, sorting, and field selection                                                |
| SQL              | Execute SQL and return columns, rows, affected-row counts, and execution time                                                                       |
| Inspection       | Table metadata, foreign-key diagrams, indexes, triggers, database size/record counts, and request analytics                                         |
| Exports          | Consistent SQLite snapshot downloads and SQL dumps                                                                                                  |
| HTTP middleware  | Authentication, configurable CORS, IP rate limiting, telemetry, and error handling                                                                  |

Schema alteration is available, but a versioned application migration history is not. Snapshot exports do not include scheduled backup management or a restore endpoint. API keys are scoped to a database, without separate read-only or per-table permissions.

## Quick start

You need Go 1.24.1 or later and a C compiler for `mattn/go-sqlite3` (CGO).

```bash
git clone https://github.com/Annany2002/nebula-backend.git
cd nebula-backend
go mod download
cat > .env <<'ENV'
SERVER_PORT=8080
JWT_SECRET=replace-with-a-long-random-secret
JWT_EXPIRATION_HOURS=24
DATABASE_DIRECTORY=data
DATABASE_DIRECTORY_FILE=metadata.db
ALLOWED_ORIGINS=http://localhost:3000
ENV
# Replace JWT_SECRET with your own secret before starting.
make run
```

```bash
curl http://localhost:8080/ping
# pong
```

Run the frontend separately using the [frontend setup instructions](https://github.com/Annany2002/nebula-frontend#local-setup). Its development port is `3000`.

### Configuration

| Variable                  | Purpose                                          | Default                                  |
| ------------------------- | ------------------------------------------------ | ---------------------------------------- |
| `SERVER_PORT`             | HTTP listen port                                 | `8080`; `8085` when `APP_ENV=production` |
| `JWT_SECRET`              | JWT signing secret                               | Required                                 |
| `JWT_EXPIRATION_HOURS`    | JWT lifetime in hours                            | `24`                                     |
| `DATABASE_DIRECTORY`      | SQLite storage directory                         | `data`                                   |
| `DATABASE_DIRECTORY_FILE` | Platform metadata filename                       | `metadata.db`                            |
| `ALLOWED_ORIGINS`         | Space-separated CORS origins                     | All origins when empty or `*`            |
| `APP_ENV`                 | Set to `production` for production configuration | Unset                                    |

The configuration loader reads `.env` during development. Set production configuration in the process environment. For the existing production VPS, explicitly use `SERVER_PORT=8085`, preserve `.env` and `data/`, and restart only the backend if keeping the current tunnel URL.

### Docker and API documentation

```bash
docker compose up --build
```

The Compose stack starts the API on `8080` and the Mintlify documentation on `3000`. The docs service is separate from the frontend; use separate ports if running both. The stack mounts `./data` for persistent storage and reads your `.env`.

The [documentation source](docs/introduction.mdx) lives in `docs/`. API routes are defined in [api/router.go](api/router.go).

## Authentication

- Public `/auth/signup` and `/auth/login` do not require credentials.
- Account/profile and API key management use `Authorization: Bearer <jwt>`.
- Database data operations accept a user JWT or `Authorization: ApiKey <key>` for that database.
- Listing, creating, and deleting databases require a user JWT. An API key cannot access another database.

Signup requires a username of at least six characters and a password of at least eight characters. Signup returns `user_id` and a message; log in separately to receive a JWT.

```bash
curl -X POST http://localhost:8080/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"username":"builder","email":"builder@example.com","password":"change-this-password"}'

curl -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"builder@example.com","password":"change-this-password"}'
```

The full API key is returned only on generation. `GET /api/v1/account/databases/:db_name/apikey` returns `key_prefix` and `created_at`, not the secret. Generating a new key invalidates the previous one.

## REST example

Set `NEBULA_TOKEN` to the JWT returned by login. The following creates a database and table, inserts a record, and lists records:

```bash
curl -X POST http://localhost:8080/api/v1/databases \
  -H "Authorization: Bearer $NEBULA_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"db_name":"myapp"}'

curl -X POST http://localhost:8080/api/v1/databases/myapp/tables \
  -H "Authorization: Bearer $NEBULA_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"table_name":"customers","columns":[{"name":"name","type":"TEXT"},{"name":"email","type":"TEXT"}]}'

curl -X POST http://localhost:8080/api/v1/databases/myapp/tables/customers/records \
  -H "Authorization: Bearer $NEBULA_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Avery Chen","email":"avery@example.com"}'

curl 'http://localhost:8080/api/v1/databases/myapp/tables/customers/records?limit=25&offset=0&sort=id&order=asc&fields=id,name,email' \
  -H "Authorization: Bearer $NEBULA_TOKEN"
```

Table creation accepts `columns` or `schema`. It automatically adds `id INTEGER PRIMARY KEY AUTOINCREMENT` and `created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`; do not include these reserved columns in the request.

Supported column types are `TEXT`, `INTEGER`, `REAL`, `NUMERIC`, `BOOLEAN`, `DATETIME`, and `BLOB`. A column can specify `foreign_key` with `target_table`, `target_column`, `on_delete`, and `on_update`.

### Record listing

```json
{
  "records": [{ "id": 1, "name": "Avery Chen", "email": "avery@example.com" }],
  "pagination": { "total": 1, "limit": 25, "offset": 0 }
}
```

| Query parameter    | Behavior                                          |
| ------------------ | ------------------------------------------------- |
| `limit`            | Page size, 1–1000; default 100                    |
| `offset`           | Records to skip; default 0                        |
| `sort`             | Existing column to sort by                        |
| `order`            | `asc` or `desc`; default `asc`                    |
| `fields`           | Comma-separated columns                           |
| Other column names | Equality filters, for example `name=Avery%20Chen` |

### Schema alteration

```bash
curl -X POST http://localhost:8080/api/v1/databases/myapp/tables/customers/alter \
  -H "Authorization: Bearer $NEBULA_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"operations":[{"action":"add_column","column":{"name":"role","type":"TEXT","default_value":"member","not_null":true}},{"action":"rename_column","old_name":"name","new_name":"full_name"}]}'
```

The endpoint also accepts a single action. Supported actions are `add_column`, `rename_column`, `drop_column`, and `rename_table`. Dropping columns removes their data; take an export before destructive changes.

## API overview

Database paths below are relative to `/api/v1/databases/:db_name`.

| Method            | Path                                        | Purpose                                               |
| ----------------- | ------------------------------------------- | ----------------------------------------------------- |
| GET, POST         | `/api/v1/databases`                         | List or create databases                              |
| GET, DELETE       | `/api/v1/databases/:db_name`                | Database details or deletion                          |
| GET, POST         | `/tables`                                   | List or create tables                                 |
| POST              | `/schema`                                   | Create a table schema                                 |
| GET               | `/tables/:table_name/schema`                | Inspect column metadata                               |
| POST              | `/tables/:table_name/alter`                 | Alter table schema                                    |
| DELETE            | `/tables/:table_name`                       | Drop a table                                          |
| GET, POST         | `/tables/:table_name/records`               | List or create records                                |
| GET, PUT, DELETE  | `/tables/:table_name/records/:record_id`    | Read, update, or delete one record                    |
| POST              | `/sql`                                      | Execute a SQL query from a `query` JSON field         |
| GET               | `/diagram`, `/objects`, `/analytics`        | Schema relationships, database objects, and analytics |
| GET               | `/export/sql`, `/export/sqlite`             | SQL dump or SQLite snapshot                           |
| GET, PUT          | `/api/v1/account/user/me`                   | Read or update the account profile                    |
| GET, POST, DELETE | `/api/v1/account/databases/:db_name/apikey` | Key metadata, generation/rotation, or revocation      |

## Architecture

```text
cmd/server/main.go        # Configuration, storage initialization, HTTP server
config/                   # Environment configuration
api/
  router.go               # Routes and middleware
  handlers/               # Authentication, databases, tables, records
  middleware/             # Auth, rate limiting, errors, telemetry
  models/                 # Request and response DTOs
internal/
  auth/                   # JWT and password hashing
  core/                   # Validation, query parameters, schema SQL
  domain/                 # Domain models
  storage/                # Metadata DB, user DBs, snapshots, SQL export
  logger/                 # Logging
```

The default storage layout is `data/metadata.db` and `data/<user_id>/<db_name>.db`. Separate database files share the host's compute and disk resources.

## Development

```bash
make install-tools
make dev          # Hot reload with Air
make test         # Go unit and integration tests
make lint         # golangci-lint using the module's Go toolchain
make check        # Format, lint, and test
```

See [CONTRIBUTING.md](CONTRIBUTING.md). This project is [MIT licensed](LICENSE).
