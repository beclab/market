package paymentnew

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// Global state machine instance
var globalStateMachine *PaymentStateMachine

// InitStateMachine initializes the global state machine
func InitStateMachine(dataSender DataSenderInterface, settingsManager *settings.SettingsManager) {
	globalStateMachine = NewPaymentStateMachine(dataSender, settingsManager)
	log.Println("Payment state machine initialized")
}

// GetStateMachine returns the global state machine
func GetStateMachine() *PaymentStateMachine {
	return globalStateMachine
}

// PaymentStatusResult represents the result of payment status check
type PaymentStatusResult struct {
	RequiresPurchase bool   `json:"requires_purchase"`
	Status           string `json:"status"`
	Message          string `json:"message"`
	PaymentError     string `json:"payment_error,omitempty"`
}

// PurchaseApp starts a purchase flow for a given user/app/source (placeholder)
// NOTE: Implementation to be added. For now, only logs and returns nil.
func PurchaseApp(userID, appID, sourceID, xForwardedHost string, appInfo *types.AppInfo) (map[string]interface{}, error) {
	log.Printf("[PurchaseApp] user=%s app=%s source=%s", userID, appID, sourceID)

	// Extract productID from app info
	if appInfo == nil {
		return nil, fmt.Errorf("app info is nil")
	}
	productID := getProductIDFromAppInfo(appInfo)
	if productID == "" {
		return nil, fmt.Errorf("product id not found from app info")
	}

	// Locate state precisely via state_machine
	var target *PaymentState
	if globalStateMachine != nil {
		if st, err := globalStateMachine.getState(userID, appID, productID); err == nil {
			target = st
		}
	}

	if target == nil {
		return nil, fmt.Errorf("payment state not found; ensure preprocessing ran")
	}

	// Trigger start_payment event with payload (state machine will advance appropriately)
	if globalStateMachine == nil {
		return nil, fmt.Errorf("state machine not initialized")
	}
	if err := globalStateMachine.processEvent(
		context.Background(),
		userID,
		appID,
		productID,
		"start_payment",
		map[string]interface{}{
			"x_forwarded_host": xForwardedHost,
		},
	); err != nil {
		return nil, fmt.Errorf("failed to process start_payment event: %w", err)
	}

	// Read latest state to decide response
	latest, _ := globalStateMachine.getState(userID, appID, productID)
	if latest == nil {
		latest = target
	}

	// Delegate response building to state machine for consistency
	return globalStateMachine.buildPurchaseResponse(userID, xForwardedHost, latest)
}

// GetPaymentStatus returns payment status inferred from PaymentState directly
// Params and return are the same as ProcessAppPaymentStatus
func GetPaymentStatus(userID, appID, sourceID string, appInfo *types.AppInfo) (*PaymentStatusResult, error) {
	if appInfo == nil {
		return nil, fmt.Errorf("app info is nil")
	}

	// Extract productID from app info
	productID := getProductIDFromAppInfo(appInfo)
	requiresPurchase := productID != "" // 是否为付费应用
	if !requiresPurchase {
		return &PaymentStatusResult{RequiresPurchase: false, Status: "not_evaluated", Message: "No payment required"}, nil
	}

	// Find state
	var state *PaymentState
	if globalStateMachine != nil {
		s, err := globalStateMachine.getState(userID, appID, productID)
		if err == nil {
			state = s
		}
	}

	if state == nil {
		// 需要购买但尚未开始任何流程，标记为 not_buy
		return &PaymentStatusResult{RequiresPurchase: true, Status: "not_buy", Message: "Payment not started"}, nil
	}

	// If DeveloperSync or LarePassSync not completed, trigger a sync once (reentrant)
	if !(state.DeveloperSync == DeveloperSyncCompleted && state.LarePassSync == LarePassSyncCompleted) {
		_ = triggerPaymentStateSync(state)
	}

	status := buildPaymentStatusFromState(state)
	result := &PaymentStatusResult{RequiresPurchase: true, Status: status}

	switch status {
	case "purchased":
		result.Message = "App is already purchased"
	case string(PaymentFrontendCompleted):
		result.Message = "Payment completed on frontend, waiting for developer confirmation"
	case string(PaymentNotificationSent):
		result.Message = "Payment notification sent"
	case string(PaymentNotNotified):
		result.Message = "Payment not started"
	// Reserved/compatibility: not produced by current flows, kept for forward/backward compatibility
	case "not_buy":
		result.Message = "Payment not started"
	default:
		result.Message = "Payment status updated"
	}

	return result, nil
}

// ProcessSignatureSubmission handles the business logic for signature submission
func ProcessSignatureSubmission(jws, signBody, user, xForwardedHost string) error {
	log.Printf("=== Payment State Machine Processing Signature Submission ===")
	log.Printf("JWS: %s", jws)
	log.Printf("SignBody: %s", signBody)
	log.Printf("User: %s", user)
	log.Printf("X-Forwarded-Host: %s", xForwardedHost)

	if globalStateMachine == nil {
		log.Printf("State machine not initialized, falling back to basic processing")
		log.Printf("=== End of Payment State Machine Processing ===")
		return nil
	}

	// Step 1: Parse productID from signBody
	productID, err := parseProductIDFromSignBody(signBody)
	if err != nil {
		log.Printf("Failed to parse productID from signBody: %v", err)
		return fmt.Errorf("failed to parse productID from signBody: %w", err)
	}

	// Step 2: Find PaymentState via user + productID
	state := globalStateMachine.findStateByUserAndProduct(user, productID)
	if state == nil {
		log.Printf("PaymentState not found for user %s and productID %s", user, productID)
		return fmt.Errorf("payment state not found for user %s and productID %s", user, productID)
	}

	// Step 3: Update status to SignatureRequiredAndSigned
	if err := globalStateMachine.updateState(state.GetKey(), func(s *PaymentState) error {
		s.SignatureStatus = SignatureRequiredAndSigned
		s.JWS = jws
		return nil
	}); err != nil {
		log.Printf("Failed to update signature status: %v", err)
		return fmt.Errorf("failed to update signature status: %w", err)
	}

	// Step 3.1: Read latest state; if already at a later stage, skip duplicate notification (idempotent guard)
	latest, _ := globalStateMachine.getState(state.UserID, state.AppID, state.ProductID)
	if latest != nil {
		if latest.PaymentStatus == PaymentNotificationSent ||
			latest.PaymentStatus == PaymentFrontendCompleted ||
			latest.PaymentStatus == PaymentDeveloperConfirmed {
			log.Printf("Skip notifying payment_required: current status=%s", latest.PaymentStatus)
			log.Printf("=== End of Payment State Machine Processing ===")
			return nil
		}
	}

	// Step 4: Notify frontend to pay (only when not notified/early stage)
	if globalStateMachine.dataSender != nil {
		if err := notifyFrontendPaymentRequired(
			globalStateMachine.dataSender,
			state.UserID,
			state.AppID,
			state.AppName,
			state.SourceID,
			state.ProductID,
			state.Developer.DID,
			xForwardedHost,
		); err != nil {
			log.Printf("Failed to notify frontend payment required: %v", err)
			return fmt.Errorf("failed to notify frontend payment required: %w", err)
		}

		// Step 4.1: After notification success, advance status to notification_sent (idempotent, no rollback)
		_ = globalStateMachine.updateState(state.GetKey(), func(s *PaymentState) error {
			switch s.PaymentStatus {
			case PaymentNotEvaluated, PaymentNotNotified:
				s.PaymentStatus = PaymentNotificationSent
			}
			return nil
		})
	}

	log.Printf("=== End of Payment State Machine Processing ===")
	return nil
}

// HandleFetchSignatureCallback handles fetch-signature callback (for new endpoint)
func HandleFetchSignatureCallback(jws, signBody, user string, code int) error {
	log.Printf("=== Payment State Machine Processing Fetch Signature Callback ===")
	log.Printf("JWS: %s", jws)
	log.Printf("SignBody: %s", signBody)
	log.Printf("User: %s", user)

	if globalStateMachine == nil {
		log.Printf("State machine not initialized, skipping fetch signature callback")
		return nil
	}

	// Delegate to state_machine (placeholder implementation)
	if err := globalStateMachine.processFetchSignatureCallback(jws, signBody, user, code); err != nil {
		log.Printf("Failed to process fetch signature callback: %v", err)
		return err
	}

	log.Printf("=== End of Fetch Signature Callback Processing ===")
	return nil
}

// StartPaymentPolling starts polling for VC after payment completion
func StartPaymentPolling(userID, sourceID, appID, txHash, xForwardedHost string, systemChainID int, appInfoLatest *types.AppInfoLatestData) error {
	log.Printf("=== Starting Payment Polling ===")
	log.Printf("User ID: %s", userID)
	log.Printf("Source ID: %s", sourceID)
	log.Printf("App ID: %s", appID)
	log.Printf("TxHash: %s", txHash)
	log.Printf("SystemChainID: %d", systemChainID)
	log.Printf("X-Forwarded-Host: %s", xForwardedHost)

	if appInfoLatest != nil && appInfoLatest.RawData != nil {
		if appInfoLatest.RawData.Name != "" {
			log.Printf("App Name: %s", appInfoLatest.RawData.Name)
		}
		if appInfoLatest.RawData.Developer != "" {
			log.Printf("Developer: %s", appInfoLatest.RawData.Developer)
		}
	}

	// Check if state machine is initialized
	if globalStateMachine == nil {
		log.Printf("State machine not initialized, cannot start payment polling")
		return fmt.Errorf("state machine not initialized")
	}

	// Get product ID from app price information
	var productID string
	if appInfoLatest != nil && appInfoLatest.AppInfo != nil {
		productID = getProductIDFromAppInfo(appInfoLatest.AppInfo)
	}

	if productID == "" {
		log.Printf("Product ID not found, using empty string")
		productID = ""
	}

	// Not computing app/developer names here

	// 获取已存在状态
	_, err := globalStateMachine.getState(userID, appID, productID)
	if err != nil {
		log.Printf("Failed to get state for polling: %v", err)
		return fmt.Errorf("payment state not found; ensure preprocessing ran: %w", err)
	}

	// Update state with payment info
	key := fmt.Sprintf("%s:%s:%s", userID, appID, productID)
	if err := globalStateMachine.updateState(key, func(s *PaymentState) error {
		s.TxHash = txHash
		s.SystemChainID = systemChainID
		s.XForwardedHost = xForwardedHost
		return nil
	}); err != nil {
		log.Printf("Failed to update state: %v", err)
		return fmt.Errorf("failed to update state: %w", err)
	}

	// Process payment_completed event to trigger polling
	if err := globalStateMachine.processEvent(
		context.Background(),
		userID,
		appID,
		productID,
		"payment_completed",
		map[string]interface{}{
			"tx_hash":         txHash,
			"system_chain_id": systemChainID,
		},
	); err != nil {
		log.Printf("Failed to process payment_completed event: %v", err)
		return fmt.Errorf("failed to process payment_completed event: %w", err)
	}

	log.Printf("Payment polling started for user %s, app %s", userID, appID)
	return nil
}

// ListPaymentStates returns all current states (for debugging/monitoring)
func ListPaymentStates() map[string]*PaymentState {
	if globalStateMachine == nil {
		return nil
	}

	globalStateMachine.mu.RLock()
	defer globalStateMachine.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]*PaymentState)
	for key, state := range globalStateMachine.states {
		result[key] = state
	}
	return result
}

// PreprocessAppPaymentData preprocesses app payment data with state bootstrap and optional receipt load
func PreprocessAppPaymentData(ctx context.Context, appInfo *types.AppInfo, userID, sourceID string, settingsManager *settings.SettingsManager, client *resty.Client) (*types.PurchaseInfo, error) {
	// Step 0: Basic validation and quick exit
	if appInfo == nil || appInfo.Price == nil {
		log.Printf("INFO: no payment section, skip")
		return nil, nil
	}

	// Step 1: Get productID
	productID := getProductIDFromAppInfo(appInfo)
	if productID == "" {
		// No valid productID -> return directly, no further processing
		log.Printf("INFO: productID not found, skip state init")
		return nil, nil
	}

	// Extract basic app information
	appID := ""
	appName := ""
	if appInfo.AppEntry != nil {
		appID = appInfo.AppEntry.ID
		appName = appInfo.AppEntry.Name
	}

	// Step 2: Obtain PaymentStates via productID (prefer state machine; empty if miss)
	if globalStateMachine == nil {
		return nil, fmt.Errorf("state machine not initialized")
	}
	var state *PaymentState
	if s, err := globalStateMachine.LoadState(userID, appID, productID); err == nil {
		state = s
	}

	// Step 3: If not exists, query developer info and create new PaymentStates
	if state == nil {
		developerName := getDeveloperNameFromPrice(appInfo)

		if client == nil {
			client = resty.New()
		}
		client.SetTimeout(3 * time.Second)

		// If developerName cannot be obtained from price, create a failure state and return error
		if developerName == "" {
			failedState := &PaymentState{
				UserID:          userID,
				AppID:           appID,
				AppName:         appName,
				SourceID:        sourceID,
				ProductID:       productID,
				DeveloperName:   "",
				Developer:       DeveloperInfo{},
				PaymentNeed:     PaymentNeedErrorMissingDeveloper,
				DeveloperSync:   DeveloperSyncFailed,
				LarePassSync:    LarePassSyncNotStarted,
				SignatureStatus: SignatureNotEvaluated,
				PaymentStatus:   PaymentNotEvaluated,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			}
			if err := globalStateMachine.SaveState(failedState); err != nil {
				return nil, fmt.Errorf("save payment state failed: %w", err)
			}
			return nil, fmt.Errorf("developer name missing in price")
		}

		// 查询开发者信息（仅为校验与状态初始化所需字段）
		dev, err := fetchDidInfo(ctx, client, developerName)
		if err != nil {
			// When DID lookup fails, also mark DeveloperSync as failed
			failedState := &PaymentState{
				UserID:          userID,
				AppID:           appID,
				AppName:         appName,
				SourceID:        sourceID,
				ProductID:       productID,
				DeveloperName:   developerName,
				Developer:       DeveloperInfo{Name: "", DID: developerName, RSAPubKey: ""},
				PaymentNeed:     PaymentNeedErrorDeveloperFetchFailed,
				DeveloperSync:   DeveloperSyncFailed,
				LarePassSync:    LarePassSyncNotStarted,
				SignatureStatus: SignatureNotEvaluated,
				PaymentStatus:   PaymentNotEvaluated,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			}
			if err := globalStateMachine.SaveState(failedState); err != nil {
				return nil, fmt.Errorf("save payment state failed: %w", err)
			}
			return nil, fmt.Errorf("fetch developer info failed: %w", err)
		}

		// Initialize and save a new PaymentState (SourceID provided externally)
		newState := &PaymentState{
			UserID:          userID,
			AppID:           appID,
			AppName:         appName,
			SourceID:        sourceID,
			ProductID:       productID,
			DeveloperName:   developerName,
			Developer:       *dev,
			PaymentNeed:     PaymentNeedRequired,
			DeveloperSync:   DeveloperSyncNotStarted,
			LarePassSync:    LarePassSyncNotStarted,
			SignatureStatus: SignatureNotEvaluated,
			PaymentStatus:   PaymentNotEvaluated,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}

		if err := globalStateMachine.SaveState(newState); err != nil {
			return nil, fmt.Errorf("save payment state failed: %w", err)
		}
		globalStateMachine.setState(newState)
		state = newState
	}

	// Step 4: Trigger sync flow for PaymentStates (must be reentrant)
	// Note: Regardless of new or existing state, always trigger a sync; implementation must be idempotent/reentrant
	_ = triggerPaymentStateSync(state)

	// Build PurchaseInfo from PaymentState (return nil if state does not exist)
	pi := buildPurchaseInfoFromState(state)
	if pi != nil && strings.EqualFold(pi.Status, "purchased") {
		ok := verifyPurchaseInfo(pi)
		if !ok {
			return nil, fmt.Errorf("purchase info not verified for user=%s app=%s product=%s", userID, appID, productID)
		}
	}
	return pi, nil
}
