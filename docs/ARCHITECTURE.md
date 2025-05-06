# Nebula Backend Architecture

## 1. Introduction

This document outlines the architecture of the Nebula BaaS backend application (as of v0.2.0). Nebula BaaS provides essential backend services (authentication, dynamic database/schema management, CRUD operations) via a RESTful API, allowing developers to quickly bootstrap their applications.

The primary deployment target for this architecture is a **containerized environment orchestrated by Kubernetes** (or similar platforms like Docker Swarm, Nomad, managed services like AWS ECS/EKS, Google GKE).

## 2. Guiding Principles

- **Layered Architecture:** Code is organized into distinct layers (API, Internal Core Logic, Storage) with clear dependencies (API depends on Internal, Internal depends on Storage/Domain).
- **Separation of Concerns:** Different functionalities (HTTP handling, authentication logic, database interaction, configuration) reside in separate packages.
- **Dependency Injection:** Dependencies (Database connections, Configuration) are explicitly passed to components, avoiding global state and improving testability.
- **API-First:** The primary way to interact with the service is through its defined RESTful API.
- **Stateless Application Logic:** The backend application containers themselves aim to be stateless; all persistent state resides in the database layer. This is crucial for horizontal scaling and resilience in orchestrated environments.

## 3. High-Level Overview

The system handles requests through the following conceptual flow:

`Client/SDK -> Internet -> Load Balancer / K8s Ingress -> Nginx (Optional Reverse Proxy/TLS) -> Backend Go Container(s) -> Metadata DB (SQLite on PV) / User DB Files (SQLite on PV)`

- **Load Balancer/Ingress:** Distributes incoming traffic across running backend container instances (pods in K8s). Handles SSL/TLS termination.
- **Backend Container(s):** Run the compiled Go application (built via `Dockerfile`). Multiple replicas can be run for high availability and scaling (limited by DB backend).
- **Persistent Storage:** A Kubernetes Persistent Volume (PV) via a Persistent Volume Claim (PVC) must be mounted to the `/backend/data` path within the container(s) to store the `metadata.db` and all user database files (`<userID>/<dbName>.db`). This ensures data persistence across container restarts/redeployments.

## 4. Component Breakdown (Directory Structure)

- **`cmd/server/main.go`:**

  - Application entry point.
  - Loads configuration (`config/`).
  - Initializes metadata database connection pool (`internal/storage`).
  - Sets up dependency injection wiring (initializes handlers/repositories/services).
  - Configures and starts the Gin HTTP server (`api/`).

- **`config/`:**

  - Defines `Config` struct.
  - Loads configuration from Environment Variables (critical for containerized deployments) with optional `.env` support for local development.

- **`api/` (API Layer - Gin Framework):**

  - **`router.go`:** Defines HTTP routes, maps routes to handlers, applies middleware (global and group-specific).
  - **`handlers/`:** Contains Gin handlers (`AuthHandler`, `DatabaseHandler`, `TableHandler`, `RecordHandler`). Responsible for parsing requests, calling internal logic, formatting responses, and propagating errors (`c.Error()`).
  - **`middleware/`:** Contains Gin middleware for cross-cutting concerns:
    - `ErrorHandler`: Centralized error handling and JSON response formatting.
    - `CombinedAuthMiddleware`: Handles both `ApiKey` (database-scoped) and `Bearer` JWT (user-scoped) authentication, setting `userID` and `databaseID` (or nil) in the context.
    - `RateLimiter`: Provides basic IP-based rate limiting (using in-memory store - consider Redis for multi-replica).
    - `CORS`: Handles Cross-Origin Resource Sharing headers (configurable via Env Vars).
  - **`models/`:** Defines Data Transfer Objects (DTOs) - Go structs matching API JSON request/response payloads.

- **`internal/` (Internal Core Logic Layer):**

  - Framework-agnostic core application logic.
  - **`domain/`:** Defines core business entities (e.g., `User`).
  - **`storage/`:** Data Access Layer. Handles all direct database interactions.
    - `database.go`: Connects to metadata DB, ensures schema, applies PRAGMAs (WAL, busy_timeout).
    - `metadata_repo.go`: Functions querying/modifying the central `metadata.db` (users, databases, api_keys tables).
    - `userdb_repo.go`: Functions for connecting to and operating on individual user SQLite files (`data/<uid>/<db>.db`), including CRUD, schema operations (`PRAGMA`). Requires paths provided by handlers after metadata lookup.
    - Defines storage-specific errors (e.g., `ErrNotFound`, `ErrConflict`).
  - **`auth/`:** Core authentication logic (password hashing/comparison, JWT generation/validation, API key hash comparison). Defines auth-specific errors.
  - **`core/`:** Shared utilities (e.g., identifier validation).
  - **`logger/`:** Implementation for structured logging throughout the application.

- **`migrations/`:** Contains SQL files for managing `metadata.db` schema changes using a migration tool (like `golang-migrate/migrate` or `pressly/goose`). Essential for reliable schema evolution.

- **`infra/`:** Holds infrastructure definitions:
  - **`docker/`**: `Dockerfile` for building the backend container image, potentially `docker-compose.yml` for local dev, example Nginx config.
  - **`k8s/`**: Kubernetes manifests (`Deployment`, `Service`, `Ingress`, `ConfigMap`, `Secret`, `PersistentVolumeClaim` for `/backend/data`).

## 5. Request Flow Example (Create Record with ApiKey)

1.  `Client Request` -> `K8s Ingress / Load Balancer` -> `Nginx (optional)` -> `Backend Pod`
2.  `Gin Engine` -> `Global Middleware` (Rate Limiter, Error Handler)
3.  `Router` matches `POST /api/v1/databases/{db_name}/tables/{table_name}/records`
4.  `Combined Auth Middleware`:
    - Detects `ApiKey ...` header.
    - Validates key via `storage` functions (prefix lookup, hash compare).
    - Sets `userID` & specific `databaseID` in context. `c.Next()`.
5.  `Record Handler` (`CreateRecord`):
    - Gets context (`userID`, `databaseID`), path params (`dbName`, `tableName`).
    - **Authorization:** Verifies `dbName` belongs to `userID` AND matches the `databaseID` from the key's scope via `storage` calls. Returns 403 via `c.Error()` if mismatch.
    - Looks up DB file path via `storage.FindDatabasePath`.
    - Connects to user DB file via `storage.ConnectUserDB`.
    - Fetches schema via `storage.PragmaTableInfo`.
    - Binds/Validates request body against schema. Returns 400 via `c.Error()` if invalid.
    - Calls `storage.InsertRecord`. Returns 500/409 via `c.Error()` if storage fails.
    - If successful, calls `c.JSON(201, ...)` to send response.
6.  `Error Handler Middleware`: If any handler called `c.Error(err)`, this middleware catches it, maps `err` to status/JSON, and sends the error response.

## 6. Data Storage & Concurrency

- **User Data:** Stored in individual **SQLite** files (`data/<userID>/<dbName>.db`).
- **Metadata:** Stored in a central **SQLite** file (`data/metadata.db`).
- **Concurrency:** WAL mode and busy timeouts are configured for `metadata.db` to improve read/write concurrency. However, SQLite still imposes **single-writer limitations per file**. This means `metadata.db` write operations are serialized, and writes to the _same_ user DB file are serialized. Writes to _different_ user DB files can happen concurrently.
- **Persistence in K8s:** Requires a **Persistent Volume Claim (PVC)** mounted at `/backend/data` in the container(s) to ensure data files survive pod restarts. The chosen PV type (e.g., EBS, EFS, Ceph) will impact performance and multi-node access capabilities (EBS is usually ReadWriteOnce).
- **Scalability Bottleneck:** The reliance on SQLite (both metadata and user files, especially if stored on a ReadWriteOnce volume) is the primary **scalability bottleneck**. High write loads or the need for features like read replicas necessitate migration to a database like PostgreSQL/MySQL (potentially hosted externally like RDS or within K8s).

## 7. Deployment Model

- **Containerization:** The Go application is built into a Docker image using `infra/docker/backend/Dockerfile`.
- **Orchestration:** Kubernetes (`infra/k8s/`) is the target orchestrator. Manifests define:
  - `Deployment`: Manages backend container replicas.
  - `Service`: Exposes the backend pods internally within the cluster.
  - `Ingress`: Manages external access to the Service, often handling hostname routing and TLS termination.
  - `ConfigMap`/`Secret`: Inject configuration (like non-sensitive env vars) and secrets (like JWT secret, DB credentials if migrated) into containers.
  - `PersistentVolumeClaim`: Requests persistent storage for the `/backend/data` directory.
- **CI/CD:** The GitHub Actions pipeline automates testing, building the Docker image, pushing it to a registry (like Docker Hub, ECR, GCR), and applying the Kubernetes manifests (`kubectl apply -f infra/k8s/`) or using tools like Helm/ArgoCD.

## 8. Future Directions

- **Database Migration:** Replace SQLite with PostgreSQL/MySQL for scalability.
- **Advanced Querying:** Add pagination, sorting, complex filtering APIs.
- **Service Layer:** Introduce a dedicated service layer between handlers and storage for more complex business logic.
- **Testing:** Comprehensive integration and end-to-end tests.
- **Observability:** Enhanced structured logging, metrics (Prometheus), tracing (OpenTelemetry).
