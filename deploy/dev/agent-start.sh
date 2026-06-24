#!/bin/sh
set -eu
exec dlv exec /usr/local/bin/lazgate-agent --headless --listen=:40000 --api-version=2 --accept-multiclient --continue -- -config /lab/agent.yaml
