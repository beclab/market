package models

import "time"

type VersionInfo struct {
	ID                 string     `bson:"_id,omitempty" json:"-"`
	AppName            string     `bson:"appName" json:"appName"`
	Version            string     `bson:"version" json:"version"`
	VersionName        string     `bson:"versionName" json:"versionName"`
	MergedAt           *time.Time `bson:"mergedAt" json:"mergedAt"`
	UpgradeDescription string     `bson:"upgradeDescription" json:"upgradeDescription"`
}

type ListVersionResponse struct {
	ResponseBase
	Data []*VersionInfo `json:"data"`
}
