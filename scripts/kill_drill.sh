#!/usr/bin/env bash
set -euo pipefail

GATEWAY_URL="${1:-http://localhost:8080}"
echo "==================================================================="
echo "   🚨  AEGISPULSE: TWO-STEP SAFETY PANIC KILL-SWITCH DRILL  🚨   "
echo "   Target Gateway: ${GATEWAY_URL}"
echo "==================================================================="

if [ -f "./bin/chaos" ]; then
    ./bin/chaos -mode=kill-drill -url="${GATEWAY_URL}"
else
    go run ./cmd/chaos -mode=kill-drill -url="${GATEWAY_URL}"
fi
