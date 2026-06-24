package provisioning

import (
	"fmt"
	"gopkg.in/yaml.v3"
)

func awgPrepareScript(s AWGState) string {
	return `set -eu
sudo -n install -d -m 700 /var/lib/lazgate-provision
sudo -n sh -s <<'AWG_PREPARE'
set -eu
umask 077
cd /var/lib/lazgate-provision
` + awgSelectPlatformScript() + `
fetch() {
 curl --proto '=https' --tlsv1.2 -fsSL --retry 2 --max-time 120 "$1" -o "$2.tmp"
 printf '%s  %s\n' "$3" "$2.tmp" | sha256sum -c - >/dev/null
 chmod 700 "$2.tmp"
 mv "$2.tmp" "$2"
}
fetch https://github.com/bivlked/amneziawg-installer/releases/download/v6.1.1/install_amneziawg.sh installer.sh e0024185e686b1b8292ea96cbf5d315ef4cadb1ed29bb46ff1aff41dbd7604d2
fetch https://github.com/bivlked/amneziawg-installer/releases/download/v6.1.1/awg_common.sh awg_common.sh 87e835bb4fc350a75de809f25d7bb7134a003b4f73f669714f8a53e9fd8ded5d
fetch https://github.com/bivlked/amneziawg-installer/releases/download/v6.1.1/manage_amneziawg.sh manage_amneziawg.sh 5cf1692e1c67ea0fe2fd585defc6b80aabe83b5bff7e5915981dfd002ea18eb1
fetch "$dkms_url" dkms.deb "$dkms_sha256"
fetch "$tools_url" tools.deb "$tools_sha256"
# Let the audited installer configure DKMS and apply its kernel compatibility
# fixes. Staging the verified archive must not execute package postinst scripts.
install -d -m 755 /var/cache/apt/archives
install -m 644 dkms.deb "/var/cache/apt/archives/amneziawg-dkms_${dkms_version}_all.deb"
cat > /etc/apt/preferences.d/lazgate-awg <<PIN
Package: amneziawg-tools amneziawg
Pin: version $tools_version
Pin-Priority: 1001

Package: amneziawg-dkms
Pin: version $dkms_version
Pin-Priority: 1001

Package: amneziawg-tools amneziawg-dkms amneziawg
Pin: version *
Pin-Priority: -1
PIN
AWG_PREPARE
cat /proc/sys/kernel/random/boot_id`
}

func awgLaunchScript(s AWGState) string {
	command := fmt.Sprintf(`#!/bin/sh
set -eu
umask 077
cd /var/lib/lazgate-provision
exec 9>/var/lib/lazgate-provision/install.lock
flock -n 9 || exit 0
rm -f exit-code
set +e
export DEBIAN_FRONTEND=noninteractive
apt-get update >packages.log 2>&1 && apt-get install --no-install-recommends -y ./tools.deb >>packages.log 2>&1
rc=$?
if [ "$rc" = 0 ]; then
 env -u AWG_BRANCH bash ./installer.sh --yes --protocol=3.1 --no-tweaks --allow-ipv6 --no-cps --port=%d --subnet=%s --server-name=%s >installer.log 2>&1
 rc=$?
fi
printf '%%s\n' "$rc" >exit-code.tmp
mv exit-code.tmp exit-code
exit "$rc"
`, s.Input.ListenPort, shellQuote(s.Input.Subnet), shellQuote(s.Input.NodeName))
	return "set -eu\n" + `if sudo -n systemctl is-active --quiet lazgate-awg-install.service; then exit 0; fi
sudo -n tee /var/lib/lazgate-provision/run.sh >/dev/null <<'AWG_RUN'
` + command + `AWG_RUN
sudo -n chmod 700 /var/lib/lazgate-provision/run.sh
sudo -n systemctl reset-failed lazgate-awg-install.service 2>/dev/null || true
sudo -n systemd-run --unit=lazgate-awg-install --collect /bin/sh /var/lib/lazgate-provision/run.sh`
}

func awgHandoffScript(s AWGState) string {
	return fmt.Sprintf(`set -eu
sudo -n sh -s <<'AWG_HANDOFF'
set -eu
umask 077
conf=/etc/amnezia/amneziawg/awg0.conf
test "$(cat /var/lib/lazgate-provision/exit-code)" = 0
if test -f /root/awg/setup_state; then
 test "$(cat /root/awg/setup_state)" = 99
fi
test -s "$conf"
if [ ! -f /var/lib/lazgate-provision/handed-off ]; then
 # Only a fresh installer-owned directory can reach this step. Remove all sample peers.
 systemctl stop awg-quick@awg0
 awk '/^\[Peer\]/{exit} !/^(PostUp|PostDown|SaveConfig)[[:space:]]*=/{print}' "$conf" > "$conf.lazgate-tmp"
 nic=$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++)if($i=="dev"){print $(i+1);exit}}')
 case "$nic" in ''|*[!a-zA-Z0-9_.:-]*) exit 1;; esac
 cat >> "$conf.lazgate-tmp" <<HOOKS
PostUp = iptables -I INPUT -p udp --dport %[1]d -m comment --comment lazgate-awg -j ACCEPT; iptables -I FORWARD -i awg0 -s %[2]s -m comment --comment lazgate-awg -j ACCEPT; iptables -I FORWARD -o awg0 -d %[2]s -m conntrack --ctstate ESTABLISHED,RELATED -m comment --comment lazgate-awg -j ACCEPT; iptables -t nat -A POSTROUTING -s %[2]s -o $nic -m comment --comment lazgate-awg -j MASQUERADE
PostDown = iptables -D INPUT -p udp --dport %[1]d -m comment --comment lazgate-awg -j ACCEPT; iptables -D FORWARD -i awg0 -s %[2]s -m comment --comment lazgate-awg -j ACCEPT; iptables -D FORWARD -o awg0 -d %[2]s -m conntrack --ctstate ESTABLISHED,RELATED -m comment --comment lazgate-awg -j ACCEPT; iptables -t nat -D POSTROUTING -s %[2]s -o $nic -m comment --comment lazgate-awg -j MASQUERADE
HOOKS
 chmod 600 "$conf.lazgate-tmp"
 mv "$conf.lazgate-tmp" "$conf"
 # These files contain the sample users' private keys. The server key files stay intact.
 find /root/awg -maxdepth 1 -type f \( -name 'my_phone*' -o -name 'my_laptop*' \) -delete
 find /root/awg/keys -maxdepth 1 -type f \( -name 'my_phone*' -o -name 'my_laptop*' \) -delete
 touch /var/lib/lazgate-provision/handed-off
fi
systemctl enable awg-quick@awg0 >/dev/null
systemctl start awg-quick@awg0
install -d -m 700 /var/lib/lazgate-agent/awg
if [ ! -s /var/lib/lazgate-agent/awg/awg0.conf ]; then
 awg showconf awg0 > /var/lib/lazgate-agent/awg/awg0.conf.tmp
 chmod 600 /var/lib/lazgate-agent/awg/awg0.conf.tmp
 mv /var/lib/lazgate-agent/awg/awg0.conf.tmp /var/lib/lazgate-agent/awg/awg0.conf
fi
install -d -m 755 /etc/systemd/system/awg-quick@awg0.service.d
cat > /etc/systemd/system/awg-quick@awg0.service.d/lazgate.conf <<'DROPIN'
[Service]
ExecStartPost=/usr/bin/awg syncconf awg0 /var/lib/lazgate-agent/awg/awg0.conf
DROPIN
systemctl daemon-reload
cat "$conf"
AWG_HANDOFF`, s.Input.ListenPort, s.Input.Subnet)
}

func awgVerifyScript(s AWGState) string {
	return fmt.Sprintf(`set -eu
sudo -n awg --version >/dev/null
sudo -n modprobe amneziawg
sudo -n systemctl is-active --quiet awg-quick@awg0
ip link show awg0 >/dev/null
test "$(sudo -n awg show awg0 listen-port)" = %d
test "$(cat /proc/sys/net/ipv4/ip_forward)" = 1
sudo -n awg showconf awg0 | grep -q '^HeaderProtectionKey = '
sudo -n iptables -C INPUT -p udp --dport %[1]d -m comment --comment lazgate-awg -j ACCEPT
sudo -n iptables -C FORWARD -i awg0 -s %[2]s -m comment --comment lazgate-awg -j ACCEPT
sudo -n iptables -t nat -S POSTROUTING | grep -F -- '-s %[2]s' | grep -q -- '--comment lazgate-awg -j MASQUERADE'
sudo -n systemctl is-active --quiet lazgate-agent
`, s.Input.ListenPort, s.Input.Subnet)
}

func (i *Installer) awgAgentScript(s AWGState) (string, error) {
	config := map[string]any{
		"node_id": s.Identity.NodeID, "installation_id": s.Identity.InstallationID, "agent_type": "amneziawg",
		"server_url": s.ServerURL, "agent_grpc_target": s.AgentTarget,
		"state_path": "/var/lib/lazgate-agent/state.db", "transport_path": "/var/lib/lazgate-agent/transport.db",
		"mtls":      map[string]string{"ca_file": "/etc/lazgate-agent/server-ca.crt", "cert_file": "/etc/lazgate-agent/node.crt", "key_file": "/etc/lazgate-agent/node.key"},
		"amneziawg": map[string]string{"interface_name": "awg0", "config_path": "/var/lib/lazgate-agent/awg/awg0.conf"},
		"sync":      map[string]int{"auth_sync_interval_seconds": 30, "telemetry_collect_interval_seconds": 30, "heartbeat_interval_seconds": 30, "reconnect_min_backoff_seconds": 1, "reconnect_max_backoff_seconds": 300},
	}
	raw, err := yaml.Marshal(config)
	if err != nil {
		return "", err
	}
	return renderAgentInstallScript(agentInstallScriptInput{AgentDownloadBase: s.DownloadBase, AgentConfig: string(raw), CAPEM: s.Identity.CAPEM, ServerCAPEM: i.agentServerCertPEM, NodeCertPEM: s.Identity.NodeCertPEM, NodeKeyPEM: s.Identity.NodeKeyPEM}, "root", "lazgate-amnezia-agent", awgAgentUnit()), nil
}

func awgAgentUnit() string {
	return `[Unit]
Description=LazGate native AmneziaWG Agent
After=network-online.target awg-quick@awg0.service
Wants=network-online.target
Requires=awg-quick@awg0.service

[Service]
ExecStart=/usr/local/bin/lazgate-amnezia-agent --config /etc/lazgate-agent/config.yaml
Restart=always
RestartSec=5
User=root
NoNewPrivileges=true
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/lazgate-agent /dev/shm /etc/amnezia/amneziawg
LockPersonality=true

[Install]
WantedBy=multi-user.target
`
}
