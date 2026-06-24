# Deployment

LazGate can be installed on a clean Debian/Ubuntu VPS with Docker Compose and
Caddy.

The installer uses the published GHCR image:

```text
ghcr.io/ro-gri/lazgate:latest
```

## One-command install

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/install.sh | sudo bash
```

If `LAZ_DOMAIN` is not provided, the installer detects the public IPv4 address
and uses:

```text
<public-ip>.sslip.io
```

## Install with a domain

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/install.sh \
  | sudo env LAZ_DOMAIN=net.example.com bash
```

The domain must resolve to the VPS. Ports `80/tcp` and `443/tcp` must be open
for Caddy and Let's Encrypt.

## Install directory

Default:

```text
/opt/lazgate
```

Files:

```text
/opt/lazgate/.env
/opt/lazgate/docker-compose.yml
/opt/lazgate/Caddyfile
/opt/lazgate/data/laz.db
/opt/lazgate/keys/
/opt/lazgate/caddy_data/
/opt/lazgate/caddy_config/
```

## Configuration

The installer writes settings to `/opt/lazgate/.env` and passes them to the
`laz` container through Docker Compose `env_file`.

Important settings:

```env
LAZ_IMAGE=ghcr.io/ro-gri/lazgate:latest
LAZ_DOMAIN=<domain>
LAZ_PUBLIC_BASE_URL=https://<domain>
LAZ_ADMIN_TOKEN=<generated>
LAZ_WEB_PREFIX=/fa-<generated>
LAZ_SECRET_KEY=<generated>
```

Keep `LAZ_SECRET_KEY` safe. Losing it makes encrypted database fields
unrecoverable.

## SSH keys for nodes

Place node SSH private keys in:

```text
/opt/lazgate/keys/
```

Inside LazGate node settings, reference them as:

```text
/app/keys/<key-name>
```

## Re-run installer

The installer refuses to overwrite an existing `/opt/lazgate` by default.

To regenerate compose/env files intentionally:

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/install.sh \
  | sudo env LAZ_FORCE=1 bash
```

Do not rotate `LAZ_SECRET_KEY` unless you know how to migrate existing encrypted
data.

## Non-interactive mode

By default, the installer waits for the user to type `ok` at the end so the
generated admin token and secret key are not missed.

For automation:

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/install.sh \
  | sudo env LAZ_NO_CONFIRM=1 bash
```

## Operations

```sh
cd /opt/lazgate
docker compose ps
docker compose logs -f
docker compose pull
docker compose up -d
```

## Uninstall

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/uninstall.sh | sudo bash
```

The uninstall script removes only the LazGate Compose services, volumes, and
install directory. It does not remove Docker Engine and does not prune Docker.
Unrelated containers, images, volumes, and networks are left untouched.

By default, it asks the user to type `delete`.

For automation:

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/uninstall.sh \
  | sudo env LAZ_FORCE=1 bash
```

By default, the LazGate image is left in Docker's local image cache. To remove
that image too:

```sh
curl -fsSL https://raw.githubusercontent.com/ro-gri/lazgate/main/deploy/uninstall.sh \
  | sudo env LAZ_REMOVE_IMAGE=1 bash
```
