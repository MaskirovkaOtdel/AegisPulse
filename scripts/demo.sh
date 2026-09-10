#!/usr/bin/env bash
set -euo pipefail

GATEWAY_URL="${1:-http://localhost:8080}"
echo "==================================================================="
echo "   🛡️  AEGISPULSE: 60-SECOND GUIDED END-TO-END DEMO SCENARIO  🛡️   "
echo "   Target Gateway: ${GATEWAY_URL}"
echo "==================================================================="

# Use compiled chaos CLI if available, otherwise run go run
if [ -f "./bin/chaos" ]; then
    ./bin/chaos -mode=demo -url="${GATEWAY_URL}"
else
    go run ./cmd/chaos -mode=demo -url="${GATEWAY_URL}"
fi
