package domain

// MCPServerConfig represents the standard MCP JSON schema inside claude_desktop_config.json
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// MCPPluginDef represents a plugin metadata definition in the registry
type MCPPluginDef struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Package          string   `json:"package"` // NPM package name, e.g., @modelcontextprotocol/server-github
	RequiredEnvVars  []string `json:"required_env_vars"`
	OptionalEnvVars  []string `json:"optional_env_vars"`
	DefaultArgs      []string `json:"default_args"` // e.g. ["-y", "@modelcontextprotocol/server-github"]
	CommandType      string   `json:"command_type"` // e.g. "npx" or "python"
}

// MCPInstallRequest is the payload from the frontend to install a plugin
type MCPInstallRequest struct {
	PluginID string            `json:"plugin_id"`
	EnvVars  map[string]string `json:"env_vars"`
}
