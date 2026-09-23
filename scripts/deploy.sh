#!/usr/bin/env bash
# Deploy the working tree to a Docker host via SSH and (re)start NetScope there.
#
#   scripts/deploy.sh [ssh-host] [remote-dir]      default: netscope /opt/netscope
#   SKIP_WEB=1   keep the remote web/ directory (do not copy the frontend sources)
#   SMOKE=1      run `make smoke` on the host after the deployment
set -euo pipefail

HOST="${1:-netscope}"
DIR="${2:-/opt/netscope}"
cd "$(dirname "$0")/.."

excludes=(--exclude=./.git --exclude=./data --exclude=./.devdata --exclude=./bin
	--exclude=./web/node_modules --exclude=./web/.svelte-kit --exclude='./internal/webui/dist/_app'
	--exclude='*.exe' --exclude='*.test')
if [[ "${SKIP_WEB:-0}" == 1 ]]; then
	excludes+=(--exclude=./web)
fi

echo "→ Übertrage Quellen nach $HOST:$DIR"
tar czf - "${excludes[@]}" . |
	ssh "$HOST" "mkdir -p '$DIR' && cd '$DIR' && tar xzf - --no-same-owner --warning=no-unknown-keyword && chmod +x scripts/*.sh"

echo "→ Baue und starte den Container"
ssh "$HOST" "cd '$DIR' && make docker-up"

if [[ "${SMOKE:-0}" == 1 ]]; then
	ssh "$HOST" "cd '$DIR' && make smoke"
fi
