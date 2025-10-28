package paymentnew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"market/internal/v2/types"
)

// getOrCreateState 获取或创建状态（内部方法）
func (psm *PaymentStateMachine) getOrCreateState(userID, appID, productID, appName, sourceID, developerName string) (*PaymentState, error) {
	key := fmt.Sprintf("%s:%s:%s", userID, appID, productID)

	psm.mu.Lock()
	defer psm.mu.Unlock()

	// Check if state already exists
	if state, exists := psm.states[key]; exists {
		log.Printf("State already exists for key %s", key)
		return state, nil
	}

	// Create new state
	state := &PaymentState{
		UserID:          userID,
		AppID:           appID,
		AppName:         appName,
		SourceID:        sourceID,
		ProductID:       productID,
		DeveloperName:   developerName,
		PaymentNeed:     PaymentNeedRequired,
		DeveloperSync:   DeveloperSyncNotStarted,
		LarePassSync:    LarePassSyncNotStarted,
		SignatureStatus: SignatureNotRequired,
		PaymentStatus:   PaymentNotNotified,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	psm.states[key] = state
	log.Printf("Created new state for key %s", key)

	return state, nil
}

// getState 获取状态（内部方法）
func (psm *PaymentStateMachine) getState(userID, appID, productID string) (*PaymentState, error) {
	key := fmt.Sprintf("%s:%s:%s", userID, appID, productID)

	psm.mu.RLock()
	defer psm.mu.RUnlock()

	state, exists := psm.states[key]
	if !exists {
		return nil, fmt.Errorf("state not found for key %s", key)
	}

	return state, nil
}

// updateState 更新状态（内部方法）
func (psm *PaymentStateMachine) updateState(key string, updater func(*PaymentState) error) error {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	state, exists := psm.states[key]
	if !exists {
		return fmt.Errorf("state not found for key %s", key)
	}

	// Copy state to avoid race condition
	newState := *state
	if err := updater(&newState); err != nil {
		return err
	}

	newState.UpdatedAt = time.Now()
	psm.states[key] = &newState

	return nil
}

// processEvent 处理事件并触发状态转换（内部方法）
func (psm *PaymentStateMachine) processEvent(ctx context.Context, userID, appID, productID, event string, payload interface{}) error {
	key := fmt.Sprintf("%s:%s:%s", userID, appID, productID)

	psm.mu.Lock()
	state, exists := psm.states[key]
	psm.mu.Unlock()

	if !exists {
		return fmt.Errorf("state not found for key %s", key)
	}

	log.Printf("Processing event %s for state %s", event, key)
	log.Printf("Current state: PaymentNeed=%v, DeveloperSync=%s, LarePassSync=%s, SignatureStatus=%s, PaymentStatus=%s",
		state.PaymentNeed, state.DeveloperSync, state.LarePassSync, state.SignatureStatus, state.PaymentStatus)

	// 根据事件类型处理状态转换
	var nextState *PaymentState
	var err error

	switch event {
	case "start_payment":
		nextState, err = psm.handleStartPayment(ctx, state, payload)
	case "signature_submitted":
		nextState, err = psm.handleSignatureSubmitted(ctx, state, payload)
	case "payment_completed":
		nextState, err = psm.handlePaymentCompleted(ctx, state, payload)
	case "vc_received":
		nextState, err = psm.handleVCReceived(ctx, state, payload)
	case "request_signature":
		nextState, err = psm.handleRequestSignature(ctx, state, payload)
	default:
		return fmt.Errorf("unknown event: %s", event)
	}

	if err != nil {
		log.Printf("Error processing event %s: %v", event, err)
		return err
	}

	// Update state
	psm.mu.Lock()
	nextState.UpdatedAt = time.Now()
	psm.states[key] = nextState
	psm.mu.Unlock()

	log.Printf("State updated after event %s", event)
	log.Printf("New state: PaymentNeed=%v, DeveloperSync=%s, LarePassSync=%s, SignatureStatus=%s, PaymentStatus=%s",
		nextState.PaymentNeed, nextState.DeveloperSync, nextState.LarePassSync, nextState.SignatureStatus, nextState.PaymentStatus)

	return nil
}

// handleStartPayment 处理开始支付事件
func (psm *PaymentStateMachine) handleStartPayment(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling start_payment event")

	newState := *state
	newState.PaymentNeed = PaymentNeedRequired

	// 根据当前状态决定下一步
	// TODO: 实现具体业务逻辑
	// 1. 检查是否需要签名
	// 2. 如果需要，更新 LarePassSync 和 SignatureStatus
	// 3. 如果不需要签名，直接通知前端支付

	return &newState, nil
}

// handleSignatureSubmitted 处理签名提交事件
func (psm *PaymentStateMachine) handleSignatureSubmitted(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling signature_submitted event")

	// 从 payload 中提取 JWS 和 SignBody
	type SignaturePayload struct {
		JWS      string
		SignBody string
	}

	payloadData, ok := payload.(SignaturePayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload for signature_submitted event")
	}

	newState := *state
	newState.JWS = payloadData.JWS
	newState.SignBody = payloadData.SignBody
	newState.LarePassSync = LarePassSyncCompleted
	newState.SignatureStatus = SignatureRequiredAndSigned

	// 后续处理：尝试从开发者获取 VC
	go psm.requestVCFromDeveloper(&newState)

	return &newState, nil
}

// handlePaymentCompleted 处理支付完成事件
func (psm *PaymentStateMachine) handlePaymentCompleted(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling payment_completed event")

	// 从 payload 中提取 txHash 和 systemChainID
	type PaymentPayload struct {
		TxHash        string
		SystemChainID int
	}

	payloadData, ok := payload.(PaymentPayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload for payment_completed event")
	}

	newState := *state
	newState.TxHash = payloadData.TxHash
	newState.SystemChainID = payloadData.SystemChainID
	newState.PaymentStatus = PaymentFrontendCompleted

	// 后续处理：轮询开发者服务获取 VC
	go psm.pollForVCFromDeveloper(&newState)

	return &newState, nil
}

// handleVCReceived 处理VC接收事件
func (psm *PaymentStateMachine) handleVCReceived(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling vc_received event")

	// 从 payload 中提取 VC
	payloadData, ok := payload.(string)
	if !ok {
		return nil, fmt.Errorf("invalid payload for vc_received event")
	}

	newState := *state
	newState.VC = payloadData
	newState.DeveloperSync = DeveloperSyncCompleted
	newState.PaymentStatus = PaymentDeveloperConfirmed

	// 存储购买信息到 Redis
	go psm.storePurchaseInfo(&newState)

	// 通知前端购买完成
	if psm.dataSender != nil {
		notifyFrontendPurchaseCompleted(psm.dataSender, newState.UserID, newState.AppID, newState.AppName, newState.SourceID)
	}

	return &newState, nil
}

// handleRequestSignature 处理请求签名事件
func (psm *PaymentStateMachine) handleRequestSignature(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling request_signature event")

	// 更新签名状态
	newState := *state
	newState.SignatureStatus = SignatureRequired

	// 通知 LarePass 进行签名
	if psm.dataSender != nil && newState.XForwardedHost != "" {
		notifyLarePassToSign(
			psm.dataSender,
			newState.UserID,
			newState.AppID,
			newState.ProductID,
			newState.TxHash,
			newState.XForwardedHost,
			newState.SystemChainID,
		)
	}

	return &newState, nil
}

// requestVCFromDeveloper 从开发者请求 VC
func (psm *PaymentStateMachine) requestVCFromDeveloper(state *PaymentState) {
	log.Printf("Requesting VC from developer for user %s, app %s", state.UserID, state.AppID)

	if state.JWS == "" {
		log.Printf("JWS is empty, cannot request VC")
		return
	}

	vc, err := queryVCFromDeveloper(state.JWS, state.DeveloperName)
	if err != nil {
		log.Printf("Failed to get VC from developer: %v", err)
		// 如果获取失败，可能需要通知前端重新支付或其他处理
		return
	}

	// 触发 VC 接收事件
	if err := psm.processEvent(context.Background(), state.UserID, state.AppID, state.ProductID, "vc_received", vc); err != nil {
		log.Printf("Failed to process vc_received event: %v", err)
	}
}

// pollForVCFromDeveloper 轮询开发者获取 VC
func (psm *PaymentStateMachine) pollForVCFromDeveloper(state *PaymentState) {
	log.Printf("Starting VC polling for user %s, app %s", state.UserID, state.AppID)

	maxAttempts := 20
	interval := 30 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		log.Printf("VC polling attempt %d/%d for user %s, app %s", attempt, maxAttempts, state.UserID, state.AppID)

		vc, err := queryVCFromDeveloper(state.JWS, state.DeveloperName)
		if err != nil {
			log.Printf("VC polling attempt %d failed: %v", attempt, err)
			if attempt == maxAttempts {
				log.Printf("All VC polling attempts failed")
				// TODO: 通知前端超时或错误
				return
			}
			time.Sleep(interval)
			continue
		}

		// VC obtained successfully
		log.Printf("VC obtained successfully on attempt %d", attempt)
		if err := psm.processEvent(context.Background(), state.UserID, state.AppID, state.ProductID, "vc_received", vc); err != nil {
			log.Printf("Failed to process vc_received event: %v", err)
		}
		return
	}
}

// storePurchaseInfo 存储购买信息到 Redis
func (psm *PaymentStateMachine) storePurchaseInfo(state *PaymentState) error {
	if psm.settingsManager == nil {
		return errors.New("settings manager is nil")
	}

	// Create purchase info
	purchaseInfo := &types.PurchaseInfo{
		VC:     state.VC,
		Status: string(state.PaymentStatus),
	}

	// Convert to JSON
	data, err := json.Marshal(purchaseInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal purchase info: %w", err)
	}

	// Generate Redis key
	key := fmt.Sprintf("payment:receipt:%s:%s:%s:%s", state.UserID, state.DeveloperName, state.AppID, state.ProductID)

	// Store in Redis
	rc := psm.settingsManager.GetRedisClient()
	if rc == nil {
		return fmt.Errorf("redis client is nil")
	}

	if err := rc.Set(key, string(data), 0); err != nil {
		return fmt.Errorf("failed to store purchase info in Redis: %w", err)
	}

	log.Printf("Purchase info stored in Redis with key: %s", key)
	return nil
}

// getStateTransitionHistory 获取状态转换历史（可选，用于调试，内部方法）
func (psm *PaymentStateMachine) getStateTransitionHistory(userID, appID, productID string) []StateTransition {
	// TODO: 实现状态转换历史记录
	return nil
}

// cleanupCompletedStates 清理已完成的状态（内部方法）
func (psm *PaymentStateMachine) cleanupCompletedStates(olderThan time.Duration) {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	for key, state := range psm.states {
		if state.IsFinalState() && state.UpdatedAt.Before(cutoff) {
			delete(psm.states, key)
			log.Printf("Cleaned up completed state: %s", key)
		}
	}
}
