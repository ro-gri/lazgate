#!/bin/sh
# Read-only production diagnostics; run as root on the provisioned host.
set -eu
test "$(id -u)" = 0
awg --version
systemctl is-active awg-quick@awg0 lazgate-agent
ip -4 address show dev awg0
awg show awg0 listen-port
test "$(cat /proc/sys/net/ipv4/ip_forward)" = 1
test "$(stat -c %a /etc/amnezia/amneziawg/awg0.conf)" = 600
test -s /var/lib/lazgate-agent/awg/awg0.conf
test "$(stat -c %a /var/lib/lazgate-agent/awg/awg0.conf)" = 600
! grep -Eq '^(Address|PostUp|PostDown|PreUp|PreDown|SaveConfig)[[:space:]]*=' /var/lib/lazgate-agent/awg/awg0.conf
iptables -t nat -S POSTROUTING | grep -- '--comment lazgate-awg -j MASQUERADE'
dpkg-query -W amneziawg-dkms amneziawg-tools
# Do not print showconf/dump: they contain private/header/peer secrets.
