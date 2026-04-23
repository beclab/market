package models

import (
	"time"

	"market/internal/v2/db/models/payload"
)

// Application mirrors the application table.
type Application struct {
	ID               int64                            `gorm:"column:id;primaryKey;autoIncrement"`
	SourceID         string                           `gorm:"column:source_id;size:50;not null;uniqueIndex:uq_application_source_app_version,priority:1"`
	AppID            string                           `gorm:"column:app_id;size:32;not null;uniqueIndex:uq_application_source_app_version,priority:2;index:idx_application_app_id"`
	AppRawID         string                           `gorm:"column:app_raw_id;size:32;not null;index:idx_application_app_raw_id"`
	AppName          string                           `gorm:"column:app_name;size:100;not null"`
	AppRawName       string                           `gorm:"column:app_raw_name;size:100;not null;index:idx_application_app_raw_name"`
	AppVersion       string                           `gorm:"column:app_version;size:16;not null;uniqueIndex:uq_application_source_app_version,priority:3"`
	AppType          string                           `gorm:"column:app_type;size:32;not null"` // app | middleware
	AppEntry         *JSONB[payload.AppEntry]         `gorm:"column:app_entry;type:jsonb"`
	AppImageAnalysis *JSONB[payload.AppImageAnalysis] `gorm:"column:app_image_analysis;type:jsonb"`
	InstalledType    string                           `gorm:"column:installed_type;size:10;not null"` // full | server | client
	IsCloned         bool                             `gorm:"column:is_cloned;not null;default:false"`
	CreatedAt        time.Time                        `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt        time.Time                        `gorm:"column:updated_at;not null;default:now()"`
}

// TableName pins the table name; we use singular naming.
func (Application) TableName() string { return "application" }
