package models

type InstallOptions struct {
	App     string `json:"appName,omitempty"`
	Dev     bool   `json:"devMode,omitempty"`
	RepoUrl string `json:"repoUrl,omitempty"`
	CfgUrl  string `json:"cfgUrl,omitempty"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

type ProviderRequestBase struct {
	Op       string      `json:"op"`
	DataType string      `json:"datatype"`
	Version  string      `json:"version"`
	Group    string      `json:"group"`
	Param    interface{} `json:"param,omitempty"`
	Token    string      `json:"token"`
}

type InstallProviderRequest struct {
	ProviderRequestBase
	Data *InstallOptions `json:"data,omitempty"`
}

type UninstallData struct {
	Name string `json:"name"`
}

type UninstallProviderRequest struct {
	ProviderRequestBase
	Data *UninstallData `json:"data,omitempty"`
}
