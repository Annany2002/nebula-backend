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

RUN mkdir -p /app/logs /app/data && chown -R appuser:appgroup /app

COPY --from=builder /app/nebula-backend-server .

# Uncomment the below line to set to release mode
# ENV GIN_MODE=release
USER appuser

EXPOSE 8080

VOLUME ["/app/data"]

ENTRYPOINT ["./nebula-backend-server"]
