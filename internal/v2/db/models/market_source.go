package models

import (
	"time"

	"market/internal/v2/db/models/payload"
)

// MarketSource mirrors the market_sources table.
type MarketSource struct {
	ID          int64                              `gorm:"column:id;primaryKey;autoIncrement"`
	SourceID    string                             `gorm:"column:source_id;size:50;not null;uniqueIndex:uq_market_sources_source_id"`
	SourceTitle string                             `gorm:"column:source_title;size:50;not null"`
	SourceURL   string                             `gorm:"column:source_url;type:text;not null"`
	SourceType  string                             `gorm:"column:source_type;size:16;not null"` // local | remote
	Description string                             `gorm:"column:description;type:text;not null;default:''"`
	Priority    int                                `gorm:"column:priority;not null;default:100"`
	Others      *JSONB[payload.MarketSourceOthers] `gorm:"column:others;type:jsonb"`
	CreatedAt   time.Time                          `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt   time.Time                          `gorm:"column:updated_at;not null;default:now()"`
}

// TableName pins the table name; we use plural naming for all tables.
func (MarketSource) TableName() string { return "market_sources" }
