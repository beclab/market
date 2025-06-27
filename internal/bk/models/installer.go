package models

// Application for install apps info
type Application struct {
	Spec ApplicationSpec `json:"spec,omitempty"`
}

type InstallationResponse struct {
	ResponseBase
	Data InstallationResponseData `json:"data"`
}

type ApplicationSpec struct {
	// the name of the application
	Name     string              `json:"name"`
	Settings ApplicationSettings `json:"settings,omitempty"`

	// The url of the icon
	Icon string `json:"icon,omitempty"`

	// the unique id of the application
	// for sys application appid equal name otherwise appid equal md5(name)[:8]
	Appid string `json:"appid"`

	// the namespace of the application
	Namespace string `json:"namespace,omitempty"`
}

type ApplicationSettings struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type InstallationResponseData struct {
	UID string `json:"uid"`
}

type InstallationStatusRespData struct {
	UID    string `json:"uid"`
	Status string `json:"status,omitempty"`
	Msg    string `json:"message,omitempty"`
}

type InstallationStatusResp struct {
	ResponseBase
	Data InstallationStatusRespData `json:"data"`
}

type StatusListResp struct {
	ResponseBase
	Data []InstallationStatusRespData `json:"data"`
}

type DepRequest struct {
	Data []Dependency `json:"data"`
}

type DependenciesResp struct {
	Response
	Data []Dependency `json:"data"`
}
