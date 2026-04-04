package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/foldermcp/foldermcp/internal/config"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/foldermcp/foldermcp/internal/workspace"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review and approve/disable discovered tools",
	Long: `In dev mode, offers a bulk approve prompt. In production mode, each tool
is reviewed individually. Use --approve and --disable flags for batch mode.`,
	RunE: runReview,
}

var reviewApproveAll bool

func init() {
	reviewCmd.Flags().StringSlice("approve", nil, "tool names to approve (batch mode)")
	reviewCmd.Flags().StringSlice("disable", nil, "tool names to disable (batch mode)")
	reviewCmd.Flags().StringSlice("confirm", nil, "tool names to set as requires_confirmation (batch mode)")
	reviewCmd.Flags().BoolVar(&reviewApproveAll, "approve-all", false, "Approve all pending tools")
	reviewCmd.Flags().String("mode", "dev", "review mode: dev, team, production")
	rootCmd.AddCommand(reviewCmd)
}

func runReview(cmd *cobra.Command, args []string) error {
	approveList, _ := cmd.Flags().GetStringSlice("approve")
	disableList, _ := cmd.Flags().GetStringSlice("disable")
	confirmList, _ := cmd.Flags().GetStringSlice("confirm")
	mode, _ := cmd.Flags().GetString("mode")

	// Check for conflicts between --approve, --disable, and --confirm.
	if len(approveList) > 0 || len(disableList) > 0 || len(confirmList) > 0 {
		seen := make(map[string]string)
		for _, name := range approveList {
			seen[name] = "--approve"
		}
		for _, name := range disableList {
			if prev, ok := seen[name]; ok {
				return fmt.Errorf("tool %q appears in both %s and --disable", name, prev)
			}
			seen[name] = "--disable"
		}
		for _, name := range confirmList {
			if prev, ok := seen[name]; ok {
				return fmt.Errorf("tool %q appears in both %s and --confirm", name, prev)
			}
		}
	}

	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	ws, err := workspace.Open(dir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	cfg, err := config.Load(ws.ProjectDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := state.Open(ws.LocalDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer func() { _ = store.Close() }()

	tools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	resources, err := store.ListResources()
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}

	if len(tools) == 0 && len(resources) == 0 {
		fmt.Fprintln(os.Stderr, "No tools or resources found. Run 'foldermcp init' first.")
		return nil
	}

	// Build a set of resource names for batch mode lookups.
	resourceNames := make(map[string]bool)
	for _, r := range resources {
		resourceNames[r.Name] = true
	}

	// --approve-all: approve all pending tools and resources non-interactively.
	if reviewApproveAll {
		var pendingTools []state.Tool
		for _, t := range tools {
			if t.State == "pending" {
				pendingTools = append(pendingTools, t)
			}
		}
		var pendingResources []state.Resource
		for _, r := range resources {
			if r.State == "pending" {
				pendingResources = append(pendingResources, r)
			}
		}

		if len(pendingTools) == 0 && len(pendingResources) == 0 {
			fmt.Fprintln(os.Stderr, "No pending tools or resources to approve.")
			return nil
		}

		// Warn about destructive tools.
		destructive := 0
		for _, t := range pendingTools {
			if t.Risk == "destructive" {
				destructive++
			}
		}
		if destructive > 0 {
			fmt.Fprintf(os.Stderr, "WARNING: %d tool(s) labeled 'destructive' will be approved\n", destructive)
		}

		for _, t := range pendingTools {
			if err := store.UpdateToolState(t.Name, "enabled"); err != nil {
				return fmt.Errorf("approve %q: %w", t.Name, err)
			}
			if cfg.Tools == nil {
				cfg.Tools = make(map[string]config.ToolConfig)
			}
			tc := cfg.Tools[t.Name]
			tc.State = "enabled"
			cfg.Tools[t.Name] = tc
		}
		for _, r := range pendingResources {
			if err := store.UpdateResourceState(r.Name, "enabled"); err != nil {
				return fmt.Errorf("approve resource %q: %w", r.Name, err)
			}
		}

		fmt.Fprintf(os.Stderr, "Approved %d tools and %d resources\n", len(pendingTools), len(pendingResources))
		return config.Save(ws.ProjectDir, cfg)
	}

	// Batch mode: apply --approve, --disable, and --confirm flags directly.
	if len(approveList) > 0 || len(disableList) > 0 || len(confirmList) > 0 {
		return reviewBatch(store, cfg, ws.ProjectDir, approveList, disableList, confirmList, resourceNames)
	}

	// Interactive mode.
	switch mode {
	case "dev", "team":
		return reviewDevMode(store, cfg, ws.ProjectDir, tools, resources)
	case "production":
		return reviewProductionMode(store, cfg, ws.ProjectDir, tools, resources)
	default:
		return fmt.Errorf("unknown mode %q (use dev, team, or production)", mode)
	}
}

func reviewBatch(store *state.Store, cfg *config.Config, dir string, approve, disable, confirm []string, resourceNames map[string]bool) error {
	for _, name := range approve {
		if resourceNames[name] {
			if err := store.UpdateResourceState(name, "enabled"); err != nil {
				return fmt.Errorf("approve resource %q: %w", name, err)
			}
			fmt.Fprintf(os.Stderr, "Approved resource: %s\n", name)
		} else {
			if err := store.UpdateToolState(name, "enabled"); err != nil {
				return fmt.Errorf("approve %q: %w", name, err)
			}
			if cfg.Tools == nil {
				cfg.Tools = make(map[string]config.ToolConfig)
			}
			tc := cfg.Tools[name]
			tc.State = "enabled"
			cfg.Tools[name] = tc
			fmt.Fprintf(os.Stderr, "Approved: %s\n", name)
		}
	}

	for _, name := range disable {
		if resourceNames[name] {
			if err := store.UpdateResourceState(name, "disabled"); err != nil {
				return fmt.Errorf("disable resource %q: %w", name, err)
			}
			fmt.Fprintf(os.Stderr, "Disabled resource: %s\n", name)
		} else {
			if err := store.UpdateToolState(name, "disabled"); err != nil {
				return fmt.Errorf("disable %q: %w", name, err)
			}
			if cfg.Tools == nil {
				cfg.Tools = make(map[string]config.ToolConfig)
			}
			tc := cfg.Tools[name]
			tc.State = "disabled"
			cfg.Tools[name] = tc
			fmt.Fprintf(os.Stderr, "Disabled: %s\n", name)
		}
	}

	for _, name := range confirm {
		// Resources don't support requires_confirmation — only tools.
		if resourceNames[name] {
			fmt.Fprintf(os.Stderr, "warning: resources do not support requires_confirmation, skipping %s\n", name)
			continue
		}
		if err := store.UpdateToolState(name, "requires_confirmation"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", name, err)
		} else {
			if cfg.Tools == nil {
				cfg.Tools = make(map[string]config.ToolConfig)
			}
			tc := cfg.Tools[name]
			tc.State = "requires_confirmation"
			cfg.Tools[name] = tc
			fmt.Fprintf(os.Stderr, "Set requires_confirmation: %s\n", name)
		}
	}

	return config.Save(dir, cfg)
}

func reviewDevMode(store *state.Store, cfg *config.Config, dir string, tools []state.Tool, resources []state.Resource) error {
	// Count pending tools.
	var pendingTools []state.Tool
	for _, t := range tools {
		if t.State == "pending" {
			pendingTools = append(pendingTools, t)
		}
	}
	var pendingResources []state.Resource
	for _, r := range resources {
		if r.State == "pending" {
			pendingResources = append(pendingResources, r)
		}
	}

	if len(pendingTools) == 0 && len(pendingResources) == 0 {
		fmt.Fprintln(os.Stderr, "No pending tools or resources to review.")
		return nil
	}

	// Count read-only vs side-effect tools.
	readOnly := 0
	sideEffects := 0
	destructive := 0
	for _, t := range pendingTools {
		if t.Risk == "destructive" {
			destructive++
		}
		if t.Risk == "" || t.Risk == "none" || t.Risk == "low" {
			readOnly++
		} else {
			sideEffects++
		}
	}
	if destructive > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d tool(s) are labeled 'destructive'\n", destructive)
	}
	fmt.Fprintf(os.Stderr, "%d tools found (%d read-only, %d with side-effects) and %d resources. Approve all for local dev? [y/N] ",
		len(pendingTools), readOnly, sideEffects, len(pendingResources))
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer == "y" || answer == "yes" {
		for _, t := range pendingTools {
			if err := store.UpdateToolState(t.Name, "enabled"); err != nil {
				return fmt.Errorf("approve %q: %w", t.Name, err)
			}
			if cfg.Tools == nil {
				cfg.Tools = make(map[string]config.ToolConfig)
			}
			tc := cfg.Tools[t.Name]
			tc.State = "enabled"
			cfg.Tools[t.Name] = tc
		}
		for _, r := range pendingResources {
			if err := store.UpdateResourceState(r.Name, "enabled"); err != nil {
				return fmt.Errorf("approve resource %q: %w", r.Name, err)
			}
		}
		fmt.Fprintf(os.Stderr, "Approved %d tools and %d resources.\n", len(pendingTools), len(pendingResources))
		return config.Save(dir, cfg)
	}

	fmt.Fprintln(os.Stderr, "No changes made.")
	return nil
}

func reviewProductionMode(store *state.Store, cfg *config.Config, dir string, tools []state.Tool, resources []state.Resource) error {
	reader := bufio.NewReader(os.Stdin)
	changed := false

	for _, t := range tools {
		if t.State != "pending" {
			continue
		}

		fmt.Fprintf(os.Stderr, "\n--- %s (tool) ---\n", t.Name)
		fmt.Fprintf(os.Stderr, "  Source: %s\n", t.SourceFile)
		fmt.Fprintf(os.Stderr, "  Risk:   %s\n", t.Risk)
		fmt.Fprintf(os.Stderr, "  Desc:   %s\n", t.Description)
		fmt.Fprintf(os.Stderr, "  [e]nable / [d]isable / [c]onfirm-required / [s]kip: ")

		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		var newState string
		switch answer {
		case "e", "enable":
			newState = "enabled"
		case "d", "disable":
			newState = "disabled"
		case "c", "confirm":
			newState = "requires_confirmation"
		case "s", "skip", "":
			continue
		default:
			fmt.Fprintf(os.Stderr, "  Unknown choice %q, skipping.\n", answer)
			continue
		}

		if err := store.UpdateToolState(t.Name, newState); err != nil {
			return fmt.Errorf("update %q: %w", t.Name, err)
		}
		if cfg.Tools == nil {
			cfg.Tools = make(map[string]config.ToolConfig)
		}
		tc := cfg.Tools[t.Name]
		tc.State = newState
		cfg.Tools[t.Name] = tc
		changed = true
		fmt.Fprintf(os.Stderr, "  -> %s\n", newState)
	}

	for _, r := range resources {
		if r.State != "pending" {
			continue
		}

		fmt.Fprintf(os.Stderr, "\n--- %s (resource) ---\n", r.Name)
		fmt.Fprintf(os.Stderr, "  Path: %s\n", r.FilePath)
		fmt.Fprintf(os.Stderr, "  MIME: %s\n", r.MimeType)
		fmt.Fprintf(os.Stderr, "  Size: %d bytes\n", r.SizeBytes)
		fmt.Fprintf(os.Stderr, "  [e]nable / [d]isable / [s]kip: ")

		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		var newState string
		switch answer {
		case "e", "enable":
			newState = "enabled"
		case "d", "disable":
			newState = "disabled"
		case "s", "skip", "":
			continue
		default:
			fmt.Fprintf(os.Stderr, "  Unknown choice %q, skipping.\n", answer)
			continue
		}

		if err := store.UpdateResourceState(r.Name, newState); err != nil {
			return fmt.Errorf("update resource %q: %w", r.Name, err)
		}
		changed = true
		fmt.Fprintf(os.Stderr, "  -> %s\n", newState)
	}

	if changed {
		return config.Save(dir, cfg)
	}
	return nil
}
