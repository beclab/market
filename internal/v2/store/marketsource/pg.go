package marketsource

import (
	"context"
	"fmt"

	"market/internal/v2/db"
	"market/internal/v2/db/models"
	"market/internal/v2/types"

	"gorm.io/gorm"
)

func SaveData(ctx context.Context, sourceID string, data *types.MarketSourceData) error {
	if sourceID == "" {
		return fmt.Errorf("sourceID cannot be empty")
	}
	if data == nil {
		return fmt.Errorf("market source data cannot be nil")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	payload := models.NewJSONB(*data)

	result := gdb.WithContext(ctx).
		Model(&models.MarketSource{}).
		Where("source_id = ?", sourceID).
		Updates(map[string]interface{}{
			"data":       &payload,
			"updated_at": gorm.Expr("NOW()"),
		})
	if result.Error != nil {
		return fmt.Errorf("update market source data: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("market source not found: %s", sourceID)
	}

	return nil
}

func HasData(ctx context.Context, sourceID string) (bool, error) {
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

func GetOthersHash(ctx context.Context, sourceID string) (string, error) {
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

