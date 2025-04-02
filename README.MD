# Nebula

A Minimum Viable Product (MVP) for a Backend-as-a-Service (BaaS) built using Go.

## Description

This project demonstrates the core concepts of a BaaS, allowing authenticated users to:

1.  Register and Login.
2.  Request the creation of their own "databases".
3.  Define table schemas within those databases.
4.  Perform basic CRUD (Create, Read, Update, Delete) operations on records within their tables.

**Simplification:** For this MVP, each user "database" is implemented as a separate SQLite file on the server (`data/<user_id>/<db_name>.db`), managed by the Go application. A central SQLite database (`data/metadata.db`) tracks users and database mappings.

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
  - `POST /api/v1/databases/{db_name}/tables/{table_name}/records`: Create a new record.
  - `GET /api/v1/databases/{db_name}/tables/{table_name}/records`: List all records in a table.
  - `GET /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Get a single record by ID.
  - `PUT /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Update an existing record.
  - `DELETE /api/v1/databases/{db_name}/tables/{table_name}/records/{record_id}`: Delete a record by ID.

## Technology Stack

- **Language:** Go
- **Web Framework:** Gin (`github.com/gin-gonic/gin`)
- **Database:** SQLite (`github.com/mattn/go-sqlite3`)
- **Authentication:** JWT (`github.com/golang-jwt/jwt/v5`)
- **Password Hashing:** Bcrypt (`golang.org/x/crypto/bcrypt`)

## Current Project Structure (Simplified)

```bash
nebula-backend/
├── data/ # Runtime SQLite database files (ignored by git)
├── go.mod # Go modules file
├── go.sum # Go modules checksums
├── .gitignore # Git ignore rules
├── config.go # Configuration variables (JWT secret, DB paths)
├── database.go # Metadata DB initialization
├── models.go # Data structure definitions (structs)
├── auth.go # Authentication logic (handlers, middleware, utils)
├── db_handlers.go # Handlers for DB/Schema/Record operations
└── main.go # Main application entry point, server setup, routing
```

## Setup & Running

**Prerequisites:**

- Go (version 1.18 or higher recommended) installed.

**Steps:**

1.  **Clone the repository (Example):**

    ```bash
    git clone https://github.com/Annany2002/nebula-backend.git
    cd nebula-backend
    ```

2.  **Install Dependencies:**

    ```bash
    go mod tidy
    ```

    _(Or `go get ./...` if needed)_

3.  **Configuration:**

    - **IMPORTANT:** Change the placeholder `jwtSecretKey` in `config.go` to a strong, unique secret key. In a real application, load this from environment variables or a secure configuration source.
    - You can adjust `jwtExpiration` in `config.go`.
    - The server runs on port `:8080` by default (defined in `main.go`).

4.  **Run the Server:**

    ```bash
    go run .
    ```

    or

    ```bash
    air
    ```

    The server should start, initialize the database in the `data/` directory, and listen on port 8080.

## API Usage / Testing Examples (`curl`)

_(Replace `<token>`, `<db_name>`, `<table_name>`, `<record_id>` with actual values)_

1.  **Signup:**

    ```bash
    curl -X POST -H "Content-Type: application/json" \
         -d '{"email": "[email address removed]", "password": "password123"}' \
         http://localhost:8080/auth/signup
    ```

2.  **Login:**

    ```bash
    curl -X POST -H "Content-Type: application/json" \
         -d '{"email": "[email address removed]", "password": "password123"}' \
         http://localhost:8080/auth/login
    ```

    _(Save the `token` from the response for subsequent requests)_

3.  **Create Database:**

    ```bash
    TOKEN="<your_jwt_token>"
    curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
         -d '{"db_name": "my_app_db"}' \
         http://localhost:8080/api/v1/databases
    ```

4.  **Define Schema:**

    ```bash
    TOKEN="<your_jwt_token>"
    DB_NAME="my_app_db"
    curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
         -d '{ "table_name": "tasks", "columns": [ {"name": "description", "type": "TEXT"}, {"name": "priority", "type": "INTEGER"}, {"name": "done", "type": "BOOLEAN"} ] }' \
         http://localhost:8080/api/v1/databases/$DB_NAME/schema
    ```

5.  **Create Record:**

    ```bash
    TOKEN="<your_jwt_token>"
    DB_NAME="my_app_db"
    TABLE_NAME="tasks"
    curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
         -d '{ "description": "Finish README", "priority": 1, "done": false }' \
         http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records
    ```

    _(Note the returned `record_id`)_

6.  **List Records:**

    ```bash
    TOKEN="<your_jwt_token>"
    DB_NAME="my_app_db"
    TABLE_NAME="tasks"
    curl -X GET -H "Authorization: Bearer $TOKEN" \
         http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records
    ```

7.  **Get Single Record:**

    ```bash
    TOKEN="<your_jwt_token>"
    DB_NAME="my_app_db"
    TABLE_NAME="tasks"
    RECORD_ID=1 # Use an actual ID
    curl -X GET -H "Authorization: Bearer $TOKEN" \
         http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records/$RECORD_ID
    ```

8.  **Update Record:**

    ```bash
    TOKEN="<your_jwt_token>"
    DB_NAME="my_app_db"
    TABLE_NAME="tasks"
    RECORD_ID=1 # Use an actual ID
    curl -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
         -d '{ "priority": 2, "done": true }' \
         http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records/$RECORD_ID
    ```

9.  **Delete Record:**
    ```bash
    TOKEN="<your_jwt_token>"
    DB_NAME="my_app_db"
    TABLE_NAME="tasks"
    RECORD_ID=1 # Use an actual ID
    curl -X DELETE -H "Authorization: Bearer $TOKEN" \
         http://localhost:8080/api/v1/databases/$DB_NAME/tables/$TABLE_NAME/records/$RECORD_ID
    ```
    _(Expects a `204 No Content` response)_

## Limitations & Future Work

This is an MVP and lacks many features of a production-ready BaaS:

- **Configuration:** Secrets (JWT key) hardcoded; should use environment variables or config files.
- **Scalability:** SQLite file-per-user has concurrency and scaling limitations. Replace with PostgreSQL/MySQL/etc.
- **Error Handling:** Basic error handling; needs more structured and informative error responses.
- **Querying:** Only basic CRUD; needs filtering, sorting, pagination, relations.
- **Data Validation:** Only basic type checking; needs more complex validation rules.
- **Migrations:** No system for evolving the `metadata.db` schema.
- **Security:** Needs rate limiting, deeper input sanitization, role-based access control, resource usage limits.
- **Database/Table Management:** No endpoints to list/delete databases or tables.
- **Logging:** Basic `log` package usage; needs structured logging.
- **Testing:** No automated unit or integration tests.
- **API Documentation:** No Swagger/OpenAPI specification.
