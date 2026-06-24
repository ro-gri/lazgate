#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

fail() { echo "Error: $*" >&2; exit 1; }
[[ $(id -u) == 0 ]] || fail 'Run as root.'
[[ $(uname -s) == Linux && -d /run/systemd/system ]] || fail 'Linux with running systemd is required.'
command -v apt-get >/dev/null || fail 'Debian/Ubuntu with apt-get is required.'
case $(uname -m) in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) fail 'Supported architectures: amd64, arm64.' ;;
esac
root=/opt/lazgate
[[ ! -e $root ]] || fail "$root exists. Back it up and uninstall before a fresh installation."
for unit in lazgate.service lazgate-caddy.service; do
  [[ $(systemctl show -p LoadState --value "$unit") == not-found ]] || fail "$unit already exists."
done
for user in lazgate lazgate-caddy; do
  if id "$user" >/dev/null 2>&1; then fail "User $user already exists."; fi
done
command -v ss >/dev/null || fail 'Install iproute2 (ss) first.'
for port in 80 443 2019 8088 9443; do
  [[ -z $(ss -H -ltn "sport = :$port") ]] || fail "TCP port $port is occupied."
done

noninteractive=${LAZ_NONINTERACTIVE:-0}
ask() {
  local prompt=$1 default=$2 answer
  if [[ $noninteractive == 1 ]]; then REPLY=$default; return; fi
  [[ -r /dev/tty && -w /dev/tty ]] || fail 'A terminal is required; automation must set LAZ_NONINTERACTIVE=1.'
  printf '%s [%s]: ' "$prompt" "$default" >/dev/tty
  IFS= read -r answer </dev/tty || fail 'Input closed.'
  REPLY=${answer:-$default}
}
ask 'Domain name (auto = temporary sslip.io)' "${LAZ_DOMAIN:-auto}"
domain=$REPLY
ask 'Application name' "${LAZ_NAME:-LazGate}"
name=$REPLY
[[ $name =~ ^[a-zA-Z0-9][a-zA-Z0-9\ ._-]{0,79}$ ]] || fail 'Name: 1-80 letters, digits, spaces, dots, underscores or hyphens.'
ask 'LazGate release version' "${LAZ_VERSION:-latest}"
version=$REPLY
[[ $version == latest || $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9.-]+)?$ ]] || fail 'Invalid release version.'
ask 'Install LazGate and its dedicated HTTPS service? Type yes' "${LAZ_ACCEPT:-no}"
[[ $REPLY == yes ]] || fail 'Installation cancelled.'

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl jq openssl tar openssh-client
fetch() { curl -fsSL --retry 3 --connect-timeout 15 --max-time 300 "$1" -o "$2"; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
trap 'echo "Installation failed. Inspect journalctl -u lazgate -u lazgate-caddy. Existing files will not be overwritten on rerun." >&2' ERR
if [[ $domain == auto ]]; then
  ip=$(curl -4fsS --connect-timeout 10 --max-time 20 https://api.ipify.org)
  [[ $ip =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || fail 'Cannot detect IPv4; specify LAZ_DOMAIN.'
  IFS=. read -r -a octets <<<"$ip"
  for octet in "${octets[@]}"; do ((10#$octet <= 255)) || fail 'Invalid public IPv4.'; done
  domain=${ip//./-}.sslip.io
fi
[[ ${#domain} -le 253 && $domain == *.* && $domain != *. ]] || fail 'Use a fully qualified DNS name.'
IFS=. read -r -a labels <<<"$domain"
for label in "${labels[@]}"; do
  [[ $label =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$ ]] || fail 'Invalid DNS name.'
done
if [[ $version == latest ]]; then
  fetch https://api.github.com/repos/ro-gri/lazgate/releases/latest "$tmp/release.json"
  version=$(jq -er '.tag_name' "$tmp/release.json")
fi
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9.-]+)?$ ]] || fail 'Invalid release tag.'
base=https://github.com/ro-gri/lazgate/releases/download/$version
artifact=laz-linux-$arch
fetch "$base/$artifact" "$tmp/$artifact"
fetch "$base/checksums.txt" "$tmp/checksums.txt"
verify() {
  local file=$1 manifest=$2 algorithm=${3:-256} checksum
  checksum=$(awk -v file="$file" '$2 == file { print $1 }' "$manifest")
  [[ $checksum =~ ^[a-fA-F0-9]+$ && ${#checksum} == $((algorithm / 4)) ]] || fail "Missing or ambiguous checksum for $file."
  (cd "$tmp"; printf '%s  %s\n' "$checksum" "$file" | "sha${algorithm}sum" --check --status) || fail "Checksum mismatch: $file"
}
verify "$artifact" "$tmp/checksums.txt"
fetch https://api.github.com/repos/caddyserver/caddy/releases/latest "$tmp/caddy.json"
caddy_tag=$(jq -er '.tag_name' "$tmp/caddy.json")
[[ $caddy_tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'Invalid Caddy release tag.'
caddy_version=${caddy_tag#v}
caddy_archive=caddy_${caddy_version}_linux_${arch}.tar.gz
caddy_base=https://github.com/caddyserver/caddy/releases/download/$caddy_tag
fetch "$caddy_base/$caddy_archive" "$tmp/$caddy_archive"
fetch "$caddy_base/caddy_${caddy_version}_checksums.txt" "$tmp/caddy-checksums.txt"
verify "$caddy_archive" "$tmp/caddy-checksums.txt" 512
tar -xzf "$tmp/$caddy_archive" -C "$tmp" caddy
chmod 755 "$tmp/$artifact" "$tmp/caddy"
"$tmp/$artifact" --version
"$tmp/caddy" version

useradd --system --user-group --home-dir "$root" --shell /usr/sbin/nologin lazgate
useradd --system --user-group --home-dir "$root/caddy" --shell /usr/sbin/nologin lazgate-caddy
install -d -m 755 "$root" "$root/bin"
install -d -m 700 -o lazgate -g lazgate "$root/data" "$root/keys"
install -d -m 700 -o lazgate-caddy -g lazgate-caddy "$root/caddy"
install -m 755 "$tmp/$artifact" "$root/bin/laz"
install -m 755 "$tmp/caddy" "$root/bin/caddy"
secret=$(openssl rand -base64 32 | tr -d '\n')
prefix=/fa-$(openssl rand -hex 12)
openssl req -x509 -newkey rsa:2048 -nodes -days 825 -subj "/CN=$domain" \
  -addext 'basicConstraints=critical,CA:FALSE' \
  -addext 'keyUsage=critical,digitalSignature,keyEncipherment' \
  -addext 'extendedKeyUsage=serverAuth' \
  -addext "subjectAltName=DNS:$domain" -keyout "$root/keys/agent-grpc.key" \
  -out "$root/keys/agent-grpc.crt" >/dev/null 2>&1
chown lazgate:lazgate "$root/keys/agent-grpc."*
chmod 600 "$root/keys/agent-grpc.key"
cat >"$root/.env" <<EOF
LAZ_NAME="$name"
LAZ_ADDR=127.0.0.1:8088
LAZ_STORAGE=sqlite
LAZ_DATA=$root/data/laz.db
LAZ_AUTH_DATA=$root/data/laz.auth.db
LAZ_TRANSPORT_DATA=$root/data/laz.transport.db
LAZ_SECRET_KEY=$secret
LAZ_PUBLIC_BASE_URL=https://$domain
LAZ_WEB_PREFIX=$prefix
LAZ_AGENT_GRPC_ADDR=0.0.0.0:9443
LAZ_AGENT_GRPC_TARGET=$domain:9443
LAZ_AGENT_DOWNLOAD_BASE=$base
LAZ_AGENT_GRPC_CERT_FILE=$root/keys/agent-grpc.crt
LAZ_AGENT_GRPC_KEY_FILE=$root/keys/agent-grpc.key
EOF
cat >"$root/Caddyfile" <<EOF
$domain {
  encode zstd gzip
  header X-Robots-Tag "noindex, nofollow, noarchive"
  reverse_proxy 127.0.0.1:8088
}
EOF
chown lazgate-caddy:lazgate-caddy "$root/Caddyfile"
cat >/etc/systemd/system/lazgate.service <<EOF
[Unit]
Description=LazGate server
Wants=network-online.target
After=network-online.target
[Service]
User=lazgate
Group=lazgate
WorkingDirectory=$root
Environment=HOME=$root
EnvironmentFile=$root/.env
ExecStart=$root/bin/laz
Restart=on-failure
RestartSec=5
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$root/data $root/keys
[Install]
WantedBy=multi-user.target
EOF
cat >/etc/systemd/system/lazgate-caddy.service <<EOF
[Unit]
Description=LazGate HTTPS proxy
Wants=network-online.target
After=network-online.target lazgate.service
[Service]
User=lazgate-caddy
Group=lazgate-caddy
Environment=HOME=$root/caddy
Environment=XDG_DATA_HOME=$root/caddy/data
Environment=XDG_CONFIG_HOME=$root/caddy/config
ExecStart=$root/bin/caddy run --config $root/Caddyfile --adapter caddyfile
ExecReload=$root/bin/caddy reload --config $root/Caddyfile --adapter caddyfile
Restart=on-failure
RestartSec=5
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$root/caddy
[Install]
WantedBy=multi-user.target
EOF
chmod 644 /etc/systemd/system/lazgate{,-caddy}.service
printf 'native-v1\n' >"$root/.lazgate-installation"
printf '%s\n' "$version" >"$root/version"
systemctl daemon-reload
systemctl enable --now lazgate.service lazgate-caddy.service
echo 'Waiting for LazGate...'
healthy=0
for ((i=0; i<60; i++)); do
  if curl -fsS --max-time 2 http://127.0.0.1:8088/healthz >/dev/null 2>&1; then healthy=1; break; fi
  sleep 2
done
[[ $healthy == 1 ]] || fail 'Health check failed. Run journalctl -u lazgate.'
tls=0
echo "Waiting for HTTPS at https://$domain (check inbound TCP 80/443 and DNS)..."
for ((i=0; i<30; i++)); do
  if curl -fsS --max-time 5 "https://$domain/healthz" >/dev/null 2>&1; then tls=1; break; fi
  sleep 2
done
[[ $tls == 1 ]] || fail 'HTTPS not ready. Check DNS, TCP 80/443 and journalctl -u lazgate-caddy. Installation files are preserved.'
invocation=$(systemctl show lazgate.service -p InvocationID --value)
[[ $invocation =~ ^[a-f0-9]{32}$ ]] || fail 'Cannot identify the running LazGate service invocation.'
bootstrap=$(journalctl "_SYSTEMD_INVOCATION_ID=$invocation" --no-pager --output=cat)
invite_url=$(printf '%s\n' "$bootstrap" | sed -n 's/^LazGate bootstrap owner invite: //p' | tail -n 1)
invite_code=$(printf '%s\n' "$bootstrap" | sed -n 's/^Invite code: //p' | tail -n 1)
invite_expires=$(printf '%s\n' "$bootstrap" | sed -n 's/^Expires at: //p' | tail -n 1)
cat <<EOF

LazGate $version installed (native binary, no Docker).
Admin URL: https://$domain$prefix/login
EOF
if [[ -n $invite_url && -n $invite_code && -n $invite_expires ]]; then
  cat <<EOF
Owner registration form: $invite_url
Invitation code: $invite_code
Invitation expires at: $invite_expires
Open the registration form and use this code to create your owner login/password.
Keep the invitation and code private: they grant owner access.
EOF
else
  echo 'Owner invitation could not be read automatically. Check the current service journal:'
  echo "  sudo journalctl _SYSTEMD_INVOCATION_ID=$invocation --no-pager"
fi
cat <<EOF

Back up /opt/lazgate/.env securely, together with your database backups.
This file contains LAZ_SECRET_KEY, which encrypts sensitive database fields
(including connection credentials and configurations). Restoring the databases
without the original key will not recover those secrets. Do not share this file
or commit it to Git. The key is not a login password and is not printed here.
Logs: journalctl -u lazgate -u lazgate-caddy -f
Allow inbound TCP 80, 443 and 9443 in the provider firewall.
EOF
if [[ $noninteractive != 1 ]]; then
  while :; do
    ask 'Type ok after saving the URLs/invitation and reading the backup instructions' ''
    [[ $REPLY == ok ]] && break
  done
fi
