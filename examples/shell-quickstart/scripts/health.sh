#!/usr/bin/env bash
# tool: health_check
# description: Check the health status of a running service.
# risk: read_only
# param: host: string: Hostname or URL to check
set -euo pipefail

HOST="${1:?Usage: health.sh <host>}"

echo "Checking health of $HOST..."

# Placeholder — replace with a real HTTP check.
if command -v curl &>/dev/null; then
    STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$HOST/healthz" 2>/dev/null || echo "000")
else
    STATUS="000"
fi

if [ "$STATUS" = "200" ]; then
    echo "OK — $HOST is healthy (HTTP $STATUS)"
else
    echo "WARN — $HOST returned HTTP $STATUS"
    exit 1
fi
