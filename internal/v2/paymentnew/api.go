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

	// 从应用信息提取 productID
	if appInfo == nil {
		return nil, fmt.Errorf("app info is nil")
	}
	productID := getProductIDFromAppInfo(appInfo)
	if productID == "" {
		return nil, fmt.Errorf("product id not found from app info")
	}

	// 通过 state_machine 精确定位状态
	var target *PaymentState
	if globalStateMachine != nil {
		if st, err := globalStateMachine.getState(userID, appID, productID); err == nil {
			target = st
		}
	}

	if target == nil {
		return nil, fmt.Errorf("payment state not found; ensure preprocessing ran")
	}

	// 1.1 No signature yet -> notify LarePass to sign
	if target.SignatureStatus == SignatureRequired || target.SignatureStatus == SignatureRequiredButPending {
		if globalStateMachine == nil || globalStateMachine.dataSender == nil {
			return nil, fmt.Errorf("state machine or data sender not initialized")
		}

		// Ensure X-Forwarded-Host is present in state for callback
		effectiveHost := target.XForwardedHost
		if effectiveHost == "" {
			effectiveHost = xForwardedHost
			if effectiveHost != "" {
				_ = globalStateMachine.updateState(target.GetKey(), func(s *PaymentState) error {
					s.XForwardedHost = effectiveHost
					return nil
				})
			}
		}

		// Notify LarePass to sign (txHash may be empty at this point)
		if err := notifyLarePassToSign(
			globalStateMachine.dataSender,
			target.UserID,
			target.AppID,
			target.ProductID,
			target.TxHash,
			effectiveHost,
			target.SystemChainID,
		); err != nil {
			return nil, fmt.Errorf("failed to notify larepass to sign: %w", err)
		}

		// Optionally mark in-progress
		_ = globalStateMachine.updateState(target.GetKey(), func(s *PaymentState) error {
			if s.LarePassSync == LarePassSyncNotStarted || s.LarePassSync == LarePassSyncFailed {
				s.LarePassSync = LarePassSyncInProgress
			}
			return nil
		})

		return map[string]interface{}{
			"status": "signature_required",
		}, nil
	}

	// 1.2 Signed -> return payment transfer info (same as notifyFrontendPaymentRequired)
	if target.SignatureStatus == SignatureRequiredAndSigned {
		developerDID := target.Developer.DID
		userDID, err := getUserDID(userID, xForwardedHost)
		if err != nil {
			return nil, fmt.Errorf("failed to get user DID: %w", err)
		}
		paymentData := createFrontendPaymentData(userDID, developerDID, target.ProductID)
		return map[string]interface{}{
			"status":       "payment_required",
			"payment_data": paymentData,
		}, nil
	}

	// 1.3 Signed and transferred -> trigger polling and return waiting status
	if target.PaymentStatus == PaymentFrontendCompleted {
		if globalStateMachine != nil {
			go globalStateMachine.pollForVCFromDeveloper(target)
		}
		return map[string]interface{}{
			"status":  "waiting_developer_confirmation",
			"message": "payment completed, waiting for developer confirmation",
		}, nil
	}

	// 1.4 VC synced with data -> already purchased
	if target.DeveloperSync == DeveloperSyncCompleted && target.VC != "" {
		return map[string]interface{}{
			"status":  "purchased",
			"message": "already purchased, ready to install",
		}, nil
	}

	return map[string]interface{}{
		"status":  "syncing",
		"message": "synchronizing payment state",
	}, nil
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

	// 若未完成 DeveloperSync 或 LarePassSync，则触发一次同步（可重入）
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

	// Step 1: 从 signBody 解析 productID
	productID, err := parseProductIDFromSignBody(signBody)
	if err != nil {
		log.Printf("Failed to parse productID from signBody: %v", err)
		return fmt.Errorf("failed to parse productID from signBody: %w", err)
	}

	// Step 2: 通过 user + productID 找到 PaymentState
	state := globalStateMachine.findStateByUserAndProduct(user, productID)
	if state == nil {
		log.Printf("PaymentState not found for user %s and productID %s", user, productID)
		return fmt.Errorf("payment state not found for user %s and productID %s", user, productID)
	}

	// Step 3: 更新状态为 SignatureRequiredAndSigned
	if err := globalStateMachine.updateState(state.GetKey(), func(s *PaymentState) error {
		s.SignatureStatus = SignatureRequiredAndSigned
		s.JWS = jws
		return nil
	}); err != nil {
		log.Printf("Failed to update signature status: %v", err)
		return fmt.Errorf("failed to update signature status: %w", err)
	}

	// Step 3.1: 读取最新状态，若已处于更晚阶段则避免重复通知（幂等与守卫）
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

	// Step 4: 发送通知让前端进行支付（仅在未通知过/早期态时）
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

		// Step 4.1: 通知成功后推进状态为 notification_sent（幂等且不回退）
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

// HandleFetchSignatureCallback 处理拉取签名回调（供新接口调用）
func HandleFetchSignatureCallback(jws, signBody, user string, code int) error {
	log.Printf("=== Payment State Machine Processing Fetch Signature Callback ===")
	log.Printf("JWS: %s", jws)
	log.Printf("SignBody: %s", signBody)
	log.Printf("User: %s", user)

	if globalStateMachine == nil {
		log.Printf("State machine not initialized, skipping fetch signature callback")
		return nil
	}

	// 委托 state_machine 处理（占位实现）
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
	// Step 0: 基础校验与快速跳过
	if appInfo == nil || appInfo.Price == nil {
		log.Printf("INFO: no payment section, skip")
		return nil, nil
	}

	// Step 1: 获取 productID
	productID := getProductIDFromAppInfo(appInfo)
	if productID == "" {
		// 没有有效的 productID 直接返回，不做后续处理
		log.Printf("INFO: productID not found, skip state init")
		return nil, nil
	}

	// 提取 app 基本信息
	appID := ""
	appName := ""
	if appInfo.AppEntry != nil {
		appID = appInfo.AppEntry.ID
		appName = appInfo.AppEntry.Name
	}

	// Step 2: 通过 productID 获取 PaymentStates（优先状态机，未命中则为空）
	if globalStateMachine == nil {
		return nil, fmt.Errorf("state machine not initialized")
	}
	var state *PaymentState
	if s, err := globalStateMachine.LoadState(userID, appID, productID); err == nil {
		state = s
	}

	// Step 3: 如果不存在则 查询开发者信息, 然后创建新的 PaymentStates
	if state == nil {
		developerName := getDeveloperNameFromPrice(appInfo)

		if client == nil {
			client = resty.New()
		}
		client.SetTimeout(3 * time.Second)

		// 如果无法从 price 获取 developerName，则创建失败状态并返回错误
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
			// DID 查询失败时，同样标记 DeveloperSync 失败
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

		// 初始化并保存新的 PaymentState（SourceID 由外部传入）
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

	// Step 4: 触发 PaymentStates 的状态同步流程（该方法需可重入）
	// 说明：无论状态是新建还是已存在，都应触发一次同步流程；实现需保证幂等/可重入
	_ = triggerPaymentStateSync(state)

	// 由 PaymentState 构建 PurchaseInfo（若状态不存在则返回 nil）
	pi := buildPurchaseInfoFromState(state)
	if pi != nil && strings.EqualFold(pi.Status, "purchased") {
		ok := verifyPurchaseInfo(pi)
		if !ok {
			return nil, fmt.Errorf("purchase info not verified for user=%s app=%s product=%s", userID, appID, productID)
		}
	}
	return pi, nil
}
