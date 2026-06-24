# Deployment

## One-command installation

Run on a clean Debian/Ubuntu VPS with systemd (amd64 or arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/install.sh | sudo bash
```

The installer asks for a DNS name (or `auto` for an IP-based `sslip.io` name),
application name and release version, then asks for explicit confirmation.
Input is read from `/dev/tty`, so piping the script does not consume answers.
At the end, type `ok` after saving the admin URL and reading the backup instructions.
The encryption key is not printed in the terminal.

LazGate and its dedicated Caddy are native release binaries with checksum
verification (SHA-256 for LazGate, SHA-512 for Caddy), running as separate unprivileged systemd services. Docker is
neither installed nor used. Existing installations, users, services and occupied
ports are rejected rather than overwritten. This is a fresh installer, not an
update or Docker-to-native migration tool.

DNS must point to this VPS. Allow inbound TCP 80/443 for HTTPS and TCP 9443 for
agent mTLS. The installer does not modify provider firewalls. Caddy obtains and
renews public TLS certificates automatically. An IP-based temporary name changes
when the public IP changes.

## First login

The final output contains the admin URL, a direct link to the owner registration
form, the invitation code and its expiration time. Open that link and register
the owner. Keep the invitation and code private: they grant owner access.

The installer reads the invitation only from the currently running service
invocation, not from previous starts. If automatic retrieval fails, use the
local journal:

```sh
sudo journalctl -u lazgate --no-pager | grep -A2 'LazGate bootstrap owner invite:'
```

Open that invitation URL and register the owner. It expires after 24 hours.
Treat the journal invitation and code as credentials.

## Files and services

- `/opt/lazgate/bin/laz`, `/opt/lazgate/bin/caddy`: binaries.
- `/opt/lazgate/.env`: root-only settings and generated encryption key.
- `/opt/lazgate/data/`: main, authentication and transport SQLite databases.
- `/opt/lazgate/keys/`: agent gRPC certificate/key and node keys.
- `/opt/lazgate/Caddyfile`, `/opt/lazgate/caddy/`: HTTPS config and certificate data.
- `/opt/lazgate/version`: installed LazGate release.
- `lazgate.service`: user `lazgate`, HTTP bound to loopback 8088.
- `lazgate-caddy.service`: user `lazgate-caddy`, TCP 80/443.

Back up `/opt/lazgate/.env` securely together with the database backups. It
contains `LAZ_SECRET_KEY`, which encrypts sensitive database fields, including
connection credentials and configurations. Restoring the databases without the
original key will not recover those secrets. The key is not a login password.
Do not share the file or commit it to Git. Losing `LAZ_SECRET_KEY` makes encrypted
database fields unrecoverable.
The installer pins agent downloads to the same release as the server.
No blank-page override is installed; the neutral built-in page is used.
To supply your own, set `LAZ_BLANK_PAGE_PATH` to a server-readable file.

```sh
sudo systemctl status lazgate lazgate-caddy
sudo journalctl -u lazgate -u lazgate-caddy -f
sudo systemctl restart lazgate
sudo systemctl reload lazgate-caddy
```

## Automation

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/install.sh \
  | sudo env LAZ_NONINTERACTIVE=1 LAZ_ACCEPT=yes LAZ_DOMAIN=net.example.com \
      LAZ_NAME=LazGate LAZ_VERSION=v1.2.3 bash
```

`LAZ_DOMAIN=auto`, `LAZ_NAME=LazGate` and `LAZ_VERSION=latest` are defaults.
Only releases containing `laz-linux-amd64` / `laz-linux-arm64` are installable.
Older agent-only releases are not supported.

## Uninstall

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/uninstall.sh | sudo bash
```

Requires typing `delete` (automation: `sudo env LAZ_FORCE=1 bash`).
Deletes both dedicated services/users and all of `/opt/lazgate`, including
databases, keys and TLS certificates. Back up first. Other services, shared
packages, unrelated Caddy installations and Docker resources are untouched.
Legacy Docker installations are refused and must be removed separately.

## Local development

The isolated Docker Compose lab remains available in [dev/README.md](dev/README.md).
It is not the production installation method.
