#!/usr/bin/env bash
# package-windows.sh — build a self-contained Windows release bundle of foldermcp
# (binary + ONNX Runtime DLLs + embedding model) that a user can unzip and run
# with NO environment variables: the binary resolves the ONNX lib and model/
# directory next to the executable (see configureOrtLib / resolveModelPaths).
#
# Requirements (Linux/WSL build host): go, x86_64-w64-mingw32-gcc, curl, python3.
# Cross-compiling the cgo stack (sqlite + sqlite-vec + tree-sitter + onnxruntime
# bindings) for Windows needs the mingw toolchain plus a sqlite3.h on the
# include path — sqlite-vec #includes it but mattn/go-sqlite3 ships it renamed.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
ORT_VERSION="${ORT_VERSION:-1.24.1}"
DIST="$REPO/dist"
STAGE="$DIST/foldermcp-windows-amd64"
CACHE="$REPO/.cache/pkg"
MODEL_DIR="$REPO/internal/v3/embed/model"

mkdir -p "$CACHE"
rm -rf "$STAGE" && mkdir -p "$STAGE/model"

echo "==> stage sqlite headers for sqlite-vec cross-compile"
MATTN="$(go list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3)"
INC="$CACHE/winsdk-include"
mkdir -p "$INC"
cp "$MATTN/sqlite3-binding.h" "$INC/sqlite3.h"
cp "$MATTN/sqlite3ext.h" "$INC/sqlite3ext.h"

echo "==> cross-compile foldermcp.exe (cgo + mingw)"
( cd "$REPO" && CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
    CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
    CGO_CFLAGS="-I$INC" \
    go build -tags "cgo sqlite_fts5" -ldflags "-s -w" \
    -o "$STAGE/foldermcp.exe" ./cmd/foldermcp )

echo "==> fetch ONNX Runtime $ORT_VERSION (win-x64)"
ORT_ZIP="$CACHE/onnxruntime-win-x64-$ORT_VERSION.zip"
if [ ! -f "$ORT_ZIP" ]; then
  curl -sSL -o "$ORT_ZIP" \
    "https://github.com/microsoft/onnxruntime/releases/download/v$ORT_VERSION/onnxruntime-win-x64-$ORT_VERSION.zip"
fi
python3 - "$ORT_ZIP" "$STAGE" <<'PY'
import sys, zipfile, os
zip_path, dst = sys.argv[1], sys.argv[2]
z = zipfile.ZipFile(zip_path)
for n in z.namelist():
    if n.endswith(".dll"):
        with open(os.path.join(dst, os.path.basename(n)), "wb") as f:
            f.write(z.read(n))
        print("   bundled", os.path.basename(n))
PY

echo "==> ensure embedding model is present"
if [ ! -f "$MODEL_DIR/model.onnx" ]; then
  ( cd "$REPO" && make v3-fetch-model )
fi
cp "$MODEL_DIR/model.onnx" "$MODEL_DIR/tokenizer.json" "$STAGE/model/"

echo "==> write install note"
cat > "$STAGE/README-INSTALL.txt" <<'TXT'
FolderMCP (Windows) — self-contained bundle

Everything needed is in this folder: foldermcp.exe, the ONNX Runtime DLLs, and
the embedding model under model\. No environment variables are required — the
binary finds the DLL and model next to itself.

Quick start (PowerShell or cmd, from this folder):
  .\foldermcp.exe index-v3  C:\path\to\your\folder     # build the local index
  .\foldermcp.exe search-v3 "where is retry logic"     # hybrid search (CLI)
  .\foldermcp.exe connect   claude-code                # wire up an AI agent (MCP)

The index lives under %USERPROFILE%\.foldermcp by default; set FOLDERMCP_STORE to
change it. Nothing leaves your machine.
TXT

echo "==> zip"
( cd "$DIST" && rm -f foldermcp-windows-amd64.zip && \
  python3 -c "import shutil; shutil.make_archive('foldermcp-windows-amd64','zip','.', 'foldermcp-windows-amd64')" )

echo "==> done: $DIST/foldermcp-windows-amd64.zip"
ls -la "$STAGE"
