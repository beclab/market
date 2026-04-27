package marketsource

import (
	"context"
	"testing"

	"market/internal/v2/types"
)

func TestSaveDataIfHashChangedValidation(t *testing.T) {
	store := NewPGStore()

	if _, err := store.SaveDataIfHashChanged(context.Background(), "", &types.MarketSourceData{}); err == nil {
		t.Fatalf("expected error when sourceID is empty")
	}

	if _, err := store.SaveDataIfHashChanged(context.Background(), "market.olares", nil); err == nil {
		t.Fatalf("expected error when data is nil")
	}
}

func TestHasDataValidation(t *testing.T) {
	store := NewPGStore()

	if _, err := store.HasData(context.Background(), ""); err == nil {
		t.Fatalf("expected error when sourceID is empty")
	}
}

func TestGetOthersHashValidation(t *testing.T) {
	store := NewPGStore()

	if _, err := store.GetOthersHash(context.Background(), ""); err == nil {
		t.Fatalf("expected error when sourceID is empty")
	}
}
