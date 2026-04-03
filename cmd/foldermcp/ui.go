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

	fmt.Fprintf(os.Stderr, "Studio available at http://localhost:%d\n", port)

	// Start server in a goroutine so we can listen for the interrupt signal.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "\nShutting down studio...")
		srv.Close()
		return nil
	case err := <-errCh:
		return err
	}
}
