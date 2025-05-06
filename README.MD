# Nebula (v0.2.0)

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
- Docker and Docker Compose

### 1. Clone the Repository

```bash
git clone https://github.com/Annany2002/nebula-backend.git
cd nebula-backend
```

### 2. Build the Docker Image

To build the Docker image for the Nebula Backend, run the following command:

```bash
docker build -t nebula-backend .
```

### 3. Create and Configure the `.env` File

- `cp .env.example .env`
- **Edit `.env`:**
  - **CRITICAL:** Set a strong, unique `JWT_SECRET`.
  - Set `ALLOWED_ORIGINS` (space-separated list of frontend origins for CORS, e.g., `"http://localhost:3000 http://your-frontend.com"`).
  - Adjust `SERVER_PORT`, `JWT_EXPIRATION_HOURS`, etc., if needed.
  - Ensure `.env` is in `.gitignore`.

### 4. Run with Docker Compose(Upcoming)

To start the application using Docker Compose:

```bash
docker-compose up --build
```

This will:

- Build the `nebula-backend` image and the `nginx` service.
- Start the backend service (with 5 replicas as specified).
- Start the Nginx reverse proxy to handle external traffic.

The backend API will be available on port `8080`, and Nginx will be available on port `80`.

### 5. Scaling the Backend

To scale the backend service to a different number of replicas, modify the `replicas` value in `docker-compose.yml` and redeploy:

```yaml
deploy:
  replicas: 10 # Scale the backend service to 10 replicas
```

Then, run:

```bash
docker-compose up --scale nebula-backend=10
```

### 6. Stopping and Removing Containers

To stop the services and remove the containers, run:

```bash
docker-compose down
```

This will stop and remove all containers defined in the `docker-compose.yml` file.

## API Usage / Testing Examples (`curl`)

_(Replace `<token>`, `<api_key>`, `<db_name>`, etc. with actual values)_

1. **Signup**: `curl -X POST ... /auth/signup ...`
2. **Login**: `curl -X POST ... /auth/login` -> **Save `<jwt_token>`**
3. **Generate API Key**: _(Requires JWT Auth)_

   ```bash
   TOKEN="<jwt_token>"
   DB_NAME="my_app_db" # Assumes this DB was created first
   curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
        -d '{"label": "my_app_key"}' \
        http://localhost:8080/api/v1/account/databases/$DB_NAME/apikeys
   ```

   -> **Save `<api_key>` (e.g., `neb_...`)**

## Conclusion

With Docker and Docker Compose, you can now easily deploy and scale your Nebula backend. The setup also includes Nginx as a reverse proxy, making it easy to manage traffic and handle load balancing.
