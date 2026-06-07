package store

import (
	"context"
	"database/sql"

	"github.com/polarisagi/hermes/internal/domain"
)

type RouteRepo struct{}

func NewRouteRepo() *RouteRepo {
	return &RouteRepo{}
}

func (r *RouteRepo) GetUserCustomRoutes(ctx context.Context) ([]domain.UserCustomRoute, error) {
	rows, err := DB().QueryContext(ctx, `SELECT id, requested_model_id, target_user_model_id, is_active FROM user_custom_routes WHERE is_active = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []domain.UserCustomRoute
	for rows.Next() {
		var rt domain.UserCustomRoute
		if err := rows.Scan(&rt.ID, &rt.RequestedModelID, &rt.TargetUserModelID, &rt.IsActive); err != nil {
			return nil, err
		}
		routes = append(routes, rt)
	}
	return routes, nil
}

func (r *RouteRepo) GetAllUserCustomRoutes(ctx context.Context) ([]domain.UserCustomRoute, error) {
	rows, err := DB().QueryContext(ctx, `SELECT id, requested_model_id, target_user_model_id, is_active FROM user_custom_routes ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []domain.UserCustomRoute
	for rows.Next() {
		var rt domain.UserCustomRoute
		if err := rows.Scan(&rt.ID, &rt.RequestedModelID, &rt.TargetUserModelID, &rt.IsActive); err != nil {
			return nil, err
		}
		routes = append(routes, rt)
	}
	return routes, nil
}

func (r *RouteRepo) CreateUserCustomRoute(ctx context.Context, rt *domain.UserCustomRoute) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO user_custom_routes (requested_model_id, target_user_model_id, is_active) VALUES (?, ?, ?)`,
			rt.RequestedModelID, rt.TargetUserModelID, rt.IsActive)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err == nil {
			rt.ID = int(id)
		}
		return nil
	})
}

func (r *RouteRepo) UpdateUserCustomRoute(ctx context.Context, rt *domain.UserCustomRoute) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE user_custom_routes SET requested_model_id = ?, target_user_model_id = ?, is_active = ? WHERE id = ?`,
			rt.RequestedModelID, rt.TargetUserModelID, rt.IsActive, rt.ID)
		return err
	})
}

func (r *RouteRepo) DeleteUserCustomRoute(ctx context.Context, id int) error {
	return ExecuteWrite(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM user_custom_routes WHERE id = ?`, id)
		return err
	})
}
