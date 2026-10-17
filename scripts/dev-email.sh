#!/usr/bin/env bash
# Local-only Mailpit + Nebula runner. Does not edit .env or system certificate trust.
set -euo pipefail
umask 077

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
STATE_DIR="$REPO_DIR/.local-email"
MAILPIT_VERSION=v1.31.5
mode="${1:-run}"
mail_pid=""
backend_pid=""
temp_dir=""

cleanup() {
  for pid in "$backend_pid" "$mail_pid"; do
    if [[ -n "$pid" ]]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  if [[ -n "$temp_dir" ]]; then rm -rf "$temp_dir"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

case "$mode" in
  setup|mailpit|run) ;;
  *) echo "Usage: $0 [setup|mailpit|run]" >&2; exit 2 ;;
esac
if [[ "${APP_ENV:-}" == production ]]; then
  echo "This runner is for local development only; unset APP_ENV=production." >&2
  exit 1
fi
for tool in curl openssl tar; do
  command -v "$tool" >/dev/null || { echo "Missing required tool: $tool" >&2; exit 1; }
done
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"
temp_dir="$(mktemp -d "$STATE_DIR/setup.XXXXXX")"

install_mailpit() {
  if [[ -x "$STATE_DIR/mailpit" && -f "$STATE_DIR/version" ]] &&
    [[ "$(cat "$STATE_DIR/version")" == "$MAILPIT_VERSION" ]]; then return; fi
  case "$(uname -s)-$(uname -m)" in
    Darwin-arm64) platform=darwin-arm64; checksum=ddb042e3a9b1f36dadb20ec4d7424c417903adef692e59f50dce0075a747e8e2 ;;
    Darwin-x86_64) platform=darwin-amd64; checksum=81dfda3824ec47cfa6dd7be6d70959bb588660c95633142cbd1f7aed62d98292 ;;
    Linux-aarch64|Linux-arm64) platform=linux-arm64; checksum=ff9b03dd4a01e9680d89837d391296942c0529d545d160a98ed62882eec0e628 ;;
    Linux-x86_64) platform=linux-amd64; checksum=5924ace84f38c83c29f941a644a2d7be6c11f339eb4a9a12ab526414897ceed1 ;;
    *) echo "Unsupported platform; use macOS or Linux on ARM64/x86_64." >&2; exit 1 ;;
  esac
  echo "Downloading Mailpit $MAILPIT_VERSION ($platform)..."
  curl -fsSL --retry 2 --connect-timeout 15 --max-time 120 \
    "https://github.com/axllent/mailpit/releases/download/$MAILPIT_VERSION/mailpit-$platform.tar.gz" \
    -o "$temp_dir/mailpit.tar.gz"
  if command -v shasum >/dev/null; then
    actual="$(shasum -a 256 "$temp_dir/mailpit.tar.gz")"
  elif command -v sha256sum >/dev/null; then
    actual="$(sha256sum "$temp_dir/mailpit.tar.gz")"
  else
    echo "Install shasum or sha256sum to verify the download." >&2; exit 1
  fi
  [[ "${actual%% *}" == "$checksum" ]] || { echo "Mailpit checksum mismatch." >&2; exit 1; }
  tar -xzf "$temp_dir/mailpit.tar.gz" -C "$temp_dir" mailpit
  chmod 700 "$temp_dir/mailpit"
  mv "$temp_dir/mailpit" "$STATE_DIR/mailpit"
  printf '%s\n' "$MAILPIT_VERSION" > "$STATE_DIR/version"
}

create_credentials() {
  for name in smtp-password jwt-secret; do
    if [[ ! -s "$STATE_DIR/$name" ]]; then
      openssl rand -hex 32 > "$temp_dir/$name"
      mv "$temp_dir/$name" "$STATE_DIR/$name"
    fi
    chmod 600 "$STATE_DIR/$name"
  done
}

create_certificates() {
  if [[ -s "$STATE_DIR/ca.pem" && -s "$STATE_DIR/server.pem" && -s "$STATE_DIR/server.key" ]] &&
    openssl x509 -in "$STATE_DIR/ca.pem" -checkend 604800 -noout >/dev/null 2>&1 &&
    openssl x509 -in "$STATE_DIR/server.pem" -checkend 604800 -noout >/dev/null 2>&1; then return; fi
  cat > "$temp_dir/ca.cnf" <<'CONF'
[req]
prompt = no
distinguished_name = dn
x509_extensions = ca
[dn]
CN = Nebula local email CA
[ca]
basicConstraints = critical,CA:true
keyUsage = critical,keyCertSign,cRLSign
CONF
  cat > "$temp_dir/server.cnf" <<'CONF'
[req]
prompt = no
distinguished_name = dn
[dn]
CN = localhost
[server]
basicConstraints = critical,CA:false
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = DNS:localhost,IP:127.0.0.1
CONF
  openssl req -x509 -newkey rsa:2048 -nodes -days 90 -sha256 \
    -config "$temp_dir/ca.cnf" -keyout "$temp_dir/ca.key" -out "$temp_dir/ca.pem" 2>/dev/null
  openssl req -new -newkey rsa:2048 -nodes -config "$temp_dir/server.cnf" \
    -keyout "$temp_dir/server.key" -out "$temp_dir/server.csr" 2>/dev/null
  openssl x509 -req -in "$temp_dir/server.csr" -CA "$temp_dir/ca.pem" \
    -CAkey "$temp_dir/ca.key" -CAcreateserial -days 90 -sha256 \
    -extfile "$temp_dir/server.cnf" -extensions server -out "$temp_dir/server.pem" 2>/dev/null
  for name in ca.pem server.pem server.key; do mv "$temp_dir/$name" "$STATE_DIR/$name"; done
  # The CA signing key is discarded; renewal generates a fresh local CA.
}

assert_port_free() {
  if command -v lsof >/dev/null && lsof -nP -iTCP:"$1" -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "Port $1 is already in use. Stop the existing local service first." >&2
    exit 1
  fi
}

install_mailpit
create_credentials
create_certificates
if [[ "$mode" == setup ]]; then
  echo "Local email tools ready. Run $0 to start Mailpit and the backend."
  exit 0
fi
assert_port_free 1025
assert_port_free 8025
if [[ "$mode" == run ]]; then
  command -v go >/dev/null || { echo "Go is required to run Nebula." >&2; exit 1; }
  assert_port_free 8080
  echo "Building the local backend..."
  (cd "$REPO_DIR" && GOCACHE="$STATE_DIR/go-cache" go build -o "$STATE_DIR/nebula-backend" ./cmd/server)
fi

# A clean environment prevents inherited Mailpit relay/forward settings from sending real mail.
env -i PATH="$PATH" MP_SMTP_AUTH="nebula:$(cat "$STATE_DIR/smtp-password")" \
  "$STATE_DIR/mailpit" --listen 127.0.0.1:8025 --smtp 127.0.0.1:1025 \
  --database "$STATE_DIR/inbox.db" --smtp-tls-cert "$STATE_DIR/server.pem" \
  --smtp-tls-key "$STATE_DIR/server.key" --smtp-require-starttls \
  --smtp-disable-rdns --disable-version-check --allowed-hosts localhost,127.0.0.1 \
  > "$STATE_DIR/mailpit.log" 2>&1 &
mail_pid=$!
ready=false
for ((attempt=0; attempt<50; attempt++)); do
  kill -0 "$mail_pid" 2>/dev/null || { echo "Mailpit failed; see $STATE_DIR/mailpit.log" >&2; exit 1; }
  if curl -fsS --max-time 1 http://127.0.0.1:8025/readyz >/dev/null 2>&1; then ready=true; break; fi
  sleep 0.2
done
[[ "$ready" == true ]] || { echo "Mailpit did not become ready." >&2; exit 1; }
echo "Email inbox -> http://localhost:8025 (local capture only)"
if [[ "$mode" == mailpit ]]; then wait "$mail_pid"; exit $?; fi

# Separate accounts/data and process-scoped CA trust; .env and normal local data stay intact.
cd "$REPO_DIR"
APP_ENV=development SERVER_PORT=8080 JWT_SECRET="$(cat "$STATE_DIR/jwt-secret")" \
  JWT_EXPIRATION_HOURS=24 DATABASE_DIRECTORY="$STATE_DIR/data" DATABASE_DIRECTORY_FILE=metadata.db \
  ALLOWED_ORIGINS=http://localhost:3000 EMAIL_VERIFICATION_ENABLED=true PASSWORD_RESET_ENABLED=true \
  APP_PUBLIC_URL=http://localhost:3000 EMAIL_VERIFICATION_TTL_MINUTES=30 PASSWORD_RESET_TTL_MINUTES=30 \
  SMTP_HOST=127.0.0.1 SMTP_PORT=1025 SMTP_USERNAME=nebula \
  SMTP_PASSWORD="$(cat "$STATE_DIR/smtp-password")" SMTP_FROM=nebula@example.test \
  SMTP_TLS_MODE=starttls SMTP_CA_FILE="$STATE_DIR/ca.pem" \
  "$STATE_DIR/nebula-backend" &
backend_pid=$!
while kill -0 "$backend_pid" 2>/dev/null && kill -0 "$mail_pid" 2>/dev/null; do sleep 1; done
echo "A local email service stopped; shutting down the other service." >&2
exit 1
