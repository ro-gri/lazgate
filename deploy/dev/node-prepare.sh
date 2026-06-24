#!/bin/sh
set -eu
ssh-keygen -A
password=$(cat /lab/root-password)
printf 'root:%s\n' "$password" | chpasswd
install -d -o 10001 -g 10001 -m 700 /var/lib/lazgate-agent
install -d -m 755 /etc/hysteria
if [ -f /lab/hysteria.yaml ] && [ ! -e /etc/hysteria/config.yaml ]; then
    install -o 10001 -g 10001 -m 640 /lab/hysteria.yaml /etc/hysteria/config.yaml
fi
