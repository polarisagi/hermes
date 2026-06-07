package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ClientBackupRecord 对应 client_config_backups 表的一行数据
type ClientBackupRecord struct {
	ID              int64
	ClientName      string
	ConfigPath      string
	OriginalContent string
	InjectedURL     string
	BackedUpAt      time.Time
	UpdatedAt       time.Time
}

type ClientBackupRepo struct {
	db *sql.DB
}

func NewClientBackupRepo(db *sql.DB) *ClientBackupRepo {
	return &ClientBackupRepo{db: db}
}

// Upsert 保存或更新指定客户端的备份记录（每个客户端只保留最新一条）
func (r *ClientBackupRepo) Upsert(ctx context.Context, clientName, configPath, content, injectedURL string) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO client_config_backups (client_name, config_path, original_content, injected_url, backed_up_at, updated_at)
			VALUES (?, ?, ?, ?, datetime('now', 'localtime'), datetime('now', 'localtime'))
			ON CONFLICT(client_name) DO UPDATE SET
				config_path      = excluded.config_path,
				original_content = excluded.original_content,
				injected_url     = excluded.injected_url,
				updated_at       = datetime('now', 'localtime')`,
			clientName, configPath, content, injectedURL)
		return err
	})
}

// UpdateInjectedURL 仅更新记录中的 injected_url
func (r *ClientBackupRepo) UpdateInjectedURL(ctx context.Context, clientName, injectedURL string) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE client_config_backups 
			SET injected_url = ?, updated_at = datetime('now', 'localtime') 
			WHERE client_name = ?`, injectedURL, clientName)
		return err
	})
}

func (r *ClientBackupRepo) Get(ctx context.Context, clientName string) (*ClientBackupRecord, error) {
	var rec ClientBackupRecord
	err := r.db.QueryRowContext(ctx, `
		SELECT id, client_name, config_path, original_content, injected_url, backed_up_at, updated_at
		FROM client_config_backups WHERE client_name = ?`, clientName).Scan(
		&rec.ID, &rec.ClientName, &rec.ConfigPath, &rec.OriginalContent, &rec.InjectedURL, &rec.BackedUpAt, &rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

func (r *ClientBackupRepo) Exists(ctx context.Context, clientName string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM client_config_backups WHERE client_name = ?", clientName).Scan(&count)
	return count > 0, err
}

func (r *ClientBackupRepo) Delete(ctx context.Context, clientName string) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM client_config_backups WHERE client_name = ?", clientName)
		return err
	})
}

func (r *ClientBackupRepo) GetAll(ctx context.Context) (map[string]*ClientBackupRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, client_name, config_path, original_content, injected_url, backed_up_at, updated_at
		FROM client_config_backups`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*ClientBackupRecord)
	for rows.Next() {
		var rec ClientBackupRecord
		if err := rows.Scan(&rec.ID, &rec.ClientName, &rec.ConfigPath, &rec.OriginalContent, &rec.InjectedURL, &rec.BackedUpAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		result[rec.ClientName] = &rec
	}
	return result, rows.Err()
}
