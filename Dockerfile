# 1. Builder Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev sqlite-dev git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=1
ENV GOOS=linux
ENV GOARCH=amd64

RUN go build -ldflags="-s -w" -o nebula-backend-server ./cmd/server/main.go


# 2. Runtime Stage
FROM alpine:latest

RUN apk add --no-cache sqlite-libs

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

RUN mkdir -p /app/logs && chown -R appuser:appgroup /app

COPY --from=builder /app/nebula-backend-server .

USER appuser

EXPOSE 8080

# HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
#   CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["./nebula-backend-server"]
