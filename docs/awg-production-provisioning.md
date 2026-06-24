# Native AmneziaWG 3.1 Provisioning

## Supported Target

Fresh Ubuntu 24.04 or 26.04, x86_64/amd64, systemd, kernel 6.7 or newer, matching
kernel headers available from apt, root or passwordless sudo, public IPv4,
and outbound HTTPS/DNS. Kernel AWG only: no userspace, Docker, ARM, or 2.0
fallback. Local ARM userspace labs are separate and are not host-installer tests.
The audited installer fixes its paths to `/root/awg` and
`/etc/amnezia/amneziawg/awg0.conf`; only `awg0` and canonical IPv4 networks
with prefixes `/16` through `/30` are accepted (matching installer v6.1.1).
The GUI lists each supported prefix, validates IPv4 syntax and network alignment
before submit, and defaults to `/24`. Server-side validation remains mandatory.
Server address is network+1; client allocation ranges from network+2 through
broadcast-1. Existing AWG/config/agent installations are refused.
Custom native nftables tables, active UFW and firewalld are refused, not reset.
The supported firewall surface is iptables (including its nft compatibility backend).
Provider firewalls must permit the chosen UDP port and agent egress.

Use the admin Nodes page, **AmneziaWG 3.1 VPS**, or POST
`/api/v1/nodes/install-amneziawg` under the configured admin prefix:

```json
{
  "ssh_host": "192.0.2.10",
  "ssh_port": 22,
  "bootstrap_user": "root",
  "bootstrap_password": "REDACTED",
  "node_name": "AWG production",
  "endpoint_host": "vpn.example.com",
  "listen_port": 51820,
  "subnet": "10.8.1.0/24",
  "allow_package_upgrade": true
}
```

Initial SSH credentials are used synchronously before enqueue, just as in the
Hysteria flow. The persistent install-input type has no bootstrap user/password
fields. Neither credential is stored in outbox, results or events. An installation-specific SSH
key and mTLS identity use the existing secret-wrapped storage/outbox; configure
`LAZ_SECRET_KEY` for production, back it up separately, and use a versioned HTTPS
`LAZ_AGENT_DOWNLOAD_BASE` with the existing split-agent checksum manifest.
The existing SSH abstraction currently accepts unknown host keys; trusted-network
bootstrap or external host identity verification is still required. This integration
does not claim SSH host-key pinning.

## Audited Sources

Official installer [v6.1.1](https://github.com/bivlked/amneziawg-installer/releases/tag/v6.1.1):

| Artifact | SHA256 |
| --- | --- |
| install_amneziawg.sh | e0024185e686b1b8292ea96cbf5d315ef4cadb1ed29bb46ff1aff41dbd7604d2 |
| awg_common.sh | 87e835bb4fc350a75de809f25d7bb7134a003b4f73f669714f8a53e9fd8ded5d |
| manage_amneziawg.sh | 5cf1692e1c67ea0fe2fd585defc6b80aabe83b5bff7e5915981dfd002ea18eb1 |

The three release assets were downloaded and hashed during implementation.
Helper versions remain the upstream default `AWG_BRANCH=v6.1.1`; overriding
that variable disables upstream verification and is deliberately not done.
The management script is only an upstream installation dependency, never a runtime API.

Published official PPA packages are selected explicitly for noble (24.04) or
resolute (26.04), not by installing the other release's packages. Exact URLs,
versions and SHA256 values live in `internal/server/provisioning/awg_packages.go`.
Package profiles:

| Ubuntu | Package | Exact Version | SHA256 |
| --- | --- | --- | --- |
| 24.04 | amneziawg-tools | 1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1 | 2c924076be2ba217eea04f9693291f01c5307a9be5aeee60d04525a50ff2804f |
| 24.04 | amneziawg-dkms | 1.0.0-0~202609061402+4569c4c~ubuntu24.04.1 | fadd07d0c2f9e89b61db713e37ba8fb05b5a15c067d13338bed3745193fa30be |
| 26.04 | amneziawg-tools | 1.0.20210914-0~202609140833+ee0f0a9~ubuntu26.04.1 | b4832405e4e82c02ce5cc70151ee02c23f1a9cfe3ab078b8c5f3579a9213e4e0 |
| 26.04 | amneziawg-dkms | 1.0.0-0~202609140848+4569c4c~ubuntu26.04.1 | 4f42ace1e325783fe0402073e634cd208f85729a6c983d8c78b8525ae2ec74a5 |

Tools correspond to official `v3.1.20260812`, commit
`ee0f0a9aa34ff0a0da4b3433b9512781cfe02843`; the kernel package corresponds
to `v3.1.20260906`, commit `4569c4c67f3a57414969260cafbbd04694fbaae0`.
APT preferences prevent selecting another AWG version. OS dependencies, kernel
security updates and DKMS build dependencies still come from the host's apt
repositories; this is not a fully reproducible OS image or offline installation.

Only tools are installed before upstream execution, with recommends disabled.
The verified DKMS archive is staged in the apt cache; the upstream installer
owns module installation, source compatibility fixes and rebuilds. In particular,
kernel 7.0.0-38 needs upstream's source repair. Installing DKMS ahead of that
repair can leave apt in a failed state; do not restore the old eager DKMS install.

Every installer invocation explicitly uses `--yes --protocol=3.1 --no-tweaks
--allow-ipv6 --no-cps`. No global IPv6 disabling is requested. LazGate manages
IPv4 tunnel addressing; optional IPv6 client routes do not configure a server
IPv6 uplink or forwarding. The upstream installer starts without CPS. The shared
installation/settings form can provide I1-I5 CPS strings, which are validated and
applied by the agent after importing the installer's server identity. Installation
completion waits for the configuration ACK rather than only an online heartbeat.
`HeaderProtectionKey` is a 32-byte base64 secret, encrypted in domain storage,
included in protected client/server profiles and protobuf delivery. S1-S4 must
be at least 12. Content padding and other selected protocol settings are preserved
end to end.

**Important:** upstream step 1 upgrades OS packages and requests reboots even
with `--no-tweaks`. The explicit package-upgrade consent is mandatory. Never
run this on a shared production host without reviewing that side effect.

## Persistent Workflow

The existing outbox carries prepare, launch, poll, handoff, register, agent,
verify, wait-agent and done steps. No operations tables or second queue exist.
`boot_id`, resume count, absolute 90-minute deadline, node IDs, certificates
and imported config are persisted in that payload. A detached systemd unit
performs long host work, with a host-side flock preventing concurrent installers.
SSH polling is bounded by the absolute deadline and installer launches by four
runs. Polling/resume messages carry updated state through normal atomic outbox
completion. Queue expiry allows a five-minute margin for reporting timeout.

An SSH reconnect is not success: installer steps 2/3 resume only after a changed
kernel boot ID. Installer completion requires state 99 and exit 0. Transient SSH
loss is polled/retried; terminal remote command failures stop the flow. After a
server restart, the normal outbox lease recovery continues the queued step.

Handoff stops the fresh sample interface, atomically removes all sample peers,
removes the generated my_phone/my_laptop artifacts, and retains the server key,
header key, protocol parameters and MTU. Routing hooks are replaced with
subnet-scoped, tagged IPv4 UDP/forward/NAT rules, without flushing other rules.
The imported server config must match the requested address and port.
Replaying handoff never rewrites peers after the handoff marker exists.

Agent registration reuses stable node/instance/installation IDs. Completion
requires interface/port/forwarding/firewall checks plus an online agent heartbeat,
not merely an active systemd unit. Existing runtime peer synchronization is reused.

## Ownership and Offline Persistence

The root-owned quick config remains mode 0600 and read-only inside the agent
service sandbox. The agent is a native systemd service with bounded
CAP_NET_ADMIN/CAP_NET_RAW; no Docker or installer commands are used at runtime.
Its config selects `interface_name: awg0` and native snapshot
`/var/lib/lazgate-agent/awg/awg0.conf`, not the quick base. Only native AWG keys
belong in that snapshot. The agent cannot edit root-executed quick hooks.

Handoff seeds the snapshot from local `awg showconf`. A root-owned quick-service
drop-in restores it using `awg syncconf` on service restart, including while the
agent is offline. The agent independently reconciles startup state. Do not pass
the agent snapshot to `awg-quick`.

## Diagnostics and Limits

Inspect root-only `/var/lib/lazgate-provision/{packages.log,installer.log,exit-code}`,
`/root/awg/install_amneziawg.log`, `journalctl -u lazgate-awg-install`, and the agent
journal. They may contain sensitive upstream diagnostics; do not publish them
unredacted. UI/outbox result events expose a public failure code/error ID, never
raw remote errors or config content. Events are scoped to the initiating identity.
`sh deploy/verify-awg-host.sh` performs read-only host diagnostics without dumping keys.

Repeated outbox delivery reuses IDs and remote markers. A second fresh API request
against an existing installation is rejected: this is not an attach/reinstall API.
After a terminal failure, inspect the host and retained outbox state before manual
recovery; do not delete `/root/awg` or rerun with `--force` to bypass ownership checks.
SSH bootstrap and key verification happen before queue persistence. A root-only
ownership marker prevents taking over a different installation. If the request
fails after remote changes or enqueue fails, the administrative account may
remain and require operator cleanup; the bootstrap password cannot be recovered
from LazGate. Persisted continuation steps remain idempotent.

Unit tests cover transitions, reboot proof, timeout/resume limits, import integrity,
route overlap, shell syntax, secret-safe results and queue continuation. A real
Ubuntu VM test is still required for installer/DKMS/reboot, firewall preservation,
and production agent sandboxing. Compose exercises userspace client traffic and
agent lifecycle locally. No production VPS was deployed by this task.
The initial schema is intended for clean installations and already contains the
current AmneziaWG fields and constraints. Historical database upgrades are not
supported by this baseline; existing installations must use a fresh database.

Run the opt-in local package checks (downloads disposable official amd64 Ubuntu
containers; requires Docker, no privileges or host mounts):

```sh
LAZ_TEST_AWG_PACKAGES=1 go test ./internal/server/provisioning -run TestAWGPackage -v -count=1
```

These verify both releases' actual tools execution, archive checksums/versions
and DKMS source presence. They do not build/load the kernel module, boot an
Ubuntu kernel or run the full host installer. A real 26.04 kernel/reboot test
remains separate from the userspace/package compatibility checks.
