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

// BuildStateResult is the single wrapper entry to build state result payload.
func BuildStateResult(input StateResultInput) MarketStateResponse {
	switch input.Scene {
	case StateResultSceneCurrentApps:
		return buildCurrentAppsStateResult(input)
	case StateResultSceneCrossUserClones:
		return buildCrossUserClonesStateResult(input)
	default:
		// Defensive fallback to preserve compatibility for unknown scenes.
		return buildCurrentAppsStateResult(input)
	}
}

func buildCurrentAppsStateResult(input StateResultInput) MarketStateResponse {
	return buildStateResultBase(input.ViewerUserID, input.Sources, input.Hash, input.Timestamp)
}

func buildCrossUserClonesStateResult(input StateResultInput) MarketStateResponse {
	// Keep current business semantics: clones response does not expose hash.
	return buildStateResultBase(input.ViewerUserID, input.Sources, "", input.Timestamp)
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
