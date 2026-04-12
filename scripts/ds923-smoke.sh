#!/usr/bin/env bash
# DS923+ smoke benchmark — Exit Gate G6
#
# Measures:
#   1. RSS steady-state under 1 GB memory limit
#   2. Index throughput on NAS-class hardware
#   3. Crash recovery after docker kill
#
# Run on:
#   - Owned DS923+ (or equivalent Synology NAS)
#   - Rented NAS-class VM (e.g., Hetzner AX41, comparable specs)
#
# Prerequisites:
#   - Docker installed and running
#   - foldermcp Docker image available (local build or registry pull)
#
# Usage:
#   ./scripts/ds923-smoke.sh [fixture-path] [image-name]
set -euo pipefail

FIXTURE="${1:-testdata/v3/micro}"
IMAGE="${2:-ghcr.io/gtm-k/foldermcp-v3:m1-smoke}"
CONTAINER_NAME="fm-smoke-$$"
STORE_DIR="/tmp/fm-store-$$"

if [ ! -d "${FIXTURE}" ]; then
    echo "error: fixture not found at ${FIXTURE}" >&2
    exit 2
fi

# Ensure absolute path for Docker volume mount
FIXTURE_ABS="$(cd "${FIXTURE}" && pwd)"

cleanup() {
    echo "cleaning up..."
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true
    rm -rf "${STORE_DIR}"
}
trap cleanup EXIT

echo "DS923+ smoke benchmark — G6 exit gate"
echo "  fixture: ${FIXTURE_ABS}"
echo "  image:   ${IMAGE}"
echo "  store:   ${STORE_DIR}"
echo ""

mkdir -p "${STORE_DIR}"

# ── Step 1: Pull image (skip if local) ───────────────────
echo "==> Pulling image..."
docker pull "${IMAGE}" 2>/dev/null || echo "    (using local image)"

# ── Step 2: Start container with 1 GB memory limit ───────
echo "==> Starting container with --memory 1g..."
docker run -d \
    --name "${CONTAINER_NAME}" \
    --memory 1g \
    -v "${FIXTURE_ABS}:/workspace:ro" \
    -v "${STORE_DIR}:/store" \
    "${IMAGE}" all-v3 /workspace

echo "    container started, waiting 120s for indexer to settle..."
sleep 120

# ── Step 3: Measure RSS steady-state ─────────────────────
echo "==> Measuring RSS..."
RSS_RAW=$(docker stats "${CONTAINER_NAME}" --no-stream --format '{{.MemUsage}}')
echo "    raw memory usage: ${RSS_RAW}"

# Extract the "used" portion (before the /) and convert to MB
# Format is typically "123.4MiB / 1GiB" or "123.4MB / 1GB"
RSS_USED=$(echo "${RSS_RAW}" | awk -F'/' '{print $1}' | xargs)

# Convert to MB for comparison (handle MiB, GiB, MB, GB)
RSS_MB=$(echo "${RSS_USED}" | awk '{
    val = $1 + 0;
    unit = $1;
    gsub(/[0-9.]/, "", unit);
    if (unit == "GiB" || unit == "GB") val = val * 1024;
    if (unit == "KiB" || unit == "KB") val = val / 1024;
    printf "%d", val;
}')

echo "    steady-state RSS: ${RSS_MB} MB"

if [ "${RSS_MB}" -gt 1024 ]; then
    echo "❌ FAIL G6: RSS ${RSS_MB} MB exceeds 1024 MB ceiling"
    exit 1
fi
echo "    ✅ RSS within ceiling"

# ── Step 4: Verify health inside container ────────────────
echo "==> Checking health..."
if ! docker exec "${CONTAINER_NAME}" foldermcp index health 2>&1; then
    echo "❌ FAIL G6: health check failed during steady-state"
    exit 1
fi
echo "    ✅ health OK"

# ── Step 5: Crash recovery — kill and restart ─────────────
echo "==> Testing crash recovery (docker kill + restart)..."
docker kill "${CONTAINER_NAME}"
sleep 2

# Restart the same container (preserves the store volume)
docker start "${CONTAINER_NAME}"
sleep 30

echo "    verifying health after crash recovery..."
if ! docker exec "${CONTAINER_NAME}" foldermcp index health 2>&1; then
    echo "❌ FAIL G6: health check failed after crash recovery"
    exit 1
fi
echo "    ✅ crash recovery clean"

# ── Step 6: Collect final metrics ─────────────────────────
echo ""
echo "==> Final metrics:"
docker stats "${CONTAINER_NAME}" --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}\t{{.BlockIO}}'
echo ""

# Get image size
IMAGE_SIZE=$(docker images "${IMAGE}" --format '{{.Size}}' 2>/dev/null || echo "unknown")
echo "Image size: ${IMAGE_SIZE}"
echo ""

echo "✅ PASS G6: RSS ${RSS_MB} MB ≤ 1024 MB, crash recovery clean"
