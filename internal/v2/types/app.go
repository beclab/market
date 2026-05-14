package types

type AppServiceResponse struct { // + app states
	Metadata *AppServiceResponseMetadata `json:"metadata"`
	Spec     *AppServiceResponseSpec     `json:"spec"`
	Status   *AppStateLatestDataStatus   `json:"status"`
}

type AppServiceResponseMetadata struct {
	Name      string `json:"name"`
	UID       string `json:"uid"`
	Namespace string `json:"namespace"`
}

type AppServiceResponseSpec struct { // + app state
	AppStateLatestDataSpecMetadata
	EntranceStatuses []AppStateLatestDataEntrances `json:"entrances"`
	SharedEntrances  []AppStateLatestDataEntrances `json:"sharedEntrances,omitempty"`
	Tailscale        map[string]interface{}        `json:"tailscale,omitempty"`
	Settings         *AppStateLatestDataSettings   `json:"settings,omitempty"`
}

type MiddlewareStatusResponse struct {
	Code int                            `json:"code"`
	Data []MiddlewareStatusResponseData `json:"data"`
}

type MiddlewareStatusResponseData struct {
	UUID           string                           `json:"uuid"`
	Namespace      string                           `json:"namespace"`
	User           string                           `json:"user"`
	ResourceStatus string                           `json:"resourceStatus"`
	ResourceType   string                           `json:"resourceType"`
	CreateTime     string                           `json:"createTime"`
	UpdateTime     string                           `json:"updateTime"`
	Metadata       MiddlewareStatusResponseMetadata `json:"metadata"`
	Version        string                           `json:"version"`
	Title          string                           `json:"title"`
	MarketSource   string                           `json:"market_source"`
}

type MiddlewareStatusResponseMetadata struct {
	Name string `json:"name"`
}
