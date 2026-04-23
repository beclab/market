package models

import (
	"time"

	"market/internal/v2/types"
)

// UserApplication mirrors the user_applications table.
type UserApplication struct {
	ID               int64                              `gorm:"column:id;primaryKey;autoIncrement"`
	UserID           string                             `gorm:"column:user_id;size:120;not null;uniqueIndex:uq_user_applications_user_app,priority:1;index:idx_user_applications_user_id"`
	ApplicationID    int64                              `gorm:"column:application_id;not null;uniqueIndex:uq_user_applications_user_app,priority:2;index:idx_user_applications_application_id"`
	AppRawData       *JSONB[types.ApplicationInfoEntry] `gorm:"column:app_raw_data;type:jsonb"`
	AppImageAnalysis *JSONB[types.ImageAnalysisResult]  `gorm:"column:app_image_analysis;type:jsonb"`
	Price            *JSONB[types.PriceConfig]          `gorm:"column:price;type:jsonb"`
	PurchaseInfo     *JSONB[types.PurchaseInfo]         `gorm:"column:purchase_info;type:jsonb"`
	IsUpgrade        bool                               `gorm:"column:is_upgrade;not null;default:false"`
	CreatedAt        time.Time                          `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt        time.Time                          `gorm:"column:updated_at;not null;default:now()"`

	Application *Application `gorm:"foreignKey:ApplicationID;references:ID;constraint:OnDelete:CASCADE"`
}

// TableName pins the table name; we use plural naming for all tables.
func (UserApplication) TableName() string { return "user_applications" }
