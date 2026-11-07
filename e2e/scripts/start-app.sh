#!/usr/bin/env bash
# Builds Pantau and serves it on port 18199 with a throwaway temp DB for e2e runs.
set -euo pipefail
cd "$(dirname "$0")/.."   # e2e/ project root
mkdir -p .tmp
go build -o .tmp/pantau ..
tmp="$(mktemp -d)"
pantau_pid=0
cleanup() { kill "$pantau_pid" 2>/dev/null; wait "$pantau_pid" 2>/dev/null; rm -rf "$tmp"; }
trap cleanup EXIT
trap 'kill -TERM "$pantau_pid" 2>/dev/null' TERM INT
./.tmp/pantau -db "$tmp/e2e.db" -port 18199 -q -disable-update-check &
pantau_pid=$!
wait "$pantau_pid"
