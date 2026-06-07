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
}

func (r *AccountLogRepo) GetDashboardStats(ctx context.Context, start, end string) ([]DashboardStat, error) {
	query := `
		SELECT 
			COALESCE(account_name, 'Default') as account,
			COALESCE(api_protocol, 'UNDEFINED') as platform,
			SUM(cost) as period_cost_usd,
			SUM(prompt_tokens) as prompt_tokens,
			SUM(completion_tokens) as completion_tokens,
			SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) as error_count,
			SUM(CASE WHEN status_code < 400 THEN 1 ELSE 0 END) as success_count
		FROM account_logs
		WHERE date(created_at) >= date(?) AND date(created_at) <= date(?)
		GROUP BY account_name, api_protocol
	`
	rows, err := DB().QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []DashboardStat
	for rows.Next() {
		var s DashboardStat
		var cost, prompt, comp *float64
		var errCnt, succCnt *int
		if err := rows.Scan(&s.Account, &s.Platform, &cost, &prompt, &comp, &errCnt, &succCnt); err != nil {
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
		stats = append(stats, s)
	}
	return stats, nil
}
