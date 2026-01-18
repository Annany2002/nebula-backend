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
- **Linting:** golangci-lint
- **Testing:** Go standard `testing`, `net/http/httptest`, `github.com/stretchr/testify/assert`

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

- Build the `nebula-backend` image on port `8080` and the `nginx` service.

### 5. Stopping and Removing Containers

To stop the services and remove the containers, run:

```bash
docker-compose down
```

This will stop and remove all containers defined in the `docker-compose.yml` file.

## Contributing

We welcome contributions!
Please check out the following before opening an issue or PR:

- [Contribution Guide](./CONTRIBUTING.md)
- [Bug Report Template](.github/ISSUE_TEMPLATE/bug_report.yaml)
- [Feature Request Template](.github/ISSUE_TEMPLATE/feature_request.yaml)
- [Pull Request Template](.github/PULL_REQUEST_TEMPLATE.md)

To begin:

```bash
# Fork + Clone
# Create a new branch
git checkout -b feat/your-feature

# Install dev tools
make install-tools

# Run checks before committing
make check

# Start dev server with hot-reload
air
```

## License

This project is licensed under the MIT License. See [LICENSE](https://github.com/Annany2002/nebula-backend/blob/main/LICENSE) for details.

## Acknowledgements

Thanks for checking out Nebula Backend! Join us in building a developer-friendly, open-source BaaS.
