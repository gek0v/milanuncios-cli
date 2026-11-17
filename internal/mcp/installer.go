package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gek0v/milanuncios-cli/internal/output"
)

// InstallResult describes the action taken by the installer.
type InstallResult struct {
	Client     string `json:"client"`
	ConfigFile string `json:"config_file"`
	Status     string `json:"status"`
	Snippet    string `json:"snippet"`
}

// InstallTarget configures an AI agent environment with milanuncios MCP server.
func InstallTarget(target string, customBinPath string) (*InstallResult, error) {
	binName := "milanuncios"
	if customBinPath != "" {
		binName = customBinPath
	}

	serverConfig := map[string]any{
		"command": binName,
		"args":    []string{"mcp"},
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}

	var targetPath string
	target = strings.ToLower(strings.TrimSpace(target))

	switch target {
	case "claude-code", "claude":
		targetPath = filepath.Join(home, ".claude.json")
	case "cursor":
		targetPath = filepath.Join(home, ".cursor", "mcp.json")
	case "codex":
		targetPath = filepath.Join(home, ".codex", "mcp.json")
	default:
		return nil, output.Usagef("unknown install target %q (supported: claude-code, cursor, codex)", target)
	}

	// Read existing config or initialize new
	cfgMap := make(map[string]any)
	if data, err := os.ReadFile(targetPath); err == nil {
		if err := json.Unmarshal(data, &cfgMap); err != nil {
			return nil, fmt.Errorf("existing configuration %s contains invalid JSON: %w (aborting to prevent data loss)", targetPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading existing config %s: %w", targetPath, err)
	}

	mcpServers, ok := cfgMap["mcpServers"].(map[string]any)
	if !ok {
		mcpServers = make(map[string]any)
		cfgMap["mcpServers"] = mcpServers
	}

	mcpServers["milanuncios"] = serverConfig

	_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
	marshaled, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(targetPath, marshaled, 0600); err != nil {
		return nil, fmt.Errorf("writing agent configuration to %s: %w", targetPath, err)
	}

	snippetBytes, _ := json.MarshalIndent(map[string]any{
		"mcpServers": map[string]any{
			"milanuncios": serverConfig,
		},
	}, "", "  ")

	return &InstallResult{
		Client:     target,
		ConfigFile: targetPath,
		Status:     "installed",
		Snippet:    string(snippetBytes),
	}, nil
}
