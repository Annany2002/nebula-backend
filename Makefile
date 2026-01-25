# Nebula Backend Makefile
# Run 'make help' to see available commands

.PHONY: help build run test lint fmt clean dev install-tools

# Default target
help:
	@echo "Available commands:"
	@echo "  make build         - Build the application"
	@echo "  make run           - Run the application"
	@echo "  make dev           - Run with hot-reload (air)"
	@echo "  make test          - Run all tests"
	@echo "  make test-coverage - Run tests with coverage"
	@echo "  make lint          - Run golangci-lint"
	@echo "  make lint-fix      - Run golangci-lint with auto-fix"
	@echo "  make fmt           - Format code with gofmt and goimports"
	@echo "  make check         - Run fmt, lint, and test"
	@echo "  make clean         - Remove build artifacts"
	@echo "  make install-tools - Install development tools"

# Build the application
build:
	go build -o bin/nebula-backend ./cmd/server/main.go

# Run the application
run:
	go run ./cmd/server/main.go

# Run with hot-reload
dev:
	air

# Run all tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run linter
lint:
	golangci-lint run ./...

# Run linter with auto-fix
lint-fix:
	golangci-lint run --fix ./...

# Format code
fmt:
	go fmt ./...
	goimports -w -local github.com/Annany2002/nebula-backend .

# Run all checks (format, lint, test)
check: fmt lint test
	@echo "All checks passed!"

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Install development tools
install-tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/air-verse/air@latest
	@echo "Development tools installed!"
