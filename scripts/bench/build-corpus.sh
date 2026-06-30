#!/usr/bin/env bash
#
# build-corpus.sh — assemble a benchmark corpus from a declarative manifest.
#
# Two modes:
#
#   --pilot  <destdir>                    OFFLINE. Copies git-tracked in-repo
#                                         testdata fixtures into <destdir> per
#                                         scripts/bench/pilot-corpus-manifest.yaml.
#                                         No network. Reproducible: same manifest
#                                         -> byte-identical tree.
#
#   --frozen <destdir> --manifest <yaml>  NETWORK. Downloads each entry's `url`,
#                                         verifies its `sha256`, and lays it out
#                                         at `dest`. This is the future >=1000-file
#                                         frozen-corpus build path (Phase 9b).
#                                         Requires --allow-network. There is no
#                                         frozen manifest in-repo yet, so this mode
#                                         refuses until one is authored.
#
# The two modes share one layout step, so the pilot and frozen corpora are
# structurally interchangeable to the indexer and harness.
#
# Self-check:
#   bash scripts/bench/build-corpus.sh --pilot /tmp/bench-pilot   # exits 0
#
# Schema: scripts/bench/corpus-manifest.schema.md
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PILOT_MANIFEST_DEFAULT="$SCRIPT_DIR/pilot-corpus-manifest.yaml"

usage() {
  cat <<'EOF'
Usage:
  build-corpus.sh --pilot <destdir> [--manifest <yaml>]
  build-corpus.sh --frozen <destdir> --manifest <yaml> --allow-network

Options:
  --pilot <destdir>     Assemble the OFFLINE pilot corpus (copies in-repo fixtures).
  --frozen <destdir>    Assemble the FROZEN corpus (download + sha256 verify).
  --manifest <yaml>     Manifest path (pilot mode defaults to the in-repo pilot manifest).
  --allow-network       Required for --frozen; acknowledges network downloads.
  -h, --help            Show this help.

Exit codes: 0 ok | 1 build error (missing source / sha mismatch) | 2 usage error.
EOF
}

# ── arg parse ─────────────────────────────────────────────
MODE=""
DEST=""
MANIFEST=""
ALLOW_NETWORK=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pilot)         MODE="pilot";  DEST="${2:-}"; shift 2 ;;
    --frozen)        MODE="frozen"; DEST="${2:-}"; shift 2 ;;
    --manifest)      MANIFEST="${2:-}"; shift 2 ;;
    --allow-network) ALLOW_NETWORK=1; shift ;;
    -h|--help)       usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ -n "$MODE" ]] || { echo "error: one of --pilot/--frozen is required" >&2; usage >&2; exit 2; }
[[ -n "$DEST" ]] || { echo "error: <destdir> is required" >&2; usage >&2; exit 2; }

# Guard against catastrophic rm targets (we clean DEST for a deterministic rebuild).
case "$DEST" in
  ""|"/"|"."|".."|"~"|"$HOME") echo "error: refusing unsafe destination: '$DEST'" >&2; exit 2 ;;
esac

# ── manifest field extractors ─────────────────────────────
# Block-style YAML; one value per entry. Order is preserved, so same-keyed
# values pair up positionally across entries. Strips quotes, CR (Windows), and
# trailing whitespace. Dependency-free (no yq).
extract_field() {
  # $1 = key, $2 = manifest path
  sed -n "s/^[[:space:]]*$1:[[:space:]]*//p" "$2" | tr -d '"\r' | sed 's/[[:space:]]*$//'
}

# read_into ARRAYNAME < input — portable `mapfile -t` replacement. macOS ships
# bash 3.2 (no `mapfile`), and this script's shebang resolves to it there, so we
# read line-by-line into the named array instead. Bash 3.2+ compatible.
read_into() {
  local __name="$1" __line
  eval "$__name=()"
  while IFS= read -r __line || [[ -n "$__line" ]]; do
    eval "$__name+=( \"\$__line\" )"
  done
}

# sha256_of FILE — print the hex sha256 using coreutils `sha256sum` (Linux) or
# `shasum -a 256` (macOS, which has no `sha256sum`).
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# ── pilot mode ────────────────────────────────────────────
build_pilot() {
  MANIFEST="${MANIFEST:-$PILOT_MANIFEST_DEFAULT}"
  [[ -f "$MANIFEST" ]] || { echo "error: pilot manifest not found: $MANIFEST" >&2; exit 1; }

  read_into SRCS   < <(extract_field src     "$MANIFEST")
  read_into DESTS  < <(extract_field dest    "$MANIFEST")
  read_into STRATA < <(extract_field stratum "$MANIFEST")

  if [[ ${#SRCS[@]} -eq 0 ]]; then
    echo "error: no 'src:' entries in $MANIFEST" >&2; exit 1
  fi
  if [[ ${#SRCS[@]} -ne ${#DESTS[@]} ]]; then
    echo "error: manifest src/dest count mismatch (${#SRCS[@]} src vs ${#DESTS[@]} dest)" >&2; exit 1
  fi

  echo "== build-corpus (pilot) =="
  echo "  manifest: $MANIFEST"
  echo "  dest:     $DEST"
  echo "  entries:  ${#SRCS[@]}"

  # Deterministic rebuild: clean then recreate.
  rm -rf -- "$DEST"
  mkdir -p -- "$DEST"

  local i src dst copied=0
  for i in "${!SRCS[@]}"; do
    src="$REPO_ROOT/${SRCS[$i]}"
    dst="$DEST/${DESTS[$i]}"
    if [[ ! -f "$src" ]]; then
      echo "error: source fixture missing: ${SRCS[$i]}" >&2
      exit 1
    fi
    mkdir -p -- "$(dirname "$dst")"
    cp -- "$src" "$dst"
    copied=$((copied + 1))
  done

  echo "  copied:   $copied files"
  # Per-stratum summary (observability).
  if [[ ${#STRATA[@]} -eq ${#SRCS[@]} ]]; then
    echo "  by stratum:"
    printf '%s\n' "${STRATA[@]}" | sort | uniq -c | sed 's/^/    /'
  fi
  echo "OK: pilot corpus assembled at $DEST"
}

# ── frozen mode (download + verify) ───────────────────────
build_frozen() {
  if [[ "$ALLOW_NETWORK" -ne 1 ]]; then
    echo "error: --frozen performs network downloads; pass --allow-network to proceed." >&2
    exit 2
  fi
  if [[ -z "$MANIFEST" ]]; then
    cat >&2 <<'EOF'
error: --frozen requires --manifest <frozen.yaml>.
The frozen >=1000-file corpus manifest does not exist yet — authoring it (with
pinned url + sha256 + redistributable license per entry) is Phase 9b. See
scripts/bench/corpus-manifest.schema.md for the field contract this mode reads.
EOF
    exit 2
  fi
  [[ -f "$MANIFEST" ]] || { echo "error: frozen manifest not found: $MANIFEST" >&2; exit 1; }

  command -v curl >/dev/null 2>&1 || { echo "error: curl not found" >&2; exit 1; }
  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    echo "error: need sha256sum (coreutils) or shasum (macOS) to verify checksums" >&2
    exit 1
  fi

  read_into URLS  < <(extract_field url    "$MANIFEST")
  read_into SHAS  < <(extract_field sha256 "$MANIFEST")
  read_into DESTS < <(extract_field dest   "$MANIFEST")

  if [[ ${#URLS[@]} -eq 0 ]]; then
    echo "error: no 'url:' entries in $MANIFEST" >&2; exit 1
  fi
  if [[ ${#URLS[@]} -ne ${#DESTS[@]} || ${#URLS[@]} -ne ${#SHAS[@]} ]]; then
    echo "error: manifest url/sha256/dest count mismatch" >&2; exit 1
  fi

  echo "== build-corpus (frozen) =="
  echo "  manifest: $MANIFEST"
  echo "  dest:     $DEST"
  echo "  entries:  ${#URLS[@]}"

  rm -rf -- "$DEST"
  mkdir -p -- "$DEST"

  local i url want dst got fetched=0
  for i in "${!URLS[@]}"; do
    url="${URLS[$i]}"
    want="${SHAS[$i]}"
    dst="$DEST/${DESTS[$i]}"
    mkdir -p -- "$(dirname "$dst")"
    echo "  fetch: $url"
    if ! curl -fsSL --retry 3 -o "$dst" "$url"; then
      echo "error: download failed: $url" >&2
      exit 1
    fi
    got="$(sha256_of "$dst")"
    if [[ "$got" != "$want" ]]; then
      echo "error: sha256 mismatch for $url" >&2
      echo "  want: $want" >&2
      echo "  got:  $got"  >&2
      exit 1
    fi
    fetched=$((fetched + 1))
  done

  echo "  fetched + verified: $fetched files"
  echo "OK: frozen corpus assembled at $DEST"
}

case "$MODE" in
  pilot)  build_pilot ;;
  frozen) build_frozen ;;
  *)      usage >&2; exit 2 ;;
esac
