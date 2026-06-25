package router

// ── MCP-facing argument types ──────────────────────────────

// SearchArgs maps from MCP foldermcp_search arguments.
type SearchArgs struct {
	Query          string
	Detail         string
	Mode           string
	ContentClasses []string
	TimeRange      *TimeRange
	MaxResults     int
}

// TimeRange represents a time filter with Unix epoch bounds.
type TimeRange struct{ From, To int64 }

// InspectArgs maps from MCP foldermcp_inspect arguments.
type InspectArgs struct {
	NodeID int64
	Detail string
}

// BrowseArgs maps from MCP foldermcp_browse arguments.
type BrowseArgs struct {
	Path     string
	MaxItems int
	Cursor   string
}

// ── MCP-facing result types (JSON-serializable) ────────────

// SearchResult is the JSON structure returned by foldermcp_search.
type SearchResult struct {
	Status        string          `json:"status"`
	Completeness  string          `json:"completeness"`
	FailedSources []string        `json:"failed_sources,omitempty"`
	Retryable     bool            `json:"retryable"`
	Sources       []SourceInfo    `json:"sources"`
	Results       []SearchHitJSON `json:"results"`
	Warnings      []string        `json:"warnings,omitempty"`
}

// SourceInfo describes the status of a single retrieval source.
type SourceInfo struct {
	SourceName   string `json:"source_name"`
	Status       string `json:"status"`
	LatencyMs    int32  `json:"latency_ms"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// SearchHitJSON is a single search result in JSON form.
type SearchHitJSON struct {
	NodeID         int64    `json:"node_id"`
	Path           string   `json:"path"`
	Title          string   `json:"title"`
	Snippet        string   `json:"snippet"`
	Score          float32  `json:"score"`
	MatchedSources []string `json:"matched_sources"`
}

// InspectResult is the JSON structure returned by foldermcp_inspect.
type InspectResult struct {
	Status string    `json:"status"`
	Node   *NodeJSON `json:"node,omitempty"`
}

// BrowseResult is the JSON structure returned by foldermcp_browse.
type BrowseResult struct {
	Status     string            `json:"status"`
	Entries    []FolderEntryJSON `json:"entries"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// NodeJSON is a hydrated node in JSON form.
type NodeJSON struct {
	NodeID       int64       `json:"node_id"`
	Path         string      `json:"path"`
	Name         string      `json:"name"`
	NodeType     string      `json:"node_type"`
	Provenance   string      `json:"provenance"`
	ContentClass string      `json:"content_class"`
	Language     string      `json:"language,omitempty"`
	Chunks       []ChunkJSON `json:"chunks,omitempty"`
}

// ChunkJSON is a single chunk in JSON form.
type ChunkJSON struct {
	ChunkID    int64  `json:"chunk_id"`
	Text       string `json:"text"`
	TokenCount int32  `json:"token_count"`
}

// FolderEntryJSON is a single directory entry in JSON form.
type FolderEntryJSON struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	IsDir        bool   `json:"is_dir"`
	Size         int64  `json:"size"`
	Mtime        int64  `json:"mtime"`
	ContentClass string `json:"content_class"`
	FileID       int64  `json:"file_id"`
}
