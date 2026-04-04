package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/lifecycle"
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

// Resource represents a discovered non-code file tracked in the state store.
type Resource struct {
	Name         string
	FilePath     string
	MimeType     string
	SizeBytes    int64
	ResourceType string
	State        string
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
		_ = db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
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
		dep_state    TEXT NOT NULL DEFAULT 'resolved',
		updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS resources (
		name          TEXT PRIMARY KEY,
		file_path     TEXT NOT NULL,
		mime_type     TEXT NOT NULL,
		size_bytes    INTEGER DEFAULT 0,
		resource_type TEXT DEFAULT 'document',
		state         TEXT DEFAULT 'pending',
		updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
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
	INSERT INTO tools (name, source_file, description, input_schema, risk, state, dep_state)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
		source_file  = excluded.source_file,
		description  = excluded.description,
		input_schema = excluded.input_schema,
		risk         = excluded.risk,
		updated_at   = CURRENT_TIMESTAMP
	`
	toolState := t.State
	if toolState == "" {
		toolState = "pending"
	}
	depState := t.DepState
	if depState == "" {
		depState = "resolved"
	}
	_, err := s.db.Exec(query, t.Name, t.SourceFile, t.Description, t.InputSchema, t.Risk, toolState, depState)
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
	defer func() { _ = rows.Close() }()

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

var validToolStates = map[string]bool{
	"pending": true, "enabled": true, "disabled": true, "requires_confirmation": true,
}
var validDepStates = map[string]bool{
	"resolved": true, "resolving": true, "failed": true,
}

// UpdateToolState sets the state column for the named tool.
// Returns an error if the state is invalid, the transition is not allowed,
// or the tool does not exist.
func (s *Store) UpdateToolState(name, newState string) error {
	if !validToolStates[newState] {
		return fmt.Errorf("invalid tool state %q", newState)
	}

	// Validate state transition when the tool already exists.
	current, err := s.GetTool(name)
	if err != nil {
		return fmt.Errorf("get tool for transition check: %w", err)
	}
	if current != nil {
		if err := lifecycle.ValidateTransition(current.State, newState); err != nil {
			return err
		}
	}

	res, err := s.db.Exec("UPDATE tools SET state = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?", newState, name)
	if err != nil {
		return fmt.Errorf("update tool state: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("tool %q not found", name)
	}
	return nil
}

// UpdateDepState sets the dep_state column for the named tool.
// Returns an error if the state is invalid or the tool does not exist.
func (s *Store) UpdateDepState(name, depState string) error {
	if !validDepStates[depState] {
		return fmt.Errorf("invalid dep state %q", depState)
	}
	res, err := s.db.Exec("UPDATE tools SET dep_state = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?", depState, name)
	if err != nil {
		return fmt.Errorf("update dep state: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("tool %q not found", name)
	}
	return nil
}

// UpsertResource inserts a new resource or updates an existing one. On conflict
// (by name), metadata fields are updated but the state column is preserved so
// that operator overrides are not lost on re-scan.
func (s *Store) UpsertResource(r Resource) error {
	const query = `
	INSERT INTO resources (name, file_path, mime_type, size_bytes, resource_type, state)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
		file_path     = excluded.file_path,
		mime_type     = excluded.mime_type,
		size_bytes    = excluded.size_bytes,
		resource_type = excluded.resource_type,
		updated_at    = CURRENT_TIMESTAMP
	`
	resState := r.State
	if resState == "" {
		resState = "pending"
	}
	resType := r.ResourceType
	if resType == "" {
		resType = "document"
	}
	_, err := s.db.Exec(query, r.Name, r.FilePath, r.MimeType, r.SizeBytes, resType, resState)
	if err != nil {
		return fmt.Errorf("upsert resource %q: %w", r.Name, err)
	}
	return nil
}

// GetResource retrieves a single resource by name. Returns nil, nil if not found.
func (s *Store) GetResource(name string) (*Resource, error) {
	const query = `
	SELECT name, file_path, mime_type, size_bytes, resource_type, state
	FROM resources
	WHERE name = ?
	`
	var r Resource
	err := s.db.QueryRow(query, name).Scan(
		&r.Name, &r.FilePath, &r.MimeType, &r.SizeBytes, &r.ResourceType, &r.State,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get resource %q: %w", name, err)
	}
	return &r, nil
}

// ListResources returns all resources ordered by name.
func (s *Store) ListResources() ([]Resource, error) {
	const query = `
	SELECT name, file_path, mime_type, size_bytes, resource_type, state
	FROM resources
	ORDER BY name
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var resources []Resource
	for rows.Next() {
		var r Resource
		if err := rows.Scan(
			&r.Name, &r.FilePath, &r.MimeType, &r.SizeBytes, &r.ResourceType, &r.State,
		); err != nil {
			return nil, fmt.Errorf("scan resource row: %w", err)
		}
		resources = append(resources, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resources: %w", err)
	}
	return resources, nil
}

var validResourceStates = map[string]bool{
	"pending": true, "enabled": true, "disabled": true,
}

// UpdateResourceState sets the state column for the named resource.
// Returns an error if the state is invalid or the resource does not exist.
func (s *Store) UpdateResourceState(name, newState string) error {
	if !validResourceStates[newState] {
		return fmt.Errorf("invalid resource state %q", newState)
	}

	res, err := s.db.Exec("UPDATE resources SET state = ?, updated_at = CURRENT_TIMESTAMP WHERE name = ?", newState, name)
	if err != nil {
		return fmt.Errorf("update resource state: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("resource %q not found", name)
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
