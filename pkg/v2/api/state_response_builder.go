package api

import "time"

// StateResultScene represents the business scene of state response.
type StateResultScene string

const (
	// StateResultSceneCurrentApps means state for current user's apps.
	StateResultSceneCurrentApps StateResultScene = "current_apps"
	// StateResultSceneCrossUserClones means state for clones owned by other users.
	StateResultSceneCrossUserClones StateResultScene = "cross_user_clones"
)

// StateResultInput is the unified input for building state response payloads.
type StateResultInput struct {
	Scene        StateResultScene
	ViewerUserID string
	Sources      map[string]*FilteredSourceDataForState
	Hash         string
	Timestamp    int64
}

// CurrentAppsStateView is the business view for /market/state.
type CurrentAppsStateView struct {
	ViewerUserID string
	Hash         string
	Sources      map[string]*FilteredSourceDataForState
	Timestamp    int64
}

// CrossUserClonesStateView is the business view for /apps/clones.
type CrossUserClonesStateView struct {
	ViewerUserID string
	Sources      map[string]*FilteredSourceDataForState
	Timestamp    int64
}

// BuildCurrentAppsStateResponse builds state response for current user's apps.
func BuildCurrentAppsStateResponse(view CurrentAppsStateView) MarketStateResponse {
	return BuildStateResult(StateResultInput{
		Scene:        StateResultSceneCurrentApps,
		ViewerUserID: view.ViewerUserID,
		Sources:      view.Sources,
		Hash:         view.Hash,
		Timestamp:    view.Timestamp,
	})
}

// BuildCrossUserClonesStateResponse builds state response for cross-user clone apps.
func BuildCrossUserClonesStateResponse(view CrossUserClonesStateView) MarketStateResponse {
	return BuildStateResult(StateResultInput{
		Scene:        StateResultSceneCrossUserClones,
		ViewerUserID: view.ViewerUserID,
		Sources:      view.Sources,
		Timestamp:    view.Timestamp,
	})
}

// BuildStateResult is the single wrapper entry to build state result payload.
func BuildStateResult(input StateResultInput) MarketStateResponse {
	sources := input.Sources
	if sources == nil {
		sources = map[string]*FilteredSourceDataForState{}
	}

	timestamp := input.Timestamp
	if timestamp <= 0 {
		timestamp = time.Now().Unix()
	}

	hash := ""
	switch input.Scene {
	case StateResultSceneCurrentApps:
		hash = input.Hash
	case StateResultSceneCrossUserClones:
		// Keep current business semantics: clones response does not expose hash.
		hash = ""
	default:
		// Defensive fallback to preserve compatibility for unknown scenes.
		hash = input.Hash
	}

	return MarketStateResponse{
		UserData: &FilteredUserDataForState{
			Sources: sources,
			Hash:    hash,
		},
		UserID:    input.ViewerUserID,
		Timestamp: timestamp,
	}
}
