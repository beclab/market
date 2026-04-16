package api

import (
	"strings"
	"time"

	"market/internal/v2/types"
	"market/internal/v2/utils"
)

type CurrentAppsStateInput struct {
	ViewerUserID string
	Sources      map[string]*FilteredSourceDataForState
	Hash         string
	Timestamp    int64
}

type CrossUserClonesStateInput struct {
	ViewerUserID string
	CloneAppName string
	CloneApps    []*utils.AppServiceResponse
	Timestamp    int64
}

func BuildCurrentAppsStateResult(input CurrentAppsStateInput) MarketStateResponse {
	return buildStateResultBase(input.ViewerUserID, input.Sources, input.Hash, input.Timestamp)
}

func BuildCrossUserClonesStateResult(input CrossUserClonesStateInput) MarketStateResponse {
	sources := buildCrossUserCloneSources(input.ViewerUserID, input.CloneAppName, input.CloneApps)

	// Keep current business semantics: clones response does not expose hash.
	return buildStateResultBase(input.ViewerUserID, sources, "", input.Timestamp)
}

func buildCrossUserCloneSources(
	viewerUserID string,
	targetRawAppName string,
	cloneApps []*utils.AppServiceResponse,
) map[string]*FilteredSourceDataForState {
	sources := make(map[string]*FilteredSourceDataForState)

	for _, app := range cloneApps {
		if app == nil || app.Status == nil {
			continue
		}
		if !isOtherAdminCloneApp(app, viewerUserID, targetRawAppName) {
			continue
		}

		stateData := buildCloneAppStateData(app)
		sourceID := app.Spec.Settings.MarketSource

		if sources[sourceID] == nil {
			sources[sourceID] = &FilteredSourceDataForState{
				Type:           types.SourceDataTypeRemote,
				AppStateLatest: make([]*types.AppStateLatestData, 0),
			}
		}
		sources[sourceID].AppStateLatest = append(sources[sourceID].AppStateLatest, stateData)
	}

	return sources
}

// isOtherAdminCloneApp checks whether the app is a clone instance
// owned by another admin for the given target raw app name.
func isOtherAdminCloneApp(
	app *utils.AppServiceResponse,
	viewerUserID string,
	targetRawAppName string,
) bool {
	if app == nil || app.Spec == nil {
		return false
	}
	if app.Spec.Owner == viewerUserID {
		return false
	}
	if !strings.HasPrefix(strings.TrimSpace(app.Spec.Settings.MarketSource), types.AppMarketSourcePrefix) {
		return false
	}
	if app.Spec.RawAppName != targetRawAppName {
		return false
	}
	if app.Spec.Name == app.Spec.RawAppName {
		return false
	}
	return true
}

func buildCloneAppStateData(app *utils.AppServiceResponse) *types.AppStateLatestData {
	specEntrance := make(map[string]types.AppStateLatestDataEntrances)
	for _, entrance := range app.Spec.EntranceStatuses {
		specEntrance[entrance.Name] = entrance
	}

	stateData := &types.AppStateLatestData{
		Type:    types.AppStateLatest,
		Version: app.Spec.Settings.Version,
		Status: &types.AppStateLatestDataSpec{
			AppStateLatestDataSpecMetadata: types.AppStateLatestDataSpecMetadata{
				Name:               app.Spec.Name,
				RawAppName:         app.Spec.RawAppName,
				Title:              app.Spec.Settings.Title,
				State:              app.Status.State,
				UpdateTime:         app.Status.UpdateTime,
				StatusTime:         app.Status.StatusTime,
				LastTransitionTime: app.Status.LastTransitionTime,
			},
		},
	}

	for _, es := range app.Status.EntranceStatuses {
		specEs := specEntrance[es.Name]
		url := specEs.Url
		id := ""
		if url != "" {
			if parts := strings.Split(url, "."); len(parts) > 0 {
				id = parts[0]
			}
		}

		stateData.Status.EntranceStatuses = append(stateData.Status.EntranceStatuses, types.AppStateLatestDataEntrances{
			ID:         id,
			Name:       es.Name,
			Host:       specEs.Host,
			Port:       specEs.Port,
			Icon:       specEs.Icon,
			Title:      specEs.Title,
			AuthLevel:  specEs.AuthLevel,
			State:      es.State,
			StatusTime: es.StatusTime,
			Reason:     es.Reason,
			Url:        specEs.Url,
			Invisible:  specEs.Invisible,
		})
	}

	if len(app.Spec.SharedEntrances) > 0 {
		stateData.Status.SharedEntrances = append(stateData.Status.SharedEntrances, app.Spec.SharedEntrances...)
	}

	return stateData
}

func buildStateResultBase(
	viewerUserID string,
	sources map[string]*FilteredSourceDataForState,
	hash string,
	timestamp int64,
) MarketStateResponse {
	if sources == nil {
		sources = map[string]*FilteredSourceDataForState{}
	}

	if timestamp <= 0 {
		timestamp = time.Now().Unix()
	}

	return MarketStateResponse{
		UserData: &FilteredUserDataForState{
			Sources: sources,
			Hash:    hash,
		},
		UserID:    viewerUserID,
		Timestamp: timestamp,
	}
}
