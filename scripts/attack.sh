#!/usr/bin/env bash
set -euo pipefail

GATEWAY_URL="${1:-http://localhost:8080}"
echo "==================================================================="
echo "   🔥  AEGISPULSE: AUTOMATED CHAOS LOAD & REPLAY TEST SUITE  🔥   "
echo "   Target Gateway: ${GATEWAY_URL}"
echo "==================================================================="

if [ -f "./bin/chaos" ]; then
    ./bin/chaos -mode=attack -url="${GATEWAY_URL}"
else
    go run ./cmd/chaos -mode=attack -url="${GATEWAY_URL}"
fi
