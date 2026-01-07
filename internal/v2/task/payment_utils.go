package task

import (
	"encoding/json"
	"fmt"

	"market/internal/v2/paymentnew"
	"market/internal/v2/settings"
	"market/internal/v2/types"

	"github.com/golang/glog"
)

// getVCFromPurchaseReceipt attempts to get VC from purchase receipt in Redis
// Returns VC string if found, empty string otherwise
func getVCFromPurchaseReceipt(settingsManager *settings.SettingsManager, userID, appID, productID, developerName string) string {
	if settingsManager == nil {
		glog.V(3).Infof("Settings manager is nil, cannot get VC from purchase receipt")
		return ""
	}

	rc := settingsManager.GetRedisClient()
	if rc == nil {
		glog.V(3).Infof("Redis client is nil, cannot get VC from purchase receipt")
		return ""
	}

	glog.V(2).Infof("getVCFromPurchaseReceipt: start lookup user=%s app=%s product=%s developer=%s", userID, appID, productID, developerName)

	// Try to get VC from payment state machine first
	if globalStateMachine := paymentnew.GetStateMachine(); globalStateMachine != nil && productID != "" {
		if state, err := globalStateMachine.LoadState(userID, appID, productID); err == nil && state != nil {
			if state.VC != "" {
				glog.V(3).Infof("Found VC from payment state for user=%s app=%s product=%s", userID, appID, productID)
				return state.VC
			}
		}
	}

	// Fallback: try to get from purchase receipt in Redis
	// Key format: payment:receipt:{userID}:{developerName}:{appID}:{productID}
	if developerName != "" && productID != "" {
		receiptKey := fmt.Sprintf("payment:receipt:%s:%s:%s:%s", userID, developerName, appID, productID)
		val, err := rc.Get(receiptKey)
		if err == nil && val != "" {
			var purchaseInfo types.PurchaseInfo
			if err := json.Unmarshal([]byte(val), &purchaseInfo); err == nil {
				if purchaseInfo.VC != "" {
					glog.V(2).Infof("Found VC from purchase receipt for user=%s app=%s product=%s", userID, appID, productID)
					return purchaseInfo.VC
				}
			} else {
				// If not JSON, assume raw VC string
				glog.V(2).Infof("Found VC (raw string) from purchase receipt for user=%s app=%s product=%s", userID, appID, productID)
				return val
			}
		}
	}

	glog.V(2).Infof("VC not found for user=%s app=%s product=%s developer=%s", userID, appID, productID, developerName)
	return ""
}

// getVCForInstall attempts to get VC for app installation
// Tries to get from payment state machine or purchase receipt
func getVCForInstall(settingsManager *settings.SettingsManager, userID, appID string, metadata map[string]interface{}) string {
	if settingsManager == nil {
		return ""
	}

	// Try to get productID and developerName from metadata
	var productID, developerName string
	if productIDData, ok := metadata["productID"].(string); ok {
		productID = productIDData
	}
	if developerNameData, ok := metadata["developerName"].(string); ok {
		developerName = developerNameData
	}

	glog.V(3).Infof("getVCForInstall: metadata snapshot user=%s app=%s productID=%s developer=%s", userID, appID, productID, developerName)

	// If we have productID, try to get from payment state machine or purchase receipt
	if productID != "" {
		return getVCFromPurchaseReceipt(settingsManager, userID, appID, productID, developerName)
	}

	// If productID is not available, try to get from payment state machine by trying appID as productID
	// This is a fallback for cases where productID might be the same as appID
	if globalStateMachine := paymentnew.GetStateMachine(); globalStateMachine != nil {
		if state, err := globalStateMachine.LoadState(userID, appID, appID); err == nil && state != nil {
			if state.VC != "" {
				glog.V(2).Infof("Found VC from payment state (using appID as productID) for user=%s app=%s", userID, appID)
				return state.VC
			}
		}
	}

	glog.V(2).Infof("VC not found for user=%s app=%s (productID not available in metadata)", userID, appID)
	return ""
}

// getVCForClone attempts to get VC for app clone using rawAppName
// Tries to get from payment state machine or purchase receipt
func getVCForClone(settingsManager *settings.SettingsManager, userID, rawAppName string, metadata map[string]interface{}) string {
	if settingsManager == nil {
		return ""
	}

	// Try to get productID and developerName from metadata
	var productID, developerName string
	if productIDData, ok := metadata["productID"].(string); ok {
		productID = productIDData
	}
	if developerNameData, ok := metadata["developerName"].(string); ok {
		developerName = developerNameData
	}

	// If we have productID, try to get from payment state machine or purchase receipt using rawAppName as appID
	if productID != "" {
		return getVCFromPurchaseReceipt(settingsManager, userID, rawAppName, productID, developerName)
	}

	// If productID is not available, try to get from payment state machine by trying rawAppName as productID
	// This is a fallback for cases where productID might be the same as rawAppName
	if globalStateMachine := paymentnew.GetStateMachine(); globalStateMachine != nil {
		if state, err := globalStateMachine.LoadState(userID, rawAppName, rawAppName); err == nil && state != nil {
			if state.VC != "" {
				glog.Infof("Found VC from payment state (using rawAppName as productID) for user=%s rawAppName=%s", userID, rawAppName)
				return state.VC
			}
		}
	}

	glog.Infof("VC not found for user=%s rawAppName=%s (productID not available in metadata)", userID, rawAppName)
	return ""
}
