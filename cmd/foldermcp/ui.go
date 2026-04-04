package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/foldermcp/foldermcp/internal/studio"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the local developer studio",
	Long:  `Starts the FolderMCP Developer Studio web dashboard for inspecting tool state, audit logs, and server status.`,
	RunE:  runUI,
}

func init() {
	uiCmd.Flags().Int("port", 3001, "port to serve the studio on")
	rootCmd.AddCommand(uiCmd)
}

func runUI(cmd *cobra.Command, args []string) error {
	port, _ := cmd.Flags().GetInt("port")

	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	stateDir := filepath.Join(dir, ".foldermcp")

	// Verify state directory exists.
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return fmt.Errorf("state directory not found at %s; run 'foldermcp init' first", stateDir)
	}

	srv := studio.NewStudioServer(stateDir, port)

	// Handle graceful shutdown on interrupt.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "\nFolderMCP Developer Studio\n")
	fmt.Fprintf(os.Stderr, "  Dashboard:  http://localhost:%d\n", port)
	fmt.Fprintf(os.Stderr, "  API:        http://localhost:%d/api/tools\n", port)
	fmt.Fprintf(os.Stderr, "  Audit Log:  http://localhost:%d/api/audit\n", port)
	fmt.Fprintf(os.Stderr, "  Status:     http://localhost:%d/api/status\n", port)
	fmt.Fprintf(os.Stderr, "\nPress Ctrl+C to stop.\n")

	// Start server in a goroutine so we can listen for the interrupt signal.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "\nShutting down studio...")
		_ = srv.Close()
		return nil
	case err := <-errCh:
		return err
	}
}
