//go:build cgo

package store

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrationFiles returns the embedded migration files in ascending numeric order.
func MigrationFiles() (fs.FS, error) {
	return fs.Sub(migrationsFS, "migrations")
}
