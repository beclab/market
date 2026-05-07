package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"market/internal/v2/helper"
	"market/internal/v2/store/marketsource"
	"market/internal/v2/types"
)

type MarketSettings = types.MarketSettings

// Redis key for market settings
const RedisKeyMarketSettings = "market:settings"

// pgLookupTimeout bounds the per-call PG read/write performed alongside
// the Redis read/write of the per-user selected_source preference.
const pgLookupTimeout = 5 * time.Second

// getMarketSettings retrieves the user's market settings.
//
// Storage split (since the per-source nsfw move):
//   - selected_source is a per-user preference and stays in Redis at
//     "market:settings:<userID>" (legacy shape, JSON-encoded MarketSettings).
//   - nsfw is a per-source flag and is read from market_sources.nsfw
//     for the row identified by the user's selected_source.
//
// The wire shape returned to the API caller stays {selected_source, nsfw};
// callers do not need to know storage moved.
func getMarketSettings(redisClient RedisClient, userID string) (*MarketSettings, error) {
	if helper.IsPublicEnvironment() {
		return &MarketSettings{
			SelectedSource: "market.olares",
			Nsfw:           false,
		}, nil
	}

	out := &MarketSettings{
		SelectedSource: "market.olares",
	}

	// 1. Pull the per-user selected_source from Redis (preserved from
	//    the pre-PG-migration shape).
	if redisClient != nil {
		userRedisKey := fmt.Sprintf("%s:%s", RedisKeyMarketSettings, userID)
		data, err := redisClient.Get(userRedisKey)
		if err != nil {
			// "key not found" is the expected first-access path; fall
			// through with the default selected_source. Any other Redis
			// error bubbles up so the caller can decide.
			if err.Error() != "key not found" {
				return nil, err
			}
		} else if data != "" {
			var stored MarketSettings
			if uerr := json.Unmarshal([]byte(data), &stored); uerr != nil {
				return nil, uerr
			}
			// Backward-compat translation for the deprecated source id.
			if stored.SelectedSource == "Official-Market-Sources" {
				stored.SelectedSource = "market.olares"
			}
			if strings.TrimSpace(stored.SelectedSource) != "" {
				out.SelectedSource = stored.SelectedSource
			}
		}
	}

	// 2. Resolve nsfw from market_sources.nsfw for the user's source.
	//    Soft-fail on missing source / read error — return out.Nsfw=false
	//    so the API contract stays "valid response with default" rather
	//    than failing the GET because a stale selected_source points at
	//    a deleted source.
	if strings.TrimSpace(out.SelectedSource) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), pgLookupTimeout)
		defer cancel()
		row, err := marketsource.GetSourceByID(ctx, out.SelectedSource)
		if err == nil && row != nil {
			out.Nsfw = row.Nsfw
		}
	}

	return out, nil
}

// updateMarketSettings applies a settings update from
// PUT /settings/market-settings.
//
// Body shape is unchanged: {selected_source, nsfw}. Storage split:
//   - selected_source is persisted to Redis as the per-user preference
//     (same key shape as the pre-migration code).
//   - nsfw is applied to market_sources.nsfw for the row identified by
//     the body's selected_source. This is the only writer of that
//     column outside the schema default; the startup default-seeding
//     path explicitly excludes nsfw from its UPSERT DoUpdates so that
//     a PUT here is never clobbered by a service restart.
//
// Public environment is a silent no-op so callers don't panic on a
// nil redisClient + nil PG handle.
func updateMarketSettings(redisClient RedisClient, userID string, settings *MarketSettings) error {
	if helper.IsPublicEnvironment() {
		return nil
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	// 1. Persist the per-user selected_source preference to Redis.
	if redisClient != nil {
		userRedisKey := fmt.Sprintf("%s:%s", RedisKeyMarketSettings, userID)
		data, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		if err := redisClient.Set(userRedisKey, string(data), 0); err != nil {
			return err
		}
	}

	// 2. Apply nsfw to the per-source row in PG. A blank selected_source
	//    is treated as "no source addressed" and skips the PG update so
	//    a PUT with only a selected_source change does not flap nsfw on
	//    an unrelated row.
	source := strings.TrimSpace(settings.SelectedSource)
	if source != "" {
		ctx, cancel := context.WithTimeout(context.Background(), pgLookupTimeout)
		defer cancel()
		if err := marketsource.UpdateSourceNsfwByID(ctx, source, settings.Nsfw); err != nil {
			return fmt.Errorf("update market_source.nsfw for %s: %w", source, err)
		}
	}

	return nil
}
