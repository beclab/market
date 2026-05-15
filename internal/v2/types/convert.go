package types

// ConvertSystemAppToAppLatest projects a sys-app AppServiceResponse
// into the AppStateLatestData wire shape consumed by /market/data.
//
// AppServiceResponse comes from unmarshaling upstream JSON, so every
// pointer field (Spec, Spec.Settings, Status) may legitimately be
// nil. A single malformed entry from app-service would otherwise
// panic the entire SCC reconcile goroutine.
func ConvertSystemAppToAppLatest(app *AppServiceResponse) *AppStateLatestData {
	if app == nil || app.Spec == nil {
		return nil
	}

	spec := &AppStateLatestDataSpec{
		AppStateLatestDataSpecMetadata: app.Spec.AppStateLatestDataSpecMetadata,
		EntranceStatuses:               app.Spec.EntranceStatuses,
		SharedEntrances:                app.Spec.SharedEntrances,
		Settings:                       app.Spec.Settings,
	}

	spec.RawAppName = app.Spec.Name
	var version string
	if app.Spec.Settings != nil {
		spec.Title = app.Spec.Settings.Title
		version = app.Spec.Settings.Version
	}

	if app.Status != nil {
		spec.State = app.Status.State
		spec.StatusTime = app.Status.StatusTime
		spec.UpdateTime = app.Status.UpdateTime
		spec.LastTransitionTime = app.Status.LastTransitionTime
	}

	return &AppStateLatestData{
		Type:    "app-state-latest",
		Version: version,
		Status:  spec,
	}
}
