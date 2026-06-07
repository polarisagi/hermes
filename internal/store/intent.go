package store

import (
	"context"
	"database/sql"

	"github.com/polarisagi/hermes/internal/domain"
)

type IntentRepo struct{}

func NewIntentRepo() *IntentRepo {
	return &IntentRepo{}
}

func (r *IntentRepo) GetSysIntent(ctx context.Context, modelID string) (string, error) {
	var tier string
	err := DB().QueryRowContext(ctx, `SELECT capability_tier FROM sys_model_intent_dict WHERE model_id = ?`, modelID).Scan(&tier)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return tier, err
}

func (r *IntentRepo) GetUserIntent(ctx context.Context, modelID string) (string, error) {
	var tier string
	err := DB().QueryRowContext(ctx, `SELECT capability_tier FROM user_model_intent_dict WHERE model_id = ?`, modelID).Scan(&tier)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return tier, err
}

func (r *IntentRepo) SaveUserIntent(ctx context.Context, intent *domain.UserModelIntentDict) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO user_model_intent_dict (model_id, capability_tier, source)
			VALUES (?, ?, ?)
			ON CONFLICT(model_id) DO UPDATE SET
				capability_tier = excluded.capability_tier,
				source = excluded.source,
				updated_at = datetime('now', 'localtime')`,
			intent.ModelID, intent.CapabilityTier, intent.Source)
		return err
	})
}

func (r *IntentRepo) SaveSysIntent(ctx context.Context, intent *domain.UserModelIntentDict) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO sys_model_intent_dict (model_id, capability_tier, source)
			VALUES (?, ?, ?)
			ON CONFLICT(model_id) DO UPDATE SET
				capability_tier = excluded.capability_tier,
				source = excluded.source`,
			intent.ModelID, intent.CapabilityTier, intent.Source)
		return err
	})
}

func (r *IntentRepo) GetAllSysIntents(ctx context.Context) (map[string]string, error) {
	rows, err := DB().QueryContext(ctx, `SELECT model_id, capability_tier FROM sys_model_intent_dict ORDER BY model_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, nil
}

func (r *IntentRepo) GetAllUserIntents(ctx context.Context) (map[string]string, error) {
	rows, err := DB().QueryContext(ctx, `SELECT model_id, capability_tier FROM user_model_intent_dict ORDER BY model_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, nil
}

func (r *IntentRepo) DeleteUserIntent(ctx context.Context, modelID string) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM user_model_intent_dict WHERE model_id = ?`, modelID)
		return err
	})
}
