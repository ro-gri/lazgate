#!/usr/bin/env sh
set -eu

INSTALL_DIR="${LAZ_INSTALL_DIR:-/opt/lazgate}"
IMAGE="${LAZ_IMAGE:-ghcr.io/ro-gri/lazgate:latest}"
APP_NAME="${LAZ_NAME:-LazGate}"
FORCE="${LAZ_FORCE:-0}"

COMPOSE_URL="${LAZ_COMPOSE_URL:-https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/docker-compose.yml}"
CADDYFILE_URL="${LAZ_CADDYFILE_URL:-https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/Caddyfile}"
BLANK_URL="${LAZ_BLANK_URL:-https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/blank.html}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1
}

random_hex() {
  bytes="$1"
  if need_cmd openssl; then
    openssl rand -hex "$bytes"
    return
  fi
  dd if=/dev/urandom bs="$bytes" count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'
}

random_b64() {
  bytes="$1"
  if need_cmd openssl; then
    openssl rand -base64 "$bytes" | tr -d '\n'
    return
  fi
  dd if=/dev/urandom bs="$bytes" count=1 2>/dev/null | base64 | tr -d '\n'
}

fetch() {
  url="$1"
  out="$2"
  if need_cmd curl; then
    curl -fsSL "$url" -o "$out"
    return
  fi
  if need_cmd wget; then
    wget -qO "$out" "$url"
    return
  fi
  echo "curl or wget is required" >&2
  exit 1
}

compose() {
  docker compose "$@" < /dev/null
}

public_ipv4() {
  for url in \
    "https://api.ipify.org" \
    "https://ifconfig.me/ip" \
    "https://icanhazip.com"
  do
    if need_cmd curl; then
      ip="$(curl -4fsSL --max-time 10 "$url" 2>/dev/null | tr -d '[:space:]' || true)"
    else
      ip="$(wget -qO- "$url" 2>/dev/null | tr -d '[:space:]' || true)"
    fi
    case "$ip" in
      *.*.*.*) printf '%s\n' "$ip"; return ;;
    esac
  done
  echo "Unable to detect public IPv4. Set LAZ_DOMAIN manually." >&2
  exit 1
}

install_docker() {
  if need_cmd docker && docker compose version >/dev/null 2>&1; then
    return
  fi

  if ! need_cmd apt-get; then
    echo "Automatic Docker installation currently supports Debian/Ubuntu with apt-get." >&2
    echo "Install Docker and Docker Compose manually, then rerun this installer." >&2
    exit 1
  fi

  apt-get update
  apt-get install -y ca-certificates curl gnupg openssl
  install -m 0755 -d /etc/apt/keyrings
  if [ ! -f /etc/apt/keyrings/docker.asc ]; then
    curl -fsSL https://download.docker.com/linux/$(. /etc/os-release; echo "$ID")/gpg -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
  fi
  . /etc/os-release
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${ID} ${VERSION_CODENAME} stable" > /etc/apt/sources.list.d/docker.list
  apt-get update
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

confirm_saved() {
  if [ "${LAZ_NO_CONFIRM:-0}" = "1" ]; then
    return
  fi

  if [ ! -r /dev/tty ] || [ ! -w /dev/tty ]; then
    echo "Unable to ask for confirmation without a TTY." >&2
    echo "Save the admin token and secret key, then rerun with LAZ_NO_CONFIRM=1 if this is automation." >&2
    exit 1
  fi

  printf '%s' "Type ok after saving the admin token and secret key: " > /dev/tty
  read -r confirmation < /dev/tty
  if [ "$confirmation" != "ok" ]; then
    echo "Confirmation was not received. Save the values above before closing this terminal." >&2
    exit 1
  fi
}

print_result() {
  cat <<EOF

LazGate installed.

Public URL: ${PUBLIC_BASE_URL}
Admin URL:  ${PUBLIC_BASE_URL}${WEB_PREFIX}/login

Admin token:
${ADMIN_TOKEN}

Secret key:
${SECRET_KEY}

Files:
  ${INSTALL_DIR}/.env
  ${INSTALL_DIR}/docker-compose.yml
  ${INSTALL_DIR}/Caddyfile
  ${INSTALL_DIR}/data/laz.db
  ${INSTALL_DIR}/keys/

Commands:
  cd ${INSTALL_DIR} && docker compose ps
  cd ${INSTALL_DIR} && docker compose logs -f
  cd ${INSTALL_DIR} && docker compose pull && docker compose up -d

EOF
}

if [ "$(id -u)" -ne 0 ]; then
  echo "Run as root, for example: curl -fsSL .../install.sh | sudo bash" >&2
  exit 1
fi

install_docker

if [ -e "$INSTALL_DIR" ] && [ "$FORCE" != "1" ]; then
  echo "$INSTALL_DIR already exists. Set LAZ_FORCE=1 to overwrite compose/env files." >&2
  exit 1
fi

DOMAIN="${LAZ_DOMAIN:-}"
if [ -z "$DOMAIN" ]; then
  IP="$(public_ipv4)"
  DOMAIN="$(printf '%s' "$IP" | tr '.' '-').sslip.io"
fi

PUBLIC_BASE_URL="${LAZ_PUBLIC_BASE_URL:-https://${DOMAIN}}"
ADMIN_TOKEN="${LAZ_ADMIN_TOKEN:-$(random_b64 48 | tr '/+' '_-' | cut -c1-64)}"
WEB_PREFIX="${LAZ_WEB_PREFIX:-/fa-$(random_hex 12)}"
SECRET_KEY="${LAZ_SECRET_KEY:-$(random_b64 32)}"

mkdir -p "$INSTALL_DIR/data" "$INSTALL_DIR/keys" "$INSTALL_DIR/caddy_data" "$INSTALL_DIR/caddy_config"
chown 10001:10001 "$INSTALL_DIR/data" "$INSTALL_DIR/keys"
fetch "$COMPOSE_URL" "$INSTALL_DIR/docker-compose.yml"
fetch "$CADDYFILE_URL" "$INSTALL_DIR/Caddyfile"
fetch "$BLANK_URL" "$INSTALL_DIR/blank.html"

umask 077
cat > "$INSTALL_DIR/.env" <<EOF
LAZ_IMAGE=${IMAGE}
LAZ_DOMAIN=${DOMAIN}
LAZ_PUBLIC_BASE_URL=${PUBLIC_BASE_URL}

LAZ_ADDR=0.0.0.0:8088
LAZ_NAME=${APP_NAME}
LAZ_STORAGE=sqlite
LAZ_DATA=/app/data/laz.db
LAZ_ADMIN_TOKEN=${ADMIN_TOKEN}
LAZ_WEB_PREFIX=${WEB_PREFIX}
LAZ_SECRET_KEY=${SECRET_KEY}
LAZ_BLANK_PAGE_PATH=/app/blank.html
EOF

chmod 600 "$INSTALL_DIR/.env"
chmod 755 "$INSTALL_DIR"

cd "$INSTALL_DIR"
compose pull
compose up -d

echo "Waiting for LazGate to start..."
ok=0
for _ in $(seq 1 30); do
  if compose exec -T laz wget -qO- http://127.0.0.1:8088/healthz >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 2
done

if [ "$ok" != "1" ]; then
  echo "LazGate did not become healthy in time. Check logs:" >&2
  echo "  cd $INSTALL_DIR && docker compose logs --tail=100" >&2
  compose ps >&2 || true
  compose logs --tail=100 >&2 || true
  exit 1
fi

echo "LazGate health check passed."
print_result
confirm_saved
echo "Installation completed."
