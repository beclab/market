package marketsource

import (
	"context"
	"encoding/json"
	"fmt"

	"market/internal/v2/db"
	"market/internal/v2/types"
)

type pgStore struct{}

// NewPGStore creates a market source store backed by PostgreSQL.
func NewPGStore() Store {
	return &pgStore{}
}

func (s *pgStore) SaveDataIfHashChanged(ctx context.Context, sourceID string, data *types.MarketSourceData) (bool, error) {
	if sourceID == "" {
		return false, fmt.Errorf("sourceID cannot be empty")
	}
	if data == nil {
		return false, fmt.Errorf("market source data cannot be nil")
	}

	xdb := db.GlobalSqlxDB()
	if xdb == nil {
		return false, fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return false, fmt.Errorf("marshal market source data: %w", err)
	}

	tx, err := xdb.BeginTxx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var existingHash string
	queryHash := `
SELECT COALESCE(data->>'hash', '')
FROM market_sources
WHERE source_id = $1
FOR UPDATE
`
	if err := tx.QueryRowxContext(ctx, queryHash, sourceID).Scan(&existingHash); err != nil {
		return false, fmt.Errorf("query existing market source hash: %w", err)
	}

	if existingHash != "" && existingHash == data.Hash {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit no-op transaction: %w", err)
		}
		return false, nil
	}

	updateQuery := `
UPDATE market_sources
SET data = $2::jsonb, updated_at = NOW()
WHERE source_id = $1
`
	result, err := tx.ExecContext(ctx, updateQuery, sourceID, payload)
	if err != nil {
		return false, fmt.Errorf("update market source data: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return false, fmt.Errorf("market source not found: %s", sourceID)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit market source data update: %w", err)
	}

	return true, nil
}

func (s *pgStore) HasData(ctx context.Context, sourceID string) (bool, error) {
	if sourceID == "" {
		return false, fmt.Errorf("sourceID cannot be empty")
	}

	xdb := db.GlobalSqlxDB()
	if xdb == nil {
		return false, fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	query := `
SELECT EXISTS (
	SELECT 1
	FROM market_sources
	WHERE source_id = $1
	  AND data IS NOT NULL
	  AND (
		COALESCE(data->>'hash', '') <> ''
		OR COALESCE(jsonb_array_length(data->'apps'), 0) > 0
	  )
)
`
	var exists bool
	if err := xdb.QueryRowxContext(ctx, query, sourceID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check market source data existence: %w", err)
	}
	return exists, nil
}

func (s *pgStore) GetOthersHash(ctx context.Context, sourceID string) (string, error) {
	if sourceID == "" {
		return "", fmt.Errorf("sourceID cannot be empty")
	}

	xdb := db.GlobalSqlxDB()
	if xdb == nil {
		return "", fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	query := `
SELECT COALESCE(data->>'hash', '')
FROM market_sources
WHERE source_id = $1
`

	var hash string
	if err := xdb.QueryRowxContext(ctx, query, sourceID).Scan(&hash); err != nil {
		return "", nil
	}
	return hash, nil
}
