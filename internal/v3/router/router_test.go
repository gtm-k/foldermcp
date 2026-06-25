package router

import (
	"context"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc"
)

// fakeIndexToolsClient implements pb.IndexToolsClient for testing.
type fakeIndexToolsClient struct {
	pb.IndexToolsClient
	searchResp  *pb.SearchBroadlyResponse
	inspectResp *pb.InspectNodeResponse
	browseResp  *pb.BrowseFolderResponse
}

func (f *fakeIndexToolsClient) SearchBroadly(_ context.Context, _ *pb.SearchBroadlyRequest, _ ...grpc.CallOption) (*pb.SearchBroadlyResponse, error) {
	return f.searchResp, nil
}
func (f *fakeIndexToolsClient) InspectNode(_ context.Context, _ *pb.InspectNodeRequest, _ ...grpc.CallOption) (*pb.InspectNodeResponse, error) {
	return f.inspectResp, nil
}
func (f *fakeIndexToolsClient) BrowseFolder(_ context.Context, _ *pb.BrowseFolderRequest, _ ...grpc.CallOption) (*pb.BrowseFolderResponse, error) {
	return f.browseResp, nil
}

func newTestRouter(fake *fakeIndexToolsClient) *Router {
	return &Router{indexTools: fake}
}

func TestSearchTranslation(t *testing.T) {
	fake := &fakeIndexToolsClient{
		searchResp: &pb.SearchBroadlyResponse{
			OverallStatus: "ok",
			Completeness:  "full",
			Sources: []*pb.SourceStatus{
				{SourceName: "fts", Status: "OK", LatencyMs: 12},
			},
			Results: []*pb.SearchHit{
				{NodeId: 1, Path: "/a.go", Title: "main", Snippet: "func main()", Score: 0.8, MatchedSources: []string{"fts"}},
				{NodeId: 2, Path: "/b.md", Title: "readme", Snippet: "# Hello", Score: 0.5, MatchedSources: []string{"filename"}},
			},
		},
	}
	r := newTestRouter(fake)
	result, err := r.Search(context.Background(), SearchArgs{
		Query: "test", Mode: "auto", Detail: "standard", MaxResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Errorf("status = %s, want ok", result.Status)
	}
	if result.Completeness != "full" {
		t.Errorf("completeness = %s, want full", result.Completeness)
	}
	if len(result.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(result.Results))
	}
	if result.Results[0].NodeID != 1 {
		t.Errorf("first result nodeID = %d, want 1", result.Results[0].NodeID)
	}
	if result.Results[0].Title != "main" {
		t.Errorf("title = %s, want main", result.Results[0].Title)
	}
	if len(result.Sources) != 1 || result.Sources[0].SourceName != "fts" {
		t.Errorf("sources mismatch: %+v", result.Sources)
	}
}

func TestSearchWithTimeRange(t *testing.T) {
	fake := &fakeIndexToolsClient{
		searchResp: &pb.SearchBroadlyResponse{
			OverallStatus: "ok",
			Completeness:  "full",
		},
	}
	r := newTestRouter(fake)
	result, err := r.Search(context.Background(), SearchArgs{
		Query:     "test",
		TimeRange: &TimeRange{From: 1000, To: 2000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Errorf("status = %s, want ok", result.Status)
	}
}

func TestInspectTranslation(t *testing.T) {
	fake := &fakeIndexToolsClient{
		inspectResp: &pb.InspectNodeResponse{
			Node: &pb.HydratedNode{
				NodeId: 42, Path: "/src/main.go", Name: "main",
				NodeType: "function", ContentClass: "code",
				Provenance: "EXTRACTED", Language: "go",
				Chunks: []*pb.HydratedChunk{
					{ChunkId: 1, Text: "func main() {}", TokenCount: 5},
				},
			},
			Status: &pb.SourceStatus{SourceName: "inspect", Status: "OK"},
		},
	}
	r := newTestRouter(fake)
	result, err := r.Inspect(context.Background(), InspectArgs{NodeID: 42, Detail: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "OK" {
		t.Errorf("status = %s, want OK", result.Status)
	}
	if result.Node == nil {
		t.Fatal("node is nil")
	}
	if result.Node.NodeID != 42 {
		t.Errorf("nodeID = %d, want 42", result.Node.NodeID)
	}
	if result.Node.Name != "main" {
		t.Errorf("name = %s, want main", result.Node.Name)
	}
	if len(result.Node.Chunks) != 1 {
		t.Errorf("chunks = %d, want 1", len(result.Node.Chunks))
	}
}

func TestInspectNotFound(t *testing.T) {
	fake := &fakeIndexToolsClient{
		inspectResp: &pb.InspectNodeResponse{
			Status: &pb.SourceStatus{SourceName: "inspect", Status: "EMPTY_BUT_EXECUTED"},
		},
	}
	r := newTestRouter(fake)
	result, err := r.Inspect(context.Background(), InspectArgs{NodeID: 999})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("status = %s, want EMPTY_BUT_EXECUTED", result.Status)
	}
	if result.Node != nil {
		t.Error("node should be nil for not-found")
	}
}

func TestBrowseTranslation(t *testing.T) {
	fake := &fakeIndexToolsClient{
		browseResp: &pb.BrowseFolderResponse{
			Entries: []*pb.FolderEntry{
				{Name: "a.go", Path: "/src/a.go", Size: 100, Mtime: 1000, ContentClass: "code", FileId: 1},
				{Name: "b.md", Path: "/src/b.md", Size: 50, Mtime: 2000, ContentClass: "document", FileId: 2},
			},
			NextCursor: "abc123",
			Status:     &pb.SourceStatus{SourceName: "browse", Status: "OK"},
		},
	}
	r := newTestRouter(fake)
	result, err := r.Browse(context.Background(), BrowseArgs{Path: "/src/", MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "OK" {
		t.Errorf("status = %s, want OK", result.Status)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(result.Entries))
	}
	if result.Entries[0].Name != "a.go" {
		t.Errorf("first entry = %s, want a.go", result.Entries[0].Name)
	}
	if result.NextCursor != "abc123" {
		t.Errorf("cursor = %s, want abc123", result.NextCursor)
	}
}

func TestBrowseEmptyResult(t *testing.T) {
	fake := &fakeIndexToolsClient{
		browseResp: &pb.BrowseFolderResponse{
			Status: &pb.SourceStatus{SourceName: "browse", Status: "OK"},
		},
	}
	r := newTestRouter(fake)
	result, err := r.Browse(context.Background(), BrowseArgs{Path: "/empty/"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "OK" {
		t.Errorf("status = %s, want OK", result.Status)
	}
	if len(result.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(result.Entries))
	}
}
