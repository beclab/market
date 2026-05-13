package settings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"market/internal/v2/helper"
	"market/internal/v2/store/marketsource"
	"market/internal/v2/types"
)

type MarketSettings = types.MarketSettings

// pgLookupTimeout bounds the per-call PG read/write performed by the
// market-settings GET/PUT handlers.
const pgLookupTimeout = 5 * time.Second

// Sentinel errors re-exported from store/marketsource so the API
// handler in pkg/v2/api can map them to HTTP status codes without
// importing the store package directly.
//
//   - ErrSourceNotFound  → HTTP 404
//   - ErrSourceNotRemote → HTTP 400
var (
	ErrSourceNotFound  = marketsource.ErrSourceNotFound
	ErrSourceNotRemote = marketsource.ErrSourceNotRemote
)

// defaultMarketSettings returns the response shape used as a fallback
// when the underlying state cannot be resolved (public environment,
// no active remote yet, PG read errors). The selected_source value
// matches createDefaultMarketSources so a fresh deployment without
// any user-driven PUT still returns a coherent answer.
func defaultMarketSettings() *MarketSettings {
	return &MarketSettings{
		SelectedSource: "market.olares",
		Nsfw:           false,
	}
}

// getMarketSettings answers GET /settings/market-settings.
//
// The wire shape stays {selected_source, nsfw} (preserved from the
// pre-PG-migration contract) but both fields are now sourced from the
// market_sources table rather than per-user Redis state:
//
//   - selected_source = source_id of the row with source_type='remote'
//     AND is_active=true.
//   - nsfw            = nsfw column of that same row.
//
// "No active remote" is treated as a soft fallback (default response)
// rather than an error, so a fresh deployment that has not yet seen a
// PUT still returns 200 with the canned default.
//
// userID is unused because the active remote is a system-wide
// singleton; it is kept on the signature for symmetry with the API
// handler call site and for any future per-user enrichment.
func getMarketSettings(_ RedisClient, _ string) (*MarketSettings, error) {
	if helper.IsPublicEnvironment() {
		return defaultMarketSettings(), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), pgLookupTimeout)
	defer cancel()

	row, err := marketsource.GetActiveRemoteSource(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active remote source: %w", err)
	}
	if row == nil {
		return defaultMarketSettings(), nil
	}
	return &MarketSettings{
		SelectedSource: row.SourceID,
		Nsfw:           row.Nsfw,
	}, nil
}

// updateMarketSettings answers PUT /settings/market-settings.
//
// The body's selected_source identifies which remote source should
// become active; the body's nsfw is applied to that same source.
// Both writes happen inside a single PG transaction owned by
// marketsource.SwitchActiveRemoteAndSetNsfw, which also enforces the
// "at most one active remote" invariant. Local sources are rejected
// with ErrSourceNotRemote — the API handler maps this to HTTP 400 so
// the client sees fail-fast feedback rather than a silent no-op.
//
// Empty selected_source is rejected as ErrSourceNotFound (HTTP 404),
// matching the "you addressed a source that does not exist" semantics
// rather than introducing a third "missing field" sentinel.
//
// Public environment is a silent no-op so callers do not panic on a
// nil PG handle or nil Redis client.
//
// userID is unused; the row itself is global and the API path
// already authenticates the caller.
func updateMarketSettings(_ RedisClient, _ string, settings *MarketSettings) error {
	if helper.IsPublicEnvironment() {
		return nil
	}
	if settings == nil {
		return errors.New("settings cannot be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), pgLookupTimeout)
	defer cancel()

	return marketsource.SwitchActiveRemoteAndSetNsfw(ctx, settings.SelectedSource, settings.Nsfw)
}
