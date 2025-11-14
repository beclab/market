package paymentnew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/go-resty/resty/v2"

	"market/internal/v2/types"
)

// FrontendPaymentStartedPayload carries frontend-provided metadata when payment is about to start on-chain
type FrontendPaymentStartedPayload struct {
	Data map[string]interface{}
}

// getOrCreateState only retrieves existing state; creation is no longer done here
// Deprecated: original getOrCreateState removed to avoid multiple creation points

// getState retrieves a state (internal method)
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

// updateState updates a state (internal method)
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

	// Sync to persistent store asynchronously to avoid blocking
	go func(st *PaymentState) {
		if err := psm.SaveState(st); err != nil {
			log.Printf("Failed to save updated state to store for key %s: %v", key, err)
		}
	}(&newState)

	return nil
}

// setState writes the given state into the state machine (create or overwrite)
func (psm *PaymentStateMachine) setState(state *PaymentState) {
	if state == nil {
		return
	}
	psm.mu.Lock()
	psm.states[state.GetKey()] = state
	psm.mu.Unlock()
}

// processEvent handles events and triggers state transitions (internal method)
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

	// Handle state transition by event type
	var nextState *PaymentState
	var err error

	switch event {
	case "start_payment":
		nextState, err = psm.handleStartPayment(ctx, state, payload)
	case "frontend_payment_started":
		nextState, err = psm.handleFrontendPaymentStarted(ctx, state, payload)
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

// handleStartPayment handles start_payment event
func (psm *PaymentStateMachine) handleStartPayment(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling start_payment event")

	newState := *state
	newState.PaymentNeed = PaymentNeedRequired

	// Extract optional fields from payload
	var xForwardedHost string
	switch v := payload.(type) {
	case map[string]interface{}:
		if hostRaw, ok := v["x_forwarded_host"]; ok {
			if hostStr, ok := hostRaw.(string); ok {
				xForwardedHost = hostStr
			}
		}
	}

	// Prefer state value; fallback to payload
	effectiveHost := newState.XForwardedHost
	if effectiveHost == "" && xForwardedHost != "" {
		effectiveHost = xForwardedHost
	}

	// Branch by current signature/payment status to advance flow
	// 1) Need to initiate signature
	if newState.SignatureStatus == SignatureRequired || newState.SignatureStatus == SignatureRequiredButPending {
		if psm != nil && psm.dataSender != nil && effectiveHost != "" {
			// write host before notifying to ensure callback URL is valid
			_ = psm.updateState(newState.GetKey(), func(s *PaymentState) error {
				if s.XForwardedHost == "" {
					s.XForwardedHost = effectiveHost
				}
				if s.LarePassSync == LarePassSyncNotStarted || s.LarePassSync == LarePassSyncFailed {
					s.LarePassSync = LarePassSyncInProgress
				}
				return nil
			})

			_ = notifyLarePassToSign(
				psm.dataSender,
				newState.UserID,
				newState.AppID,
				newState.ProductID,
				newState.TxHash,
				effectiveHost,
				newState.SystemChainID,
				newState.DeveloperName,
			)
		}
		return &newState, nil
	}

	// 2) Already signed -> notify frontend to pay (idempotent)
	if newState.SignatureStatus == SignatureRequiredAndSigned {
		if psm != nil && psm.dataSender != nil {
			// Guard: skip duplicate notifications when already advanced
			latest := &newState
			if st, err := psm.getState(newState.UserID, newState.AppID, newState.ProductID); err == nil && st != nil {
				latest = st
			}
			if !(latest.PaymentStatus == PaymentNotificationSent ||
				latest.PaymentStatus == PaymentFrontendStarted ||
				latest.PaymentStatus == PaymentFrontendCompleted ||
				latest.PaymentStatus == PaymentDeveloperConfirmed) {
				_ = notifyFrontendPaymentRequired(
					psm.dataSender,
					newState.UserID,
					newState.AppID,
					newState.AppName,
					newState.SourceID,
					newState.ProductID,
					newState.Developer.DID,
					effectiveHost,
					nil, // appInfo not available in state machine context
				)

				_ = psm.updateState(newState.GetKey(), func(s *PaymentState) error {
					switch s.PaymentStatus {
					case PaymentNotEvaluated, PaymentNotNotified:
						s.PaymentStatus = PaymentNotificationSent
					}
					return nil
				})
			}
		}
		return &newState, nil
	}

	// 3) Frontend payment completed -> start polling developer for VC
	if newState.PaymentStatus == PaymentFrontendCompleted {
		if psm != nil {
			go psm.pollForVCFromDeveloper(&newState)
		}
		return &newState, nil
	}

	// 3.5) Frontend payment started -> wait for completion
	if newState.PaymentStatus == PaymentFrontendStarted {
		return &newState, nil
	}

	// 4) VC already synced -> nothing to do
	if newState.DeveloperSync == DeveloperSyncCompleted && newState.VC != "" {
		return &newState, nil
	}

	// Default: no-op after marking payment need
	return &newState, nil
}

// handleFrontendPaymentStarted handles frontend_payment_started event
func (psm *PaymentStateMachine) handleFrontendPaymentStarted(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling frontend_payment_started event")

	var data map[string]interface{}

	switch v := payload.(type) {
	case FrontendPaymentStartedPayload:
		data = v.Data
	case *FrontendPaymentStartedPayload:
		if v != nil {
			data = v.Data
		}
	case map[string]interface{}:
		data = v
	case map[string]string:
		if len(v) > 0 {
			data = make(map[string]interface{}, len(v))
			for k, val := range v {
				data[k] = val
			}
		}
	default:
		if payload != nil {
			return nil, fmt.Errorf("invalid payload for frontend_payment_started event: %T", payload)
		}
	}

	newState := *state
	if len(data) > 0 {
		copyData := make(map[string]interface{}, len(data))
		for k, val := range data {
			copyData[k] = val
		}
		newState.FrontendData = copyData
	} else {
		newState.FrontendData = nil
	}
	newState.PaymentStatus = PaymentFrontendStarted

	return &newState, nil
}

// handleSignatureSubmitted handles signature_submitted event
func (psm *PaymentStateMachine) handleSignatureSubmitted(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling signature_submitted event")

	// Extract JWS and SignBody from payload
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

	// Follow-up: try to request VC from developer
	go psm.requestVCFromDeveloper(&newState)

	return &newState, nil
}

// handlePaymentCompleted handles payment_completed event
func (psm *PaymentStateMachine) handlePaymentCompleted(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling payment_completed event")

	// Extract txHash and systemChainID from payload
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
	newState.FrontendData = nil

	// Follow-up: poll developer service for VC
	go psm.pollForVCFromDeveloper(&newState)

	return &newState, nil
}

// handleVCReceived handles vc_received event
func (psm *PaymentStateMachine) handleVCReceived(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling vc_received event")

	// Extract VC from payload
	payloadData, ok := payload.(string)
	if !ok {
		return nil, fmt.Errorf("invalid payload for vc_received event")
	}

	newState := *state
	newState.VC = payloadData
	newState.DeveloperSync = DeveloperSyncCompleted
	newState.PaymentStatus = PaymentDeveloperConfirmed
	newState.FrontendData = nil

	// Store purchase info to Redis
	go psm.storePurchaseInfo(&newState)

	// Notify frontend of purchase completion
	if psm.dataSender != nil {
		notifyFrontendPurchaseCompleted(psm.dataSender, newState.UserID, newState.AppID, newState.AppName, newState.SourceID)
	}

	return &newState, nil
}

// handleRequestSignature handles request_signature event
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
			newState.DeveloperName,
		)
	}

	return &newState, nil
}

// requestVCFromDeveloper requests VC from developer
func (psm *PaymentStateMachine) requestVCFromDeveloper(state *PaymentState) {
	log.Printf("Requesting VC from developer for user %s, app %s", state.UserID, state.AppID)

	if state.JWS == "" {
		log.Printf("JWS is empty, cannot request VC")
		return
	}

	result, err := queryVCFromDeveloper(state.JWS, state.DeveloperName)
	if err != nil {
		log.Printf("Failed to get VC from developer: %v", err)
		// If failed to obtain, may need to notify frontend to repay or other handling
		return
	}

	// Only when code==0 we trigger vc_received event
	if result.Code == 0 && result.VC != "" {
		if err := psm.processEvent(context.Background(), state.UserID, state.AppID, state.ProductID, "vc_received", result.VC); err != nil {
			log.Printf("Failed to process vc_received event: %v", err)
		}
	}
}

// pollForVCFromDeveloper polls developer for VC
func (psm *PaymentStateMachine) pollForVCFromDeveloper(state *PaymentState) {
	log.Printf("Starting VC polling for user %s, app %s", state.UserID, state.AppID)

	key := state.GetKey()

	// Reentrancy guard: return if already in progress; otherwise mark in progress
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

		// Fetch latest state to avoid stale closure copy
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

		// Exponential backoff + jitter
		sleep := baseBackoff * time.Duration(1<<uint(attempt-1))
		if sleep > maxBackoff {
			sleep = maxBackoff
		}
		jitter := time.Duration(rand.Intn(250)) * time.Millisecond
		time.Sleep(sleep + jitter)
	}
}

// storePurchaseInfo stores purchase info into Redis
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

// getStateTransitionHistory retrieves state transition history (optional, for debugging; internal)
func (psm *PaymentStateMachine) getStateTransitionHistory(userID, appID, productID string) []StateTransition {
	// TODO: Implement state transition history
	return nil
}

// cleanupCompletedStates cleans up completed states (internal method)
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

// LoadState unified entry: try memory first, then fallback to Redis and write back to memory
func (psm *PaymentStateMachine) LoadState(userID, appID, productID string) (*PaymentState, error) {
	if psm == nil {
		return nil, fmt.Errorf("state machine is nil")
	}
	if st, err := psm.getState(userID, appID, productID); err == nil && st != nil {
		return st, nil
	}
	// Fallback to Redis
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

// SaveState unified entry: write to Redis and memory
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

// DeleteState unified entry: delete from Redis and memory
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

// triggerPaymentStateSync triggers sync flow for PaymentStates (placeholder implementation)
func triggerPaymentStateSync(state *PaymentState) error {
	if state == nil {
		return nil
	}

	// LarePassSync scheduling logic (reentrant)
	switch state.LarePassSync {
	case LarePassSyncNotStarted:
		// Mark as in-progress and trigger once
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
					state.DeveloperName,
				)
			}
		}
	case LarePassSyncInProgress:
		// Trigger again to avoid flow stuck due to network issues
		if globalStateMachine != nil && globalStateMachine.dataSender != nil {
			_ = notifyLarePassToFetchSignature(
				globalStateMachine.dataSender,
				state.UserID,
				state.AppID,
				state.ProductID,
				state.XForwardedHost,
				state.SystemChainID,
				state.DeveloperName,
			)
		}
	case LarePassSyncCompleted:
		// Next check: sync VC data and status
		if globalStateMachine != nil {
			globalStateMachine.triggerVCSync(state)
		}
		return nil
	case LarePassSyncFailed:
		// End on failure
		return nil
	default:
		return nil
	}

	return nil
}

// Handle fetch-signature callback (placeholder implementation)
func (psm *PaymentStateMachine) processFetchSignatureCallback(jws, signBody, user string, signed bool) error {
	// Code is parsed by upper layer; no need to read from signBody here

	// Parse productId from signBody, then locate state via user+productId
	productID, err := parseProductIDFromSignBody(signBody)
	if err != nil {
		return fmt.Errorf("failed to parse productId: %w", err)
	}
	state := psm.findStateByUserAndProduct(user, productID)
	if state == nil {
		return fmt.Errorf("no payment state found for user %s and product %s", user, productID)
	}

	// Update LarePassSync and signature status
	if err := psm.updateState(state.GetKey(), func(s *PaymentState) error {
		s.LarePassSync = LarePassSyncCompleted
		if signed {
			s.SignatureStatus = SignatureRequiredAndSigned
			s.JWS = jws
		} else {
			s.SignatureStatus = SignatureRequired
			if jws == "" {
				s.JWS = ""
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// When code==0, trigger the next sync step (same as LarePassSyncCompleted)
	if signed {
		updatedState, _ := psm.getState(state.UserID, state.AppID, state.ProductID)
		if updatedState != nil {
			psm.triggerVCSync(updatedState)
		}
	}

	return nil
}

// findStateByUserAndProduct matches state via userId + productId
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

// triggerVCSync syncs VC data and status: if JWS exists, request VC from developer service
func (psm *PaymentStateMachine) triggerVCSync(state *PaymentState) {
	if state == nil {
		return
	}
	if state.JWS == "" {
		log.Printf("triggerVCSync: JWS empty, skip for %s", state.GetKey())
		return
	}

	// Query VC asynchronously
	go func() {
		result, err := queryVCFromDeveloper(state.JWS, state.DeveloperName)
		if err != nil {
			// On network failure: only set DeveloperSync to failed; do not change Signature status
			log.Printf("triggerVCSync: failed to query VC from developer (network error): %v", err)
			_ = psm.updateState(state.GetKey(), func(s *PaymentState) error {
				s.DeveloperSync = DeveloperSyncFailed
				return nil
			})
			return
		}

		// For code==0: go unified path → trigger vc_received to finalize state/persistence/notification
		if result.Code == 0 && result.VC != "" {
			if err := psm.processEvent(context.Background(), state.UserID, state.AppID, state.ProductID, "vc_received", result.VC); err != nil {
				log.Printf("triggerVCSync: failed to process vc_received event: %v", err)
			}
			return
		}

		// Non-success path: only update sync and signature status
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

// BuildPurchaseResponse builds API-facing response content based on current state
// Only constructs response payload; it does not introduce new side effects.
func (psm *PaymentStateMachine) buildPurchaseResponse(userID, xForwardedHost string, state *PaymentState, appInfo *types.AppInfo) (map[string]interface{}, error) {
	if state == nil {
		return nil, fmt.Errorf("state is nil")
	}

	// 1) Need signature
	if state.SignatureStatus == SignatureRequired || state.SignatureStatus == SignatureRequiredButPending {
		return map[string]interface{}{
			"status": "signature_required",
		}, nil
	}

	// Create HTTP client for API calls
	httpClient := resty.New()
	httpClient.SetTimeout(10 * time.Second)
	ctx := context.Background()

	// Extract developer name and price config
	var developerName string
	var priceConfig *types.PriceConfig
	if appInfo != nil {
		priceConfig = appInfo.Price
		developerName = getDeveloperNameFromPrice(appInfo)
		log.Printf("buildPurchaseResponse: Extracted developerName=%s from appInfo", developerName)
	} else {
		// Fallback to state's developer name
		developerName = state.DeveloperName
		log.Printf("buildPurchaseResponse: Using developerName=%s from state (appInfo is nil)", developerName)
	}

	// 2) Signed -> return payment data for frontend transfer
	if state.SignatureStatus == SignatureRequiredAndSigned {
		developerDID := state.Developer.DID
		userDID, err := getUserDID(userID, xForwardedHost)
		if err != nil {
			return nil, fmt.Errorf("failed to get user DID: %w", err)
		}
		paymentData := createFrontendPaymentData(ctx, httpClient, userDID, developerDID, state.ProductID, priceConfig, developerName, psm.settingsManager, state.SourceID)
		return map[string]interface{}{
			"status":       "payment_required",
			"payment_data": paymentData,
		}, nil
	}

	// 2.5) Has JWS and payment notification sent -> return payment data for frontend transfer
	// This handles cases where signature status might be in error state but JWS exists and payment notification was sent
	if state.JWS != "" && state.PaymentStatus == PaymentNotificationSent {
		developerDID := state.Developer.DID
		userDID, err := getUserDID(userID, xForwardedHost)
		if err != nil {
			return nil, fmt.Errorf("failed to get user DID: %w", err)
		}
		paymentData := createFrontendPaymentData(ctx, httpClient, userDID, developerDID, state.ProductID, priceConfig, developerName, psm.settingsManager, state.SourceID)
		return map[string]interface{}{
			"status":       "payment_required",
			"payment_data": paymentData,
		}, nil
	}

	// 3) Frontend has completed payment
	if state.PaymentStatus == PaymentFrontendStarted {
		response := map[string]interface{}{
			"status": "payment_frontend_started",
		}
		if len(state.FrontendData) > 0 {
			response["frontend_data"] = state.FrontendData
		}
		return response, nil
	}

	if state.PaymentStatus == PaymentFrontendCompleted {
		return map[string]interface{}{
			"status":  "waiting_developer_confirmation",
			"message": "payment completed, waiting for developer confirmation",
		}, nil
	}

	// 4) VC present -> purchased
	if state.DeveloperSync == DeveloperSyncCompleted && state.VC != "" {
		return map[string]interface{}{
			"status":  "purchased",
			"message": "already purchased, ready to install",
		}, nil
	}

	// Default syncing - log all state information for debugging
	log.Printf("buildPurchaseResponse: returning syncing status for user=%s, app=%s, product=%s", userID, state.AppID, state.ProductID)
	log.Printf("buildPurchaseResponse: PaymentNeed=%s, DeveloperSync=%s, LarePassSync=%s, SignatureStatus=%s, PaymentStatus=%s",
		state.PaymentNeed, state.DeveloperSync, state.LarePassSync, state.SignatureStatus, state.PaymentStatus)
	log.Printf("buildPurchaseResponse: JWS present=%v, VC present=%v, TxHash=%s, SystemChainID=%d",
		state.JWS != "", state.VC != "", state.TxHash, state.SystemChainID)
	log.Printf("buildPurchaseResponse: XForwardedHost=%s, FrontendData length=%d, CreatedAt=%v, UpdatedAt=%v",
		state.XForwardedHost, len(state.FrontendData), state.CreatedAt, state.UpdatedAt)
	log.Printf("buildPurchaseResponse: DeveloperName=%s, SourceID=%s, AppName=%s",
		state.DeveloperName, state.SourceID, state.AppName)
	if state.Developer.DID != "" {
		log.Printf("buildPurchaseResponse: Developer.DID=%s", state.Developer.DID)
	}

	return map[string]interface{}{
		"status":  "syncing",
		"message": "synchronizing payment state",
	}, nil
}
