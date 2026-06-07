package domain

// ClientStatus 表示一个客户端软件的当前配置状态（用于 API 响应）
type ClientStatus struct {
	Name         string `json:"name"`
	IsInstalled  bool   `json:"is_installed"`           // 检测到该客户端已安装（配置文件目录存在）
	IsConfigured bool   `json:"is_configured"`          // 已被 PolarisAGI-Hermes 注入代理配置
	HasBackup    bool   `json:"has_backup"`             // client_config_backups 表中存在备份记录
	InjectedURL  string `json:"injected_url,omitempty"` // 记录实际注入的 URL
	Error        string `json:"error,omitempty"`
}
