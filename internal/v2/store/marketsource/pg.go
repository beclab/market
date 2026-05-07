package marketsource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"market/internal/v2/db"
	"market/internal/v2/db/models"
	"market/internal/v2/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GetMarketSources() ([]*models.MarketSource, error) {
	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	var ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var sources []*models.MarketSource
	result := gdb.WithContext(ctx).Order("source_id ASC").Find(&sources)
	if result.Error != nil {
		return nil, result.Error
	}

	return sources, nil
}

// UpsertSources inserts new market_sources rows or updates the descriptor
// columns of existing ones, keyed by the source_id unique index. The
// catalogue payload column (`data`) is intentionally excluded from
// DoUpdates so a startup-time upsert does not clobber the JSONB snapshot
// that the syncer has already populated for an existing source.
//
// Empty / whitespace SourceID rows are skipped silently — the caller
// (settingsManager.saveMarketSourcesToStore) iterates a settings.MarketSource
// slice that the API contract guarantees has non-empty IDs, so a violation
// here would be a programming error rather than user input.
func UpsertSources(ctx context.Context, sources []*models.MarketSource) error {
	if len(sources) == 0 {
		return nil
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	rows := make([]*models.MarketSource, 0, len(sources))
	for _, src := range sources {
		if src == nil {
			continue
		}
		if strings.TrimSpace(src.SourceID) == "" {
			continue
		}
		rows = append(rows, src)
	}
	if len(rows) == 0 {
		return nil
	}

	if err := gdb.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "source_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"source_title",
				"source_url",
				"source_type",
				"description",
				"priority",
			}),
		}).
		Create(&rows).Error; err != nil {
		return fmt.Errorf("upsert market_sources: %w", err)
	}

	return nil
}

// DeleteSourceByID removes a single market_sources row by its source_id.
// Returns nil when no matching row exists so the caller can treat
// "already gone" the same way the Redis path did.
func DeleteSourceByID(ctx context.Context, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return fmt.Errorf("sourceID cannot be empty")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	if err := gdb.WithContext(ctx).
		Where("source_id = ?", sourceID).
		Delete(&models.MarketSource{}).Error; err != nil {
		return fmt.Errorf("delete market_source %s: %w", sourceID, err)
	}
	return nil
}

// Count returns the total number of market_sources rows. Used by the
// settings bootstrap path to decide whether the catalogue is empty
// (first deploy → seed defaults) or already populated (merge defaults
// with existing config).
func Count(ctx context.Context) (int64, error) {
	gdb := db.Global()
	if gdb == nil {
		return 0, fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	var n int64
	if err := gdb.WithContext(ctx).
		Model(&models.MarketSource{}).
		Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count market_sources: %w", err)
	}
	return n, nil
}

// TruncateAll removes every market_sources row. Intended for the
// CLEAR_CACHE=true debug path — it is more destructive than the
// previous Redis-only clear because the JSONB `data` column lives on
// the same row, so the syncer will have to refetch the catalogue
// payload after the next startup.
func TruncateAll(ctx context.Context) error {
	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	if err := gdb.WithContext(ctx).
		Where("1 = 1").
		Delete(&models.MarketSource{}).Error; err != nil {
		return fmt.Errorf("truncate market_sources: %w", err)
	}
	return nil
}

// GetSourceByID fetches a single market_sources row by its source_id.
// Returns (nil, nil) when no matching row exists; the caller decides
// whether absence is an error.
func GetSourceByID(ctx context.Context, sourceID string) (*models.MarketSource, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("sourceID cannot be empty")
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	var row models.MarketSource
	err := gdb.WithContext(ctx).
		Where("source_id = ?", sourceID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get market_source %s: %w", sourceID, err)
	}
	return &row, nil
}

// UpdateSourceNsfwByID flips the `nsfw` flag of a single market_sources
// row. It is the only writer of that column outside the schema default,
// so the startup default-seeding path (UpsertSources, which excludes
// nsfw from DoUpdates) never races with this UPDATE.
//
// Returns nil when no matching row exists so the caller can treat
// "set nsfw on a source we don't track" as a soft no-op rather than an
// error — the typical trigger is a stale per-user `selected_source`
// pointing at a source that has since been deleted.
func UpdateSourceNsfwByID(ctx context.Context, sourceID string, nsfw bool) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return fmt.Errorf("sourceID cannot be empty")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	res := gdb.WithContext(ctx).
		Model(&models.MarketSource{}).
		Where("source_id = ?", sourceID).
		Update("nsfw", nsfw)
	if res.Error != nil {
		return fmt.Errorf("update market_source.nsfw (source=%s): %w", sourceID, res.Error)
	}
	return nil
}

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

	gdb := db.Global()
	if gdb == nil {
		return false, fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	query := `
SELECT EXISTS (
	SELECT 1
	FROM market_sources
	WHERE source_id = ?
	  AND data IS NOT NULL
	  AND (
		COALESCE(data->>'hash', '') <> ''
		OR COALESCE(jsonb_array_length(data->'apps'), 0) > 0
	  )
)
`
	var exists bool
	if err := gdb.WithContext(ctx).Raw(query, sourceID).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("check market source data existence: %w", err)
	}
	return exists, nil
}

func GetOthersHash(ctx context.Context, sourceID string) (string, error) {
	if sourceID == "" {
		return "", fmt.Errorf("sourceID cannot be empty")
	}

	gdb := db.Global()
	if gdb == nil {
		return "", fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	query := `
SELECT COALESCE(data->>'hash', '')
FROM market_sources
WHERE source_id = ?
`

	var hash string
	if err := gdb.WithContext(ctx).Raw(query, sourceID).Scan(&hash).Error; err != nil {
		return "", nil
	}
	return hash, nil
}
