package store

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/polarisagi/hermes/internal/domain"
)

type AccountLogRepo struct{}

var (
	accountLogRepoInstance = &AccountLogRepo{}
)

func GetAccountLogRepo() *AccountLogRepo {
	return accountLogRepoInstance
}

const insertAccountLog = `
INSERT INTO account_logs
    (account_name, api_protocol, requested_model_id, actual_model_id, prompt_tokens, completion_tokens, total_tokens, latency_ms, status_code, error_msg, cost, client_name)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// SaveAsync sends the log to the global write channel for asynchronous writing
func (r *AccountLogRepo) SaveAsync(log *domain.AccountLog) {
	ExecuteWriteAsync(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), insertAccountLog,
			log.AccountName, log.APIProtocol, log.RequestedModelID, log.ActualModelID,
			log.PromptTokens, log.CompletionTokens, log.TotalTokens,
			log.LatencyMs, log.StatusCode, log.ErrorMsg, log.Cost, log.ClientName,
		)
		if err != nil {
			slog.Warn("计费流水(AccountLog)写入失败", "error", err, "model", log.ActualModelID)
		}
		return err
	})
}

type DashboardStat struct {
	Account          string  `json:"account"`
	Platform         string  `json:"platform"`
	PeriodCostUSD    float64 `json:"period_cost_usd"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ErrorCount       int     `json:"error_count"`
	SuccessCount     int     `json:"success_count"`
	Balance          float64 `json:"balance"`
	LimitPercent     float64 `json:"limit_percent"`
	ValidFrom        string  `json:"valid_from"`
	CycleCostUSD     float64 `json:"cycle_cost_usd"`
}

func (r *AccountLogRepo) GetDashboardStats(ctx context.Context, start, end string) ([]DashboardStat, error) {
	query := `
		SELECT 
			COALESCE(NULLIF(al.account_name, ''), 'Default') as account,
			COALESCE(NULLIF(al.api_protocol, ''), 'UNDEFINED') as platform,
			SUM(al.cost) as period_cost_usd,
			SUM(al.prompt_tokens) as prompt_tokens,
			SUM(al.completion_tokens) as completion_tokens,
			SUM(CASE WHEN al.status_code >= 400 THEN 1 ELSE 0 END) as error_count,
			SUM(CASE WHEN al.status_code < 400 THEN 1 ELSE 0 END) as success_count,
			MAX(up.balance) as balance,
			MAX(up.limit_percent) as limit_percent,
			MAX(up.valid_from) as valid_from,
			MAX(up.used_amount) as cycle_cost_usd
		FROM account_logs al
		LEFT JOIN user_providers up ON al.account_name = up.name
		WHERE date(al.created_at) >= date(?) AND date(al.created_at) <= date(?)
		GROUP BY al.account_name, al.api_protocol
	`
	rows, err := DB().QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []DashboardStat
	for rows.Next() {
		var s DashboardStat
		var cost, prompt, comp, balance, limit, cycle *float64
		var errCnt, succCnt *int
		var validFrom sql.NullString
		if err := rows.Scan(&s.Account, &s.Platform, &cost, &prompt, &comp, &errCnt, &succCnt, &balance, &limit, &validFrom, &cycle); err != nil {
			return nil, err
		}
		if cost != nil {
			s.PeriodCostUSD = *cost
		}
		if prompt != nil {
			s.PromptTokens = int64(*prompt)
		}
		if comp != nil {
			s.CompletionTokens = int64(*comp)
		}
		if errCnt != nil {
			s.ErrorCount = *errCnt
		}
		if succCnt != nil {
			s.SuccessCount = *succCnt
		}
		if balance != nil {
			s.Balance = *balance
		}
		if limit != nil {
			s.LimitPercent = *limit
		}
		if validFrom.Valid {
			s.ValidFrom = validFrom.String
		}
		if cycle != nil {
			s.CycleCostUSD = *cycle
		}
		stats = append(stats, s)
	}
	return stats, nil
}
