# Nebula

An Minimum Viable Product (MVP) for a Backend-as-a-Service (BaaS) built using Go.

## Description

This project demonstrates the core concepts of a BaaS, allowing authenticated users to:

1.  Register and Login securely (JWT-based).
2.  Request the creation of their own "databases".
3.  Define table schemas within those databases.
4.  Perform basic CRUD (Create, Read, Update, Delete) operations on records within their tables, with basic filtering support.

**Simplification:** For this MVP, each user "database" is implemented as a separate SQLite file on the server (`data/<user_id>/<db_name>.db`), managed by the Go application. A central SQLite database (`data/metadata.db`) tracks users and database mappings.

**Improvements:** This version features a refactored structure (`cmd`, `api`, `internal`), externalized configuration (via `.env`/environment variables), centralized error handling, abstracted storage logic, stricter validation, and foundational automated tests.

## Features

- **Authentication:**
  - `POST /auth/signup`: User registration.
  - `POST /auth/login`: User login, returns JWT.
- **Protected API Group (`/api/v1`):** Requires `Authorization: Bearer <token>` header.
  - `GET /api/v1/me`: Test endpoint to check authentication and get user ID.
- **Database Management:**
  - `POST /api/v1/databases`: Register a new database name for the user.
  - `POST /api/v1/databases/{db_name}/schema`: Define a table schema within a registered database.
- **Record CRUD Operations:**
  - `POST /api/v1/databases/{db_name}/tables/{table_name}/records`: Create a new record (with type validation).
  - `GET /api/v1/databases/{db_name}/tables/{table_name}/records`: List records in a table (supports basic `?column=value` filtering).
  - `GET /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Get a single record by ID.
  - `PUT /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Update an existing record (with type validation).
  - `DELETE /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Delete a record by ID.

## Technology Stack

- **Language:** Go
- **Web Framework:** Gin (`github.com/gin-gonic/gin`)
- **Database:** SQLite (`github.com/mattn/go-sqlite3`)
- **Authentication:** JWT (`github.com/golang-jwt/jwt/v5`)
- **Password Hashing:** Bcrypt (`golang.org/x/crypto/bcrypt`)
- **Configuration:** `github.com/joho/godotenv` (for `.env` loading)
- **Testing:** Go standard `testing`, `net/http/httptest`, `github.com/stretchr/testify/assert`

## Project Structure

```bash
nebula-backend/
├── api/                 # API layer (HTTP handlers, routing, middleware, DTOs)
│   ├── handlers/        # Gin handlers (auth_handler.go, database_handler.go, record_handler.go)
│   ├── middleware/      # Middleware (auth_middleware.go, error_handler.go)
│   ├── models/          # Request/Response DTOs (auth_models.go, database_models.go)
│   └── router.go        # Gin router setup and route definitions
├── cmd/                 # Main application(s) entry point
│   └── server/          # Specific server application binary
│       └── main.go      # Main func, dependency injection, starts server
├── config/              # Configuration loading (config.go)
├── internal/            # Internal application logic (not for external use)
│   ├── auth/            # Authentication core logic (hashing, jwt)
│   ├── core/            # Core helper functions (e.g., validation.go)
│   ├── domain/          # Core domain models (e.g., User struct)
│   └── storage/         # Data access layer (metadata_repo.go, userdb_repo.go, database.go)
├── data/                # Runtime data (SQLite files) - ignored by git
├── .env.example         # Example environment file (Copy to .env)
├── .gitignore           # Git ignore rules
├── go.mod               # Go modules file
├── go.sum               # Go modules checksums
└── README.md            # This file
```

- **`cmd/server/main.go`**: Application entry point. Initializes dependencies (config, DB) and starts the API server.
- **`api/`**: Handles HTTP requests/responses, routing, middleware, and API-specific data models (DTOs).
- **`internal/`**: Contains core business logic, domain models, and data storage interactions, independent of the API layer.
- **`config/`**: Manages application configuration loading.
- **`data/`**: Stores runtime data (SQLite files). Not committed to Git.

## Setup & Running

**Prerequisites:**

- Go (version 1.18 or higher recommended) installed.

**Steps:**

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/Annany2002/nebula-backend.git
    cd nebula-backend
    ```

2.  **Install Dependencies:**

    ```bash
    go mod tidy
    ```

3.  **Configuration:**

    - This project uses environment variables for configuration, with support for a `.env` file for local development.
    - **Copy the example:** `cp .env.example .env`
    - **Edit `.env`:**
      - **CRITICAL:** Set `JWT_SECRET` to a strong, unique, randomly generated secret key (at least 32 characters recommended). **Do not use the default placeholder.**
      - Adjust `SERVER_PORT`, `JWT_EXPIRATION_HOURS`, `DB_DIR`, `DB_FILE` if needed (defaults are usually fine for local dev).
    - Ensure the `.env` file is listed in your `.gitignore`.

4.  **Run the Server:**
    ```bash
    go run ./cmd/server/main.go
    ```
    or using `air` for live reloading (if installed):
    ```bash
    air
    ```
    The server should start, load configuration, initialize the database in the `data/` directory, and listen on the configured port (default `:8080`).

## API Usage / Testing Examples (`curl`)

_(The API endpoints remain the same. Replace `<token>`, `<db_name>`, etc. with actual values. Use the email/password you signed up with.)_

1.  **Signup:**
    ```bash
    curl -X POST -H "Content-Type: application/json" -d '{"email": "user@example.com", "password": "password123"}' http://localhost:8080/auth/signup
    ```
2.  **Login:**
    ```bash
    curl -X POST -H "Content-Type: application/json" -d '{"email": "user@example.com", "password": "password123"}' http://localhost:8080/auth/login
    # Save the token from the response
    ```
3.  **Create Database:** _(Requires valid token)_
    ```bash
    TOKEN="<your_jwt_token>"
    curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"db_name": "my_app_db"}' http://localhost:8080/api/v1/databases
    ```
4.  **Define Schema:** _(Requires valid token)_
    ```bash
    # ... set TOKEN and DB_NAME=my_app_db ...
    curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
         -d '{ "table_name": "tasks", "columns": [ {"name": "description", "type": "TEXT"}, {"name": "priority", "type": "INTEGER"}, {"name": "done", "type": "BOOLEAN"} ] }' \
         http://localhost:8080/api/v1/databases/$DB_NAME/schema
    ```
5.  **Create Record:** _(Requires valid token)_
    ```bash
    # ... set TOKEN, DB_NAME=my_app_db, TABLE_NAME=tasks ...
    curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
         -d '{ "description": "Document new structure", "priority": 1, "done": false }' \
         http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records
    ```
6.  **List Records (with Filter):** _(Requires valid token)_
    ```bash
    # ... set TOKEN, DB_NAME=my_app_db, TABLE_NAME=tasks ...
    # Example filter:
    curl -X GET -H "Authorization: Bearer $TOKEN" "http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records?priority=1&done=false"
    ```
7.  **Get Single Record:** _(Requires valid token)_
    ```bash
    # ... set TOKEN, DB_NAME, TABLE_NAME, RECORD_ID=1 ...
    curl -X GET -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records/$RECORD_ID
    ```
8.  **Update Record:** _(Requires valid token)_
    ```bash
    # ... set TOKEN, DB_NAME, TABLE_NAME, RECORD_ID=1 ...
    curl -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
         -d '{ "done": true }' \
         http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records/$RECORD_ID
    ```
9.  **Delete Record:** _(Requires valid token)_
    ```bash
    # ... set TOKEN, DB_NAME, TABLE_NAME, RECORD_ID=1 ...
    curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records/$RECORD_ID
    ```

## Limitations & Future Work

While significantly improved, this remains an MVP and has limitations for production use:

- **Scalability:** The core SQLite file-per-user approach has inherent concurrency and scaling limitations. **Next Step:** Migrate to PostgreSQL/MySQL.
- **Querying:** Basic equality filtering exists, but advanced querying (sorting, pagination, complex operators, relations) is missing. **Next Step:** Implement pagination and sorting.
- **Database/Table Management:** No API endpoints to list/delete databases or tables. **Next Step:** Add management endpoints.
- **Migrations:** No system for evolving the `metadata.db` schema or user schemas. **Next Step:** Implement a migration tool (e.g., `golang-migrate`).
- **Security:** Foundational security is better (no hardcoded secrets), but needs rate limiting, deeper input sanitization, Role-Based Access Control (RBAC), resource usage limits.
- **Data Validation:** Type checking exists, but more complex business logic validation rules are needed.
- **Testing:** Basic auth integration tests and validation unit tests exist, but comprehensive coverage (especially for CRUD operations) is needed. **Next Step:** Add integration tests for all endpoints.
- **Logging:** Uses standard `log`; should be upgraded to structured logging (e.g., `zerolog`, `zap`).
- **API Documentation:** No Swagger/OpenAPI specification. **Next Step:** Add Swagger generation.
- **Missing BaaS Features:** Lacks file storage, serverless functions, real-time features, etc.
