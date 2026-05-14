package types

func ConvertSystemAppToAppLatest(app *AppServiceResponse) *AppStateLatestData {
	var spec = &AppStateLatestDataSpec{
		AppStateLatestDataSpecMetadata: app.Spec.AppStateLatestDataSpecMetadata,
		EntranceStatuses:               app.Spec.EntranceStatuses,
		SharedEntrances:                app.Spec.SharedEntrances,
		Settings:                       app.Spec.Settings,
	}

	spec.Title = app.Spec.Title
	spec.RawAppName = app.Spec.RawAppName
	spec.State = app.Status.State
	spec.StatusTime = app.Status.StatusTime
	spec.UpdateTime = app.Status.UpdateTime
	spec.LastTransitionTime = app.Status.LastTransitionTime

	var res = &AppStateLatestData{
		Type:    "app-state-latest",
		Version: app.Spec.Settings.Version,
		Status:  spec,
	}

	return res
}
