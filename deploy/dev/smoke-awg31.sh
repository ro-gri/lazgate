#!/usr/bin/env bash
# Local userspace AWG 3.1 smoke. No compose, nested Docker, or host routes.
# Usage: bash deploy/dev/smoke-awg31.sh [image]
# Uses the already-built image; source pins belong to deploy/dev/Dockerfile.
set -euo pipefail
image=${1:-lazgate-dev-amnezia}
run="lazgate-awg31-$(date +%s)-$$-${RANDOM}"
label="io.lazgate.awg31-smoke=$run"
network=""
containers=()
cleanup() {
    rc=$?
    trap - EXIT
    for c in "${containers[@]}"; do
        if [ "$rc" -ne 0 ]; then
            docker exec "$c" bash -c 'cat /smoke/runtime.log 2>/dev/null; awg show awg0 2>/dev/null' >&2 || true
        fi
        docker rm -f "$c" >/dev/null 2>&1 || true
    done
    if [ -n "$network" ]; then docker network rm "$network" >/dev/null 2>&1 || true; fi
    exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
command -v docker >/dev/null
# Resolve once, so a concurrent image rebuild cannot mix binaries between peers.
image_id=$(docker image inspect "$image" --format '{{.Id}}')
echo "AWG smoke image: $image_id"
network=$(docker network create --internal --label "$label" "$run")
for role in server client; do
    c="$run-$role"
    # Track only successfully created IDs, never remove by broad name/label filters.
    id=$(docker create --name "$c" --label "$label" --network "$network" \
        --cap-add NET_ADMIN --device /dev/net/tun --entrypoint /bin/bash \
        "$image_id" -c 'exec sleep infinity')
    containers+=("$id")
    docker start "$id" >/dev/null
    docker exec "$id" bash -c 'umask 077; mkdir /smoke; awg genkey > /smoke/private; awg pubkey < /smoke/private > /smoke/public'
done
server=${containers[0]}
client=${containers[1]}
docker exec "$server" awg --version | tee /dev/stderr | awk '/v3\.1\./ {ok=1} END {exit !ok}'
server_pub=$(docker exec "$server" cat /smoke/public)
client_pub=$(docker exec "$client" cat /smoke/public)
server_ip=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$server")
hpk=$(docker exec "$server" awg genpsk)
for c in "$server" "$client"; do
    docker exec -i "$c" bash -c 'umask 077; cat > /smoke/base.conf' <<EOF
[Interface]
HeaderProtectionKey = $hpk
ListenPort = 51820
Jc = 4
Jmin = 40
Jmax = 70
S1 = 32
S2 = 48
S3 = 16
S4 = 16
H1 = 1001
H2 = 1002
H3 = 1003
H4 = 1004
EOF
    docker exec "$c" bash -c 'printf "PrivateKey = %s\n" "$(cat /smoke/private)" >> /smoke/base.conf'
done
start_runtime() {
    c=$1 address=$2 config=$3
    docker exec -d "$c" bash -c 'exec amneziawg-go -f awg0 > /smoke/runtime.log 2>&1'
    docker exec "$c" bash -ec '
        for ((i=0;i<100;i++)); do
            if [ -S /var/run/amneziawg/awg0.sock ]; then break; fi
            sleep .1
        done
        awg setconf awg0 "$2"
        ip address replace "$1/24" dev awg0
        ip link set awg0 mtu 1376 up
    ' -- "$address" "$config"
}
start_runtime "$server" 10.203.31.1 /smoke/base.conf
start_runtime "$client" 10.203.31.2 /smoke/base.conf
docker exec "$server" bash -ec 'openssl req -x509 -newkey rsa:2048 -nodes -keyout /smoke/tls.key -out /smoke/tls.crt -subj /CN=awg-smoke -days 1 > /smoke/tls.log 2>&1'
start_https() {
    docker exec -d "$server" bash -c 'exec openssl s_server -quiet -accept 10.203.31.1:8443 -cert /smoke/tls.crt -key /smoke/tls.key -www > /smoke/https.log 2>&1'
}
traffic() {
    docker exec "$client" curl --silent --show-error --fail --insecure --noproxy '*' \
        --connect-timeout 3 --max-time 5 https://10.203.31.1:8443/ -o /dev/null
}
eventually_traffic() {
    for ((attempt=0;attempt<12;attempt++)); do
        if traffic 2>/dev/null; then return; fi
        sleep 1
    done
    echo 'FAIL: tunnel HTTPS did not recover' >&2
    return 1
}
assert_counters() {
    for c in "$server" "$client"; do
        docker exec "$c" awg show awg0 latest-handshakes | awk 'NF == 2 && $2 > 0 {ok=1} END {exit !ok}'
        docker exec "$c" awg show awg0 transfer | awk 'NF == 3 && $2 > 0 && $3 > 0 {ok=1; print "  RX=" $2 " TX=" $3} END {exit !ok}'
    done
}
start_https
docker exec "$client" awg set awg0 peer "$server_pub" allowed-ips 10.203.31.1/32 endpoint "$server_ip:51820" persistent-keepalive 1
docker exec "$server" awg set awg0 peer "$client_pub" allowed-ips 10.203.31.2/32
eventually_traffic
assert_counters
echo 'PASS: peer add, tunnel HTTPS, handshakes and bidirectional counters'
docker exec "$server" awg set awg0 peer "$client_pub" remove
test -z "$(docker exec "$server" awg show awg0 peers)"
for ((attempt=0;attempt<2;attempt++)); do
    if traffic 2>/dev/null; then echo 'FAIL: traffic passed after peer removal' >&2; exit 1; fi
done
echo 'PASS: peer removal blocks fresh HTTPS connections'
docker exec "$server" awg set awg0 peer "$client_pub" allowed-ips 10.203.31.2/32
eventually_traffic
assert_counters
echo 'PASS: peer re-add restores traffic'
# Explicit saved-config restore, not an assertion about agent/systemd persistence.
docker exec "$server" bash -ec 'umask 077; awg showconf awg0 > /smoke/saved.conf; grep -q "^HeaderProtectionKey = " /smoke/saved.conf'
docker restart --time 2 "$server" >/dev/null
start_runtime "$server" 10.203.31.1 /smoke/saved.conf
test "$(docker exec "$server" awg show awg0 peers)" = "$client_pub"
start_https
eventually_traffic
assert_counters
echo 'PASS: container/runtime restart + saved HPK/peer config restore + traffic'
echo 'PASS: AWG 3.1 smoke complete (own resources cleaned on EXIT)'
