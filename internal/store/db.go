package store

import (
	"database/sql"
	"embed"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/polarisagi/hermes/internal/config"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var db *sql.DB

func DB() *sql.DB {
	return db
}

func getDBPath() string {
	if config.GlobalConfig.Database.Path != "" {
		return config.GlobalConfig.Database.Path
	}

	workDir := config.GlobalConfig.Server.WorkDir
	if workDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Error("无法获取用户主目录，回退到当前目录", "error", err)
			return "./polarisagi_hermes.db"
		}
		workDir = filepath.Join(home, ".polarisagi", "hermes")
	}

	if err := os.MkdirAll(workDir, 0755); err != nil {
		slog.Error("无法创建配置目录，回退到当前目录", "error", err)
		return "./polarisagi_hermes.db"
	}

	return filepath.Join(workDir, "polarisagi_hermes.db")
}

// InitDB 初始化 SQLite 数据库连接并运行 Migrations
func InitDB() {
	var err error
	dbPath := getDBPath()
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		slog.Error("无法打开数据库", "error", err)
		os.Exit(1)
	}
	slog.Info("使用数据库文件", "path", dbPath)

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)

	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		slog.Error("设置 WAL 模式失败", "error", err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL;"); err != nil {
		slog.Error("设置 synchronous 失败", "error", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		slog.Error("设置 busy_timeout 失败", "error", err)
	}

	runMigrations()

	writeChan = make(chan WriteTask, 1000)
	go writerWorker()
}

func runMigrations() {
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS _migrations (filename TEXT PRIMARY KEY, applied_at DATETIME DEFAULT (datetime('now', 'localtime')))"); err != nil {
		slog.Error("无法创建迁移追踪表", "error", err)
		os.Exit(1)
	}

	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		slog.Error("无法读取迁移文件夹", "error", err)
		os.Exit(1)
	}

	var fileNames []string
	for _, f := range files {
		fileNames = append(fileNames, f.Name())
	}
	sort.Strings(fileNames)

	for _, name := range fileNames {
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM _migrations WHERE filename = ?", name).Scan(&exists); err == nil && exists > 0 {
			slog.Debug("迁移已执行，跳过", "file", name)
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			slog.Error("无法读取迁移文件", "file", name, "error", err)
			os.Exit(1)
		}
		if _, err := db.Exec(string(content)); err != nil {
			slog.Error("执行迁移文件失败", "file", name, "error", err)
			os.Exit(1)
		}

		if _, err := db.Exec("INSERT INTO _migrations (filename) VALUES (?)", name); err != nil {
			slog.Error("记录迁移状态失败", "file", name, "error", err)
		}

		slog.Info("成功加载数据库表结构", "file", name)
	}
}

func CloseDB() {
	if db != nil {
		db.Close()
	}
}

// WriteTask represents a database write operation to be executed sequentially.
type WriteTask struct {
	Action func(tx *sql.Tx) error
	Result chan error
}

var writeChan chan WriteTask

// writerWorker processes write tasks sequentially to prevent SQLITE_BUSY.
func writerWorker() {
	for task := range writeChan {
		tx, err := db.Begin()
		if err != nil {
			if task.Result != nil {
				task.Result <- err
			}
			continue
		}

		if err := task.Action(tx); err != nil {
			_ = tx.Rollback()
			if task.Result != nil {
				task.Result <- err
			}
		} else {
			err = tx.Commit()
			if task.Result != nil {
				task.Result <- err
			}
		}
	}
}

// ExecuteWrite runs a database transaction inside the central writer goroutine.
func ExecuteWrite(action func(tx *sql.Tx) error) error {
	resultCh := make(chan error, 1)
	writeChan <- WriteTask{
		Action: action,
		Result: resultCh,
	}
	return <-resultCh
}

// ExecuteWriteAsync queues a write task and returns immediately.
func ExecuteWriteAsync(action func(tx *sql.Tx) error) {
	writeChan <- WriteTask{
		Action: action,
		Result: nil,
	}
}
