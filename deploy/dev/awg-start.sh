#!/bin/bash
set -euo pipefail
# Userspace is deliberately selected only in this development environment.
amneziawg-go -f awg0 &
runtime_pid=$!
trap 'kill "$runtime_pid" 2>/dev/null || true; wait "$runtime_pid" 2>/dev/null || true' EXIT
trap 'exit 0' TERM INT
for attempt in $(seq 1 100); do
    [ -S /var/run/amneziawg/awg0.sock ] && break
    kill -0 "$runtime_pid" 2>/dev/null || exit 1
    sleep 0.1
done
install -d -o 10001 -g 10001 -m 700 /var/lib/lazgate-agent/awg
if [ ! -f /var/lib/lazgate-agent/awg/awg0.conf ]; then
    install -o 10001 -g 10001 -m 600 /lab/awg.conf /var/lib/lazgate-agent/awg/awg0.conf
fi
awg setconf awg0 /var/lib/lazgate-agent/awg/awg0.conf
install -d -m 700 /etc/amnezia/amneziawg
if [ ! -f /etc/amnezia/amneziawg/awg0.conf ]; then
    install -m 600 /lab/awg.conf /etc/amnezia/amneziawg/awg0.conf
    sed -i '/^\[Interface\]/a Address = 10.8.1.1/24' /etc/amnezia/amneziawg/awg0.conf
fi
address=$(awk -F ' = ' '$1 == "Address" {print $2; exit}' /etc/amnezia/amneziawg/awg0.conf)
ip address replace "$address" dev awg0
ip link set awg0 mtu 1376 up
sysctl -w net.ipv4.ip_forward=1 >/dev/null
outbound=$(ip -4 route show default | awk 'NR == 1 { print $5 }')
iptables -t nat -C POSTROUTING -s 10.8.1.0/24 -o "$outbound" -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s 10.8.1.0/24 -o "$outbound" -j MASQUERADE
iptables -C FORWARD -i awg0 -j ACCEPT 2>/dev/null || iptables -A FORWARD -i awg0 -j ACCEPT
iptables -C FORWARD -o awg0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || iptables -A FORWARD -o awg0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
wait "$runtime_pid"
