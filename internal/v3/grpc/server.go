//go:build cgo

package grpc

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"

	"github.com/gtm-k/foldermcp/internal/v3/embed"
	"github.com/gtm-k/foldermcp/internal/v3/grpc/admin"
	"github.com/gtm-k/foldermcp/internal/v3/grpc/query"
	"github.com/gtm-k/foldermcp/internal/v3/grpc/tools"
	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// Server assembles all gRPC services and delegates RPC calls to handlers.
type Server struct {
	pb.UnimplementedIndexQueryServer
	pb.UnimplementedIndexToolsServer
	pb.UnimplementedIndexAdminServer
	pb.UnimplementedIndexCapabilitiesServer

	db       *sql.DB
	grpcSrv  *grpc.Server
	listener net.Listener

	// Narrow retrieval handlers
	vector   *query.VectorHandler
	fts      *query.FTSHandler
	filename *query.FilenameHandler
	metadata *query.MetadataHandler
	graph    *query.GraphHandler
	nodes    *query.NodesHandler
	chunks   *query.ChunksHandler
	blobs    *query.BlobsHandler
	batch    *query.BatchHandler

	// High-level tool handlers
	searchBroadly *tools.SearchBroadlyHandler
	inspect       *tools.InspectHandler
	browse        *tools.BrowseHandler

	// Admin + capabilities
	adminH       *admin.Handler
	capabilities *CapabilitiesHandler
}

// ServerOpts configures the gRPC server assembly.
type ServerOpts struct {
	DB       *sql.DB
	Embedder *embed.Embedder // may be nil — semantic search disabled
	// Watch surfaces the watch loop's process-level SLO counters
	// (events_reconciled_total + watch_overflow_recovery_age_seconds) in the
	// Status RPC. nil when the daemon is not running in --watch mode (FIX D —
	// without this the D14 SLO observability is dark in production).
	Watch admin.WatchMetricsProvider
}

// NewServer creates a fully wired Server with all handlers connected.
func NewServer(opts ServerOpts) *Server {
	vec := &query.VectorHandler{DB: opts.DB}
	// Central read-side gate: if the stored index's quantization mode is
	// incompatible with this binary's query encoder, refuse vector search at
	// the handler shared by the VectorSearch RPC, SearchBroadly, and Batch —
	// rather than compare fixed-scale query codes against an old per-vector
	// index and return silent garbage. The CLI's embedder-nil path only
	// covered query-text embedding; a client calling VectorSearch with its own
	// int8 codes bypassed it. Computed from the DB here so a future NewServer
	// caller cannot forget to pass it.
	if ok, _ := embed.SemanticIndexCompatible(opts.DB); !ok {
		vec.Disabled = true
	}
	fts := &query.FTSHandler{DB: opts.DB}
	fn := &query.FilenameHandler{DB: opts.DB}
	md := &query.MetadataHandler{DB: opts.DB}
	gr := &query.GraphHandler{DB: opts.DB}
	nd := &query.NodesHandler{DB: opts.DB}
	ch := &query.ChunksHandler{DB: opts.DB}
	bl := &query.BlobsHandler{DB: opts.DB}

	return &Server{
		db:       opts.DB,
		vector:   vec,
		fts:      fts,
		filename: fn,
		metadata: md,
		graph:    gr,
		nodes:    nd,
		chunks:   ch,
		blobs:    bl,
		batch: &query.BatchHandler{
			Vector: vec, FTS: fts, Filename: fn, Metadata: md,
			Graph: gr, Nodes: nd, Chunks: ch,
		},
		searchBroadly: &tools.SearchBroadlyHandler{
			DB: opts.DB, Vector: vec, FTS: fts, Filename: fn,
			Embedder: opts.Embedder,
		},
		inspect:      &tools.InspectHandler{DB: opts.DB, Nodes: nd},
		browse:       &tools.BrowseHandler{DB: opts.DB},
		adminH:       &admin.Handler{DB: opts.DB, StartTime: time.Now(), Watch: opts.Watch},
		capabilities: &CapabilitiesHandler{DB: opts.DB},
	}
}

// Serve binds the gRPC server to the listener and blocks until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	s.listener = l
	s.grpcSrv = grpc.NewServer()

	pb.RegisterIndexQueryServer(s.grpcSrv, s)
	pb.RegisterIndexToolsServer(s.grpcSrv, s)
	pb.RegisterIndexAdminServer(s.grpcSrv, s)
	pb.RegisterIndexCapabilitiesServer(s.grpcSrv, s)

	errCh := make(chan error, 1)
	go func() { errCh <- s.grpcSrv.Serve(l) }()

	select {
	case <-ctx.Done():
		s.grpcSrv.GracefulStop()
		return ctx.Err()
	case err := <-errCh:
		return fmt.Errorf("grpc serve: %w", err)
	}
}

// ─── IndexQuery delegates ────────────────────────────────

func (s *Server) VectorSearch(ctx context.Context, req *pb.VectorSearchRequest) (*pb.VectorSearchResponse, error) {
	return s.vector.Search(ctx, req)
}
func (s *Server) FTSSearch(ctx context.Context, req *pb.FTSSearchRequest) (*pb.FTSSearchResponse, error) {
	return s.fts.Search(ctx, req)
}
func (s *Server) FilenameSearch(ctx context.Context, req *pb.FilenameSearchRequest) (*pb.FilenameSearchResponse, error) {
	return s.filename.Search(ctx, req)
}
func (s *Server) MetadataSearch(ctx context.Context, req *pb.MetadataSearchRequest) (*pb.MetadataSearchResponse, error) {
	return s.metadata.Search(ctx, req)
}
func (s *Server) GraphExpand(ctx context.Context, req *pb.GraphExpandRequest) (*pb.GraphExpandResponse, error) {
	return s.graph.Expand(ctx, req)
}
func (s *Server) GetNodes(ctx context.Context, req *pb.GetNodesRequest) (*pb.GetNodesResponse, error) {
	return s.nodes.Get(ctx, req)
}
func (s *Server) GetChunks(ctx context.Context, req *pb.GetChunksRequest) (*pb.GetChunksResponse, error) {
	return s.chunks.Get(ctx, req)
}
func (s *Server) GetBlob(ctx context.Context, req *pb.GetBlobRequest) (*pb.GetBlobResponse, error) {
	return s.blobs.Get(ctx, req)
}
func (s *Server) Batch(ctx context.Context, req *pb.BatchRequest) (*pb.BatchResponse, error) {
	return s.batch.Batch(ctx, req)
}

// ─── IndexTools delegates ────────────────────────────────

func (s *Server) SearchBroadly(ctx context.Context, req *pb.SearchBroadlyRequest) (*pb.SearchBroadlyResponse, error) {
	return s.searchBroadly.SearchBroadly(ctx, req)
}
func (s *Server) InspectNode(ctx context.Context, req *pb.InspectNodeRequest) (*pb.InspectNodeResponse, error) {
	return s.inspect.Inspect(ctx, req)
}
func (s *Server) BrowseFolder(ctx context.Context, req *pb.BrowseFolderRequest) (*pb.BrowseFolderResponse, error) {
	return s.browse.Browse(ctx, req)
}

// ─── IndexAdmin delegates ────────────────────────────────

func (s *Server) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	return s.adminH.Status(ctx, req)
}
func (s *Server) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	return s.adminH.Health(ctx, req)
}

// ─── IndexCapabilities delegate ──────────────────────────

func (s *Server) GetCapabilities(ctx context.Context, req *pb.GetCapabilitiesRequest) (*pb.GetCapabilitiesResponse, error) {
	return s.capabilities.GetCapabilities(ctx, req)
}
