#!/usr/bin/env bash
# Helper to run the G39 retrieval harness against the micro-fixture.
# Runs entirely inside WSL2. Sets up isolated HOME/store dirs.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

BIN="${FOLDERMCP_BIN:-$REPO/bin/foldermcp-linux}"
FIXTURE="${FIXTURE:-$REPO/testdata/v3/micro}"
LABELED="${LABELED:-$REPO/testdata/v3/micro_labeled_queries.yaml}"

# Clean up any lingering processes
pkill -9 -f foldermcp-linux 2>/dev/null || true
sleep 1

# Isolated runtime dirs
RUN_DIR="/tmp/fm-g39-run"
STORE_DIR="/tmp/fm-g39-store"
rm -rf "$RUN_DIR" "$STORE_DIR"
mkdir -p "$RUN_DIR/.foldermcp/run" "$STORE_DIR"

export HOME="$RUN_DIR"
export FOLDERMCP_STORE="$STORE_DIR"
SOCK="$HOME/.foldermcp/run/serve.sock"

echo "== config =="
echo "  BIN:     $BIN"
echo "  FIXTURE: $FIXTURE"
echo "  LABELED: $LABELED"
echo "  HOME:    $HOME"
echo "  SOCK:    $SOCK"
echo "  STORE:   $FOLDERMCP_STORE"
echo ""

# Start server
echo "== starting all-v3 =="
"$BIN" all-v3 "$FIXTURE" > /tmp/fm-g39.log 2>&1 &
ALL_PID=$!
echo "  PID: $ALL_PID"

cleanup() {
    kill "$ALL_PID" 2>/dev/null || true
    wait "$ALL_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for socket
for i in $(seq 1 60); do
    if [ -S "$SOCK" ]; then
        echo "  socket ready after ${i}s"
        break
    fi
    sleep 1
done
if [ ! -S "$SOCK" ]; then
    echo "  FAIL: socket did not appear within 60s"
    echo "  server log:"
    cat /tmp/fm-g39.log
    exit 2
fi

# Let indexer finish first pass. Even a 12-file fixture can take 20-40s
# on the first run because ONNX model loading + tree-sitter grammar init
# + sqlite-vec extension load all happen lazily.
echo "  letting indexer settle (45s)..."
sleep 45

echo ""
echo "== server log =="
cat /tmp/fm-g39.log
echo ""

echo "== indexer state =="
"$BIN" index-v3 health 2>&1 || true
"$BIN" index-v3 status 2>&1 || true
echo ""

echo "== DB content counts =="
DB_FILE="$FOLDERMCP_STORE/default/index.db"
if [ -f "$DB_FILE" ]; then
    echo "DB: $DB_FILE"
    sqlite3 "$DB_FILE" <<SQL 2>&1
.mode column
.header on
SELECT 'files' AS table_, COUNT(*) AS cnt FROM files;
SELECT 'nodes' AS table_, COUNT(*) AS cnt FROM nodes;
SELECT 'chunks' AS table_, COUNT(*) AS cnt FROM chunks;
SELECT 'embeddings' AS table_, COUNT(*) AS cnt FROM vec_embeddings;
SQL
fi
echo ""

echo "== running retrieval harness =="
# NOTE: the harness is now a multi-file package (retrieval-harness.go +
# metrics.go + baselines.go), so it must be run as the package `./scripts`,
# not a single .go file. -corpus enables the agentic-grep + raw-read baselines
# (requires `rg` on PATH; `pdftotext` for the pdf stratum).
go run -tags harness "$REPO/scripts" \
    -labeled "$LABELED" \
    -socket "$SOCK" \
    -corpus "$FIXTURE"
