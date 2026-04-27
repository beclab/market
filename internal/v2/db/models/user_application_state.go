package models

import (
	"time"

	"market/internal/v2/db/models/payload"
)

// UserApplicationState mirrors the user_application_states table: per-user
// installation runtime, paired 1:1 with user_applications via
// user_application_id.
//
// state / reason / message / progress are stored verbatim from app-service
// NATS messages; the database does not constrain their values, so the
// application layer is responsible for valid transitions.
type UserApplicationState struct {
	ID                int64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserApplicationID int64 `gorm:"column:user_application_id;not null;uniqueIndex:uq_user_application_states_ua"`

	InstalledVersion string `gorm:"column:installed_version;size:32"`
	TargetVersion    string `gorm:"column:target_version;size:32"`
	IsSysApp         bool   `gorm:"column:is_sys_app;not null;default:false"`

	State    string `gorm:"column:state;size:64;not null;default:'';index:idx_user_application_states_state"`
	Reason   string `gorm:"column:reason;size:200;not null;default:''"`
	Message  string `gorm:"column:message;type:text;not null;default:''"`
	Progress string `gorm:"column:progress;size:10;not null;default:''"`

	// Runtime data delivered by app-service callbacks; entrances/shared_entrances
	// carry the URLs assigned by the cluster, status_entrances carries per-
	// entrance health (status.entranceStatuses).
	Entrances       *JSONB[[]payload.RuntimeEntrance] `gorm:"column:entrances;type:jsonb"`
	SharedEntrances *JSONB[[]payload.RuntimeEntrance] `gorm:"column:shared_entrances;type:jsonb"`
	StatusEntrances *JSONB[[]payload.EntranceStatus]  `gorm:"column:status_entrances;type:jsonb"`

	CreatedAt time.Time `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;default:now()"`

	UserApplication *UserApplication `gorm:"foreignKey:UserApplicationID;references:ID;constraint:OnDelete:CASCADE"`
}

// TableName pins the table name; we use plural naming for all tables.
func (UserApplicationState) TableName() string { return "user_application_states" }
