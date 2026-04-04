// Package studio provides an embedded web server for the FolderMCP Developer
// Studio dashboard. It reads tool state and audit logs from the SQLite store
// and presents them in a browser-based UI.
package studio

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed static/index.html
var staticFiles embed.FS

// toolJSON is the JSON representation of a tool for the API.
type toolJSON struct {
	Name        string `json:"name"`
	SourceFile  string `json:"source_file"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
	State       string `json:"state"`
	DepState    string `json:"dep_state"`
}

// auditJSON is the JSON representation of an audit log entry.
type auditJSON struct {
	ID           int    `json:"id"`
	ToolName     string `json:"tool_name"`
	Action       string `json:"action"`
	Params       string `json:"params"`
	Caller       string `json:"caller"`
	ResultStatus string `json:"result_status"`
	Timestamp    string `json:"timestamp"`
}

// statusJSON is the JSON representation of the server status summary.
type statusJSON struct {
	TotalTools   int            `json:"total_tools"`
	StateCounts  map[string]int `json:"state_counts"`
	DepCounts    map[string]int `json:"dep_counts"`
	UptimeString string         `json:"uptime"`
}

// StudioServer serves the Developer Studio web UI and its backing API.
type StudioServer struct {
	stateDir  string
	port      int
	startTime time.Time
	db        *sql.DB
}

// NewStudioServer creates a new StudioServer that reads state from stateDir
// and listens on the given port.
func NewStudioServer(stateDir string, port int) *StudioServer {
	return &StudioServer{
		stateDir: stateDir,
		port:     port,
	}
}

// Handler returns the http.Handler for the studio server. This is useful
// for testing without starting a full TCP listener.
func (s *StudioServer) Handler() (http.Handler, error) {
	if err := s.openDB(); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	// Serve static files from the embedded filesystem.
	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("sub static fs: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticSub)))

	// API routes.
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/audit", s.handleAudit)
	mux.HandleFunc("/api/status", s.handleStatus)

	return mux, nil
}

// Start opens the state database and starts the HTTP server. It blocks until
// the server is shut down.
func (s *StudioServer) Start() error {
	s.startTime = time.Now()

	handler, err := s.Handler()
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	return http.ListenAndServe(addr, handler)
}

// openDB opens the SQLite database from the state directory.
func (s *StudioServer) openDB() error {
	dbPath := s.stateDir + "/state.db"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	s.db = db
	s.startTime = time.Now()
	return nil
}

// Close closes the underlying database connection.
func (s *StudioServer) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *StudioServer) handleTools(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT name, source_file, description, risk, state, dep_state
		FROM tools ORDER BY name
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = rows.Close() }()

	tools := []toolJSON{}
	for rows.Next() {
		var t toolJSON
		if err := rows.Scan(&t.Name, &t.SourceFile, &t.Description, &t.Risk, &t.State, &t.DepState); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tools = append(tools, t)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tools)
}

func (s *StudioServer) handleAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT id, tool_name, action, params, caller, result_status, timestamp
		FROM audit_log ORDER BY id DESC LIMIT 100
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = rows.Close() }()

	entries := []auditJSON{}
	for rows.Next() {
		var e auditJSON
		if err := rows.Scan(&e.ID, &e.ToolName, &e.Action, &e.Params, &e.Caller, &e.ResultStatus, &e.Timestamp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

func (s *StudioServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT name, state, dep_state FROM tools
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = rows.Close() }()

	status := statusJSON{
		StateCounts: map[string]int{},
		DepCounts:   map[string]int{},
	}
	for rows.Next() {
		var name, st, dep string
		if err := rows.Scan(&name, &st, &dep); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		status.TotalTools++
		status.StateCounts[st]++
		status.DepCounts[dep]++
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	uptime := time.Since(s.startTime).Truncate(time.Second)
	status.UptimeString = uptime.String()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}
