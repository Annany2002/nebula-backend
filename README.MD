<div align="center">

# Nebula

**Open-source Backend-as-a-Service built with Go**

[![Go Version](https://img.shields.io/badge/Go-1.18+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

[Documentation](http://localhost:3000) | [API Reference](http://localhost:3000/api-reference/overview) | [Contributing](CONTRIBUTING.md)

</div>

---

## What is Nebula?

Nebula is a lightweight **Backend-as-a-Service (BaaS)** that lets you spin up a backend in minutes. Create databases, define schemas, and perform CRUD operations through a simple REST API.

### Key Features

| Feature | Description |
|---------|-------------|
| **Dual Authentication** | JWT tokens for user management, API keys for database access |
| **Dynamic Databases** | Create isolated SQLite databases on-the-fly |
| **Schema Management** | Define tables with custom columns and types |
| **Full CRUD** | Create, read, update, delete records with filtering |
| **Rate Limiting** | Built-in IP-based rate limiting |
| **CORS Support** | Configurable cross-origin policies |

## Quick Start

```bash
# Clone
git clone https://github.com/Annany2002/nebula-backend.git
cd nebula-backend

# Configure
cp .env.example .env
# Edit .env: set JWT_SECRET (required) and ALLOWED_ORIGINS

# Run
docker-compose up --build
```

**Services:**
- API: `http://localhost:8080`
- Docs: `http://localhost:3000`

### Verify Installation

```bash
curl http://localhost:8080/ping
# Returns: pong
```

## API Overview

### Authentication
```bash
# Sign up
curl -X POST http://localhost:8080/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "username": "myuser", "password": "password123"}'

# Login (returns JWT)
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "password": "password123"}'
```

### Using the API
```bash
# Create a database
curl -X POST http://localhost:8080/api/v1/databases \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"db_name": "myapp"}'

# Create a table
curl -X POST http://localhost:8080/api/v1/databases/myapp/schema \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"table_name": "users", "columns": [{"name": "name", "type": "TEXT"}]}'

# Insert a record
curl -X POST http://localhost:8080/api/v1/databases/myapp/tables/users/records \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"name": "John Doe"}'
```

## Tech Stack

| Category | Technology |
|----------|------------|
| Language | Go 1.18+ |
| Framework | Gin |
| Database | SQLite |
| Auth | JWT, API Keys, bcrypt |
| Linting | golangci-lint |

## Development

```bash
# Install development tools
make install-tools

# Run with hot-reload
air

# Run tests
make test

# Run linter
make lint

# Run all checks (fmt + lint + test)
make check
```

## Documentation

Full documentation is available via Mintlify:

```bash
docker-compose up --build
# Open http://localhost:3000
```

Covers:
- Getting started guide
- Architecture overview
- Complete API reference
- Deployment guides

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

```bash
git checkout -b feat/your-feature
make check  # Run before committing
```

## License

[MIT License](LICENSE) - feel free to use in your projects.

---

<div align="center">

**[GitHub](https://github.com/Annany2002/nebula-backend)** | **[Report Bug](https://github.com/Annany2002/nebula-backend/issues)** | **[Request Feature](https://github.com/Annany2002/nebula-backend/issues)**

</div>
