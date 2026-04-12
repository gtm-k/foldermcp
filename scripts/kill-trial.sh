#!/usr/bin/env bash
# Stratified kill-trial harness — Exit Gate G4
#
# Spawns `foldermcp index` on a copy of the micro-fixture, kills it at
# 10 different timing windows, restarts, and verifies DB integrity.
#
# Kill windows (10 trials, 2 each per spec §13.1):
#   1-2. WAL commit mid-batch (kill at 2-3s)
#   3-4. Tombstone write / file deletion mid-pass (kill at 3-5s)
#   5-6. Pre-migration snapshot (kill at 1-2s, early startup)
#   7-8. Embedding write (kill at 5-8s, during ONNX inference)
#   9-10. Random mid-pass SIGKILL (kill at 2-6s)
#
# Usage:
#   ./scripts/kill-trial.sh [fixture-path] [foldermcp-binary]
#
# Requires: Linux (Unix signals, Unix domain sockets)
set -euo pipefail

FIXTURE_SRC="${1:-testdata/v3/micro}"
BIN="${2:-bin/foldermcp}"

if [ ! -d "${FIXTURE_SRC}" ]; then
    echo "error: fixture not found at ${FIXTURE_SRC}" >&2
    exit 2
fi

if [ ! -x "${BIN}" ]; then
    echo "error: foldermcp binary not found at ${BIN}" >&2
    exit 2
fi

# Create isolated working directories
WORK="$(mktemp -d)"
FIXTURE="${WORK}/fixture"
STORE="${WORK}/store"
cp -r "${FIXTURE_SRC}" "${FIXTURE}"
mkdir -p "${STORE}"

cleanup() {
    rm -rf "${WORK}"
}
trap cleanup EXIT

echo "Kill-trial harness — G4 exit gate"
echo "  fixture: ${FIXTURE_SRC} → ${FIXTURE}"
echo "  store:   ${STORE}"
echo "  binary:  ${BIN}"
echo ""

# Define kill windows: (trial_name, min_delay_seconds, max_delay_seconds)
# Each window targets a different crash-sensitive phase of the indexer.
TRIALS=(
    "wal-commit-1:2:3"
    "wal-commit-2:2:3"
    "tombstone-1:3:5"
    "tombstone-2:3:5"
    "pre-snapshot-1:1:2"
    "pre-snapshot-2:1:2"
    "embedding-1:5:8"
    "embedding-2:5:8"
    "random-1:2:6"
    "random-2:2:6"
)

PASSED=0
FAILED=0
TOTAL=${#TRIALS[@]}

for trial_spec in "${TRIALS[@]}"; do
    IFS=':' read -r name min_delay max_delay <<< "${trial_spec}"
    # Compute random delay within the window
    delay_range=$((max_delay - min_delay + 1))
    delay=$((min_delay + RANDOM % delay_range))

    echo "==> Trial: ${name} (kill after ${delay}s)"

    # Start the indexer
    FOLDERMCP_STORE="${STORE}" "${BIN}" index "${FIXTURE}" &
    PID=$!
    sleep "${delay}"

    # SIGKILL — no graceful shutdown, exercises crash recovery
    kill -9 "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true

    echo "    killed (pid ${PID})"

    # Verify DB integrity after crash
    if ! FOLDERMCP_STORE="${STORE}" "${BIN}" index health 2>&1; then
        echo "    ❌ FAIL: health check failed after kill"
        FAILED=$((FAILED + 1))
        continue
    fi

    # Resume indexing to verify the indexer can recover and complete.
    # The indexer runs in watch mode (never exits on its own), so timeout
    # returning 124 is expected. We capture $? directly because `if !`
    # clobbers it to 0 inside the then-block.
    set +e
    FOLDERMCP_STORE="${STORE}" timeout 120 "${BIN}" index "${FIXTURE}" 2>&1
    EXIT_CODE=$?
    set -e

    if [ "${EXIT_CODE}" -ne 0 ] && [ "${EXIT_CODE}" -ne 124 ]; then
        echo "    ❌ FAIL: resume indexing failed with exit code ${EXIT_CODE}"
        FAILED=$((FAILED + 1))
        continue
    fi
    if [ "${EXIT_CODE}" -eq 124 ]; then
        echo "    resumed (timed out after 120s — expected for watch mode)"
    fi

    # Final health check after recovery
    if ! FOLDERMCP_STORE="${STORE}" "${BIN}" index health 2>&1; then
        echo "    ❌ FAIL: health check failed after recovery"
        FAILED=$((FAILED + 1))
        continue
    fi

    echo "    ✅ PASS"
    PASSED=$((PASSED + 1))
done

echo ""
echo "Results: ${PASSED}/${TOTAL} passed, ${FAILED}/${TOTAL} failed"

if [ "${FAILED}" -gt 0 ]; then
    echo "❌ FAIL G4: ${FAILED} kill trial(s) failed"
    exit 1
fi

echo "✅ PASS G4: ${TOTAL}/${TOTAL} kill trials recovered cleanly"
