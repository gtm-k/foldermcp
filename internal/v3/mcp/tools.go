package mcp

import (
	"context"

	"github.com/gtm-k/foldermcp/internal/v3/router"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Shim) registerTools() {
	s.server.AddTool(searchTool(), s.handleSearch)
	s.server.AddTool(inspectTool(), s.handleInspect)
	s.server.AddTool(browseTool(), s.handleBrowse)
}

// ---------------------------------------------------------------------------
// Tool definitions — descriptions from spec §9.6
// ---------------------------------------------------------------------------

func searchTool() mcp.Tool {
	return mcp.NewTool("foldermcp_search",
		mcp.WithDescription(
			`Search the workspace for content relevant to a query. `+
				`USE THIS as the default tool for any discovery, lookup, or `+
				`'what do I have about X' question. It already combines semantic, `+
				`lexical, filename, and metadata signals — you do not need to call `+
				`other tools to cover those. Do NOT use foldermcp_neighbors or `+
				`foldermcp_inspect for discovery.`,
		),
		mcp.WithString("query", mcp.Required(), mcp.Description("The search query in natural language")),
		mcp.WithString("detail", mcp.Description("brief|standard|full, default standard")),
		mcp.WithString("mode", mcp.Description("auto|semantic|lexical|filename, default auto")),
		mcp.WithArray("content_classes", mcp.Description("Optional filter: code|document|image|media|data")),
		mcp.WithNumber("time_range_from", mcp.Description("Optional Unix epoch start for time filtering")),
		mcp.WithNumber("time_range_to", mcp.Description("Optional Unix epoch end for time filtering")),
	)
}

func inspectTool() mcp.Tool {
	return mcp.NewTool("foldermcp_inspect",
		mcp.WithDescription(
			`Inspect a specific node you already have an id for. `+
				`USE THIS only after foldermcp_search has returned a node_id and `+
				`you want more detail about that one item. Do NOT use this to search.`,
		),
		mcp.WithNumber("node_id", mcp.Required(), mcp.Description("The node ID returned by foldermcp_search")),
		mcp.WithString("detail", mcp.Description("brief|standard|full, default brief")),
	)
}

func browseTool() mcp.Tool {
	return mcp.NewTool("foldermcp_browse",
		mcp.WithDescription(
			`List the contents of a workspace folder by path. USE THIS `+
				`when you know the folder path and want to navigate by location, `+
				`not by content relevance.`,
		),
		mcp.WithString("path", mcp.Required(), mcp.Description("Folder path (e.g. /src/ or /docs/api/)")),
		mcp.WithNumber("max_items", mcp.Description("Default 50")),
		mcp.WithString("cursor", mcp.Description("Pagination cursor from a previous response")),
	)
}

// ---------------------------------------------------------------------------
// Handlers — translate MCP args → router calls → MCP results.
// User-facing errors use mcp.NewToolResultError so the LLM can read them.
// Bare error returns are reserved for infrastructure failures.
// ---------------------------------------------------------------------------

func (s *Shim) handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := router.SearchArgs{
		Query:          req.GetString("query", ""),
		Detail:         req.GetString("detail", "standard"),
		Mode:           req.GetString("mode", "auto"),
		ContentClasses: req.GetStringSlice("content_classes", nil),
		MaxResults:     20,
	}
	if from, to := req.GetInt("time_range_from", 0), req.GetInt("time_range_to", 0); from != 0 || to != 0 {
		args.TimeRange = &router.TimeRange{From: int64(from), To: int64(to)}
	}
	res, err := s.router.Search(ctx, args)
	if err != nil {
		return mcp.NewToolResultError("search failed: " + sanitizeError(err)), nil
	}
	return mcp.NewToolResultJSON(res)
}

func (s *Shim) handleInspect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := router.InspectArgs{
		NodeID: int64(req.GetInt("node_id", 0)),
		Detail: req.GetString("detail", "brief"),
	}
	res, err := s.router.Inspect(ctx, args)
	if err != nil {
		return mcp.NewToolResultError("inspect failed: " + sanitizeError(err)), nil
	}
	return mcp.NewToolResultJSON(res)
}

func (s *Shim) handleBrowse(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := router.BrowseArgs{
		Path:     req.GetString("path", ""),
		MaxItems: req.GetInt("max_items", 50),
		Cursor:   req.GetString("cursor", ""),
	}
	res, err := s.router.Browse(ctx, args)
	if err != nil {
		return mcp.NewToolResultError("browse failed: " + sanitizeError(err)), nil
	}
	return mcp.NewToolResultJSON(res)
}

// sanitizeError strips internal details (SQL, gRPC metadata) from errors
// before surfacing them to the LLM. Only the status code message is kept.
func sanitizeError(err error) string {
	// gRPC status errors already have sanitized messages from the server.
	// For safety, return a generic message rather than raw err.Error().
	return "service unavailable"
}
