// Package router is the thin MCP→gRPC translator per spec §10.1.
// Its only job: parse MCP tool args → marshal to one IndexTools gRPC call →
// unmarshal gRPC response to MCP-ready JSON result.
// No fusion, no shaping, no plan construction — all of that lives in indexd.
package router

import (
	"context"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc"
)

// Router translates MCP tool calls into IndexTools gRPC RPCs.
type Router struct {
	conn         *grpc.ClientConn
	indexTools   pb.IndexToolsClient
	capabilities pb.IndexCapabilitiesClient
}

// New creates a Router bound to the given gRPC connection.
func New(conn *grpc.ClientConn) *Router {
	return &Router{
		conn:         conn,
		indexTools:   pb.NewIndexToolsClient(conn),
		capabilities: pb.NewIndexCapabilitiesClient(conn),
	}
}

// Search marshals the MCP foldermcp_search args into a SearchBroadly RPC.
func (r *Router) Search(ctx context.Context, args SearchArgs) (*SearchResult, error) {
	req := &pb.SearchBroadlyRequest{
		Query:          args.Query,
		Detail:         args.Detail,
		Mode:           args.Mode,
		ContentClasses: args.ContentClasses,
		MaxResults:     int32(args.MaxResults),
	}
	if args.TimeRange != nil {
		req.TimeRangeFrom = args.TimeRange.From
		req.TimeRangeTo = args.TimeRange.To
	}
	resp, err := r.indexTools.SearchBroadly(ctx, req)
	if err != nil {
		return nil, err
	}
	return searchResultFromProto(resp), nil
}

// Inspect marshals the MCP foldermcp_inspect args into an InspectNode RPC.
func (r *Router) Inspect(ctx context.Context, args InspectArgs) (*InspectResult, error) {
	resp, err := r.indexTools.InspectNode(ctx, &pb.InspectNodeRequest{
		NodeId: args.NodeID, Detail: args.Detail,
	})
	if err != nil {
		return nil, err
	}
	return inspectResultFromProto(resp), nil
}

// Browse marshals the MCP foldermcp_browse args into a BrowseFolder RPC.
func (r *Router) Browse(ctx context.Context, args BrowseArgs) (*BrowseResult, error) {
	resp, err := r.indexTools.BrowseFolder(ctx, &pb.BrowseFolderRequest{
		Path: args.Path, MaxItems: int32(args.MaxItems), Cursor: args.Cursor,
	})
	if err != nil {
		return nil, err
	}
	return browseResultFromProto(resp), nil
}
