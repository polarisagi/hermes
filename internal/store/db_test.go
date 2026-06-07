package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/polarisagi/hermes/internal/config"
)

func TestInitDB(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sqlite-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	config.GlobalConfig.Database.Path = dbPath

	// This should run all migrations successfully and not call os.Exit(1)
	InitDB()
	defer CloseDB()

	// Verify that the _migrations table has our migration files
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query _migrations: %v", err)
	}
	if count < 1 {
		t.Errorf("expected at least 1 migration applied, got %d", count)
	}
}
