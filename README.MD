# Nebula (v0.1.1+)

An improved Minimum Viable Product (MVP) for a Backend-as-a-Service (BaaS) built using Go. This version includes core management APIs, CORS support, rate limiting, structured logging, and flexible authentication.

## Description

This project demonstrates the core concepts of a BaaS, allowing authenticated users to:

1.  Register and Login securely (JWT for management, API Keys for data access).
2.  Request the creation of their own "databases", list them, and delete them.
3.  Define table schemas within those databases, retrieve schema info, list tables, and delete tables.
4.  Perform basic CRUD (Create, Read, Update, Delete) operations on records within their tables, with basic filtering support.
5.  Generate and use database-scoped API keys for programmatic access.
6.  Interact with the API within basic rate limits, respecting CORS policies.

**Simplification:** For this MVP, each user "database" is implemented as a separate SQLite file on the server (`data/<user_id>/<db_name>.db`), managed by the Go application. A central SQLite database (`data/metadata.db`) tracks users and database mappings.

**Improvements:** This version features a refactored structure (`cmd`, `api`, `internal`), externalized configuration (via `.env`/environment variables), centralized error handling, abstracted storage logic, stricter validation, foundational automated tests, DB/Table management APIs, CORS support, custom rate limiting, structured logging, and dual authentication support (JWT/ApiKey).

## Features

- **Authentication & Authorization:**
  - `POST /auth/signup`: User registration.
  - `POST /auth/login`: User login, returns JWT (primarily for account actions).
  - **Dual Auth Support:** `/api/v1` endpoints accept `Authorization: ApiKey <key>` (database-scoped) OR `Authorization: Bearer <token>` (user-scoped JWT). Middleware determines scope.
  - `POST /api/v1/account/databases/{db_name}/apikeys`: (JWT Protected) Generate a new database-scoped API key.
- **Protected API Group (`/api/v1` & `/api/v1/account`):** Basic IP-based rate limiting and CORS applied.
  - `GET /api/v1/health`: Protected health check endpoint showing auth user ID and scope (user or specific database ID).
  - `GET /api/v1/user/:user_id`: Protected endpoint to find user details (Access permissions might apply within handler).
- **Database Management:**
  - `POST /api/v1/databases`: Register a new database name (requires user-scoped auth).
  - `GET /api/v1/databases`: List database names registered by the user (requires user-scoped auth).
  - `DELETE /api/v1/databases/{db_name}`: Delete a database registration (requires user-scoped auth, attempts file deletion).
- **Schema & Table Management:**
  - `POST /api/v1/databases/{db_name}/schema`: Define a table schema within the specified database.
  - `GET /api/v1/databases/{db_name}/tables/{table_name}/schema`: Retrieve schema info for a specific table.
  - `GET /api/v1/databases/{db_name}/tables`: List tables within a specific database.
  - `DELETE /api/v1/databases/{db_name}/tables/{table_name}`: Delete (drop) a table within a specific database.
- **Record CRUD Operations:**
  - `POST /api/v1/databases/{db_name}/tables/{table_name}/records`: Create a new record (with type validation).
  - `GET /api/v1/databases/{db_name}/tables/{table_name}/records`: List records in a table (supports basic `?column=value` filtering).
  - `GET /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Get a single record by ID.
  - `PUT /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Update an existing record (with type validation).
  - `DELETE /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Delete a record by ID.
- **Cross-Cutting Concerns:**
  - CORS enabled (configurable via `.env`).
  - Custom IP-based Rate Limiting middleware active.
  - Custom Structured Logging implemented (`internal/logger`).
  - Centralized Error Handling middleware.

## Technology Stack

- **Language:** Go
- **Web Framework:** Gin (`github.com/gin-gonic/gin`)
- **Database:** SQLite (`github.com/mattn/go-sqlite3`)
- **Authentication:** JWT (`github.com/golang-jwt/jwt/v5`), API Keys
- **Password Hashing:** Bcrypt (`golang.org/x/crypto/bcrypt`)
- **Configuration:** `github.com/joho/godotenv`
- **CORS:** `github.com/gin-contrib/cors`
- **Rate Limiting:** Custom Middleware
- **Logging:** Custom Structured Logger (`internal/logger`)
- **Testing:** Go standard `testing`, `net/http/httptest`, `github.com/stretchr/testify/assert`

## Project Structure

```bash
nebula-backend/
├── api/                 # API layer (HTTP handlers, routing, middleware, DTOs)
│   ├── handlers/
│   ├── middleware/
│   ├── models/
│   └── router.go
├── cmd/                 # Main application(s) entry point
│   └── server/
│       └── main.go
├── config/              # Configuration loading
├── internal/            # Internal application logic
│   ├── auth/
│   ├── core/
│   ├── domain/
│   ├── logger/          # Structured logger implementation
│   └── storage/
├── data/                # Runtime data (SQLite files) - ignored by git
├── .env.example         # Example environment file
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

_*(Key directories explained: `cmd`=entrypoint, `api`=HTTP layer, `internal`=core logic/storage, `config`=config loading, `data`=runtime files)*_

## Setup & Running

**Prerequisites:**

- Go (version 1.18+ recommended).

**Steps:**

1.  **Clone:** `git clone https://github.com/Annany2002/nebula-backend.git && cd nebula-backend`
2.  **Dependencies:** `go mod tidy`
3.  **Configuration:**
    - `cp .env.example .env`
    - **Edit `.env`:**
      - **CRITICAL:** Set a strong, unique `JWT_SECRET`.
      - Set `ALLOWED_ORIGINS` (space-separated list of frontend origins for CORS, e.g., `"http://localhost:3000 http://your-frontend.com"`).
      - Adjust `SERVER_PORT`, `JWT_EXPIRATION_HOURS`, etc., if needed.
    - Ensure `.env` is in `.gitignore`.
4.  **Run:**
    ```bash
    go run ./cmd/server/main.go
    # OR using air (if installed)
    # air
    ```
    Server starts on the configured port (default `:8080`).

## API Usage / Testing Examples (`curl`)

_(Replace `<token>`, `<api_key>`, `<db_name>`, etc. Use your actual values)_

1.  **Signup:** `curl -X POST ... /auth/signup ...`
2.  **Login:** `curl -X POST ... /auth/login` -> **Save `<jwt_token>`**
3.  **Generate API Key:** _(Requires JWT Auth)_
    ```bash
    TOKEN="<jwt_token>"
    DB_NAME="my_app_db" # Assumes this DB was created first
    curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
         -d '{"label": "my_app_key"}' \
         http://localhost:8080/api/v1/account/databases/$DB_NAME/apikeys
    ```
    -> **Save `<api_key>` (e.g., `neb_...`)**
4.  **List Databases:** _(Requires User-Scope Auth: JWT or User API Key if implemented)_
    ```bash
    TOKEN="<jwt_token_or_user_api_key>" # Use appropriate scheme (Bearer or ApiKey)
    curl -X GET -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/databases
    ```
5.  **List Tables:** _(Requires DB-Scoped ApiKey OR User-Scoped Auth)_
    ```bash
    API_KEY="<api_key>" # e.g., neb_...
    DB_NAME="my_app_db"
    curl -X GET -H "Authorization: ApiKey $API_KEY" http://localhost:8080/api/v1/databases/$DB_NAME/tables
    ```
6.  **Create Record:** _(Requires DB-Scoped ApiKey OR User-Scoped Auth)_
    ```bash
    API_KEY="<api_key>"
    DB_NAME="my_app_db"
    TABLE_NAME="tasks"
    curl -X POST -H "Authorization: ApiKey $API_KEY" -H "Content-Type: application/json" \
         -d '{ "description": "Test API Key", "priority": 1 }' \
         http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records
    ```
7.  **List Records (Filter):** _(Requires DB-Scoped ApiKey OR User-Scoped Auth)_
    ```bash
    API_KEY="<api_key>"
    DB_NAME="my_app_db"
    TABLE_NAME="tasks"
    curl -X GET -H "Authorization: ApiKey $API_KEY" "http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records?priority=1"
    ```
    (See previous README version or test files for other CRUD examples)\_

## Limitations & Future Work

This remains an MVP. Key limitations and future directions:

- **Scalability:** SQLite file-per-user is the primary bottleneck. **Next Step:** Migrate to PostgreSQL/MySQL.
- **Querying:** Only basic equality filtering. **Next Step:** Implement pagination, sorting, advanced filtering.
- **Testing:** Foundational tests exist, but comprehensive integration test coverage for all APIs (CRUD, Mgmt) is needed.
- **Next Step:** Add more tests.
- **Security:** Basic rate limiting exists. Needs user-based rate limits, deeper sanitization, RBAC.
- **Migrations:** No system for schema evolution. **Next Step:** Implement migration tooling.
- **API Documentation:** No auto-generated docs. **Next Step:** Add Swagger/OpenAPI.
- **Missing BaaS Features:** File storage, real-time, etc.

## License
