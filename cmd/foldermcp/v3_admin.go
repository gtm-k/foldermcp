//go:build cgo

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gtm-k/foldermcp/internal/v3/grpc/admin"
	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/store"
)

var v3IndexHealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Report integrity + quick_check status of the indexer DB",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runV3IndexHealth(cmd.Context())
	},
}

var v3IndexStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report indexer pipeline progress",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runV3IndexStatus(cmd.Context())
	},
}

func runV3IndexHealth(ctx context.Context) error {
	db, err := openIndexDBReadOnly()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	h := &admin.Handler{DB: db}
	resp, err := h.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		return err
	}
	fmt.Printf("quick_check=%v integrity_check=%v %s\n",
		resp.DbQuickCheckOk, resp.IntegrityCheckOk, resp.ErrorMessage)
	if !resp.DbQuickCheckOk || !resp.IntegrityCheckOk {
		return fmt.Errorf("health check failed: %s", resp.ErrorMessage)
	}
	return nil
}

func runV3IndexStatus(ctx context.Context) error {
	db, err := openIndexDBReadOnly()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	h := &admin.Handler{DB: db}
	resp, err := h.Status(ctx, &pb.StatusRequest{})
	if err != nil {
		return err
	}
	fmt.Printf("files_total=%d files_indexed=%d instance_uuid=%s uptime=%ds\n",
		resp.FilesTotal, resp.FilesIndexed, resp.InstanceUuid, resp.UptimeSeconds)
	for k, v := range resp.PassCounts {
		fmt.Printf("  %s=%d\n", k, v)
	}
	return nil
}

func openIndexDBReadOnly() (*sql.DB, error) {
	storeDir := os.Getenv("FOLDERMCP_STORE")
	if storeDir == "" {
		home, _ := os.UserHomeDir()
		storeDir = filepath.Join(home, ".foldermcp", "store", "default")
	}
	return store.Open(store.Options{
		Path:     filepath.Join(storeDir, "index.db"),
		Tier:     store.DetectTier(detectRAMMB()),
		ReadOnly: true,
	})
}

func init() {
	v3IndexCmd.AddCommand(v3IndexHealthCmd)
	v3IndexCmd.AddCommand(v3IndexStatusCmd)
}
