#!/usr/bin/env bash
set -Eeuo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
root=/opt/lazgate
[[ -e $root ]] || { echo 'Nothing to uninstall.'; exit 0; }
[[ -f $root/.lazgate-installation && $(cat "$root/.lazgate-installation") == native-v1 ]] || {
  echo 'Not a native LazGate installation; refusing to delete it.' >&2; exit 1;
}
echo 'This removes LazGate services, binaries, databases, secrets and dedicated Caddy TLS data in /opt/lazgate.'
echo 'Other services, Caddy installations and Docker resources are not touched.'
if [[ ${LAZ_FORCE:-0} != 1 ]]; then
  printf 'Type delete to continue: ' >/dev/tty
  IFS= read -r answer </dev/tty
  [[ $answer == delete ]] || exit 1
fi
systemctl disable --now lazgate-caddy.service lazgate.service
rm -f /etc/systemd/system/lazgate.service /etc/systemd/system/lazgate-caddy.service
systemctl daemon-reload
systemctl reset-failed lazgate.service lazgate-caddy.service 2>/dev/null || true
rm -rf -- "$root"
for user in lazgate lazgate-caddy; do
  if id "$user" >/dev/null 2>&1; then userdel "$user"; fi
  if getent group "$user" >/dev/null; then groupdel "$user"; fi
done
echo 'LazGate uninstalled. Shared system packages were left installed.'
