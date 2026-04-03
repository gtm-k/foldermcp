package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/state"
)

// AgentCard represents an A2A-compatible agent card that describes this
// agent's identity and capabilities for discovery by other agents.
type AgentCard struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	URL          string       `json:"url"`
	Version      string       `json:"version"`
	Capabilities []Capability `json:"capabilities"`
}

// Capability describes a single capability (tool) exposed by the agent.
type Capability struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GenerateAgentCard creates an AgentCard from the given metadata and tool list.
// Each tool with a non-empty State of "approved" (or any tool if no state
// filtering is needed) is mapped to a Capability entry.
func GenerateAgentCard(name, version, url string, tools []state.Tool) *AgentCard {
	capabilities := make([]Capability, 0, len(tools))
	for _, t := range tools {
		capabilities = append(capabilities, Capability{
			Name:        t.Name,
			Description: t.Description,
		})
	}

	return &AgentCard{
		Name:         name,
		Description:  fmt.Sprintf("%s — AI tool server powered by FolderMCP", name),
		URL:          url,
		Version:      version,
		Capabilities: capabilities,
	}
}

// WriteAgentCard serialises the AgentCard to JSON and writes it as
// agent-card.json in the specified directory.
func WriteAgentCard(dir string, card *AgentCard) error {
	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agent card: %w", err)
	}

	outPath := filepath.Join(dir, "agent-card.json")
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write agent-card.json: %w", err)
	}

	return nil
}
