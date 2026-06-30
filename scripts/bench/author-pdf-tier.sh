#!/usr/bin/env bash
#
# author-pdf-tier.sh — APPEND the PDF stratum to the frozen-corpus manifest.
#
# Source: the Federal Register public API (federalregister.gov), whose documents
# are U.S. Government works (PUBLIC DOMAIN), born-digital, and resolve to
# permanent govinfo.gov PDF URLs. Each candidate is fetched, size-capped, and
# gated by a pdftotext-yield assertion (>= MINCHARS non-space chars) so any
# scanned/image-only PDF is rejected — the same guarantee the indexer needs.
#
# This APPENDS to an existing manifest (authored by author-frozen-corpus.sh for
# the git tiers). Run it ONCE after the git tiers; re-running author-frozen-corpus.sh
# truncates the manifest, so re-append afterwards.
#
# Usage: author-pdf-tier.sh <workdir> <manifest> <notice> [target]
set -uo pipefail

WORK="${1:-/tmp/pdf-harvest}"
MANIFEST="${2:?manifest path required}"
NOTICE="${3:?notice path required}"
TARGET="${4:-105}"
MINCHARS=1500
MAXBYTES=$((5 * 1024 * 1024))

mkdir -p "$WORK/dl"
log() { echo "[pdf] $*" >&2; }

command -v python3 >/dev/null || { log "FATAL: python3 required"; exit 1; }
command -v pdftotext >/dev/null || { log "FATAL: pdftotext required"; exit 1; }

# 1. Collect Federal Register {document_number, pdf_url} over a FIXED window
#    (oldest-first → deterministic selection on re-run).
python3 - "$WORK/list.tsv" <<'PY'
import json, sys, urllib.request
out = sys.argv[1]
base = "https://www.federalregister.gov/api/v1/documents.json"
rows = []
for page in range(1, 5):  # up to 400 candidates
    url = (base + "?per_page=100&page=%d"
           "&conditions[publication_date][gte]=2024-01-02"
           "&conditions[publication_date][lte]=2024-03-31"
           "&fields[]=document_number&fields[]=pdf_url&order=oldest" % page)
    try:
        with urllib.request.urlopen(url, timeout=40) as r:
            d = json.load(r)
    except Exception as e:
        sys.stderr.write("api page %d: %s\n" % (page, e)); break
    res = d.get("results", [])
    if not res:
        break
    for x in res:
        dn, pu = x.get("document_number"), x.get("pdf_url")
        if dn and pu:
            rows.append((dn, pu))
with open(out, "w") as f:
    for dn, pu in rows:
        f.write("%s\t%s\n" % (dn, pu))
sys.stderr.write("collected %d FR candidates\n" % len(rows))
PY

[ -s "$WORK/list.tsv" ] || { log "FATAL: no FR candidates collected"; exit 1; }

# 2. Fetch + gate + emit until TARGET kept.
kept=0 tried=0 skipped_big=0 skipped_scan=0 skipped_fetch=0
while IFS=$'\t' read -r dn pu; do
  [ "$kept" -ge "$TARGET" ] && break
  tried=$((tried + 1))
  f="$WORK/dl/$dn.pdf"
  if ! curl -fsSL --max-time 90 --retry 2 -o "$f" "$pu"; then
    skipped_fetch=$((skipped_fetch + 1)); log "fetch fail $dn"; rm -f "$f"; continue
  fi
  bytes=$(stat -c%s "$f" 2>/dev/null || echo 0)
  if [ "$bytes" -gt "$MAXBYTES" ] || [ "$bytes" -lt 1024 ]; then
    skipped_big=$((skipped_big + 1)); rm -f "$f"; continue
  fi
  chars=$(pdftotext "$f" - 2>/dev/null | tr -d '[:space:]' | wc -c)
  if [ "$chars" -lt "$MINCHARS" ]; then
    skipped_scan=$((skipped_scan + 1)); log "reject $dn (yield $chars < $MINCHARS — scan/empty)"; rm -f "$f"; continue
  fi
  sum=$(sha256sum "$f" | awk '{print $1}')
  {
    printf '  - id: pdf-fr-%s\n' "$(printf '%s' "$dn" | tr -cd 'A-Za-z0-9_-')"
    printf '    url: %s\n' "$pu"
    printf '    sha256: %s\n' "$sum"
    printf '    license: public-domain\n'
    printf '    stratum: pdf\n'
    printf '    dest: pdf/federal-register/%s.pdf\n' "$dn"
    printf '    source: federalregister.gov FR-doc %s\n' "$dn"
  } >> "$MANIFEST"
  kept=$((kept + 1))
done < "$WORK/list.tsv"

log "PDF tier: kept=$kept tried=$tried (rejected: fetch=$skipped_fetch oversize=$skipped_big scan/empty=$skipped_scan)"
printf '  %-22s %-18s %s\n' "federalregister.gov" "public-domain" "pdf ($kept files) U.S. Government works" >> "$NOTICE"
echo "[pdf] DONE."
