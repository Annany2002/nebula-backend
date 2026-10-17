# Contributing to Nebula Backend

Use issues for reproducible bugs, feature proposals and documentation corrections. Keep pull requests focused and include tests for changes to authentication, authorization, persistence or API behavior. Never include credentials, live database files or account recovery links in issues or commits.

## Development setup

Use the Go version declared in [go.mod](go.mod), a C compiler for the SQLite CGO driver, Git and Make. Docker is optional.

```bash
git clone https://github.com/<your-username>/nebula-backend.git
cd nebula-backend
cp .env.example .env
# Set JWT_SECRET and local ALLOWED_ORIGINS in .env.
make run
```

The API listens on port 8080 locally. Run `make dev` with Air installed for automatic reloads. In a workspace containing all three repositories, run Git commands in the relevant repository, not the parent directory.

## Account email testing

Run `./scripts/dev-email.sh` for local Mailpit, isolated accounts and a TLS-authenticated capture inbox at `http://localhost:8025`. It enables verification and password recovery without changing `.env`, normal local data or system certificate trust. Start the frontend on port 3000. The workspace runner `./dev.sh --email` starts both applications and the inbox together.

See the [configuration guide](docs/guides/configuration.mdx) for production SMTP. Keep production mail disabled in tests; use the injectable `email.Sender` boundary for deterministic delivery failures and captured messages.

## Verification

```bash
go test -race ./...
make lint
```

Run `make fmt` to apply Go formatting and import order. `make install-tools` installs the development tools used by the Makefile. Use temporary SQLite files for storage tests, and cover rollback, replay, concurrent writes, cancellation and authorization boundaries where relevant. Use the SQLite backup API for a consistent live snapshot; copying a WAL database file alone is unsafe.

## Pull requests

Start a branch from current `origin/main`, use conventional commit messages and group related changes. Describe the problem, resulting behavior, verification and deployment requirements. Update the relevant API reference and configuration docs with contract changes. The [PR template](.github/ISSUE_TEMPLATE/PULL_REQUEST_TEMPLATE.md) and [issue templates](.github/ISSUE_TEMPLATE/) are available in this repository.

The Go server, frontend and SDK are separate repositories. Coordinate compatibility changes explicitly; leave opt-in features disabled until compatible clients are deployed. Do not overwrite runtime data or environment files during deployment. Restart only the backend when preserving a running Cloudflare Quick Tunnel URL.

## Documentation and license

The backend [docs directory](docs/) is the canonical REST API and operator reference. See [documentation publishing](docs/guides/documentation.mdx) for Mintlify setup and validation. The [README](README.MD) summarizes features and onboarding. Write direct instructions, use working links, and distinguish implemented behavior from planned capabilities.

Contributions are licensed under [MIT](LICENSE). Keep discussions respectful and report security-sensitive details privately rather than posting secrets publicly.
