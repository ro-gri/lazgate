#!/bin/sh
set -eu
set -a
. /app/data/runtime.env
set +a
exec dlv exec /usr/local/bin/laz --headless --listen=:40000 --api-version=2 --accept-multiclient --continue
