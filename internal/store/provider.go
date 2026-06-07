package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/polarisagi/hermes/internal/domain"
)

type ProviderRepo struct{}

func NewProviderRepo() *ProviderRepo {
	return &ProviderRepo{}
}

func (r *ProviderRepo) GetUserProviders(ctx context.Context) ([]domain.UserProvider, error) {
	query := `
		SELECT id, name, provider_id, auth_credentials,
		       priority, weight, concurrency_limit, min_interval_sec, timeout_sec, retry_times, status,
		       balance, limit_percent, used_amount, IFNULL(valid_from, ''), IFNULL(valid_to, ''), created_at
		FROM user_providers
		ORDER BY id ASC
	`
	rows, err := DB().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []domain.UserProvider
	var providerIDs []interface{}
	providerMap := make(map[int]*domain.UserProvider)

	for rows.Next() {
		var p domain.UserProvider
		var creds []byte
		err := rows.Scan(
			&p.ID, &p.Name, &p.ProviderID, &creds,
			&p.Priority, &p.Weight, &p.ConcurrencyLimit, &p.MinIntervalSec, &p.TimeoutSec, &p.RetryTimes, &p.Status,
			&p.Balance, &p.LimitPercent, &p.UsedAmount, &p.ValidFrom, &p.ValidTo, &p.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		p.AuthCredentials = json.RawMessage(creds)
		providers = append(providers, p)
	}

	for i := range providers {
		p := &providers[i]
		providerIDs = append(providerIDs, p.ID)
		providerMap[p.ID] = p
	}

	if len(providerIDs) > 0 {
		epQuery := `SELECT id, user_provider_id, sys_endpoint_id, is_enabled, custom_base_url FROM user_access_endpoints ORDER BY id ASC`
		epRows, err := DB().QueryContext(ctx, epQuery)
		if err == nil {
			defer epRows.Close()
			for epRows.Next() {
				var ep domain.UserAccessEndpoint
				if err := epRows.Scan(&ep.ID, &ep.UserProviderID, &ep.SysEndpointID, &ep.IsEnabled, &ep.CustomBaseURL); err == nil {
					if p, exists := providerMap[ep.UserProviderID]; exists {
						p.Endpoints = append(p.Endpoints, ep)
					}
				}
			}
		}
	}

	return providers, nil
}

func (r *ProviderRepo) GetSysProvider(ctx context.Context, providerID string) (*domain.SysProvider, error) {
	query := `SELECT provider_id, provider_name FROM sys_providers WHERE provider_id = ?`
	var p domain.SysProvider
	err := DB().QueryRowContext(ctx, query, providerID).Scan(&p.ProviderID, &p.ProviderName)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ProviderRepo) GetSysAccessEndpoint(ctx context.Context, endpointID string) (*domain.SysAccessEndpoint, error) {
	query := `
		SELECT endpoint_id, provider_id, display_name, api_protocol, default_base_url, auth_type, auth_header, required_credential_fields, display_order
		FROM sys_access_endpoints
		WHERE endpoint_id = ?
	`
	var e domain.SysAccessEndpoint
	var reqFields []byte
	err := DB().QueryRowContext(ctx, query, endpointID).Scan(
		&e.EndpointID, &e.ProviderID, &e.DisplayName, &e.APIProtocol, &e.DefaultBaseURL, &e.AuthType, &e.AuthHeader, &reqFields, &e.DisplayOrder,
	)
	if err != nil {
		return nil, err
	}
	e.RequiredCredentialFields = json.RawMessage(reqFields)
	return &e, nil
}

func (r *ProviderRepo) GetAllSysProviders(ctx context.Context) ([]domain.SysProvider, error) {
	rows, err := DB().QueryContext(ctx, "SELECT provider_id, provider_name FROM sys_providers ORDER BY provider_name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []domain.SysProvider
	for rows.Next() {
		var p domain.SysProvider
		if err := rows.Scan(&p.ProviderID, &p.ProviderName); err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, nil
}

func (r *ProviderRepo) GetSysAccessEndpointsByProvider(ctx context.Context, providerID string) ([]domain.SysAccessEndpoint, error) {
	query := `SELECT endpoint_id, provider_id, display_name, api_protocol, default_base_url, auth_type, auth_header, required_credential_fields, display_order
	          FROM sys_access_endpoints WHERE provider_id = ? ORDER BY display_order ASC`
	rows, err := DB().QueryContext(ctx, query, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []domain.SysAccessEndpoint
	for rows.Next() {
		var e domain.SysAccessEndpoint
		var reqFields []byte
		if err := rows.Scan(&e.EndpointID, &e.ProviderID, &e.DisplayName, &e.APIProtocol, &e.DefaultBaseURL, &e.AuthType, &e.AuthHeader, &reqFields, &e.DisplayOrder); err != nil {
			return nil, err
		}
		e.RequiredCredentialFields = json.RawMessage(reqFields)
		endpoints = append(endpoints, e)
	}
	return endpoints, nil
}

func (r *ProviderRepo) GetAllSysAccessEndpoints(ctx context.Context) ([]domain.SysAccessEndpoint, error) {
	query := "SELECT endpoint_id, provider_id, display_name, api_protocol, default_base_url, auth_type, auth_header, required_credential_fields, display_order FROM sys_access_endpoints ORDER BY display_order ASC"
	rows, err := DB().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []domain.SysAccessEndpoint
	for rows.Next() {
		var e domain.SysAccessEndpoint
		var reqFields []byte
		if err := rows.Scan(&e.EndpointID, &e.ProviderID, &e.DisplayName, &e.APIProtocol, &e.DefaultBaseURL, &e.AuthType, &e.AuthHeader, &reqFields, &e.DisplayOrder); err != nil {
			return nil, err
		}
		e.RequiredCredentialFields = json.RawMessage(reqFields)
		endpoints = append(endpoints, e)
	}
	return endpoints, nil
}

func (r *ProviderRepo) CreateUserProvider(ctx context.Context, p *domain.UserProvider) error {
	query := `
		INSERT INTO user_providers (
			name, provider_id, auth_credentials,
			priority, weight, concurrency_limit, min_interval_sec, timeout_sec, retry_times, status, balance, limit_percent, used_amount, valid_from, valid_to
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	creds, _ := json.Marshal(p.AuthCredentials)
	if len(creds) == 0 || string(creds) == "null" {
		creds = []byte("{}")
	}

	err := ExecuteWrite(func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query,
			p.Name, p.ProviderID, creds,
			p.Priority, p.Weight, p.ConcurrencyLimit, p.MinIntervalSec, p.TimeoutSec, p.RetryTimes, p.Status, p.Balance, p.LimitPercent, p.UsedAmount, p.ValidFrom, p.ValidTo,
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err == nil {
			p.ID = int(id)
		}

		if p.ID > 0 {
			epQuery := `INSERT INTO user_access_endpoints (user_provider_id, sys_endpoint_id, is_enabled, custom_base_url) VALUES (?, ?, ?, ?)`
			for i := range p.Endpoints {
				ep := &p.Endpoints[i]
				ep.UserProviderID = p.ID
				res, err := tx.ExecContext(ctx, epQuery, ep.UserProviderID, ep.SysEndpointID, ep.IsEnabled, ep.CustomBaseURL)
				if err == nil {
					epID, _ := res.LastInsertId()
					ep.ID = int(epID)
				}
			}
		}

		return nil
	})

	if err == nil && p.ID > 0 {
		_ = r.SeedUserModels(ctx, p.ID, p.ProviderID)
	}
	return err
}

// SeedUserModels 批量将该厂商的系统模型导入为用户模型实例，tier 以 sys_model_intent_dict 为单一数据源
func (r *ProviderRepo) SeedUserModels(ctx context.Context, userProviderID int, providerID string) error {
	seedSQL := `
		INSERT OR IGNORE INTO user_models (user_provider_id, display_name, model_id, capability_tier, is_active)
		SELECT DISTINCT
			? AS user_provider_id,
			sm.display_name,
			sm.model_id,
			COALESCE(
				(SELECT capability_tier FROM sys_model_intent_dict WHERE model_id = sm.model_id),
				'smart'
			) AS capability_tier,
			1 AS is_active
		FROM sys_models sm
		JOIN sys_provider_models spm ON sm.model_id = spm.model_id
		WHERE spm.provider_id = ?
	`
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, seedSQL, userProviderID, providerID)
		return err
	})
}

func (r *ProviderRepo) UpdateUserProvider(ctx context.Context, p *domain.UserProvider) error {
	query := `
		UPDATE user_providers SET
			name = ?, provider_id = ?, auth_credentials = ?,
			priority = ?, weight = ?, concurrency_limit = ?, min_interval_sec = ?, timeout_sec = ?, retry_times = ?, status = ?, balance = ?, limit_percent = ?, valid_from = ?, valid_to = ?
		WHERE id = ?
	`
	creds, _ := json.Marshal(p.AuthCredentials)
	if len(creds) == 0 || string(creds) == "null" {
		creds = []byte("{}")
	}

	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query,
			p.Name, p.ProviderID, creds,
			p.Priority, p.Weight, p.ConcurrencyLimit, p.MinIntervalSec, p.TimeoutSec, p.RetryTimes, p.Status, p.Balance, p.LimitPercent, p.ValidFrom, p.ValidTo,
			p.ID,
		)
		if err != nil {
			return err
		}

		// Replace endpoints
		_, err = tx.ExecContext(ctx, `DELETE FROM user_access_endpoints WHERE user_provider_id = ?`, p.ID)
		if err != nil {
			return err
		}

		epQuery := `INSERT INTO user_access_endpoints (user_provider_id, sys_endpoint_id, is_enabled, custom_base_url) VALUES (?, ?, ?, ?)`
		for i := range p.Endpoints {
			ep := &p.Endpoints[i]
			ep.UserProviderID = p.ID
			res, err := tx.ExecContext(ctx, epQuery, ep.UserProviderID, ep.SysEndpointID, ep.IsEnabled, ep.CustomBaseURL)
			if err == nil {
				epID, _ := res.LastInsertId()
				ep.ID = int(epID)
			}
		}

		return nil
	})
}

func (r *ProviderRepo) DeleteUserProvider(ctx context.Context, id int) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM user_providers WHERE id = ?", id)
		return err
	})
}

func (r *ProviderRepo) InsertSysProviderIfNotExists(ctx context.Context, p *domain.SysProvider) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO sys_providers (provider_id, provider_name, display_order) VALUES (?, ?, 999)`,
			p.ProviderID, p.ProviderName,
		)
		return err
	})
}
