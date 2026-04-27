package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"market/internal/v2/db"
	"market/internal/v2/db/models"
	"market/internal/v2/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func QueryAppsByIDs(ctx context.Context, sourceID string, ids []string) ([]*models.Application, error) {
	if sourceID == "" || len(ids) == 0 {
		return []*models.Application{}, nil
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	uniqueIDs := uniqueNonEmpty(ids)
	if len(uniqueIDs) == 0 {
		return []*models.Application{}, nil
	}

	var apps []*models.Application
	result := gdb.WithContext(ctx).
		Where("source_id = ? AND app_id IN ?", sourceID, uniqueIDs).
		Find(&apps)
	if result.Error != nil {
		return nil, fmt.Errorf("query applications error: %w", result.Error)
	}

	return apps, nil
}

type BatchSyncInput struct {
	SourceID      string
	RequestApps   map[string]interface{}
	AddedAppIDs   []string
	ChangedAppIDs []string
}

// SyncBatchApps applies added/version-changed app updates in one transaction.
func SyncBatchApps(ctx context.Context, in BatchSyncInput) error {
	if in.SourceID == "" {
		return nil
	}
	if len(in.AddedAppIDs) == 0 && len(in.ChangedAppIDs) == 0 {
		return nil
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before market source store usage")
	}

	allIDs := append([]string{}, in.AddedAppIDs...)
	allIDs = append(allIDs, in.ChangedAppIDs...)
	allRows := buildApplicationRows(in.SourceID, in.RequestApps, allIDs)
	if len(allRows) == 0 {
		return nil
	}

	return upsertApplications(gdb.WithContext(ctx), allRows)
}

func upsertApplications(tx *gorm.DB, rows []*models.Application) error {
	if len(rows) == 0 {
		return nil
	}

	if err := tx.
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "source_id"},
				{Name: "app_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"app_name",
				"app_version",
				"app_type",
				"app_entry",
				"price",
			}),
		}).
		Create(&rows).Error; err != nil {
		return fmt.Errorf("upsert applications error: %w", err)
	}

	return nil
}

func buildApplicationRows(sourceID string, requestApps map[string]interface{}, appIDs []string) []*models.Application {
	uniqueIDs := uniqueNonEmpty(appIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	rows := make([]*models.Application, 0, len(uniqueIDs))
	for _, appID := range uniqueIDs {
		raw, ok := requestApps[appID]
		if !ok {
			continue
		}
		appMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		appVersion, _ := appMap["version"].(string)
		appVersion = strings.TrimSpace(appVersion)
		if appVersion == "" {
			continue
		}

		appName, _ := appMap["name"].(string)
		appName = strings.TrimSpace(appName)
		if appName == "" {
			appName = appID
		}

		row := &models.Application{
			SourceID:   sourceID,
			AppID:      appID,
			AppName:    appName,
			AppVersion: appVersion,
			AppType:    normalizeAppType(appMap["cfgType"]),
		}

		if appEntry := types.NewApplicationInfoEntry(appMap); appEntry != nil {
			entryJSON := models.NewJSONB(*appEntry)
			row.AppEntry = &entryJSON
		}

		if price := parsePriceConfig(appMap["price"]); price != nil {
			priceJSON := models.NewJSONB(*price)
			row.Price = &priceJSON
		}

		rows = append(rows, row)
	}
	return rows
}

func parsePriceConfig(raw interface{}) *types.PriceConfig {
	switch v := raw.(type) {
	case *types.PriceConfig:
		return v
	case types.PriceConfig:
		return &v
	case map[string]interface{}:
		if len(v) == 0 {
			return nil
		}
		payload, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var out types.PriceConfig
		if err := json.Unmarshal(payload, &out); err != nil {
			return nil
		}
		return &out
	default:
		return nil
	}
}

func normalizeAppType(raw interface{}) string {
	cfgType, _ := raw.(string)
	cfgType = strings.ToLower(strings.TrimSpace(cfgType))
	if strings.Contains(cfgType, "middleware") {
		return "middleware"
	}
	return "app"
}

func uniqueNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
