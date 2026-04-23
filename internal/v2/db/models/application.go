package models

import (
	"time"

	"market/internal/v2/types"
)

// Application mirrors the applications table.
type Application struct {
	ID               int64                              `gorm:"column:id;primaryKey;autoIncrement"`
	SourceID         string                             `gorm:"column:source_id;size:50;not null;uniqueIndex:uq_applications_source_app_version,priority:1"`
	AppID            string                             `gorm:"column:app_id;size:32;not null;uniqueIndex:uq_applications_source_app_version,priority:2;index:idx_applications_app_id"`
	AppRawID         string                             `gorm:"column:app_raw_id;size:32;not null;index:idx_applications_app_raw_id"`
	AppName          string                             `gorm:"column:app_name;size:100;not null"`
	AppRawName       string                             `gorm:"column:app_raw_name;size:100;not null;index:idx_applications_app_raw_name"`
	AppVersion       string                             `gorm:"column:app_version;size:16;not null;uniqueIndex:uq_applications_source_app_version,priority:3"`
	AppType          string                             `gorm:"column:app_type;size:32;not null"` // app | middleware
	AppEntry         *JSONB[types.ApplicationInfoEntry] `gorm:"column:app_entry;type:jsonb"`
	AppImageAnalysis *JSONB[types.ImageAnalysisResult]  `gorm:"column:app_image_analysis;type:jsonb"`
	Price            *JSONB[types.PriceConfig]          `gorm:"column:price;type:jsonb"`
	InstalledType    string                             `gorm:"column:installed_type;size:10;not null"` // full | server | client
	CreatedAt        time.Time                          `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt        time.Time                          `gorm:"column:updated_at;not null;default:now()"`
}

// TableName pins the table name; we use plural naming for all tables.
func (Application) TableName() string { return "applications" }
