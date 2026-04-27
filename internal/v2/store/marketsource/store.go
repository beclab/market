package marketsource

import (
	"context"

	"market/internal/v2/types"
)

// Store defines persistence operations for market_sources.data.
type Store interface {
	// SaveDataIfHashChanged upserts the JSONB payload when hash differs.
	// Returns changed=true when data was written.
	SaveDataIfHashChanged(ctx context.Context, sourceID string, data *types.MarketSourceData) (changed bool, err error)

	// HasData returns whether market_sources.data contains a meaningful snapshot.
	HasData(ctx context.Context, sourceID string) (bool, error)

	// GetOthersHash returns the persisted Others.Hash for a source.
	GetOthersHash(ctx context.Context, sourceID string) (string, error)
}
