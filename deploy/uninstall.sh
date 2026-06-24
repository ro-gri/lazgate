#!/usr/bin/env sh
set -eu

INSTALL_DIR="${LAZ_INSTALL_DIR:-/opt/lazgate}"
FORCE="${LAZ_FORCE:-0}"
REMOVE_IMAGE="${LAZ_REMOVE_IMAGE:-0}"

confirm_delete() {
  if [ "$FORCE" = "1" ]; then
    return
  fi

  if [ ! -r /dev/tty ] || [ ! -w /dev/tty ]; then
    echo "Unable to ask for confirmation without a TTY." >&2
    echo "Rerun with LAZ_FORCE=1 if this is automation." >&2
    exit 1
  fi

  printf '%s' "Type delete to continue: " > /dev/tty
  read -r confirmation < /dev/tty
  if [ "$confirmation" != "delete" ]; then
    echo "Uninstall cancelled."
    exit 1
  fi
}

if [ "$(id -u)" -ne 0 ]; then
  echo "Run as root, for example: curl -fsSL .../uninstall.sh | sudo bash" >&2
  exit 1
fi

if [ ! -e "$INSTALL_DIR" ]; then
  echo "$INSTALL_DIR does not exist. Nothing to uninstall."
  exit 0
fi

if [ ! -f "$INSTALL_DIR/docker-compose.yml" ]; then
  echo "$INSTALL_DIR does not look like a LazGate installation: docker-compose.yml is missing." >&2
  echo "Refusing to remove it automatically." >&2
  exit 1
fi

if ! grep -q 'lazgate' "$INSTALL_DIR/docker-compose.yml"; then
  echo "$INSTALL_DIR/docker-compose.yml does not look like a LazGate compose file." >&2
  echo "Refusing to remove it automatically." >&2
  exit 1
fi

cat <<EOF
This will uninstall LazGate from:

  $INSTALL_DIR

It will stop and remove only the LazGate Docker Compose services and volumes
declared in $INSTALL_DIR/docker-compose.yml, then remove $INSTALL_DIR.

It will not remove Docker Engine.
It will not prune Docker.
It will not remove unrelated containers, volumes, networks, or images.

EOF

confirm_delete

cd "$INSTALL_DIR"

image=""
if [ -f .env ]; then
  image="$(sed -n 's/^LAZ_IMAGE=//p' .env | tail -1)"
fi

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  if ! docker compose down --volumes --remove-orphans; then
    echo "docker compose down reported an error. Continuing with filesystem cleanup." >&2
    echo "Check Docker manually if LazGate containers or network remain." >&2
  fi
else
  echo "docker compose is not available; skipping compose down." >&2
fi

if [ "$REMOVE_IMAGE" = "1" ] && [ -n "$image" ] && command -v docker >/dev/null 2>&1; then
  docker image rm "$image" >/dev/null 2>&1 || true
fi

cd /
rm -rf "$INSTALL_DIR"

cat <<EOF
LazGate uninstalled.

Removed:
  $INSTALL_DIR

Left untouched:
  Docker Engine
  unrelated Docker containers
  unrelated Docker images
  unrelated Docker volumes
  unrelated Docker networks

EOF
