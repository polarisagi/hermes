package store

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/polarisagi/hermes/internal/domain"
)

type RoutingLogRepo struct{}

func NewRoutingLogRepo() *RoutingLogRepo {
	return &RoutingLogRepo{}
}

const insertRoutingLog = `
INSERT INTO routing_logs
    (requested_model, resolved_tier, resolution_src, tier_degraded, original_tier, actual_model, user_provider_id, client_name)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

// SaveAsync 异步落盘，不阻塞请求热路径
func (r *RoutingLogRepo) SaveAsync(log *domain.RoutingLog) {
	ExecuteWriteAsync(func(tx *sql.Tx) error {
		degraded := 0
		if log.TierDegraded {
			degraded = 1
		}
		_, err := tx.ExecContext(context.Background(), insertRoutingLog,
			log.RequestedModel, log.ResolvedTier, log.ResolutionSrc,
			degraded, log.OriginalTier,
			log.ActualModel, log.UserProviderID, log.ClientName,
		)
		if err != nil {
			slog.Warn("路由日志写入失败", "error", err)
		}
		return err
	})
}

func (r *RoutingLogRepo) GetRecent(ctx context.Context, limit int) ([]domain.RoutingLog, error) {
	rows, err := DB().QueryContext(ctx, `
		SELECT id, created_at, requested_model, resolved_tier, resolution_src,
		       tier_degraded, COALESCE(original_tier,''), COALESCE(provider_name,''),
		       COALESCE(actual_model,''), COALESCE(user_provider_id,0)
		FROM routing_logs
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []domain.RoutingLog
	for rows.Next() {
		var l domain.RoutingLog
		var degraded int
		if err := rows.Scan(
			&l.ID, &l.CreatedAt, &l.RequestedModel, &l.ResolvedTier, &l.ResolutionSrc,
			&degraded, &l.OriginalTier, &l.ProviderName, &l.ActualModel, &l.UserProviderID,
		); err != nil {
			return nil, err
		}
		l.TierDegraded = degraded == 1
		logs = append(logs, l)
	}
	return logs, nil
}

// PruneOldLogs 清理超过 retentionDays 天的日志
func (r *RoutingLogRepo) PruneOldLogs(ctx context.Context, retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format("2006-01-02 15:04:05")
	return ExecuteWrite(func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM routing_logs WHERE created_at < ?`, cutoff)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n > 0 {
			slog.Info("路由日志自动清理完成", "deleted_rows", n, "retention_days", retentionDays)
		}
		return nil
	})
}
