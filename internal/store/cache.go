package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/polarisagi/hermes/internal/domain"
)

type ExternalCacheRepo struct{}

func NewExternalCacheRepo() *ExternalCacheRepo {
	return &ExternalCacheRepo{}
}

func (r *ExternalCacheRepo) UpsertProvider(ctx context.Context, p *domain.ExternalProviderCache) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO external_provider_cache
				(provider_id, source, provider_name, description, website_url, api_base_url, api_protocol, status, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', datetime('now', 'localtime'))
			ON CONFLICT(provider_id, source) DO UPDATE SET
				provider_name = CASE WHEN excluded.provider_name != '' THEN excluded.provider_name ELSE external_provider_cache.provider_name END,
				description   = CASE WHEN excluded.description   != '' THEN excluded.description   ELSE external_provider_cache.description   END,
				website_url   = CASE WHEN excluded.website_url   != '' THEN excluded.website_url   ELSE external_provider_cache.website_url   END,
				api_base_url  = CASE WHEN excluded.api_base_url  != '' THEN excluded.api_base_url  ELSE external_provider_cache.api_base_url  END,
				api_protocol  = CASE WHEN excluded.api_protocol  != '' THEN excluded.api_protocol  ELSE external_provider_cache.api_protocol  END,
				synced_at     = datetime('now', 'localtime')`,
			p.ProviderID, p.Source, p.ProviderName, p.Description,
			p.WebsiteURL, p.APIBaseURL, p.APIProtocol,
		)
		return err
	})
}

func (r *ExternalCacheRepo) GetPendingProviders(ctx context.Context) ([]domain.ExternalProviderCache, error) {
	rows, err := DB().QueryContext(ctx, `
		SELECT id, provider_id, source, provider_name, description,
		       website_url, api_base_url, api_protocol, status, synced_at
		FROM external_provider_cache WHERE status = 'pending' ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.ExternalProviderCache
	for rows.Next() {
		var p domain.ExternalProviderCache
		if err := rows.Scan(
			&p.ID, &p.ProviderID, &p.Source, &p.ProviderName, &p.Description,
			&p.WebsiteURL, &p.APIBaseURL, &p.APIProtocol, &p.Status, &p.SyncedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func (r *ExternalCacheRepo) MarkProviderPromoted(ctx context.Context, id int) error {
	now := time.Now()
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE external_provider_cache SET status='promoted', promoted_at=? WHERE id=?`, now, id)
		return err
	})
}

func (r *ExternalCacheRepo) MarkProviderRejected(ctx context.Context, id int, reason string) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE external_provider_cache SET status='rejected', reject_reason=? WHERE id=?`, reason, id)
		return err
	})
}

func (r *ExternalCacheRepo) UpsertModel(ctx context.Context, m *domain.ExternalModelCache) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO external_model_cache
				(model_id, source, display_name, capability_tier, tier_infer_src,
				context_length, max_output_tokens,
				supports_vision, supports_audio_input, supports_audio_output, supports_tools,
				prompt_price_per_1k, completion_price_per_1k,
				released_at, is_legacy, status, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', datetime('now', 'localtime'))
			ON CONFLICT(model_id, source) DO UPDATE SET
				display_name          = CASE WHEN excluded.display_name          != '' THEN excluded.display_name          ELSE external_model_cache.display_name          END,
				capability_tier       = CASE WHEN excluded.capability_tier       != '' THEN excluded.capability_tier       ELSE external_model_cache.capability_tier       END,
				tier_infer_src        = CASE WHEN excluded.tier_infer_src        != '' THEN excluded.tier_infer_src        ELSE external_model_cache.tier_infer_src        END,
				context_length        = CASE WHEN excluded.context_length        > 0   THEN excluded.context_length        ELSE external_model_cache.context_length        END,
				max_output_tokens     = CASE WHEN excluded.max_output_tokens     > 0   THEN excluded.max_output_tokens     ELSE external_model_cache.max_output_tokens     END,
				supports_vision       = excluded.supports_vision       OR external_model_cache.supports_vision,
				supports_audio_input  = excluded.supports_audio_input  OR external_model_cache.supports_audio_input,
				supports_audio_output = excluded.supports_audio_output OR external_model_cache.supports_audio_output,
				supports_tools        = excluded.supports_tools        OR external_model_cache.supports_tools,
				prompt_price_per_1k     = CASE WHEN excluded.prompt_price_per_1k     > 0 THEN excluded.prompt_price_per_1k     ELSE external_model_cache.prompt_price_per_1k     END,
				completion_price_per_1k = CASE WHEN excluded.completion_price_per_1k > 0 THEN excluded.completion_price_per_1k ELSE external_model_cache.completion_price_per_1k END,
				released_at           = CASE WHEN excluded.released_at           > 0   THEN excluded.released_at           ELSE external_model_cache.released_at           END,
				is_legacy             = excluded.is_legacy OR external_model_cache.is_legacy,
				synced_at             = datetime('now', 'localtime')`,
			m.ModelID, m.Source, m.DisplayName, m.CapabilityTier, m.TierInferSrc,
			m.ContextLength, m.MaxOutputTokens,
			m.SupportsVision, m.SupportsAudioInput, m.SupportsAudioOutput, m.SupportsTools,
			m.PromptPricePer1k, m.CompletionPricePer1k,
			m.ReleasedAt, m.IsLegacy,
		)
		return err
	})
}

func (r *ExternalCacheRepo) GetPendingModels(ctx context.Context) ([]domain.ExternalModelCache, error) {
	rows, err := DB().QueryContext(ctx, `
		SELECT id, model_id, source, display_name, capability_tier, tier_infer_src,
		       context_length, max_output_tokens,
		       supports_vision, supports_audio_input, supports_audio_output, supports_tools,
		       prompt_price_per_1k, completion_price_per_1k,
		       released_at, is_legacy, status, synced_at
		FROM external_model_cache WHERE status = 'pending' ORDER BY released_at DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.ExternalModelCache
	for rows.Next() {
		var m domain.ExternalModelCache
		if err := rows.Scan(
			&m.ID, &m.ModelID, &m.Source, &m.DisplayName, &m.CapabilityTier, &m.TierInferSrc,
			&m.ContextLength, &m.MaxOutputTokens,
			&m.SupportsVision, &m.SupportsAudioInput, &m.SupportsAudioOutput, &m.SupportsTools,
			&m.PromptPricePer1k, &m.CompletionPricePer1k,
			&m.ReleasedAt, &m.IsLegacy, &m.Status, &m.SyncedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *ExternalCacheRepo) MarkModelPromoted(ctx context.Context, id int) error {
	now := time.Now()
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE external_model_cache SET status='promoted', promoted_at=? WHERE id=?`, now, id)
		return err
	})
}

func (r *ExternalCacheRepo) MarkModelRejected(ctx context.Context, id int, reason string) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE external_model_cache SET status='rejected', reject_reason=? WHERE id=?`, reason, id)
		return err
	})
}
