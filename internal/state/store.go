package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Tool represents a discovered tool tracked in the state store.
type Tool struct {
	Name        string
	SourceFile  string
	Description string
	InputSchema string
	Risk        string
	State       string
	DepState    string
}

// Store wraps a SQLite database for persisting tool state and audit logs.
type Store struct {
	db *sql.DB
}

// Open creates the stateDir directory if needed, opens a SQLite database at
// stateDir/state.db, and runs schema migrations. Returns a ready-to-use Store.
func Open(stateDir string) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	dbPath := filepath.Join(stateDir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate runs the schema migrations to create the required tables.
func (s *Store) migrate() error {
	const schema = `
	CREATE TABLE IF NOT EXISTS tools (
		name         TEXT PRIMARY KEY,
		source_file  TEXT NOT NULL DEFAULT '',
		description  TEXT NOT NULL DEFAULT '',
		input_schema TEXT NOT NULL DEFAULT '',
		risk         TEXT NOT NULL DEFAULT '',
		state        TEXT NOT NULL DEFAULT 'pending',
		dep_state    TEXT NOT NULL DEFAULT 'resolving',
		updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS audit_log (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		tool_name     TEXT NOT NULL,
		action        TEXT NOT NULL,
		params        TEXT NOT NULL DEFAULT '',
		caller        TEXT NOT NULL DEFAULT '',
		result_status TEXT NOT NULL DEFAULT '',
		timestamp     DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("execute schema: %w", err)
	}
	return nil
}

// UpsertTool inserts a new tool or updates an existing one. On conflict (by
// name), all metadata fields are updated but the state and dep_state columns
// are preserved so that operator overrides are not lost on re-scan.
func (s *Store) UpsertTool(t Tool) error {
	const query = `
	INSERT INTO tools (name, source_file, description, input_schema, risk)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
		source_file  = excluded.source_file,
		description  = excluded.description,
		input_schema = excluded.input_schema,
		risk         = excluded.risk,
		updated_at   = CURRENT_TIMESTAMP
	`
	_, err := s.db.Exec(query, t.Name, t.SourceFile, t.Description, t.InputSchema, t.Risk)
	if err != nil {
		return fmt.Errorf("upsert tool %q: %w", t.Name, err)
	}
	return nil
}

// GetTool retrieves a single tool by name. Returns nil, nil if not found.
func (s *Store) GetTool(name string) (*Tool, error) {
	const query = `
	SELECT name, source_file, description, input_schema, risk, state, dep_state
	FROM tools
	WHERE name = ?
	`
	var t Tool
	err := s.db.QueryRow(query, name).Scan(
		&t.Name, &t.SourceFile, &t.Description,
		&t.InputSchema, &t.Risk, &t.State, &t.DepState,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tool %q: %w", name, err)
	}
	return &t, nil
}

// ListTools returns all tools ordered by name.
func (s *Store) ListTools() ([]Tool, error) {
	const query = `
	SELECT name, source_file, description, input_schema, risk, state, dep_state
	FROM tools
	ORDER BY name
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	defer rows.Close()

	var tools []Tool
	for rows.Next() {
		var t Tool
		if err := rows.Scan(
			&t.Name, &t.SourceFile, &t.Description,
			&t.InputSchema, &t.Risk, &t.State, &t.DepState,
		); err != nil {
			return nil, fmt.Errorf("scan tool row: %w", err)
		}
		tools = append(tools, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tools: %w", err)
	}
	return tools, nil
}

// UpdateToolState sets the state column for the named tool.
func (s *Store) UpdateToolState(name, newState string) error {
	const query = `UPDATE tools SET state = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?`
	_, err := s.db.Exec(query, newState, name)
	if err != nil {
		return fmt.Errorf("update tool state %q: %w", name, err)
	}
	return nil
}

// UpdateDepState sets the dep_state column for the named tool.
func (s *Store) UpdateDepState(name, depState string) error {
	const query = `UPDATE tools SET dep_state = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?`
	_, err := s.db.Exec(query, depState, name)
	if err != nil {
		return fmt.Errorf("update dep state %q: %w", name, err)
	}
	return nil
}

// LogAudit appends a row to the audit_log table.
func (s *Store) LogAudit(toolName, action, params, caller, resultStatus string) error {
	const query = `
	INSERT INTO audit_log (tool_name, action, params, caller, result_status)
	VALUES (?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, toolName, action, params, caller, resultStatus)
	if err != nil {
		return fmt.Errorf("log audit: %w", err)
	}
	return nil
}
