#!/usr/bin/env bash
# Compile and run in a single step via `go run` — no separate binary artifact.
# Arguments are forwarded to the program, e.g.:
#   ./dev.sh                  # daemon: pipeline + HTTP/WS API, no TUI
#   ./dev.sh -testnet         # Hyperliquid testnet
#   ./dev.sh -config foo.toml # alternate config
#
# Runs from the repo root so config.toml and ./data resolve correctly.
# (run.sh builds a persistent ./hyperagent binary; this one builds-and-runs in
# one go, ideal for iterating during development.)
set -euo pipefail
cd "$(dirname "$0")"

exec go run ./src "$@"
