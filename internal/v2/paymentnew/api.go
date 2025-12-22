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
	RequiresPurchase bool                   `json:"requires_purchase"`
	Status           string                 `json:"status"`
	Message          string                 `json:"message"`
	PaymentError     string                 `json:"payment_error,omitempty"`
	FrontendData     map[string]interface{} `json:"frontend_data,omitempty"`
}

// PurchaseApp starts a purchase flow for a given user/app/source (placeholder)
// NOTE: Implementation to be added. For now, only logs and returns nil.
func PurchaseApp(userID, appID, sourceID, xForwardedHost string, appInfo *types.AppInfo) (map[string]interface{}, error) {
	log.Printf("[PurchaseApp] user=%s app=%s source=%s", userID, appID, sourceID)

	// Extract productID from app info with correct priority
	if appInfo == nil {
		return nil, fmt.Errorf("app info is nil")
	}

	// Use real app ID from AppEntry, not the URL parameter (which might be name)
	realAppID := appID
	if appInfo.AppEntry != nil && appInfo.AppEntry.ID != "" {
		realAppID = appInfo.AppEntry.ID
		log.Printf("PurchaseApp: Using real app ID from AppEntry: %s (URL param was: %s)", realAppID, appID)
	}

	var productID string
	if appInfo.Price != nil && appInfo.Price.Paid != nil {
		if appInfo.Price.Paid.ProductID != "" {
			productID = appInfo.Price.Paid.ProductID
			log.Printf("PurchaseApp: Using productID from Price.Paid: %s", productID)
		}
	}
	if productID == "" {
		productID = getProductIDFromAppInfo(appInfo)
		if productID != "" {
			log.Printf("PurchaseApp: Using productID from Products: %s", productID)
		}
	}
	if productID == "" {
		// Final fallback: for paid apps without explicit product_id
		productID = realAppID
		log.Printf("PurchaseApp: productID not found in price, using real appID as fallback: %s", productID)
	}

	// Locate state precisely via state_machine (use realAppID, not URL param)
	var target *PaymentState
	if globalStateMachine != nil {
		// Try getState first (memory only, same as GetPaymentStatus)
		if st, err := globalStateMachine.getState(userID, realAppID, productID); err == nil {
			target = st
			log.Printf("PurchaseApp: Found state in memory for user=%s app=%s productID=%s", userID, realAppID, productID)
		} else {
			// Try LoadState (will check Redis and load to memory)
			log.Printf("PurchaseApp: State not in memory, trying LoadState from Redis. Error: %v", err)
			if st, err := globalStateMachine.LoadState(userID, realAppID, productID); err == nil {
				target = st
				log.Printf("PurchaseApp: Found state in Redis and loaded to memory for user=%s app=%s productID=%s", userID, realAppID, productID)
			} else {
				log.Printf("PurchaseApp: State not found in Redis either. Error: %v", err)
			}
		}
	}

	if target == nil {
		// State not found - likely because:
		// 1. Program restarted and state was not loaded from Redis
		// 2. Preprocessing ran before the productID fix, using wrong key
		// 3. Preprocessing hasn't run yet
		// Try to create state via preprocessing now
		log.Printf("PurchaseApp: Payment state not found for user=%s app=%s productID=%s. Attempting to create state via preprocessing.", userID, realAppID, productID)

		// Get settingsManager from state machine
		if globalStateMachine == nil || globalStateMachine.settingsManager == nil {
			return nil, fmt.Errorf("state machine or settings manager not initialized; cannot create payment state")
		}

		// Trigger preprocessing to create the state
		client := resty.New()
		client.SetTimeout(3 * time.Second)
		_, err := PreprocessAppPaymentData(
			context.Background(),
			appInfo,
			userID,
			sourceID,
			globalStateMachine.settingsManager,
			client,
		)
		if err != nil {
			log.Printf("PurchaseApp: Failed to preprocess payment data: %v", err)
			return nil, fmt.Errorf("failed to create payment state: %w", err)
		}

		// Try to load the state again after preprocessing
		if st, err := globalStateMachine.LoadState(userID, realAppID, productID); err == nil {
			target = st
			log.Printf("PurchaseApp: Successfully created and loaded state after preprocessing")
		} else {
			log.Printf("PurchaseApp: State still not found after preprocessing. Error: %v", err)
			return nil, fmt.Errorf("payment state not found after preprocessing for product '%s'", productID)
		}
	}

	// Trigger start_payment event with payload (state machine will advance appropriately)
	if globalStateMachine == nil {
		return nil, fmt.Errorf("state machine not initialized")
	}
	if err := globalStateMachine.processEvent(
		context.Background(),
		userID,
		realAppID,
		productID,
		"start_payment",
		map[string]interface{}{
			"x_forwarded_host": xForwardedHost,
		},
	); err != nil {
		return nil, fmt.Errorf("failed to process start_payment event: %w", err)
	}

	// Read latest state to decide response
	latest, _ := globalStateMachine.getState(userID, realAppID, productID)
	if latest == nil {
		latest = target
	}

	// Correct SourceID if it doesn't match the request sourceID
	if latest != nil && sourceID != "" && latest.SourceID != sourceID {
		log.Printf("PurchaseApp: Correcting SourceID mismatch - state has %s, request has %s", latest.SourceID, sourceID)
		_ = globalStateMachine.updateState(latest.GetKey(), func(s *PaymentState) error {
			s.SourceID = sourceID
			return nil
		})
		// Reload state to get updated SourceID
		if updated, err := globalStateMachine.getState(userID, realAppID, productID); err == nil && updated != nil {
			latest = updated
			log.Printf("PurchaseApp: SourceID corrected to %s", latest.SourceID)
		}
	}

	// Update XForwardedHost in state if available (needed for LarePass callbacks)
	if xForwardedHost != "" && latest.XForwardedHost == "" {
		_ = globalStateMachine.updateState(latest.GetKey(), func(s *PaymentState) error {
			s.XForwardedHost = xForwardedHost
			return nil
		})
		// Reload state to get updated XForwardedHost
		if updated, err := globalStateMachine.getState(userID, realAppID, productID); err == nil && updated != nil {
			latest = updated
		}
	}

	// Trigger sync again now that XForwardedHost is available
	// Skip if we're still waiting for a (re)signature to be produced to avoid double-publishing
	// Also skip if VC already confirmed (purchase completed) to avoid unnecessary operations
	// Also skip error states (error_no_record, error_need_resign) as they are terminal states
	if latest != nil &&
		latest.SignatureStatus != SignatureRequired &&
		latest.SignatureStatus != SignatureRequiredButPending &&
		latest.SignatureStatus != SignatureErrorNoRecord &&
		latest.SignatureStatus != SignatureErrorNeedReSign &&
		!(latest.DeveloperSync == DeveloperSyncCompleted && latest.LarePassSync == LarePassSyncCompleted) &&
		!(latest.DeveloperSync == DeveloperSyncCompleted && latest.VC != "") {
		log.Printf("PurchaseApp: Triggering payment state sync after start_payment event (signature status=%s)", latest.SignatureStatus)
		_ = triggerPaymentStateSync(latest)
	} else if latest != nil {
		if latest.DeveloperSync == DeveloperSyncCompleted && latest.VC != "" {
			log.Printf("PurchaseApp: Skip triggerPaymentStateSync because VC already confirmed (purchase completed)")
		} else if latest.SignatureStatus == SignatureErrorNoRecord || latest.SignatureStatus == SignatureErrorNeedReSign {
			log.Printf("PurchaseApp: Skip triggerPaymentStateSync because signature is in error state (status=%s)", latest.SignatureStatus)
		} else {
			log.Printf("PurchaseApp: Skip triggerPaymentStateSync because signature is still pending (status=%s)", latest.SignatureStatus)
		}
	}

	// Delegate response building to state machine for consistency
	response, err := globalStateMachine.buildPurchaseResponse(userID, xForwardedHost, latest, appInfo)
	if err == nil && response != nil {
		if status, ok := response["status"]; ok {
			log.Printf("PurchaseApp: buildPurchaseResponse returned status=%v for user=%s app=%s product=%s", status, userID, realAppID, productID)
		} else {
			log.Printf("PurchaseApp: buildPurchaseResponse returned payload without status for user=%s app=%s product=%s", userID, realAppID, productID)
		}
	} else if err != nil {
		log.Printf("PurchaseApp: buildPurchaseResponse failed for user=%s app=%s product=%s err=%v", userID, realAppID, productID, err)
	}
	return response, err
}

// GetPaymentStatus returns payment status inferred from PaymentState directly
// Params and return are the same as ProcessAppPaymentStatus
func GetPaymentStatus(userID, appID, sourceID, xForwardedHost string, appInfo *types.AppInfo) (*PaymentStatusResult, error) {
	if appInfo == nil {
		return nil, fmt.Errorf("app info is nil")
	}

	// Step 1: Check if app is a paid app using correct logic (check Price.Paid.Price, not Products)
	isPaidApp, err := checkIfAppIsPaid(appInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to check if app is paid: %w", err)
	}

	if !isPaidApp {
		// Free app - no payment required
		return &PaymentStatusResult{RequiresPurchase: false, Status: "free", Message: "This is a free app"}, nil
	}

	// Use real app ID from AppEntry, not the URL parameter (which might be name)
	realAppID := appID
	if appInfo.AppEntry != nil && appInfo.AppEntry.ID != "" {
		realAppID = appInfo.AppEntry.ID
		log.Printf("GetPaymentStatus: Using real app ID from AppEntry: %s (URL param was: %s)", realAppID, appID)
	}

	// Step 2: Determine productID for state machine lookup
	// Priority order:
	// 1. For paid apps with Price.Paid.ProductID (buyout), use Price.Paid.ProductID
	// 2. For apps with Products (in-app purchases), use productID from Products
	// 3. Fallback to realAppID if neither exists
	var productID string
	if appInfo.Price != nil && appInfo.Price.Paid != nil {
		if appInfo.Price.Paid.ProductID != "" {
			// Paid buyout app with product_id - use it
			productID = appInfo.Price.Paid.ProductID
			log.Printf("GetPaymentStatus: Paid buyout app, using productID from Price.Paid: %s", productID)
		} else if len(appInfo.Price.Paid.Price) > 0 {
			// Paid buyout app without product_id - use realAppID as fallback
			productID = realAppID
			log.Printf("GetPaymentStatus: Paid buyout app without product_id, using realAppID as fallback: %s", productID)
		}
	}

	// If not found from Paid section, try Products (for in-app purchases)
	if productID == "" {
		productID = getProductIDFromAppInfo(appInfo)
		if productID != "" {
			log.Printf("GetPaymentStatus: Using productID from Products: %s", productID)
		}
	}

	// Final fallback: use realAppID
	if productID == "" {
		productID = realAppID
		log.Printf("GetPaymentStatus: No productID found, using realAppID as final fallback: %s", productID)
	}

	// Step 3: Find state (try memory first, then fallback to Redis)
	// Use realAppID for state lookup to match PurchaseApp behavior
	var state *PaymentState
	if globalStateMachine != nil {
		// Try getState first (memory only)
		if s, err := globalStateMachine.getState(userID, realAppID, productID); err == nil {
			state = s
			log.Printf("GetPaymentStatus: Found state in memory for user=%s app=%s productID=%s", userID, realAppID, productID)
		} else {
			// Try LoadState (will check Redis and load to memory)
			log.Printf("GetPaymentStatus: State not in memory, trying LoadState from Redis. Error: %v", err)
			if s, err := globalStateMachine.LoadState(userID, realAppID, productID); err == nil {
				state = s
				log.Printf("GetPaymentStatus: Found state in Redis and loaded to memory for user=%s app=%s productID=%s", userID, realAppID, productID)
			} else {
				log.Printf("GetPaymentStatus: State not found in Redis either. Error: %v", err)
			}
		}
	}

	if state == nil {
		log.Printf("GetPaymentStatus: state not found for user=%s app=%s product=%s -> not_buy", userID, realAppID, productID)
		// 需要购买但尚未开始任何流程，标记为 not_buy
		return &PaymentStatusResult{RequiresPurchase: true, Status: "not_buy", Message: "Payment not started"}, nil
	}

	// Update XForwardedHost if it is missing in state but provided by caller
	if globalStateMachine != nil && xForwardedHost != "" && state.XForwardedHost == "" {
		if err := globalStateMachine.updateState(state.GetKey(), func(s *PaymentState) error {
			if s.XForwardedHost == "" {
				s.XForwardedHost = xForwardedHost
			}
			return nil
		}); err != nil {
			log.Printf("GetPaymentStatus: Failed to update XForwardedHost: %v", err)
		} else if updated, err := globalStateMachine.getState(userID, realAppID, productID); err == nil && updated != nil {
			state = updated
		}
	}

	log.Printf("GetPaymentStatus: state snapshot user=%s app=%s product=%s developerSync=%s signatureStatus=%s paymentStatus=%s VC_present=%t",
		userID, realAppID, productID, state.DeveloperSync, state.SignatureStatus, state.PaymentStatus, state.VC != "")

	// If DeveloperSync or LarePassSync not completed, trigger a sync once (reentrant)
	if !(state.DeveloperSync == DeveloperSyncCompleted && state.LarePassSync == LarePassSyncCompleted) {
		_ = triggerPaymentStateSync(state)
	}

	status := BuildPaymentStatusFromState(state)
	result := &PaymentStatusResult{RequiresPurchase: true, Status: status}

	switch status {
	case "purchased":
		result.Message = "App is already purchased"
	case "waiting_developer_confirmation":
		result.Message = "Payment completed on frontend, waiting for developer confirmation"
	case "payment_frontend_started":
		result.Message = "Frontend preparing on-chain payment"
		if state != nil && len(state.FrontendData) > 0 {
			result.FrontendData = state.FrontendData
		}
	case "payment_required":
		result.Message = "Payment required"
		// Include frontend data if it exists
		if state != nil && len(state.FrontendData) > 0 {
			result.FrontendData = state.FrontendData
		}
	case "payment_retry_required":
		result.Message = "Developer has no matching payment record, please retry payment"
		// Include frontend data if it exists
		if state != nil && len(state.FrontendData) > 0 {
			result.FrontendData = state.FrontendData
		}
	case "notification_sent":
		result.Message = "Payment notification sent"
	case "signature_required":
		result.Message = "Signature required"
	case "signature_no_record":
		result.Message = "Developer has no matching payment record, please retry payment"
	case "signature_need_resign":
		result.Message = "Signature invalid or expired, please re-sign to continue"
	case string(PaymentNotNotified), "not_buy":
		result.Message = "Payment not started"
	case "not_evaluated":
		result.Message = "Payment status not evaluated"
	default:
		result.Message = "Payment status updated"
	}

	log.Printf("GetPaymentStatus: responding with status=%s message=%s for user=%s app=%s product=%s", result.Status, result.Message, userID, realAppID, productID)

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
		s.LarePassSync = LarePassSyncCompleted
		return nil
	}); err != nil {
		log.Printf("Failed to update signature status: %v", err)
		return fmt.Errorf("failed to update signature status: %w", err)
	}

	// Step 3.1: Read latest state for downstream decisions
	latest, _ := globalStateMachine.getState(state.UserID, state.AppID, state.ProductID)
	if latest == nil {
		latest = state
	}

	// Determine follow-up actions based on payment status
	// Check if this is a restore purchase scenario: if payment status is early stage (not_evaluated/not_notified)
	// but signature was submitted, it could be restore purchase - we should try VC polling directly
	// Otherwise, if payment status indicates frontend hasn't been notified yet, notify frontend to pay
	isEarlyStage := latest.PaymentStatus == PaymentNotEvaluated || latest.PaymentStatus == PaymentNotNotified
	isPaymentCompleted := latest.PaymentStatus == PaymentFrontendCompleted
	isNotificationSent := latest.PaymentStatus == PaymentNotificationSent || latest.PaymentStatus == PaymentFrontendStarted

	// For normal purchase: if early stage and not notification sent, notify frontend to pay
	// For restore purchase: signature was submitted, we should try VC polling directly
	// Also trigger VC polling if payment is already completed
	shouldNotifyFrontend := isEarlyStage && !isNotificationSent && !isPaymentCompleted
	// Trigger VC polling if: payment completed, OR notification already sent (restore purchase may have triggered signature)
	shouldPollDeveloper := isPaymentCompleted || isNotificationSent || (isEarlyStage && latest.JWS != "")

	// Step 4: Notify frontend to pay (only for normal purchase flow, not restore purchase)
	if shouldNotifyFrontend && globalStateMachine.dataSender != nil {
		developerDID := getValidDeveloperDID(state)
		if developerDID == "" {
			log.Printf("Cannot notify frontend payment required: invalid developer DID in state")
			return fmt.Errorf("invalid developer DID in state, cannot notify frontend")
		}
		if err := notifyFrontendPaymentRequired(
			globalStateMachine.dataSender,
			state.UserID,
			state.AppID,
			state.AppName,
			state.SourceID,
			state.ProductID,
			developerDID,
			xForwardedHost,
			nil, // appInfo not available in SubmitSignature context
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
	} else if !shouldNotifyFrontend {
		log.Printf("Skip notifying payment_required: current status=%s", latest.PaymentStatus)
	}

	// Step 5: Start VC polling if:
	// - Payment already completed (normal flow), OR
	// - Notification already sent (restore purchase or retry scenario - signature received, directly query VC), OR
	// - Early stage but signature exists (restore purchase scenario)
	if shouldPollDeveloper {
		if isPaymentCompleted {
			log.Printf("Payment already completed; restarting VC polling with refreshed signature")
		} else if isNotificationSent {
			log.Printf("Restore purchase/retry scenario detected (notification sent with signature); starting VC polling directly")
		} else {
			log.Printf("Early stage with signature detected; starting VC polling (restore purchase scenario)")
		}
		latestCopy := *latest
		go globalStateMachine.pollForVCFromDeveloper(&latestCopy)
	}

	log.Printf("=== End of Payment State Machine Processing ===")
	return nil
}

// HandleFetchSignatureCallback handles fetch-signature callback (for new endpoint)
func HandleFetchSignatureCallback(jws, signBody, user string, signed bool) error {
	log.Printf("=== Payment State Machine Processing Fetch Signature Callback ===")
	log.Printf("JWS: %s", jws)
	log.Printf("SignBody: %s", signBody)
	log.Printf("User: %s", user)
	log.Printf("Signed: %v", signed)

	if globalStateMachine == nil {
		log.Printf("State machine not initialized, skipping fetch signature callback")
		return nil
	}

	// Delegate to state_machine (placeholder implementation)
	if err := globalStateMachine.processFetchSignatureCallback(jws, signBody, user, signed); err != nil {
		log.Printf("Failed to process fetch signature callback: %v", err)
		return err
	}

	log.Printf("=== End of Fetch Signature Callback Processing ===")
	return nil
}

func resolveProductID(appInfo *types.AppInfo, realAppID string) string {
	if appInfo == nil {
		return realAppID
	}

	if appInfo.Price != nil && appInfo.Price.Paid != nil {
		if pid := strings.TrimSpace(appInfo.Price.Paid.ProductID); pid != "" {
			return pid
		}
		if len(appInfo.Price.Paid.Price) > 0 && realAppID != "" {
			return realAppID
		}
	}

	if pid := getProductIDFromAppInfo(appInfo); pid != "" {
		return pid
	}

	return realAppID
}

// ResendPaymentVCToLarePass re-sends confirmed VC to LarePass (topic: save_payment_vc)
// 用于补偿初次推送可能未送达的场景；仅在 VC 已确认后允许调用。
func ResendPaymentVCToLarePass(userID, productID string) error {
	if globalStateMachine == nil {
		return fmt.Errorf("state machine not initialized")
	}

	state := globalStateMachine.findStateByUserAndProduct(userID, productID)
	if state == nil {
		return fmt.Errorf("payment state not found for user %s and product %s", userID, productID)
	}

	latest, err := globalStateMachine.getState(state.UserID, state.AppID, state.ProductID)
	if err == nil && latest != nil {
		state = latest
	}

	if state.VC == "" {
		return fmt.Errorf("vc not available for user %s product %s", userID, productID)
	}

	if state.DeveloperSync != DeveloperSyncCompleted || state.PaymentStatus != PaymentDeveloperConfirmed {
		return fmt.Errorf("payment not confirmed, developer_sync=%s payment_status=%s", state.DeveloperSync, state.PaymentStatus)
	}

	if globalStateMachine.dataSender == nil {
		return fmt.Errorf("data sender is nil")
	}

	stateCopy := *state
	return notifyLarePassToSaveVC(globalStateMachine.dataSender, &stateCopy)
}

// StartFrontendPayment marks payment state as frontend started and caches frontend provided data
func StartFrontendPayment(userID, appID, sourceID, productID, xForwardedHost string, appInfo *types.AppInfo, frontendData map[string]interface{}) (map[string]interface{}, error) {
	log.Printf("=== StartFrontendPayment ===")
	log.Printf("User ID: %s", userID)
	log.Printf("Source ID: %s", sourceID)
	log.Printf("App ID: %s", appID)
	log.Printf("X-Forwarded-Host: %s", xForwardedHost)

	if appInfo == nil {
		return nil, fmt.Errorf("app info is nil")
	}

	if globalStateMachine == nil {
		return nil, fmt.Errorf("state machine not initialized")
	}

	realAppID := appID
	if appInfo.AppEntry != nil && appInfo.AppEntry.ID != "" {
		realAppID = appInfo.AppEntry.ID
		log.Printf("StartFrontendPayment: Using real app ID from AppEntry: %s (URL param was: %s)", realAppID, appID)
	}

	resolvedProductID := strings.TrimSpace(productID)
	if resolvedProductID != "" {
		log.Printf("StartFrontendPayment: Using productID from request: %s", resolvedProductID)
	} else {
		log.Printf("StartFrontendPayment: Request missing productID, deriving from app info")
		resolvedProductID = resolveProductID(appInfo, realAppID)
	}

	state, err := globalStateMachine.getState(userID, realAppID, resolvedProductID)
	if err != nil || state == nil {
		log.Printf("StartFrontendPayment: State not found in memory, attempting to load from Redis. Err: %v", err)
		state, err = globalStateMachine.LoadState(userID, realAppID, resolvedProductID)
		if err != nil || state == nil {
			return nil, fmt.Errorf("payment state not found for frontend payment start: %w", err)
		}
	}

	if xForwardedHost != "" {
		_ = globalStateMachine.updateState(state.GetKey(), func(s *PaymentState) error {
			if s.XForwardedHost == "" {
				s.XForwardedHost = xForwardedHost
			}
			return nil
		})
	}

	payload := FrontendPaymentStartedPayload{Data: frontendData}
	if err := globalStateMachine.processEvent(
		context.Background(),
		userID,
		realAppID,
		resolvedProductID,
		"frontend_payment_started",
		payload,
	); err != nil {
		return nil, fmt.Errorf("failed to process frontend_payment_started event: %w", err)
	}

	latest, _ := globalStateMachine.getState(userID, realAppID, resolvedProductID)
	if latest == nil {
		latest = state
	}

	// Correct SourceID if it doesn't match the request sourceID
	if latest != nil && sourceID != "" && latest.SourceID != sourceID {
		log.Printf("StartFrontendPayment: Correcting SourceID mismatch - state has %s, request has %s", latest.SourceID, sourceID)
		_ = globalStateMachine.updateState(latest.GetKey(), func(s *PaymentState) error {
			s.SourceID = sourceID
			return nil
		})
		// Reload state to get updated SourceID
		if updated, err := globalStateMachine.getState(userID, realAppID, resolvedProductID); err == nil && updated != nil {
			latest = updated
			log.Printf("StartFrontendPayment: SourceID corrected to %s", latest.SourceID)
		}
	}

	if xForwardedHost != "" && latest != nil && latest.XForwardedHost == "" {
		_ = globalStateMachine.updateState(latest.GetKey(), func(s *PaymentState) error {
			if s.XForwardedHost == "" {
				s.XForwardedHost = xForwardedHost
			}
			return nil
		})
		if updated, err := globalStateMachine.getState(userID, realAppID, resolvedProductID); err == nil && updated != nil {
			latest = updated
		}
	}

	return globalStateMachine.buildPurchaseResponse(userID, xForwardedHost, latest, appInfo)
}

// StartPaymentPolling starts polling for VC after payment completion
func StartPaymentPolling(userID, sourceID, appID, productID, txHash, xForwardedHost string, appInfoLatest *types.AppInfoLatestData) error {
	log.Printf("=== Starting Payment Polling ===")
	log.Printf("User ID: %s", userID)
	log.Printf("Source ID: %s", sourceID)
	log.Printf("App ID: %s", appID)
	log.Printf("Product ID: %s", productID)
	log.Printf("TxHash: %s", txHash)
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

	realAppID := appID
	if appInfoLatest != nil && appInfoLatest.AppInfo != nil && appInfoLatest.AppInfo.AppEntry != nil && appInfoLatest.AppInfo.AppEntry.ID != "" {
		realAppID = appInfoLatest.AppInfo.AppEntry.ID
	}

	resolvedProductID := strings.TrimSpace(productID)
	if resolvedProductID != "" {
		log.Printf("StartPaymentPolling: Using productID from request: %s", resolvedProductID)
	} else if appInfoLatest != nil && appInfoLatest.AppInfo != nil {
		log.Printf("StartPaymentPolling: Request missing productID, deriving from app info")
		resolvedProductID = resolveProductID(appInfoLatest.AppInfo, realAppID)
	} else {
		log.Printf("StartPaymentPolling: No productID and no app info, reverting to app ID: %s", realAppID)
		resolvedProductID = realAppID
	}

	// 获取已存在状态
	_, err := globalStateMachine.getState(userID, realAppID, resolvedProductID)
	if err != nil {
		log.Printf("Failed to get state for polling: %v", err)
		return fmt.Errorf("payment state not found; ensure preprocessing ran: %w", err)
	}

	// Update state with payment info
	key := fmt.Sprintf("%s:%s:%s", userID, realAppID, resolvedProductID)
	if err := globalStateMachine.updateState(key, func(s *PaymentState) error {
		s.TxHash = txHash
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
		realAppID,
		resolvedProductID,
		"payment_completed",
		map[string]interface{}{
			"tx_hash": txHash,
		},
	); err != nil {
		log.Printf("Failed to process payment_completed event: %v", err)
		return fmt.Errorf("failed to process payment_completed event: %w", err)
	}

	log.Printf("Payment polling started for user %s, app %s, product %s", userID, realAppID, resolvedProductID)
	return nil
}

// RestorePurchase handles restore purchase flow
// This function initiates signature request if needed, then directly queries VC from developer
// It differs from PurchaseApp in that it doesn't notify frontend to pay, as payment has already been completed
func RestorePurchase(userID, appID, sourceID, xForwardedHost string, appInfo *types.AppInfo) (map[string]interface{}, error) {
	log.Printf("[RestorePurchase] user=%s app=%s source=%s", userID, appID, sourceID)

	// Extract productID from app info with correct priority
	if appInfo == nil {
		return nil, fmt.Errorf("app info is nil")
	}

	// Use real app ID from AppEntry, not the URL parameter (which might be name)
	realAppID := appID
	if appInfo.AppEntry != nil && appInfo.AppEntry.ID != "" {
		realAppID = appInfo.AppEntry.ID
		log.Printf("RestorePurchase: Using real app ID from AppEntry: %s (URL param was: %s)", realAppID, appID)
	}

	var productID string
	if appInfo.Price != nil && appInfo.Price.Paid != nil {
		if appInfo.Price.Paid.ProductID != "" {
			productID = appInfo.Price.Paid.ProductID
			log.Printf("RestorePurchase: Using productID from Price.Paid: %s", productID)
		}
	}
	if productID == "" {
		productID = getProductIDFromAppInfo(appInfo)
		if productID != "" {
			log.Printf("RestorePurchase: Using productID from Products: %s", productID)
		}
	}
	if productID == "" {
		// Final fallback: for paid apps without explicit product_id
		productID = realAppID
		log.Printf("RestorePurchase: productID not found in price, using real appID as fallback: %s", productID)
	}

	// Check if state machine is initialized
	if globalStateMachine == nil {
		return nil, fmt.Errorf("state machine not initialized")
	}

	// Locate or create state
	var target *PaymentState
	if st, err := globalStateMachine.getState(userID, realAppID, productID); err == nil {
		target = st
		log.Printf("RestorePurchase: Found state in memory for user=%s app=%s productID=%s", userID, realAppID, productID)
	} else {
		// Try LoadState (will check Redis and load to memory)
		log.Printf("RestorePurchase: State not in memory, trying LoadState from Redis. Error: %v", err)
		if st, err := globalStateMachine.LoadState(userID, realAppID, productID); err == nil {
			target = st
			log.Printf("RestorePurchase: Found state in Redis and loaded to memory for user=%s app=%s productID=%s", userID, realAppID, productID)
		} else {
			log.Printf("RestorePurchase: State not found in Redis either. Error: %v", err)
		}
	}

	// If state not found, try to create via preprocessing
	if target == nil {
		log.Printf("RestorePurchase: Payment state not found for user=%s app=%s productID=%s. Attempting to create state via preprocessing.", userID, realAppID, productID)

		if globalStateMachine == nil || globalStateMachine.settingsManager == nil {
			return nil, fmt.Errorf("state machine or settings manager not initialized; cannot create payment state")
		}

		// Trigger preprocessing to create the state
		client := resty.New()
		client.SetTimeout(3 * time.Second)
		_, err := PreprocessAppPaymentData(
			context.Background(),
			appInfo,
			userID,
			sourceID,
			globalStateMachine.settingsManager,
			client,
		)
		if err != nil {
			log.Printf("RestorePurchase: Failed to preprocess payment data: %v", err)
			return nil, fmt.Errorf("failed to create payment state: %w", err)
		}

		// Try to load the state again after preprocessing
		if st, err := globalStateMachine.LoadState(userID, realAppID, productID); err == nil {
			target = st
			log.Printf("RestorePurchase: Successfully created and loaded state after preprocessing")
		} else {
			log.Printf("RestorePurchase: State still not found after preprocessing. Error: %v", err)
			return nil, fmt.Errorf("payment state not found after preprocessing for product '%s'", productID)
		}
	}

	// Update XForwardedHost if needed
	if xForwardedHost != "" && target.XForwardedHost == "" {
		_ = globalStateMachine.updateState(target.GetKey(), func(s *PaymentState) error {
			s.XForwardedHost = xForwardedHost
			return nil
		})
		// Reload state
		if updated, err := globalStateMachine.getState(userID, realAppID, productID); err == nil && updated != nil {
			target = updated
		}
	}

	// Correct SourceID if it doesn't match
	if target != nil && sourceID != "" && target.SourceID != sourceID {
		log.Printf("RestorePurchase: Correcting SourceID mismatch - state has %s, request has %s", target.SourceID, sourceID)
		_ = globalStateMachine.updateState(target.GetKey(), func(s *PaymentState) error {
			s.SourceID = sourceID
			return nil
		})
		if updated, err := globalStateMachine.getState(userID, realAppID, productID); err == nil && updated != nil {
			target = updated
		}
	}

	// Get latest state
	latest, _ := globalStateMachine.getState(userID, realAppID, productID)
	if latest == nil {
		latest = target
	}

	// If VC already exists and confirmed, return success
	if latest.VC != "" && latest.DeveloperSync == DeveloperSyncCompleted {
		return map[string]interface{}{
			"status":  "purchased",
			"message": "Purchase already restored, VC confirmed",
		}, nil
	}

	// Check if signature is available
	if latest.SignatureStatus == SignatureRequiredAndSigned && latest.JWS != "" {
		// Signature is ready, directly start VC polling
		log.Printf("RestorePurchase: Signature already available, starting VC polling for user=%s app=%s product=%s", userID, realAppID, productID)
		go globalStateMachine.pollForVCFromDeveloper(latest)
		return map[string]interface{}{
			"status":  "syncing",
			"message": "Restore purchase started, polling for VC",
		}, nil
	}

	// If signature is not available, trigger signature request
	if latest.SignatureStatus == SignatureRequired || latest.SignatureStatus == SignatureNotEvaluated ||
		latest.SignatureStatus == SignatureRequiredButPending {
		log.Printf("RestorePurchase: Signature not available, triggering signature request for user=%s app=%s product=%s", userID, realAppID, productID)

		// Update XForwardedHost if needed before requesting signature
		effectiveHost := latest.XForwardedHost
		if effectiveHost == "" && xForwardedHost != "" {
			effectiveHost = xForwardedHost
		}
		if effectiveHost == "" {
			return nil, fmt.Errorf("X-Forwarded-Host is required for restore purchase but not available")
		}

		// Update state to mark signature request in progress
		// For restore purchase, we set payment status to notification_sent to indicate
		// that this is a restore purchase flow (not a new purchase), so signature callback
		// will trigger VC polling directly instead of notifying frontend to pay
		_ = globalStateMachine.updateState(latest.GetKey(), func(s *PaymentState) error {
			if s.XForwardedHost == "" {
				s.XForwardedHost = effectiveHost
			}
			if s.LarePassSync == LarePassSyncNotStarted || s.LarePassSync == LarePassSyncFailed {
				s.LarePassSync = LarePassSyncInProgress
			}
			if s.SignatureStatus == SignatureNotEvaluated || s.SignatureStatus == SignatureRequired {
				s.SignatureStatus = SignatureRequired
			}
			// Mark as notification_sent to indicate restore purchase flow
			// This will cause ProcessSignatureSubmission to trigger VC polling directly
			if s.PaymentStatus == PaymentNotEvaluated || s.PaymentStatus == PaymentNotNotified {
				s.PaymentStatus = PaymentNotificationSent
			}
			return nil
		})

		// Reload latest state
		if updated, err := globalStateMachine.getState(userID, realAppID, productID); err == nil && updated != nil {
			latest = updated
		}

		// Notify LarePass to sign
		if globalStateMachine.dataSender != nil {
			_ = notifyLarePassToSign(
				globalStateMachine.dataSender,
				latest.UserID,
				latest.AppID,
				latest.ProductID,
				latest.TxHash,
				effectiveHost,
				latest.DeveloperName,
				false, // not a re-sign flow
			)

			// Notify frontend that signature is required for restore purchase
			if err := notifyFrontendStateUpdate(globalStateMachine.dataSender, latest.UserID, latest.AppID, latest.AppName, latest.SourceID, "signature_required"); err != nil {
				log.Printf("Failed to notify frontend signature_required for restore purchase: %v", err)
			}
		}

		return map[string]interface{}{
			"status":  "signature_required",
			"message": "Signature required for restore purchase, please sign and wait for callback",
		}, nil
	}

	// Handle error states - for restore purchase, we should trigger re-sign even if signature is invalid
	if latest.SignatureStatus == SignatureErrorNeedReSign {
		log.Printf("RestorePurchase: Signature invalid, triggering re-sign request for user=%s app=%s product=%s", userID, realAppID, productID)

		// Update XForwardedHost if needed before requesting signature
		effectiveHost := latest.XForwardedHost
		if effectiveHost == "" && xForwardedHost != "" {
			effectiveHost = xForwardedHost
		}
		if effectiveHost == "" {
			return nil, fmt.Errorf("X-Forwarded-Host is required for restore purchase but not available")
		}

		// Reset signature status and trigger re-sign
		_ = globalStateMachine.updateState(latest.GetKey(), func(s *PaymentState) error {
			if s.XForwardedHost == "" {
				s.XForwardedHost = effectiveHost
			}
			// Reset signature status to required for re-sign
			s.SignatureStatus = SignatureRequired
			s.JWS = "" // Clear invalid JWS
			s.SignBody = ""
			if s.LarePassSync == LarePassSyncNotStarted || s.LarePassSync == LarePassSyncFailed || s.LarePassSync == LarePassSyncCompleted {
				s.LarePassSync = LarePassSyncInProgress
			}
			// Mark as notification_sent to indicate restore purchase flow
			if s.PaymentStatus == PaymentNotEvaluated || s.PaymentStatus == PaymentNotNotified {
				s.PaymentStatus = PaymentNotificationSent
			}
			return nil
		})

		// Reload latest state
		if updated, err := globalStateMachine.getState(userID, realAppID, productID); err == nil && updated != nil {
			latest = updated
		}

		// Notify LarePass to sign (re-sign flow)
		if globalStateMachine.dataSender != nil {
			_ = notifyLarePassToSign(
				globalStateMachine.dataSender,
				latest.UserID,
				latest.AppID,
				latest.ProductID,
				latest.TxHash,
				effectiveHost,
				latest.DeveloperName,
				true, // isReSign = true for re-sign flow
			)

			// Notify frontend that signature is required for restore purchase
			if err := notifyFrontendStateUpdate(globalStateMachine.dataSender, latest.UserID, latest.AppID, latest.AppName, latest.SourceID, "signature_required"); err != nil {
				log.Printf("Failed to notify frontend signature_required for restore purchase re-sign: %v", err)
			}
		}

		return map[string]interface{}{
			"status":  "signature_required",
			"message": "Signature invalid, re-signing for restore purchase",
		}, nil
	}

	if latest.SignatureStatus == SignatureErrorNoRecord {
		return map[string]interface{}{
			"status":  "signature_no_record",
			"message": "No payment record found, cannot restore purchase",
		}, nil
	}

	// Default: syncing
	return map[string]interface{}{
		"status":  "syncing",
		"message": "Restore purchase in progress",
	}, nil
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

	// Step 1: Get productID with correct priority (Price.Paid.ProductID > Products > appID)
	var productID string
	if appInfo.Price != nil && appInfo.Price.Paid != nil {
		if appInfo.Price.Paid.ProductID != "" {
			productID = appInfo.Price.Paid.ProductID
			log.Printf("PreprocessAppPaymentData: Using productID from Price.Paid: %s", productID)
		}
	}
	if productID == "" {
		productID = getProductIDFromAppInfo(appInfo)
		if productID != "" {
			log.Printf("PreprocessAppPaymentData: Using productID from Products: %s", productID)
		}
	}
	if productID == "" {
		// For paid apps, we can use appID as productID if no explicit product_id
		if appInfo.AppEntry != nil {
			productID = appInfo.AppEntry.ID
			log.Printf("PreprocessAppPaymentData: Using appID as productID fallback: %s", productID)
		} else {
			// No valid productID -> return directly, no further processing
			log.Printf("INFO: productID not found and no appID available, skip state init")
			return nil, nil
		}
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
			// Note: Developer.DID should be empty when DID lookup fails, not set to developerName
			failedState := &PaymentState{
				UserID:          userID,
				AppID:           appID,
				AppName:         appName,
				SourceID:        sourceID,
				ProductID:       productID,
				DeveloperName:   developerName,
				Developer:       DeveloperInfo{Name: "", DID: "", RSAPubKey: ""},
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
		ok := verifyPurchaseInfo(pi, state.ProductID, state.DeveloperName)
		if !ok {
			return nil, fmt.Errorf("purchase info not verified for user=%s app=%s product=%s", userID, appID, productID)
		}
	}
	return pi, nil
}
