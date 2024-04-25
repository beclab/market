package models

type UpgradeOptions struct {
	CfgURL  string `json:"cfgURL,omitempty"`
	RepoURL string `json:"repoURL"`
	Version string `json:"version"`
	Source  string `json:"source"`
}

type VersionData struct {
	Version string `json:"version"`
}

type VersionResponse struct {
	ResponseBase
	Data VersionData `json:"data"`
}
