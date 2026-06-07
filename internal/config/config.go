package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type ServerConfig struct {
	ListenAddr string `toml:"listen_addr"`
	WorkDir    string `toml:"work_dir"`
}

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type SiliconFlowConfig struct {
	Enabled bool   `toml:"enabled"`
	APIKey  string `toml:"api_key"`
}

type SyncConfig struct {
	DataDir           string            `toml:"data_dir"`
	EnableGitTracking bool              `toml:"enable_git_tracking"`
	SiliconFlow       SiliconFlowConfig `toml:"siliconflow"`
}

type Config struct {
	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Sync     SyncConfig     `toml:"sync"`
}

var GlobalConfig Config

// LoadConfig 解析 TOML 配置文件并填充到 GlobalConfig 中
func LoadConfig(path string) error {
	// 设置默认值
	GlobalConfig = Config{
		Server: ServerConfig{
			ListenAddr: "127.0.0.1:27777",
			WorkDir:    "",
		},
		Database: DatabaseConfig{
			Path: "",
		},
		Sync: SyncConfig{
			DataDir:           "~/.polarisagi/hermes/data/external",
			EnableGitTracking: false,
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("配置文件不存在，将使用默认内置配置", "path", path)
			return nil
		}
		return err
	}

	if err := toml.Unmarshal(data, &GlobalConfig); err != nil {
		return err
	}

	GlobalConfig.Server.WorkDir = expandPath(GlobalConfig.Server.WorkDir)
	GlobalConfig.Database.Path = expandPath(GlobalConfig.Database.Path)
	GlobalConfig.Sync.DataDir = expandPath(GlobalConfig.Sync.DataDir)

	// TEST_MODE=true 时切换到测试端口 28889，避免与线上 27777 冲突
	if os.Getenv("TEST_MODE") == "true" {
		GlobalConfig.Server.ListenAddr = "127.0.0.1:28889"
	}

	return nil
}

// expandPath expands the tilde (~) in a path to the user's home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
