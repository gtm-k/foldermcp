#!/usr/bin/env bash
# gen-edge-cases.sh — Generate edge-case files for the QA workspace.
#
# Creates files that exercise walker resilience:
#   - Empty file (0 bytes)
#   - File with no extension
#   - Files with emoji and non-Latin characters in names
#   - Symlinks (circular and normal)
#   - Deep nesting (>20 levels)
#   - Large file (>100 MB, sparse)
#   - Non-Latin script content
#
# Usage: scripts/gen-edge-cases.sh <dest_dir>

set -euo pipefail

DEST="${1:?Usage: gen-edge-cases.sh <dest_dir>}"
mkdir -p "${DEST}"

echo "Generating edge cases in ${DEST} ..."

# 1. Empty file
touch "${DEST}/empty-file"

# 2. File with no extension
echo "This file has no extension." > "${DEST}/no-extension-file"

# 3. Emoji filename
echo "File with emoji in the name." > "${DEST}/🎉-party-file.txt"

# 4. Non-Latin filename (Japanese)
echo "日本語のファイル名テスト" > "${DEST}/テスト-test.txt"

# 5. Non-Latin filename (Arabic)
echo "محتوى الاختبار" > "${DEST}/اختبار.txt"

# 6. Deep nesting (25 levels)
DEEP="${DEST}/deep"
for i in $(seq 1 25); do
    DEEP="${DEEP}/level${i}"
done
mkdir -p "${DEEP}"
echo "File at depth 25" > "${DEEP}/deep-file.txt"

# 7. Symlink to a regular file
echo "Symlink target content." > "${DEST}/symlink-target.txt"
ln -sf "symlink-target.txt" "${DEST}/symlink-to-target.txt" 2>/dev/null || \
    echo "Symlinks not supported on this filesystem" > "${DEST}/symlink-to-target.txt"

# 8. Dangling symlink (target doesn't exist)
ln -sf "nonexistent-target.txt" "${DEST}/dangling-symlink.txt" 2>/dev/null || \
    echo "Dangling symlink placeholder" > "${DEST}/dangling-symlink.txt"

# 9. Large sparse file (100 MB, but uses almost no disk space)
dd if=/dev/zero of="${DEST}/large-sparse-100mb.bin" bs=1 count=0 seek=$((100*1024*1024)) 2>/dev/null || \
    truncate -s 100M "${DEST}/large-sparse-100mb.bin" 2>/dev/null || \
    echo "Could not create sparse file" > "${DEST}/large-sparse-100mb.bin"

# 10. File with mixed encoding (UTF-8 BOM + content)
printf '\xEF\xBB\xBF' > "${DEST}/utf8-bom-file.txt"
echo "This file starts with a UTF-8 BOM marker." >> "${DEST}/utf8-bom-file.txt"

# 11. Binary-looking file (random bytes)
dd if=/dev/urandom of="${DEST}/random-bytes.bin" bs=1024 count=10 2>/dev/null || \
    echo "Binary content placeholder" > "${DEST}/random-bytes.bin"

# 12. File with very long name
LONGNAME=$(python3 -c "print('a'*200)" 2>/dev/null || printf 'a%.0s' $(seq 1 200))
echo "Long filename test" > "${DEST}/${LONGNAME}.txt" 2>/dev/null || \
    echo "Long filename test" > "${DEST}/long-name-fallback.txt"

echo "Generated edge cases in ${DEST}"
