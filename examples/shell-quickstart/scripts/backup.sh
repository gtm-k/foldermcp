#!/usr/bin/env bash
# tool: backup_database
# description: Create a compressed backup of the PostgreSQL database.
# risk: side_effects
# param: database: string: Name of the database to back up
# param: output_dir: string: Directory to write the backup file
set -euo pipefail

DATABASE="${1:?Usage: backup.sh <database> <output_dir>}"
OUTPUT_DIR="${2:?Usage: backup.sh <database> <output_dir>}"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$OUTPUT_DIR/${DATABASE}_${TIMESTAMP}.sql.gz"

echo "Backing up database '$DATABASE'..."

# Placeholder — replace with real pg_dump invocation.
# pg_dump "$DATABASE" | gzip > "$BACKUP_FILE"
mkdir -p "$OUTPUT_DIR"
echo "-- placeholder backup of $DATABASE at $TIMESTAMP" | gzip > "$BACKUP_FILE"

echo "Backup written to $BACKUP_FILE"
