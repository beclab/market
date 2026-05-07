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

// ErrSourceNotFound is returned by SwitchActiveRemoteAndSetNsfw when
// the requested source_id has no row in market_sources. Callers map
// this to HTTP 404 so the client distinguishes "you typed a source
// that does not exist" from an internal server error.
var ErrSourceNotFound = errors.New("market source not found")

// ErrSourceNotRemote is returned by SwitchActiveRemoteAndSetNsfw when
// the requested source_id exists but its source_type is not 'remote'.
// The /settings/market-settings handler only manipulates remote
// sources; pointing it at a local source is a client bug. Callers map
// this to HTTP 400.
var ErrSourceNotRemote = errors.New("market source is not of type remote")

// GetActiveRemoteSource returns the single market_sources row with
// source_type='remote' AND is_active=true, or (nil, nil) if no such
// row exists. The caller decides what to fall back to (typically the
// hardcoded default selected_source).
//
// The partial unique index uq_market_sources_one_active_remote
// guarantees this query returns at most one row, so LIMIT 1 is more
// of a safety belt than a correctness requirement.
func GetActiveRemoteSource(ctx context.Context) (*models.MarketSource, error) {
	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	var row models.MarketSource
	err := gdb.WithContext(ctx).
		Where("source_type = ? AND is_active = ?", "remote", true).
		Limit(1).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active remote source: %w", err)
	}
	return &row, nil
}

// SwitchActiveRemoteAndSetNsfw atomically promotes sourceID to be the
// active remote source and updates its nsfw flag, in a single
// transaction.
//
// The transaction layout:
//  1. SELECT ... FOR UPDATE on the target row to lock it and read
//     source_type for the validation below. Concurrent PUTs targeting
//     the same source serialise here.
//  2. Validate source existence and source_type='remote'. Local
//     sources are rejected with ErrSourceNotRemote (client bug —
//     /settings/market-settings only manipulates remotes).
//  3. UPDATE every other remote row to is_active=false. Doing this
//     before step 4 keeps the partial unique index
//     uq_market_sources_one_active_remote satisfied at every
//     statement boundary, even when the target was already active or
//     when another remote was the previous active.
//  4. UPDATE the target to is_active=true and set its nsfw column to
//     the requested value, in one statement.
//
// The order of statements 3 → 4 is required by the partial unique
// index: any reordering would risk transient "two actives" violations
// that PostgreSQL would refuse with a unique constraint failure. This
// helper is the only blessed writer of the is_active column outside
// the schema default, so callers should never wire a separate
// UPDATE.
//
// Errors:
//   - ErrSourceNotFound — sourceID does not exist
//   - ErrSourceNotRemote — sourceID exists but source_type != 'remote'
//   - other errors are wrapped fmt.Errorf with context
func SwitchActiveRemoteAndSetNsfw(ctx context.Context, sourceID string, nsfw bool) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return ErrSourceNotFound
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1+2. Lock the target row and validate source_type.
		var row models.MarketSource
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_id = ?", sourceID).
			First(&row).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSourceNotFound
			}
			return fmt.Errorf("lock market_source %s: %w", sourceID, err)
		}
		if row.SourceType != "remote" {
			return ErrSourceNotRemote
		}

		// 3. Clear is_active on every other remote row. We deliberately
		//    include rows that already have is_active=false; the row-level
		//    write is cheap and the WHERE clause stays simple.
		if err := tx.Model(&models.MarketSource{}).
			Where("source_type = ? AND source_id <> ?", "remote", sourceID).
			Update("is_active", false).Error; err != nil {
			return fmt.Errorf("clear other active remotes: %w", err)
		}

		// 4. Promote target + set nsfw in one statement.
		if err := tx.Model(&models.MarketSource{}).
			Where("source_id = ?", sourceID).
			Updates(map[string]interface{}{
				"is_active": true,
				"nsfw":      nsfw,
			}).Error; err != nil {
			return fmt.Errorf("activate market_source %s: %w", sourceID, err)
		}
		return nil
	})
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

// GetActiveRemoteSourceHash returns the catalogue hash
// (data->>'hash') of the currently-active remote source, or "" when
// no remote row has is_active=true or its data JSONB is NULL / has no
// 'hash' key. Used by GET /market/hash to surface chart-repo's
// catalogue fingerprint to client polling.
//
// The partial unique index uq_market_sources_one_active_remote
// guarantees this query returns at most one row, so LIMIT 1 is a
// safety belt rather than a correctness requirement. The query is
// ctx-cancellable end-to-end (gorm/pgx propagates ctx to the wire),
// so a slow PG can be aborted by the handler's request timeout
// without an extra goroutine wrapper.
func GetActiveRemoteSourceHash(ctx context.Context) (string, error) {
	gdb := db.Global()
	if gdb == nil {
		return "", fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	const query = `
SELECT COALESCE(data->>'hash', '')
FROM market_sources
WHERE source_type = 'remote' AND is_active = true
LIMIT 1
`

	var hash string
	if err := gdb.WithContext(ctx).Raw(query).Scan(&hash).Error; err != nil {
		return "", fmt.Errorf("get active remote source hash: %w", err)
	}
	return hash, nil
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
