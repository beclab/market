package paymentnew

import (
	"context"
	"fmt"
	"log"
	"os"
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
	IsPaid       bool   `json:"is_paid"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	PaymentError string `json:"payment_error,omitempty"`
}

// ProcessAppPaymentStatus handles the complete payment status check and processing
func ProcessAppPaymentStatus(userID, appID, sourceID string, appInfo *types.AppInfo) (*PaymentStatusResult, error) {
	log.Printf("Processing payment status for user: %s, app: %s, source: %s", userID, appID, sourceID)

	// Step 1: Check if app is paid app
	isPaidApp, err := checkIfAppIsPaid(appInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to check if app is paid: %w", err)
	}

	if !isPaidApp {
		log.Printf("App %s is not a paid app", appID)
		return &PaymentStatusResult{
			IsPaid:  false,
			Status:  "free",
			Message: "This is a free app",
		}, nil
	}

	// Step 2: Get payment status
	paymentStatus, err := getPaymentStatus(appInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment status: %w", err)
	}

	// Step 3: Handle different payment statuses
	result := &PaymentStatusResult{
		IsPaid: true,
		Status: paymentStatus,
	}

	switch paymentStatus {
	case "purchased":
		log.Printf("App %s is already purchased by user %s", appID, userID)
		result.Message = "App is already purchased"

	case "paying":
		log.Printf("App %s payment is in progress for user %s", appID, userID)
		result.Message = "Payment is in progress"

	case "not_buy":
		log.Printf("App %s is not purchased by user %s, starting payment flow", appID, userID)
		result.Message = "Starting payment flow"

		// Start payment process using state machine
		if err := StartPaymentProcess(userID, appID, sourceID, appInfo); err != nil {
			log.Printf("Failed to start payment process: %v", err)
			result.PaymentError = err.Error()
		}

	case "not_sign":
		log.Printf("App %s payment signature is missing for user %s, retrying payment flow", appID, userID)
		result.Message = "Payment signature missing, retrying payment flow"

		// Retry payment process using state machine
		if err := RetryPaymentProcess(userID, appID, sourceID, appInfo); err != nil {
			log.Printf("Failed to retry payment process: %v", err)
			result.PaymentError = err.Error()
		}

	case "not_pay":
		log.Printf("App %s payment is not completed for user %s, retrying payment flow", appID, userID)
		result.Message = "Payment not completed, retrying payment flow"

		// Retry payment process using state machine
		if err := RetryPaymentProcess(userID, appID, sourceID, appInfo); err != nil {
			log.Printf("Failed to retry payment process: %v", err)
			result.PaymentError = err.Error()
		}

	default:
		log.Printf("Unknown payment status %s for app %s and user %s", paymentStatus, appID, userID)
		result.Message = "Unknown payment status"
	}

	log.Printf("Payment status processing completed for app: %s, user: %s, status: %s", appID, userID, paymentStatus)
	return result, nil
}

// StartPaymentProcess starts the payment process for a new purchase
func StartPaymentProcess(userID, appID, sourceID string, appInfo *types.AppInfo) error {
	log.Printf("=== Payment State Machine Starting Payment Process ===")
	log.Printf("User ID: %s", userID)
	log.Printf("App ID: %s", appID)

	// Check if state machine is initialized
	if globalStateMachine == nil {
		log.Printf("State machine not initialized, cannot start payment process")
		return fmt.Errorf("state machine not initialized")
	}

	// Extract required information from appInfo
	if appInfo == nil {
		return fmt.Errorf("app info is nil")
	}

	// Get app name from app info
	appName := ""
	if appInfo.AppEntry != nil {
		appName = appInfo.AppEntry.Name
	}

	// Get product ID from app price information
	productID := getProductIDFromAppInfo(appInfo)

	// Extract developer DID from app price info and extract identifier
	developerName := ""
	if appInfo.Price != nil {
		developerDID := appInfo.Price.Developer.DID
		log.Printf("Extracted developer DID from price info: %s", developerDID)

		// Extract identifier from DID format (e.g., "did:key:z6Mkt...")
		if strings.HasPrefix(developerDID, "did:key:") {
			developerName = strings.TrimPrefix(developerDID, "did:key:")
			log.Printf("Extracted developer identifier: %s", developerName)
		} else {
			developerName = developerDID
			log.Printf("Warning: DID format not recognized, using full DID as identifier: %s", developerDID)
		}
	}
	if developerName == "" {
		log.Printf("Warning: developer DID not found in price info for app %s", appID)
		developerName = "unknown"
	}

	// Create or get existing state
	state, err := globalStateMachine.getOrCreateState(userID, appID, productID, appName, sourceID, developerName)
	if err != nil {
		log.Printf("Error creating payment state: %v", err)
		return fmt.Errorf("failed to create payment state: %w", err)
	}

	// Process start_payment event
	if err := globalStateMachine.processEvent(context.Background(), userID, appID, productID, "start_payment", nil); err != nil {
		log.Printf("Error processing start_payment event: %v", err)
		return fmt.Errorf("failed to process start_payment event: %w", err)
	}

	log.Printf("Payment state created/retrieved: %s:%s:%s", state.UserID, state.AppID, state.ProductID)
	log.Printf("=== End of Payment State Machine Starting Payment Process ===")

	return nil
}

// RetryPaymentProcess retries the payment process for existing purchase attempts
func RetryPaymentProcess(userID, appID, sourceID string, appInfo *types.AppInfo) error {
	log.Printf("=== Payment State Machine Retrying Payment Process ===")
	log.Printf("User ID: %s", userID)
	log.Printf("App ID: %s", appID)

	// Check if state machine is initialized
	if globalStateMachine == nil {
		log.Printf("State machine not initialized, cannot retry payment process")
		return fmt.Errorf("state machine not initialized")
	}

	// Extract required information from appInfo
	if appInfo == nil {
		return fmt.Errorf("app info is nil")
	}

	// Get product ID from app price information
	productID := getProductIDFromAppInfo(appInfo)

	// Check if state exists
	state, err := globalStateMachine.getState(userID, appID, productID)
	if err != nil {
		log.Printf("No existing state found for retry, creating new state")
		// Create new state if none exists
		return StartPaymentProcess(userID, appID, sourceID, appInfo)
	}

	log.Printf("Found existing state for user %s, app %s", userID, appID)

	// Check state and decide retry strategy
	// TODO: 根据状态机的五个维度状态决定重试策略
	log.Printf("Current state: PaymentNeed=%v, DeveloperSync=%s, LarePassSync=%s, SignatureStatus=%s, PaymentStatus=%s",
		state.PaymentNeed, state.DeveloperSync, state.LarePassSync, state.SignatureStatus, state.PaymentStatus)

	// Retry by triggering appropriate event based on current state
	if state.LarePassSync == LarePassSyncNotStarted || state.LarePassSync == LarePassSyncFailed {
		// Need to request signature
		if err := globalStateMachine.processEvent(context.Background(), userID, appID, productID, "request_signature", nil); err != nil {
			log.Printf("Failed to process request_signature event: %v", err)
			return err
		}
	}

	log.Printf("Retry payment process completed for state %s:%s:%s",
		state.UserID, state.AppID, state.ProductID)
	log.Printf("=== End of Payment State Machine Retrying Payment Process ===")

	return nil
}

// ProcessSignatureSubmission handles the business logic for signature submission
func ProcessSignatureSubmission(jws, signBody, user string) error {
	log.Printf("=== Payment State Machine Processing Signature Submission ===")
	log.Printf("JWS: %s", jws)
	log.Printf("SignBody: %s", signBody)
	log.Printf("User: %s", user)

	if globalStateMachine == nil {
		log.Printf("State machine not initialized, falling back to basic processing")
		log.Printf("=== End of Payment State Machine Processing ===")
		return nil
	}

	// Find the state for this user (we need to find by userID since we don't have appID/productID in the signature)
	// TODO: Implement finding state by userID only
	// For now, this is a placeholder
	log.Printf("Processing signature submission through state machine")

	// Trigger signature_submitted event
	// TODO: Extract appID and productID from the signature or state
	// This should be implemented based on how we store the mapping

	log.Printf("=== End of Payment State Machine Processing ===")
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

	// Get or create state
	appName := appID
	if appInfoLatest != nil && appInfoLatest.RawData != nil && appInfoLatest.RawData.Name != "" {
		appName = appInfoLatest.RawData.Name
	}

	developerName := ""
	if appInfoLatest != nil && appInfoLatest.RawData != nil {
		developerName = appInfoLatest.RawData.Developer
	}
	if developerName == "" {
		developerName = "unknown"
	}

	// Get or create state
	_, err := globalStateMachine.getOrCreateState(userID, appID, productID, appName, sourceID, developerName)
	if err != nil {
		log.Printf("Failed to get or create state: %v", err)
		return fmt.Errorf("failed to get or create state: %w", err)
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

// PreprocessAppPaymentData preprocesses app payment data: validates payment info, verifies developer RSA key, and loads purchase info from Redis
func PreprocessAppPaymentData(ctx context.Context, appInfo *types.AppInfo, userID, devNameForKey, appNameForKey string, settingsManager *settings.SettingsManager, client *resty.Client) (*types.PurchaseInfo, error) {
	// Check if there is a payment section in AppInfo
	if appInfo == nil || appInfo.Price == nil {
		log.Printf("INFO: no payment section, skip")
		return nil, nil
	}

	// Call DID interface to get developer information
	didBase := os.Getenv("DID_GATE_BASE")
	if didBase == "" {
		didBase = "https://did-gate.mdogs.me"
	}
	developerName := devNameForKey
	if developerName == "" && appInfo.AppEntry != nil {
		developerName = appInfo.AppEntry.Developer
	}

	if client == nil {
		client = resty.New()
	}
	client.SetTimeout(3 * time.Second)

	dev, err := fetchDeveloperInfo(ctx, client, didBase, developerName)
	if err != nil {
		return nil, fmt.Errorf("fetch developer info failed: %w", err)
	}

	// Compare rsaPubKey
	ok, err := verifyPaymentConsistency(appInfo, dev)
	if err != nil {
		return nil, fmt.Errorf("verify payment failed: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("rsa public key not matched for developer=%s app=%s", developerName, appInfo.AppEntry.ID)
	}

	// Get product ID from app price information
	productID := getProductIDFromAppInfo(appInfo)

	// Get VC and status from redis
	key := redisPurchaseKey(userID, devNameForKey, appNameForKey, productID)
	pi, err := getPurchaseInfoFromRedis(settingsManager, key)
	if err != nil {
		// If there is no purchase record, it is not considered an error, only recorded
		log.Printf("INFO: purchase info not found for key=%s: %v", key, err)
		return nil, nil
	}

	// Verify purchase info if status is purchased
	if pi.Status == "purchased" {
		ok := verifyPurchaseInfo(pi)
		if !ok {
			return nil, fmt.Errorf("purchase info not verified for key=%s", key)
		}
	}

	return pi, nil
}
