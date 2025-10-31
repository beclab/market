package paymentnew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"time"

	"market/internal/v2/types"
)

// getOrCreateState 仅获取已存在的状态；不再在此处创建
// 已废弃：原 getOrCreateState 已移除，避免多处创建状态

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

	// 同步到持久层（异步执行，避免阻塞）
	go func(st *PaymentState) {
		if err := psm.SaveState(st); err != nil {
			log.Printf("Failed to save updated state to store for key %s: %v", key, err)
		}
	}(&newState)

	return nil
}

// setState 将给定状态写入状态机（创建或覆盖）
func (psm *PaymentStateMachine) setState(state *PaymentState) {
	if state == nil {
		return
	}
	psm.mu.Lock()
	psm.states[state.GetKey()] = state
	psm.mu.Unlock()
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

	result, err := queryVCFromDeveloper(state.JWS, state.DeveloperName)
	if err != nil {
		log.Printf("Failed to get VC from developer: %v", err)
		// 如果获取失败，可能需要通知前端重新支付或其他处理
		return
	}

	// 只有 code=0 时才触发 VC 接收事件
	if result.Code == 0 && result.VC != "" {
		if err := psm.processEvent(context.Background(), state.UserID, state.AppID, state.ProductID, "vc_received", result.VC); err != nil {
			log.Printf("Failed to process vc_received event: %v", err)
		}
	}
}

// pollForVCFromDeveloper 轮询开发者获取 VC
func (psm *PaymentStateMachine) pollForVCFromDeveloper(state *PaymentState) {
	log.Printf("Starting VC polling for user %s, app %s", state.UserID, state.AppID)

	key := state.GetKey()

	// 防重入：如果已在进行中则直接返回；否则标记为进行中
	if err := psm.updateState(key, func(s *PaymentState) error {
		if s.DeveloperSync == DeveloperSyncInProgress {
			return fmt.Errorf("poll already in progress")
		}
		if s.DeveloperSync == DeveloperSyncNotStarted || s.DeveloperSync == DeveloperSyncFailed {
			s.DeveloperSync = DeveloperSyncInProgress
		}
		return nil
	}); err != nil {
		log.Printf("Skip starting VC polling: %v", err)
		return
	}

	// Polling controls
	maxAttempts := 20
	maxDuration := 10 * time.Minute
	baseBackoff := 5 * time.Second
	maxBackoff := 60 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), maxDuration)
	defer cancel()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			log.Printf("VC polling timed out after %v for user %s, app %s", maxDuration, state.UserID, state.AppID)
			_ = psm.updateState(key, func(s *PaymentState) error {
				if s.DeveloperSync != DeveloperSyncCompleted {
					s.DeveloperSync = DeveloperSyncFailed
				}
				return nil
			})
			return
		default:
		}

		// 拉取最新状态，避免闭包副本过期
		latest, err := psm.getState(state.UserID, state.AppID, state.ProductID)
		if err == nil && latest != nil {
			if (latest.VC != "" && latest.DeveloperSync == DeveloperSyncCompleted) || latest.PaymentStatus == PaymentDeveloperConfirmed {
				log.Printf("VC already confirmed; stop polling for user %s, app %s", state.UserID, state.AppID)
				return
			}
		}

		log.Printf("VC polling attempt %d/%d for user %s, app %s", attempt, maxAttempts, state.UserID, state.AppID)

		result, qerr := queryVCFromDeveloper(state.JWS, state.DeveloperName)
		if qerr != nil {
			log.Printf("VC polling attempt %d failed: %v", attempt, qerr)
		} else if result.Code == 0 && result.VC != "" {
			log.Printf("VC obtained successfully on attempt %d", attempt)
			if err := psm.processEvent(context.Background(), state.UserID, state.AppID, state.ProductID, "vc_received", result.VC); err != nil {
				log.Printf("Failed to process vc_received event: %v", err)
			}
			return
		} else {
			log.Printf("VC query returned code=%d, continuing poll...", result.Code)
		}

		if attempt == maxAttempts {
			log.Printf("All VC polling attempts completed without success")
			_ = psm.updateState(key, func(s *PaymentState) error {
				if s.DeveloperSync != DeveloperSyncCompleted {
					s.DeveloperSync = DeveloperSyncFailed
				}
				return nil
			})
			return
		}

		// 指数退避 + 抖动
		sleep := baseBackoff * time.Duration(1<<uint(attempt-1))
		if sleep > maxBackoff {
			sleep = maxBackoff
		}
		jitter := time.Duration(rand.Intn(250)) * time.Millisecond
		time.Sleep(sleep + jitter)
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

// LoadState 统一入口：先内存，后 Redis 回源并写回内存
func (psm *PaymentStateMachine) LoadState(userID, appID, productID string) (*PaymentState, error) {
	if psm == nil {
		return nil, fmt.Errorf("state machine is nil")
	}
	if st, err := psm.getState(userID, appID, productID); err == nil && st != nil {
		return st, nil
	}
	// 回源 Redis
	if psm.settingsManager == nil {
		return nil, fmt.Errorf("settings manager is nil")
	}
	rc := psm.settingsManager.GetRedisClient()
	if rc == nil {
		return nil, fmt.Errorf("redis client is nil")
	}
	redisKey := getRedisStateKey(userID, appID, productID)
	val, err := rc.Get(redisKey)
	if err != nil || val == "" {
		return nil, fmt.Errorf("state not found in redis: %w", err)
	}
	var st PaymentState
	if err := json.Unmarshal([]byte(val), &st); err != nil {
		return nil, fmt.Errorf("failed to parse state from redis: %w")
	}
	psm.setState(&st)
	return &st, nil
}

// SaveState 统一入口：写 Redis 并写内存
func (psm *PaymentStateMachine) SaveState(state *PaymentState) error {
	if psm == nil || state == nil {
		return fmt.Errorf("nil state machine or state")
	}
	if psm.settingsManager == nil {
		return fmt.Errorf("settings manager is nil")
	}
	rc := psm.settingsManager.GetRedisClient()
	if rc == nil {
		return fmt.Errorf("redis client is nil")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	redisKey := getRedisStateKey(state.UserID, state.AppID, state.ProductID)
	if err := rc.Set(redisKey, string(data), 0); err != nil {
		return fmt.Errorf("failed to save state to redis: %w", err)
	}
	psm.setState(state)
	return nil
}

// DeleteState 统一入口：删 Redis 并删内存
func (psm *PaymentStateMachine) DeleteState(userID, appID, productID string) error {
	if psm == nil {
		return fmt.Errorf("state machine is nil")
	}
	if psm.settingsManager != nil {
		if rc := psm.settingsManager.GetRedisClient(); rc != nil {
			if err := rc.Del(getRedisStateKey(userID, appID, productID)); err != nil {
				return fmt.Errorf("failed to delete state from redis: %w", err)
			}
		}
	}
	key := fmt.Sprintf("%s:%s:%s", userID, appID, productID)
	psm.mu.Lock()
	delete(psm.states, key)
	psm.mu.Unlock()
	return nil
}

// triggerPaymentStateSync 触发 PaymentStates 的状态同步流程（占位实现）
func triggerPaymentStateSync(state *PaymentState) error {
	if state == nil {
		return nil
	}

	// LarePassSync 调度逻辑（可重入）
	switch state.LarePassSync {
	case LarePassSyncNotStarted:
		// 标记为进行中并触发一次
		if globalStateMachine != nil {
			_ = globalStateMachine.updateState(state.GetKey(), func(s *PaymentState) error {
				s.LarePassSync = LarePassSyncInProgress
				return nil
			})
			if globalStateMachine.dataSender != nil {
				_ = notifyLarePassToFetchSignature(
					globalStateMachine.dataSender,
					state.UserID,
					state.AppID,
					state.ProductID,
					state.XForwardedHost,
					state.SystemChainID,
				)
			}
		}
	case LarePassSyncInProgress:
		// 再次触发，避免网络异常导致流程卡死
		if globalStateMachine != nil && globalStateMachine.dataSender != nil {
			_ = notifyLarePassToFetchSignature(
				globalStateMachine.dataSender,
				state.UserID,
				state.AppID,
				state.ProductID,
				state.XForwardedHost,
				state.SystemChainID,
			)
		}
	case LarePassSyncCompleted:
		// 下一项检查：同步 VC 信息与状态
		if globalStateMachine != nil {
			globalStateMachine.triggerVCSync(state)
		}
		return nil
	case LarePassSyncFailed:
		// 失败直接结束
		return nil
	default:
		return nil
	}

	return nil
}

// 处理 fetch-signature 回调（占位实现）
func (psm *PaymentStateMachine) processFetchSignatureCallback(jws, signBody, user string, code int) error {
	// 已由上层解析出 code，这里不再从 signBody 读取

	// 从 signBody 解析 productId，然后通过 user+productId 精确定位状态
	productID, err := parseProductIDFromSignBody(signBody)
	if err != nil {
		return fmt.Errorf("failed to parse productId: %w", err)
	}
	state := psm.findStateByUserAndProduct(user, productID)
	if state == nil {
		return fmt.Errorf("no payment state found for user %s and product %s", user, productID)
	}

	// 更新 LarePassSync 与签名状态
	if err := psm.updateState(state.GetKey(), func(s *PaymentState) error {
		s.LarePassSync = LarePassSyncCompleted
		if code == 0 {
			s.SignatureStatus = SignatureRequiredAndSigned
			s.JWS = jws
		} else if code == 1 {
			s.SignatureStatus = SignatureRequired
		}
		return nil
	}); err != nil {
		return err
	}

	// code==0 时，触发下一项同步检查（与 LarePassSyncCompleted 时相同）
	if code == 0 {
		updatedState, _ := psm.getState(state.UserID, state.AppID, state.ProductID)
		if updatedState != nil {
			psm.triggerVCSync(updatedState)
		}
	}

	return nil
}

// findStateByUserAndProduct 通过 userId + productId 精确匹配状态
func (psm *PaymentStateMachine) findStateByUserAndProduct(userID, productID string) *PaymentState {
	psm.mu.RLock()
	defer psm.mu.RUnlock()
	for _, st := range psm.states {
		if st != nil && st.UserID == userID && st.ProductID == productID {
			return st
		}
	}
	return nil
}

// triggerVCSync 同步 VC 信息与状态：若已有 JWS，则向开发者服务请求 VC
func (psm *PaymentStateMachine) triggerVCSync(state *PaymentState) {
	if state == nil {
		return
	}
	if state.JWS == "" {
		log.Printf("triggerVCSync: JWS empty, skip for %s", state.GetKey())
		return
	}

	// 异步查询 VC
	go func() {
		result, err := queryVCFromDeveloper(state.JWS, state.DeveloperName)
		if err != nil {
			// 网络请求失败，仅设置 DeveloperSync 为 failed，不修改 Signature 状态
			log.Printf("triggerVCSync: failed to query VC from developer (network error): %v", err)
			_ = psm.updateState(state.GetKey(), func(s *PaymentState) error {
				s.DeveloperSync = DeveloperSyncFailed
				return nil
			})
			return
		}

		// code==0 直接走统一路径：触发 vc_received 事件，完成设置终态/落库/通知
		if result.Code == 0 && result.VC != "" {
			if err := psm.processEvent(context.Background(), state.UserID, state.AppID, state.ProductID, "vc_received", result.VC); err != nil {
				log.Printf("triggerVCSync: failed to process vc_received event: %v", err)
			}
			return
		}

		// 非成功场景：仅更新同步与签名状态
		_ = psm.updateState(state.GetKey(), func(s *PaymentState) error {
			s.DeveloperSync = DeveloperSyncCompleted
			switch result.Code {
			case 1:
				s.SignatureStatus = SignatureErrorNoRecord
			case 2:
				s.SignatureStatus = SignatureErrorNeedReSign
			}
			return nil
		})
	}()
}
