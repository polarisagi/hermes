package store

import (
	"context"
	"database/sql"

	"github.com/polarisagi/hermes/internal/domain"
)

type ModelRepo struct{}

func NewModelRepo() *ModelRepo {
	return &ModelRepo{}
}

func (r *ModelRepo) GetUserModels(ctx context.Context) ([]domain.UserModel, error) {
	query := `
		SELECT id, user_provider_id, IFNULL(display_name, ''), model_id, capability_tier, is_active
		FROM user_models
		WHERE is_active = 1
		ORDER BY capability_tier ASC, model_id ASC
	`
	rows, err := DB().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []domain.UserModel
	for rows.Next() {
		var m domain.UserModel
		if err := rows.Scan(&m.ID, &m.UserProviderID, &m.DisplayName, &m.ModelID, &m.CapabilityTier, &m.IsActive); err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, nil
}

func (r *ModelRepo) GetSysModels(ctx context.Context) ([]domain.SysModel, error) {
	query := `
		SELECT model_id, display_name, capability_tier, context_length, max_output_tokens, supports_vision, supports_audio_input, supports_audio_output, supports_tools, prompt_price_per_1k, completion_price_per_1k, released_at, is_active, version_weight, is_legacy
		FROM sys_models
		ORDER BY model_id ASC
	`
	rows, err := DB().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []domain.SysModel
	for rows.Next() {
		var m domain.SysModel
		if err := rows.Scan(
			&m.ModelID, &m.DisplayName, &m.CapabilityTier, &m.ContextLength, &m.MaxOutputTokens, &m.SupportsVision, &m.SupportsAudioInput, &m.SupportsAudioOutput, &m.SupportsTools, &m.PromptPricePer1k, &m.CompletionPricePer1k, &m.ReleasedAt, &m.IsActive, &m.VersionWeight, &m.IsLegacy,
		); err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, nil
}

func (r *ModelRepo) UpsertSysModel(ctx context.Context, m *domain.SysModel) error {
	query := `
		INSERT INTO sys_models (model_id, display_name, capability_tier, context_length, max_output_tokens, supports_vision, supports_audio_input, supports_audio_output, supports_tools, prompt_price_per_1k, completion_price_per_1k, released_at, is_active, version_weight, is_legacy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(model_id) DO UPDATE SET
			display_name = CASE WHEN excluded.display_name != '' THEN excluded.display_name ELSE sys_models.display_name END,
			capability_tier = CASE WHEN excluded.capability_tier != '' THEN excluded.capability_tier ELSE sys_models.capability_tier END,
			context_length = CASE WHEN excluded.context_length > 0 THEN excluded.context_length ELSE sys_models.context_length END,
			max_output_tokens = CASE WHEN excluded.max_output_tokens > 0 THEN excluded.max_output_tokens ELSE sys_models.max_output_tokens END,
			supports_vision = excluded.supports_vision,
			supports_audio_input = excluded.supports_audio_input,
			supports_audio_output = excluded.supports_audio_output,
			supports_tools = excluded.supports_tools,
			prompt_price_per_1k = CASE WHEN excluded.prompt_price_per_1k > 0 THEN excluded.prompt_price_per_1k ELSE sys_models.prompt_price_per_1k END,
			completion_price_per_1k = CASE WHEN excluded.completion_price_per_1k > 0 THEN excluded.completion_price_per_1k ELSE sys_models.completion_price_per_1k END,
			released_at = CASE WHEN excluded.released_at > 0 THEN excluded.released_at ELSE sys_models.released_at END,
			version_weight = excluded.version_weight,
			is_legacy = excluded.is_legacy
	`
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query,
			m.ModelID, m.DisplayName, m.CapabilityTier, m.ContextLength, m.MaxOutputTokens, m.SupportsVision, m.SupportsAudioInput, m.SupportsAudioOutput, m.SupportsTools, m.PromptPricePer1k, m.CompletionPricePer1k, m.ReleasedAt, m.IsActive, m.VersionWeight, m.IsLegacy,
		)
		return err
	})
}

func (r *ModelRepo) UpsertSysProviderModel(ctx context.Context, m *domain.SysProviderModel) error {
	query := `
		INSERT INTO sys_provider_models (provider_id, model_id, actual_model_id)
		VALUES (?, ?, ?)
		ON CONFLICT(provider_id, model_id) DO UPDATE SET actual_model_id = excluded.actual_model_id
	`
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, m.ProviderID, m.ModelID, m.ActualModelID)
		return err
	})
}

func (r *ModelRepo) UpdateSysModelLegacyStatus(ctx context.Context, modelPrefix string, isLegacy bool) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE sys_models SET is_legacy = ? WHERE model_id LIKE ?`, isLegacy, modelPrefix+"%")
		return err
	})
}

func (r *ModelRepo) UpdateUserModelTier(ctx context.Context, id int, tier string) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE user_models SET capability_tier = ? WHERE id = ?`, tier, id)
		return err
	})
}

func (r *ModelRepo) CreateUserModel(ctx context.Context, m *domain.UserModel) error {
	if m.DisplayName == "" {
		m.DisplayName = m.ModelID
	}
	return ExecuteWrite(func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO user_models (user_provider_id, display_name, model_id, capability_tier, is_active) VALUES (?, ?, ?, ?, 1)`,
			m.UserProviderID, m.DisplayName, m.ModelID, m.CapabilityTier,
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err == nil {
			m.ID = int(id)
		}
		return nil
	})
}

func (r *ModelRepo) DeleteUserModel(ctx context.Context, id int) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM user_models WHERE id = ?`, id)
		return err
	})
}

func (r *ModelRepo) GetUserModelsByProvider(ctx context.Context, userProviderID int) ([]domain.UserModel, error) {
	rows, err := DB().QueryContext(ctx, `
		SELECT id, user_provider_id, IFNULL(display_name, ''), model_id, capability_tier, is_active
		FROM user_models WHERE user_provider_id = ? ORDER BY capability_tier, model_id`, userProviderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []domain.UserModel
	for rows.Next() {
		var m domain.UserModel
		if err := rows.Scan(&m.ID, &m.UserProviderID, &m.DisplayName, &m.ModelID, &m.CapabilityTier, &m.IsActive); err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, nil
}

func (r *ModelRepo) GetSysProviderModels(ctx context.Context) ([]domain.SysProviderModel, error) {
	rows, err := DB().QueryContext(ctx, `SELECT provider_id, model_id, actual_model_id FROM sys_provider_models`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []domain.SysProviderModel
	for rows.Next() {
		var m domain.SysProviderModel
		if err := rows.Scan(&m.ProviderID, &m.ModelID, &m.ActualModelID); err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, nil
}
