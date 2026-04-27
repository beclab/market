package models

import (
	"time"

	"market/internal/v2/db/models/payload"
	"market/internal/v2/types"
)

// UserApplication mirrors the user_applications table: per-user view of a
// rendered manifest.
type UserApplication struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	SourceID   string `gorm:"column:source_id;size:50;not null;index:idx_user_applications_source_id"`
	UserID     string `gorm:"column:user_id;size:120;not null;uniqueIndex:uq_user_applications_user_app,priority:1;index:idx_user_applications_user_id"`
	AppID      string `gorm:"column:app_id;size:32;not null;uniqueIndex:uq_user_applications_user_app,priority:2;index:idx_user_applications_app_id"`
	AppRawID   string `gorm:"column:app_raw_id;size:32;not null;index:idx_user_applications_app_raw_id"`
	AppName    string `gorm:"column:app_name;size:100;not null"`
	AppRawName string `gorm:"column:app_raw_name;size:100;not null;index:idx_user_applications_app_raw_name"`

	ManifestVersion string `gorm:"column:manifest_version;size:32;not null"`
	ManifestType    string `gorm:"column:manifest_type;size:32;not null"`
	APIVersion      string `gorm:"column:api_version;size:32"`

	// Manifest top-level blocks. Payload structs are placeholders until the
	// corresponding business types land.
	Metadata        *JSONB[payload.ManifestMetadata] `gorm:"column:metadata;type:jsonb"`
	Spec            *JSONB[payload.ManifestSpec]     `gorm:"column:spec;type:jsonb"`
	Resources       *JSONB[payload.Resources]        `gorm:"column:resources;type:jsonb"`
	Options         *JSONB[payload.Options]          `gorm:"column:options;type:jsonb"`
	Entrances       *JSONB[[]payload.Entrance]       `gorm:"column:entrances;type:jsonb"`
	SharedEntrances *JSONB[[]payload.Entrance]       `gorm:"column:shared_entrances;type:jsonb"`
	Ports           *JSONB[[]payload.Port]           `gorm:"column:ports;type:jsonb"`
	Tailscale       *JSONB[payload.TailscaleSpec]    `gorm:"column:tailscale;type:jsonb"`
	Permission      *JSONB[payload.Permission]       `gorm:"column:permission;type:jsonb"`
	Middleware      *JSONB[payload.Middleware]       `gorm:"column:middleware;type:jsonb"`
	Envs            *JSONB[[]payload.Env]            `gorm:"column:envs;type:jsonb"`

	// Pricing & purchase live alongside the manifest data because they are
	// per-(user, app) too.
	Price        *JSONB[types.PriceConfig]  `gorm:"column:price;type:jsonb"`
	PurchaseInfo *JSONB[types.PurchaseInfo] `gorm:"column:purchase_info;type:jsonb"`

	RenderStatus              string `gorm:"column:render_status;size:16;not null;default:'pending'"` // pending | success | failed
	RenderError               string `gorm:"column:render_error;size:200"`
	RenderConsecutiveFailures int    `gorm:"column:render_consecutive_failures;not null;default:0"`

	IsUpgrade bool      `gorm:"column:is_upgrade;not null;default:false"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;default:now()"`
}

// TableName pins the table name; we use plural naming for all tables.
func (UserApplication) TableName() string { return "user_applications" }
