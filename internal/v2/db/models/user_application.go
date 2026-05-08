package models

import (
	"time"

	"market/internal/v2/types"
)

// UserApplication mirrors the user_applications table: per-user view of a
// rendered manifest.
type UserApplication struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	SourceID   string `gorm:"column:source_id;size:50;not null;uniqueIndex:uq_user_applications_user_source_app,priority:2;index:idx_user_applications_source_id"`
	UserID     string `gorm:"column:user_id;size:120;not null;uniqueIndex:uq_user_applications_user_source_app,priority:1;index:idx_user_applications_user_id;index:idx_user_applications_user_app,priority:1"`
	AppID      string `gorm:"column:app_id;size:32;not null;uniqueIndex:uq_user_applications_user_source_app,priority:3;index:idx_user_applications_app_id;index:idx_user_applications_user_app,priority:2"`
	AppRawID   string `gorm:"column:app_raw_id;size:32;not null;index:idx_user_applications_app_raw_id"`
	AppName    string `gorm:"column:app_name;size:100;not null"`
	AppRawName string `gorm:"column:app_raw_name;size:100;not null;index:idx_user_applications_app_raw_name"`

	// ManifestVersion stores the OlaresManifest schema version
	// (= rawData.ConfigVersion / olaresManifest.version, e.g. "0.8.10")
	// returned by chart-repo's raw_data_ex. It is NOT the app's logical
	// version — that lives in metadata->>'version' (sourced from
	// cfg.Metadata.Version) and applications.app_version. For upgrade
	// detection / candidate selection see ListRenderCandidates in
	// store/userapp.go, which compares metadata->>'version' against
	// applications.app_version. Do not reuse ManifestVersion for that.
	ManifestVersion string `gorm:"column:manifest_version;size:32;not null;default:''"`
	ManifestType    string `gorm:"column:manifest_type;size:32;not null;default:''"`
	APIVersion      string `gorm:"column:api_version;size:32"`

	// Manifest top-level blocks. We store map payloads (see
	// types.UserAppManifest / types.BuildUserAppManifest) keyed by oac's
	// json-tag shape so the columns mirror chart-repo's typed
	// raw_data_ex wire form one-to-one.
	//
	// metadata holds the OlaresManifest "metadata" block (name / title /
	// description / icon / categories / appid / version / rating /
	// target / type) populated from cfg.Metadata.
	Metadata        *JSONB[map[string]any]   `gorm:"column:metadata;type:jsonb"`
	Spec            *JSONB[map[string]any]   `gorm:"column:spec;type:jsonb"`
	Resources       *JSONB[map[string]any]   `gorm:"column:resources;type:jsonb"`
	Options         *JSONB[map[string]any]   `gorm:"column:options;type:jsonb"`
	Entrances       *JSONB[[]map[string]any] `gorm:"column:entrances;type:jsonb"`
	SharedEntrances *JSONB[[]map[string]any] `gorm:"column:shared_entrances;type:jsonb"`
	Ports           *JSONB[[]map[string]any] `gorm:"column:ports;type:jsonb"`
	Tailscale       *JSONB[map[string]any]   `gorm:"column:tailscale;type:jsonb"`
	Permission      *JSONB[map[string]any]   `gorm:"column:permission;type:jsonb"`
	Middleware      *JSONB[map[string]any]   `gorm:"column:middleware;type:jsonb"`
	Envs            *JSONB[[]map[string]any] `gorm:"column:envs;type:jsonb"`

	// Pricing & purchase live alongside the manifest data because they are
	// per-(user, app) too.
	Price        *JSONB[types.PriceConfig]  `gorm:"column:price;type:jsonb"`
	PurchaseInfo *JSONB[types.PurchaseInfo] `gorm:"column:purchase_info;type:jsonb"`

	// I18n stores chart-repo's localised metadata bundle keyed by
	// locale -> {field -> string}. Sourced directly from the
	// sync-app response top-level field, not derived from raw_data_ex.
	I18n *JSONB[map[string]map[string]string] `gorm:"column:i18n;type:jsonb"`
	// VersionHistory stores chart-repo's per-app changelog. Sourced
	// directly from the sync-app response top-level field; the
	// previous "splice into spec.versionHistory" path is gone.
	VersionHistory *JSONB[[]types.VersionInfo] `gorm:"column:version_history;type:jsonb"`
	// ImageAnalysis stores chart-repo's per-app docker image analysis
	// result (types.ImageAnalysisResult). Sourced directly from the
	// sync-app response top-level image_analysis field (sibling of
	// raw_data_ex), persisted verbatim. Pointer-typed so the failure /
	// never-rendered placeholder rows can be written without it; nil
	// here means the row predates this column's existence or the
	// upstream omitted the field.
	ImageAnalysis *JSONB[types.ImageAnalysisResult] `gorm:"column:image_analysis;type:jsonb"`

	RenderStatus              string `gorm:"column:render_status;size:16;not null;default:'pending'"`
	RenderError               string `gorm:"column:render_error;size:200"`
	RenderConsecutiveFailures int    `gorm:"column:render_consecutive_failures;not null;default:0"`

	IsUpgrade bool      `gorm:"column:is_upgrade;not null;default:false"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;default:now()"`
}

// TableName pins the table name; we use plural naming for all tables.
func (UserApplication) TableName() string { return "user_applications" }
