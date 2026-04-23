package models

import (
	"time"

	"market/internal/v2/db/models/payload"
)

// UserApplicationState mirrors the user_application_states table.
type UserApplicationState struct {
	ID                int64                              `gorm:"column:id;primaryKey;autoIncrement"`
	UserApplicationID int64                              `gorm:"column:user_application_id;not null;uniqueIndex:uq_user_application_states_ua;index:idx_user_application_states_ua"`
	AppVersion        string                             `gorm:"column:app_version;size:16;not null"`
	State             string                             `gorm:"column:state;size:64;not null;default:'';index:idx_user_application_states_state"`
	Reason            string                             `gorm:"column:reason;size:200;not null;default:''"`
	Message           string                             `gorm:"column:message;size:200;not null;default:''"`
	Progress          string                             `gorm:"column:progress;size:10;not null;default:''"`
	Spec              *JSONB[payload.UserAppStateSpec]   `gorm:"column:spec;type:jsonb"`
	Status            *JSONB[payload.UserAppStateStatus] `gorm:"column:status;type:jsonb"`
	CreatedAt         time.Time                          `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt         time.Time                          `gorm:"column:updated_at;not null;default:now()"`

	UserApplication *UserApplication `gorm:"foreignKey:UserApplicationID;references:ID;constraint:OnDelete:CASCADE"`
}

// TableName pins the table name; we use plural naming for all tables.
func (UserApplicationState) TableName() string { return "user_application_states" }
