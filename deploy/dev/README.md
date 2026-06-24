# Local VPS Lab

Disposable development environment. Never deploy this compose file on a public
host: node containers are privileged, share the Docker VM cgroup hierarchy, run
SSH, and have unauthenticated Delve listeners. Host-published ports bind only to
127.0.0.1. Node root passwords and mTLS keys are generated into local named
volumes; no production credentials, host directories, or host Docker socket are
mounted.

## Start

Start Docker Desktop with Linux containers, then from the repository root:

```sh
docker compose -f deploy/dev/compose.yml up --build -d
docker compose -f deploy/dev/compose.yml ps -a
docker compose -f deploy/dev/compose.yml logs server
```

Admin: http://localhost:18088/admin . Complete the bootstrap owner invite using
the URL/code printed in server logs. Two native nodes are already registered.
Account/connection creation, sync, telemetry, hold/resume and deletion use the
real application paths. Hysteria install-over-SSH itself is not exercised by
bootstrap: this lab starts preinstalled runtimes and registered agents.

The one-shot `bootstrap` service initializes the SQLite store, independent agent
certificates, server TLS certificate, VPN configuration and random credentials.
It preserves an initialized lab. A partial initialization fails explicitly;
reset only the disposable lab volumes before trying again.

Old DinD-based lab volumes are not automatically converted or deleted. Use a
different Compose project name (and free host ports) to preserve them, or
explicitly reset disposable volumes before starting this version.

## Topology

Three long-running containers plus one completed bootstrap container:

- `server`: LazGate Server under Delve; separate main/auth/transport SQLite DBs.
- `hysteria`: Ubuntu 24.04, systemd PID 1, SSH, Hysteria service and Hysteria agent.
- `amnezia`: Ubuntu 24.04, systemd PID 1, SSH, Amnezia agent and official
  AmneziaWG 3.1 userspace runtime. The agent executes the local `awg` tool.
  There is no nested Docker daemon or Docker-based command execution.

Each node has its own agent DB/queue and configuration volume. AWG 3.1 is built
from pinned official source commits for the node architecture, including ARM64;
the lab does not rely on cross-architecture emulation. Userspace execution is
an explicit development choice, never a production provisioning fallback.

Systemd and network administration need privileged containers and writable cgroups. Docker Desktop
Enhanced Container Isolation, restricted/rootless runtimes or unsupported cgroup
settings may prevent startup. The kernel is Docker Desktop's Linux VM kernel,
not a VPS kernel. AWG uses `/dev/net/tun`; it does not install a kernel module.

## Debuggers

Go binaries are built with `-gcflags='all=-N -l'`. Delve starts them immediately
with `--continue` and supports disconnect/reconnect with `--accept-multiclient`.

| Application | Remote debugger |
| --- | --- |
| Server | localhost:40000 |
| Hysteria agent | localhost:40001 |
| Amnezia agent | localhost:40002 |

Use GoLand Go Remote or another Delve JSON-RPC client. Map remote `/src` to your
local repository root. These listeners use Delve API v2, not DAP.

Rebuild/recreate only the changed service:

```sh
docker compose -f deploy/dev/compose.yml up -d --build server
docker compose -f deploy/dev/compose.yml up -d --build hysteria
docker compose -f deploy/dev/compose.yml up -d --build amnezia
```

## Inspect

```sh
docker compose -f deploy/dev/compose.yml exec hysteria systemctl status hysteria-server lazgate-agent
docker compose -f deploy/dev/compose.yml exec hysteria journalctl -u lazgate-agent -n 100 --no-pager
docker compose -f deploy/dev/compose.yml exec amnezia systemctl status lab-awg lazgate-agent
docker compose -f deploy/dev/compose.yml exec amnezia awg show awg0
docker compose -f deploy/dev/compose.yml exec hysteria cat /lab/root-password
docker compose -f deploy/dev/compose.yml exec amnezia cat /lab/root-password
```

SSH: root@localhost ports 2222 (Hysteria) and 2223 (Amnezia), using the respective
generated passwords. Native VPN endpoints are localhost:18443/UDP and
localhost:18520/UDP. They are intended for clients on this Mac, not an iPhone
or another LAN machine. Hysteria has a self-signed development certificate:
export `/lab/tls.crt` from its container and configure certificate trust in the
test client. The generated subscription alone does not establish that trust.

Ports can be overridden with `LAZ_DEV_HTTP_PORT`,
`LAZ_DEV_HY2_PORT`, `LAZ_DEV_AWG_PORT`, `LAZ_DEV_HY2_SSH_PORT`,
`LAZ_DEV_AWG_SSH_PORT`, `LAZ_DEV_SERVER_DEBUG_PORT`, `LAZ_DEV_HY2_DEBUG_PORT`,
`LAZ_DEV_AWG_DEBUG_PORT`. Keep endpoint settings stable after bootstrap.

Build args in `Dockerfile`: `GO_IMAGE`, `DELVE_VERSION`, `HYSTERIA_VERSION`,
`NODE_IMAGE`, `AWG_GO_REF`, `AWG_TOOLS_REF`. Set
`LAZ_DEV_AWG_NODE_IMAGE=ubuntu:26.04` when explicitly rebuilding the Amnezia lab
to select Ubuntu 26.04 instead of the default 24.04; Hysteria remains unchanged.
Changing container userspace still does not change the Docker VM kernel or
exercise production DKMS/reboot. AWG source commits correspond to official
amneziawg-go v3.1.20260828 and amneziawg-tools v3.1.20260812.
Hysteria and AWG source versions are pinned; the debugger follows its
upstream latest release. These images are only for development; production uses native binaries.

## Stop / Reset

```sh
# Preserve all lab data:
docker compose -f deploy/dev/compose.yml down

# Destroy only this compose project's disposable volumes:
docker compose -f deploy/dev/compose.yml down -v
```

Do not run global Docker prune commands: other projects may be using this daemon.
External internet access is needed during builds.

## Test Coverage Boundary

After building the Amnezia image, run the isolated native-runtime smoke test:

```sh
bash deploy/dev/smoke-awg31.sh lazgate-dev-amnezia
```

It creates and removes its own two containers/network, verifies real tunnel
HTTPS, counters, peer removal/re-add and saved-config restart recovery. It does
not replace the application/agent synchronization tests.

Compose is the primary integration environment: exercise peer creation,
hold/resume, deletion, reconnect, runtime restart, configuration recovery,
traffic counters and actual VPN traffic locally. A container restart verifies
process/state recovery, not an operating-system reboot. Only kernel-specific
DKMS installation, module compatibility and genuine reboot recovery require an
explicit VM/VPS test. Do not treat userspace success as kernel-install coverage.

Application-level verification also exercised portal-issued AWG 3.1 config,
HTTPS forwarding/NAT to another isolated test container, telemetry arriving in
the server dashboard, hold/resume/delete, and AWG restart while LazGate Server
was offline. After removing/re-adding a peer or restarting AWG, the test client
explicitly reconnected to establish fresh session keys.

One environment limitation remains: the tested Docker Desktop external-internet
path returned SYN-ACK packets that the tunnel client counted as TCP checksum
errors. The isolated-container HTTPS forwarding/NAT path passed. Do not report
external-internet connectivity as verified on that Docker Desktop setup.
