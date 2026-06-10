package clientcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/polarisagi/hermes/internal/domain"
)

var defaultMCPRegistry = []domain.MCPPluginDef{
	{
		ID:              "github",
		Name:            "GitHub",
		Description:     "Search repositories, open PRs, manage issues, and read code.",
		Package:         "@modelcontextprotocol/server-github",
		RequiredEnvVars: []string{"GITHUB_PERSONAL_ACCESS_TOKEN"},
		OptionalEnvVars: []string{},
		CommandType:     "npx",
		DefaultArgs:     []string{"-y", "@modelcontextprotocol/server-github"},
	},
	{
		ID:              "google-drive",
		Name:            "Google Drive",
		Description:     "Search and read files from your Google Drive.",
		Package:         "@modelcontextprotocol/server-gdrive",
		RequiredEnvVars: []string{}, // requires oauth setup or service account in practice, but let's keep it simple
		OptionalEnvVars: []string{"GDRIVE_CREDENTIALS"},
		CommandType:     "npx",
		DefaultArgs:     []string{"-y", "@modelcontextprotocol/server-gdrive"},
	},
	{
		ID:              "filesystem",
		Name:            "Local Filesystem",
		Description:     "Secure local file access for Claude.",
		Package:         "@modelcontextprotocol/server-filesystem",
		RequiredEnvVars: []string{}, // Technically needs a path arg, we will handle args dynamically or rely on user modifying it later
		OptionalEnvVars: []string{},
		CommandType:     "npx",
		DefaultArgs:     []string{"-y", "@modelcontextprotocol/server-filesystem", "/"}, // defaults to root or home
	},
	{
		ID:              "sqlite",
		Name:            "SQLite Database",
		Description:     "Query local SQLite databases.",
		Package:         "@modelcontextprotocol/server-sqlite",
		RequiredEnvVars: []string{},
		OptionalEnvVars: []string{},
		CommandType:     "npx",
		DefaultArgs:     []string{"-y", "@modelcontextprotocol/server-sqlite"},
	},
}

type MCPManager struct{}

func NewMCPManager() *MCPManager {
	return &MCPManager{}
}

func (m *MCPManager) GetRegistry() []domain.MCPPluginDef {
	return defaultMCPRegistry
}

func (m *MCPManager) getDesktopConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir, err := os.UserConfigDir()
	base := filepath.Join(home, "Library", "Application Support")
	if err == nil {
		base = dir
	}
	return filepath.Join(base, "Claude", "claude_desktop_config.json"), nil
}

func (m *MCPManager) GetInstalledPlugins() (map[string]domain.MCPServerConfig, error) {
	path, err := m.getDesktopConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]domain.MCPServerConfig), nil
		}
		return nil, err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	serversMap, ok := root["mcpServers"].(map[string]any)
	if !ok {
		return make(map[string]domain.MCPServerConfig), nil
	}

	result := make(map[string]domain.MCPServerConfig)
	for id, val := range serversMap {
		b, err := json.Marshal(val)
		if err == nil {
			var cfg domain.MCPServerConfig
			if json.Unmarshal(b, &cfg) == nil {
				result[id] = cfg
			}
		}
	}

	return result, nil
}

func (m *MCPManager) InstallPlugin(req domain.MCPInstallRequest) error {
	var def *domain.MCPPluginDef
	for _, p := range defaultMCPRegistry {
		if p.ID == req.PluginID {
			def = &p
			break
		}
	}
	if def == nil {
		return fmt.Errorf("plugin %s not found in registry", req.PluginID)
	}

	for _, required := range def.RequiredEnvVars {
		if val, ok := req.EnvVars[required]; !ok || val == "" {
			return fmt.Errorf("missing required environment variable: %s", required)
		}
	}

	path, err := m.getDesktopConfigPath()
	if err != nil {
		return err
	}

	var root map[string]any
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &root)
	}
	if root == nil {
		root = make(map[string]any)
	}

	serversMap, ok := root["mcpServers"].(map[string]any)
	if !ok {
		serversMap = make(map[string]any)
	}

	cfg := domain.MCPServerConfig{
		Command: def.CommandType,
		Args:    def.DefaultArgs,
		Env:     req.EnvVars,
	}

	serversMap[def.ID] = cfg
	root["mcpServers"] = serversMap

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	return atomicWriteFile(path, out, 0644)
}

func (m *MCPManager) UninstallPlugin(pluginID string) error {
	path, err := m.getDesktopConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}

	serversMap, ok := root["mcpServers"].(map[string]any)
	if !ok {
		return nil // Nothing to uninstall
	}

	if _, exists := serversMap[pluginID]; !exists {
		return nil
	}

	delete(serversMap, pluginID)
	root["mcpServers"] = serversMap

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}

	return atomicWriteFile(path, out, 0644)
}
