#!/usr/bin/env bash
# Compile the hyperagent binary to the repo root (no run).
# Use ./run.sh to build and launch in one step.
set -euo pipefail
cd "$(dirname "$0")"

go build -o hyperagent ./src
echo "built ./hyperagent"
