package models

import (
	"time"

	"market/internal/v2/types"
)

// Application mirrors the applications table: per-source app catalog.
type Application struct {
	ID         int64                              `gorm:"column:id;primaryKey;autoIncrement"`
	SourceID   string                             `gorm:"column:source_id;size:50;not null;uniqueIndex:uq_applications_source_app_version,priority:1;index:idx_applications_source_id"`
	AppID      string                             `gorm:"column:app_id;size:32;not null;uniqueIndex:uq_applications_source_app_version,priority:2;index:idx_applications_app_id"`
	AppName    string                             `gorm:"column:app_name;size:100;not null"`
	AppVersion string                             `gorm:"column:app_version;size:16;not null;uniqueIndex:uq_applications_source_app_version,priority:3"`
	AppType    string                             `gorm:"column:app_type;size:32;not null"` // app | middleware
	AppEntry   *JSONB[types.ApplicationInfoEntry] `gorm:"column:app_entry;type:jsonb"`
	Price      *JSONB[types.PriceConfig]          `gorm:"column:price;type:jsonb"`
	CreatedAt  time.Time                          `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt  time.Time                          `gorm:"column:updated_at;not null;default:now()"`
}

// TableName pins the table name; we use plural naming for all tables.
func (Application) TableName() string { return "applications" }
