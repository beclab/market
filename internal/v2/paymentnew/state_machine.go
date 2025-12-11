package paymentnew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"market/internal/v2/types"
)

// FrontendPaymentStartedPayload carries frontend-provided metadata when payment is about to start on-chain
type FrontendPaymentStartedPayload struct {
	Data map[string]interface{} `json:"data"`
}

func cloneFrontendData(data map[string]interface{}) map[string]interface{} {
	if len(data) == 0 {
		return nil
	}
	copyData := make(map[string]interface{}, len(data))
	for k, val := range data {
		copyData[k] = val
	}
	return copyData
}

func attachFrontendPayload(response map[string]interface{}, data map[string]interface{}) {
	cloned := cloneFrontendData(data)
	// Always set frontend_data field, even if empty, to ensure consistent response structure
	if cloned == nil {
		cloned = make(map[string]interface{})
	}
	response["frontend_data"] = cloned
	response["frontend_payment_started_payload"] = FrontendPaymentStartedPayload{
		Data: cloned,
	}
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
	psm.mu.RLock()
	state, exists := psm.states[key]
	psm.mu.RUnlock()

	if !exists {
		// Try to load from Redis if not in memory
		// Extract userID, appID, productID from key (format: "userID:appID:productID")
		parts := strings.Split(key, ":")
		if len(parts) != 3 {
			return fmt.Errorf("invalid state key format: %s", key)
		}
		if loaded, err := psm.LoadState(parts[0], parts[1], parts[2]); err == nil && loaded != nil {
			state = loaded
			log.Printf("updateState: Loaded state from Redis for key %s", key)
		} else {
			return fmt.Errorf("state not found for key %s", key)
		}
	} else {
		// State exists in memory, but if FrontendData is nil/empty, reload from Redis to ensure we have latest data
		// This prevents overwriting FrontendData that was saved by a recent operation
		if state.FrontendData == nil || len(state.FrontendData) == 0 {
			parts := strings.Split(key, ":")
			if len(parts) == 3 {
				if loaded, err := psm.forceReloadState(parts[0], parts[1], parts[2]); err == nil && loaded != nil {
					// Only use loaded state if it has FrontendData, otherwise keep memory state
					if loaded.FrontendData != nil && len(loaded.FrontendData) > 0 {
						state = loaded
						log.Printf("updateState: Reloaded state from Redis for key %s (memory state had empty FrontendData)", key)
					}
				}
			}
		}
	}

	// Copy state to avoid race condition
	newState := *state
	if err := updater(&newState); err != nil {
		return err
	}

	newState.UpdatedAt = time.Now()

	psm.mu.Lock()
	psm.states[key] = &newState
	psm.mu.Unlock()

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
		// Try to load from Redis if not in memory
		if loaded, err := psm.LoadState(userID, appID, productID); err == nil && loaded != nil {
			state = loaded
			log.Printf("processEvent: Loaded state from Redis for key %s", key)
		} else {
			return fmt.Errorf("state not found for key %s", key)
		}
	}

	log.Printf("Processing event %s for state %s", event, key)
	log.Printf("Current state: PaymentNeed=%v, DeveloperSync=%s, LarePassSync=%s, SignatureStatus=%s, PaymentStatus=%s, FrontendData length=%d",
		state.PaymentNeed, state.DeveloperSync, state.LarePassSync, state.SignatureStatus, state.PaymentStatus, len(state.FrontendData))

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

	// Update state in memory and persist
	psm.mu.Lock()
	nextState.UpdatedAt = time.Now()
	psm.states[key] = nextState
	psm.mu.Unlock()

	if err := psm.SaveState(nextState); err != nil {
		log.Printf("Failed to persist state after event %s for %s: %v", event, key, err)
	}

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
	// 0) Error states that should short-circuit (no further processing)
	if newState.SignatureStatus == SignatureErrorNoRecord {
		log.Printf("handleStartPayment: signature status is error_no_record, no further processing needed for %s", newState.GetKey())
		log.Printf("handleStartPayment: FrontendData length=%d before returning", len(newState.FrontendData))
		// Ensure FrontendData is preserved (should already be set, but ensure it's not nil)
		if newState.FrontendData == nil {
			newState.FrontendData = make(map[string]interface{})
		}
		return &newState, nil
	}

	// 0.5) Signature invalid → reset and re-trigger sign flow
	isReSignFlow := false
	if newState.SignatureStatus == SignatureErrorNeedReSign {
		log.Printf("handleStartPayment: signature marked invalid, resetting state for re-sign (%s)", newState.GetKey())
		resetForResign := func(ps *PaymentState) {
			ps.SignatureStatus = SignatureRequired
			ps.JWS = ""
			ps.SignBody = ""
		}
		resetForResign(&newState)
		isReSignFlow = true

		if psm != nil {
			if err := psm.updateState(newState.GetKey(), func(s *PaymentState) error {
				resetForResign(s)
				if s.XForwardedHost == "" && effectiveHost != "" {
					s.XForwardedHost = effectiveHost
				}
				return nil
			}); err != nil {
				log.Printf("handleStartPayment: failed to persist re-sign reset: %v", err)
			}
		}
	}

	// 1) Need to initiate signature
	if newState.SignatureStatus == SignatureRequired || newState.SignatureStatus == SignatureRequiredButPending {
		if newState.XForwardedHost == "" && effectiveHost != "" {
			newState.XForwardedHost = effectiveHost
		} else if effectiveHost == "" && newState.XForwardedHost != "" {
			effectiveHost = newState.XForwardedHost
		}
		if isReSignFlow {
			newState.LarePassSync = LarePassSyncInProgress
		} else if newState.LarePassSync == LarePassSyncNotStarted || newState.LarePassSync == LarePassSyncFailed {
			newState.LarePassSync = LarePassSyncInProgress
		}

		if psm != nil && psm.dataSender != nil && effectiveHost != "" {
			// write host before notifying to ensure callback URL is valid
			_ = psm.updateState(newState.GetKey(), func(s *PaymentState) error {
				if s.XForwardedHost == "" {
					s.XForwardedHost = effectiveHost
				}
				if isReSignFlow {
					s.LarePassSync = LarePassSyncInProgress
				} else if s.LarePassSync == LarePassSyncNotStarted || s.LarePassSync == LarePassSyncFailed {
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
				newState.DeveloperName,
				isReSignFlow,
			)

			// Notify frontend of signature_required status
			if err := notifyFrontendStateUpdate(psm.dataSender, newState.UserID, newState.AppID, newState.AppName, newState.SourceID, "signature_required"); err != nil {
				log.Printf("Failed to notify frontend signature_required for user=%s app=%s product=%s: %v", newState.UserID, newState.AppID, newState.ProductID, err)
			} else {
				log.Printf("Successfully notified frontend signature_required for user=%s app=%s product=%s", newState.UserID, newState.AppID, newState.ProductID)
			}
		}
		return &newState, nil
	}

	// 1.5) Signed + frontend already completed -> restart VC polling (e.g. re-sign flow)
	// Skip if VC already confirmed to avoid unnecessary polling
	if newState.SignatureStatus == SignatureRequiredAndSigned &&
		newState.PaymentStatus == PaymentFrontendCompleted {
		if newState.DeveloperSync == DeveloperSyncCompleted && newState.VC != "" {
			log.Printf("handleStartPayment: VC already confirmed, skipping VC polling for %s", newState.GetKey())
			return &newState, nil
		}
		log.Printf("handleStartPayment: signature ready and payment already completed; restarting VC polling for %s", newState.GetKey())
		if psm != nil {
			go psm.pollForVCFromDeveloper(&newState)
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
				developerDID := getValidDeveloperDID(&newState)
				if developerDID != "" {
					_ = notifyFrontendPaymentRequired(
						psm.dataSender,
						newState.UserID,
						newState.AppID,
						newState.AppName,
						newState.SourceID,
						newState.ProductID,
						developerDID,
						effectiveHost,
						nil, // appInfo not available in state machine context
					)
				} else {
					log.Printf("Cannot notify frontend payment required: invalid developer DID in newState")
				}

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
	newState.FrontendData = cloneFrontendData(data)
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

	// Extract txHash from payload
	type PaymentPayload struct {
		TxHash string
	}

	payloadData, ok := payload.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid payload for payment_completed event")
	}

	newState := *state
	if txHash, ok := payloadData["tx_hash"].(string); ok {
		newState.TxHash = txHash
	}
	newState.PaymentStatus = PaymentFrontendCompleted

	// Notify frontend of waiting_developer_confirmation status
	if psm.dataSender != nil {
		if err := notifyFrontendStateUpdate(psm.dataSender, newState.UserID, newState.AppID, newState.AppName, newState.SourceID, "waiting_developer_confirmation"); err != nil {
			log.Printf("Failed to notify frontend waiting_developer_confirmation for user=%s app=%s product=%s: %v", newState.UserID, newState.AppID, newState.ProductID, err)
		} else {
			log.Printf("Successfully notified frontend waiting_developer_confirmation for user=%s app=%s product=%s", newState.UserID, newState.AppID, newState.ProductID)
		}
	}

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
	log.Printf("vc_received: updated state user=%s app=%s product=%s developerSync=%s paymentStatus=%s", newState.UserID, newState.AppID, newState.ProductID, newState.DeveloperSync, newState.PaymentStatus)

	// Store purchase info to Redis
	go func(stateCopy PaymentState) {
		if err := psm.storePurchaseInfo(&stateCopy); err != nil {
			log.Printf("storePurchaseInfo failed for user=%s app=%s product=%s: %v", stateCopy.UserID, stateCopy.AppID, stateCopy.ProductID, err)
		} else {
			log.Printf("storePurchaseInfo succeeded for user=%s app=%s product=%s", stateCopy.UserID, stateCopy.AppID, stateCopy.ProductID)
		}
	}(newState)

	// Notify frontend of purchase completion
	if psm.dataSender != nil {
		if err := notifyFrontendPurchaseCompleted(psm.dataSender, newState.UserID, newState.AppID, newState.AppName, newState.SourceID); err != nil {
			log.Printf("Failed to notify frontend purchase completed for user=%s app=%s product=%s: %v", newState.UserID, newState.AppID, newState.ProductID, err)
		} else {
			log.Printf("Successfully notified frontend purchase completed for user=%s app=%s product=%s", newState.UserID, newState.AppID, newState.ProductID)
		}

		// Also push VC to LarePass for persistence
		if err := notifyLarePassToSaveVC(psm.dataSender, &newState); err != nil {
			log.Printf("Failed to notify LarePass save_payment_vc for user=%s app=%s product=%s: %v", newState.UserID, newState.AppID, newState.ProductID, err)
		} else {
			log.Printf("Successfully notified LarePass save_payment_vc for user=%s app=%s product=%s", newState.UserID, newState.AppID, newState.ProductID)
		}
	} else {
		log.Printf("Warning: dataSender is nil, cannot notify frontend purchase completed for user=%s app=%s product=%s", newState.UserID, newState.AppID, newState.ProductID)
	}

	return &newState, nil
}

// handleRequestSignature handles request_signature event
func (psm *PaymentStateMachine) handleRequestSignature(ctx context.Context, state *PaymentState, payload interface{}) (*PaymentState, error) {
	log.Printf("Handling request_signature event")

	// 更新签名状态
	newState := *state
	newState.SignatureStatus = SignatureRequired

	isReSign := state.SignatureStatus == SignatureErrorNeedReSign
	if payloadMap, ok := payload.(map[string]interface{}); ok {
		if raw, ok := payloadMap["resign"]; ok {
			switch v := raw.(type) {
			case bool:
				isReSign = v
			case string:
				isReSign = strings.EqualFold(v, "true") || v == "1"
			}
		}
	}

	// 通知 LarePass 进行签名
	if psm.dataSender != nil && newState.XForwardedHost != "" {
		notifyLarePassToSign(
			psm.dataSender,
			newState.UserID,
			newState.AppID,
			newState.ProductID,
			newState.TxHash,
			newState.XForwardedHost,
			newState.DeveloperName,
			isReSign,
		)
	}

	// Notify frontend of signature_required status
	if psm.dataSender != nil {
		if err := notifyFrontendStateUpdate(psm.dataSender, newState.UserID, newState.AppID, newState.AppName, newState.SourceID, "signature_required"); err != nil {
			log.Printf("Failed to notify frontend signature_required for user=%s app=%s product=%s: %v", newState.UserID, newState.AppID, newState.ProductID, err)
		} else {
			log.Printf("Successfully notified frontend signature_required for user=%s app=%s product=%s", newState.UserID, newState.AppID, newState.ProductID)
		}
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

	// Refresh state from store to avoid stale in-memory copy (especially after re-sign)
	if _, err := psm.forceReloadState(state.UserID, state.AppID, state.ProductID); err != nil {
		log.Printf("VC polling: failed to refresh state from store: %v", err)
	}

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
	fixedInterval := 5 * time.Second

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
			if (latest.VC != "" && latest.DeveloperSync == DeveloperSyncCompleted) || (latest.PaymentStatus == PaymentDeveloperConfirmed && latest.VC != "") {
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
			if result.Code == 0 && result.VC == "" {
				log.Printf("VC polling attempt %d failed: code=0 but VC empty; treating as developer error", attempt)
				if attempt == maxAttempts {
					_ = psm.updateState(key, func(s *PaymentState) error {
						if s.DeveloperSync != DeveloperSyncCompleted {
							s.DeveloperSync = DeveloperSyncFailed
						}
						return nil
					})
					return
				}
				continue
			}

			switch result.Code {
			case 1:
				log.Printf("VC polling attempt %d received no record; will keep polling until max attempts", attempt)
				if attempt == maxAttempts {
					log.Printf("VC polling reached max attempts with no record; updating state as no record")
					_ = psm.updateState(key, func(s *PaymentState) error {
						s.DeveloperSync = DeveloperSyncCompleted
						s.SignatureStatus = SignatureErrorNoRecord
						return nil
					})
					// Notify frontend of state update
					if psm.dataSender != nil {
						latest, _ := psm.getState(state.UserID, state.AppID, state.ProductID)
						if latest != nil {
							var statusToNotify string
							if latest.PaymentStatus == PaymentNotificationSent {
								statusToNotify = "payment_retry_required"
							} else {
								statusToNotify = "signature_no_record"
							}
							if err := notifyFrontendStateUpdate(psm.dataSender, latest.UserID, latest.AppID, latest.AppName, latest.SourceID, statusToNotify); err != nil {
								log.Printf("Failed to notify frontend %s for user=%s app=%s product=%s: %v", statusToNotify, latest.UserID, latest.AppID, latest.ProductID, err)
							} else {
								log.Printf("Successfully notified frontend %s for user=%s app=%s product=%s", statusToNotify, latest.UserID, latest.AppID, latest.ProductID)
							}
						}
					}
					return
				}
				// keep polling
			case 2:
				log.Printf("VC polling received terminal code=2 (need re-sign), stop polling for user %s, app %s", state.UserID, state.AppID)
				_ = psm.updateState(key, func(s *PaymentState) error {
					s.DeveloperSync = DeveloperSyncCompleted
					s.SignatureStatus = SignatureErrorNeedReSign
					return nil
				})
				// Notify frontend of signature_need_resign status
				if psm.dataSender != nil {
					latest, _ := psm.getState(state.UserID, state.AppID, state.ProductID)
					if latest != nil {
						if err := notifyFrontendStateUpdate(psm.dataSender, latest.UserID, latest.AppID, latest.AppName, latest.SourceID, "signature_need_resign"); err != nil {
							log.Printf("Failed to notify frontend signature_need_resign for user=%s app=%s product=%s: %v", latest.UserID, latest.AppID, latest.ProductID, err)
						} else {
							log.Printf("Successfully notified frontend signature_need_resign for user=%s app=%s product=%s", latest.UserID, latest.AppID, latest.ProductID)
						}
					}
				}
				return
			default:
				log.Printf("VC polling attempt %d received unexpected code=%d; treating as developer error", attempt, result.Code)
				if attempt == maxAttempts {
					_ = psm.updateState(key, func(s *PaymentState) error {
						if s.DeveloperSync != DeveloperSyncCompleted {
							s.DeveloperSync = DeveloperSyncFailed
						}
						return nil
					})
					return
				}
			}
			// fallthrough continues to backoff/sleep for next attempt
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

		// Fixed interval between attempts as requested
		time.Sleep(fixedInterval)
	}
}

// storePurchaseInfo stores purchase info into Redis
func (psm *PaymentStateMachine) storePurchaseInfo(state *PaymentState) error {
	if psm.settingsManager == nil {
		log.Printf("storePurchaseInfo: settings manager is nil for user=%s app=%s product=%s", state.UserID, state.AppID, state.ProductID)
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
		log.Printf("storePurchaseInfo: failed to store purchase info in Redis for key=%s: %v", key, err)
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
		return nil, fmt.Errorf("failed to parse state from redis: %w", err)
	}
	// Ensure FrontendData is at least an empty map, not nil, to avoid issues with omitempty
	// This ensures FrontendData is always available even if it wasn't serialized in Redis
	if st.FrontendData == nil {
		st.FrontendData = make(map[string]interface{})
	}
	psm.setState(&st)
	return &st, nil
}

// forceReloadState bypasses in-memory cache and forcibly loads state from Redis
func (psm *PaymentStateMachine) forceReloadState(userID, appID, productID string) (*PaymentState, error) {
	if psm == nil {
		return nil, fmt.Errorf("state machine is nil")
	}
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
		return nil, fmt.Errorf("failed to parse state from redis: %w", err)
	}
	// Ensure FrontendData is at least an empty map, not nil, to avoid issues with omitempty
	// This ensures FrontendData is always available even if it wasn't serialized in Redis
	if st.FrontendData == nil {
		st.FrontendData = make(map[string]interface{})
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
	// Ensure FrontendData is at least an empty map, not nil, before serialization
	// This ensures the field is always present in JSON, even if empty
	if state.FrontendData == nil {
		state.FrontendData = make(map[string]interface{})
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

	// Signature still pending or needs re-sign: wait until sign flow produces JWS before fetching
	// Error states are terminal and should not trigger further sync attempts
	if state.SignatureStatus == SignatureRequired ||
		state.SignatureStatus == SignatureRequiredButPending ||
		state.SignatureStatus == SignatureErrorNeedReSign ||
		state.SignatureStatus == SignatureErrorNoRecord {
		log.Printf("triggerPaymentStateSync: Signature status %s for %s, skip fetch-signature push", state.SignatureStatus, state.GetKey())
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
					state.AppName,
					state.SourceID,
					state.ProductID,
					state.XForwardedHost,
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
				state.AppName,
				state.SourceID,
				state.ProductID,
				state.XForwardedHost,
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

	if signed {
		// When code==0, trigger the next sync step (same as LarePassSyncCompleted)
		updatedState, _ := psm.getState(state.UserID, state.AppID, state.ProductID)
		if updatedState != nil {
			psm.triggerVCSync(updatedState)
		}
	} else {
		psm.notifyNotBuyState(state.UserID, state.AppID, state.ProductID)
	}

	return nil
}

// notifyNotBuyState sends `not_buy` status update when signature fetch reports no record
func (psm *PaymentStateMachine) notifyNotBuyState(userID, appID, productID string) {
	if psm == nil || psm.dataSender == nil {
		return
	}

	state, err := psm.getState(userID, appID, productID)
	if err != nil || state == nil {
		return
	}

	// Only error_no_record can transition to not_buy (per design)
	if state.SignatureStatus != SignatureErrorNoRecord {
		return
	}

	if !(state.PaymentStatus == PaymentNotEvaluated || state.PaymentStatus == PaymentNotNotified) {
		return
	}

	// Ensure status recorded as not_notified
	if state.PaymentStatus == PaymentNotEvaluated {
		_ = psm.updateState(state.GetKey(), func(s *PaymentState) error {
			if s.PaymentStatus == PaymentNotEvaluated {
				s.PaymentStatus = PaymentNotNotified
			}
			return nil
		})
		state.PaymentStatus = PaymentNotNotified
	}

	appName := state.AppName
	if appName == "" {
		appName = state.AppID
	}

	if err := notifyFrontendStateUpdate(psm.dataSender, state.UserID, state.AppID, appName, state.SourceID, "not_buy"); err != nil {
		log.Printf("notifyNotBuyState: failed to notify frontend not_buy for user=%s app=%s product=%s: %v",
			state.UserID, state.AppID, state.ProductID, err)
	} else {
		log.Printf("notifyNotBuyState: notified frontend not_buy for user=%s app=%s product=%s",
			state.UserID, state.AppID, state.ProductID)
	}
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

		// Notify frontend of state update
		if psm.dataSender != nil {
			latest, _ := psm.getState(state.UserID, state.AppID, state.ProductID)
			if latest != nil {
				var statusToNotify string
				switch result.Code {
				case 1:
					// Check if payment notification was sent to determine status
					if latest.PaymentStatus == PaymentNotificationSent {
						statusToNotify = "payment_retry_required"
					} else {
						statusToNotify = "signature_no_record"
					}
				case 2:
					statusToNotify = "signature_need_resign"
				}
				if statusToNotify != "" {
					if err := notifyFrontendStateUpdate(psm.dataSender, latest.UserID, latest.AppID, latest.AppName, latest.SourceID, statusToNotify); err != nil {
						log.Printf("Failed to notify frontend %s for user=%s app=%s product=%s: %v", statusToNotify, latest.UserID, latest.AppID, latest.ProductID, err)
					} else {
						log.Printf("Successfully notified frontend %s for user=%s app=%s product=%s", statusToNotify, latest.UserID, latest.AppID, latest.ProductID)
					}
				}
			}
		}
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

	// 1.6) VC already confirmed -> purchased (check before signature/payment status)
	// This takes priority over other states to avoid returning payment_required when already purchased
	if state.DeveloperSync == DeveloperSyncCompleted && state.VC != "" {
		return map[string]interface{}{
			"status":  "purchased",
			"message": "already purchased, ready to install",
		}, nil
	}

	// 1.7) Frontend has completed payment -> waiting_developer_confirmation
	// Check this early to avoid returning other states when payment is already completed
	// This prevents frontend from continuing operations when waiting for developer confirmation
	if state.PaymentStatus == PaymentFrontendCompleted {
		response := map[string]interface{}{
			"status":  "waiting_developer_confirmation",
			"message": "payment completed, waiting for developer confirmation",
		}
		attachFrontendPayload(response, state.FrontendData)
		return response, nil
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

	// 1.5) Error states handling: check if we can still return payment info for retry
	// If JWS exists and payment notification was sent, allow frontend to retry payment
	// Note: This should not apply when PaymentFrontendCompleted (already handled above)
	if state.SignatureStatus == SignatureErrorNoRecord {
		// If JWS exists and payment notification was sent (or frontend already proceeded), return payment data to allow retry
		// Use payment_retry_required instead of payment_required to distinguish from unpaid state
		// Exclude PaymentFrontendCompleted as it's already handled above
		if state.JWS != "" &&
			(state.PaymentStatus == PaymentNotificationSent ||
				state.PaymentStatus == PaymentFrontendStarted) {
			log.Printf("buildPurchaseResponse: error_no_record but JWS and notification exist, returning payment data for retry")
			log.Printf("buildPurchaseResponse: FrontendData length=%d for payment_retry_required", len(state.FrontendData))
			developerDID := getValidDeveloperDID(state)
			if developerDID == "" {
				return nil, fmt.Errorf("invalid developer DID in state, cannot create payment data")
			}
			userDID, err := getUserDID(userID, xForwardedHost)
			if err != nil {
				return nil, fmt.Errorf("failed to get user DID: %w", err)
			}
			paymentData := createFrontendPaymentData(ctx, httpClient, userDID, developerDID, state.ProductID, priceConfig, developerName, psm.settingsManager, state.SourceID)
			response := map[string]interface{}{
				"status":       "payment_retry_required",
				"payment_data": paymentData,
				"message":      "developer has no matching payment record, please retry payment",
			}
			// Ensure FrontendData is attached - attachFrontendPayload now always sets frontend_data field
			attachFrontendPayload(response, state.FrontendData)
			log.Printf("buildPurchaseResponse: payment_retry_required response includes frontend_data with %d keys", len(response["frontend_data"].(map[string]interface{})))
			return response, nil
		}
		// Otherwise return error message
		return map[string]interface{}{
			"status":  "signature_no_record",
			"message": "developer has no matching payment record, please retry payment",
		}, nil
	}
	if state.SignatureStatus == SignatureErrorNeedReSign {
		return map[string]interface{}{
			"status":  "signature_need_resign",
			"message": "signature invalid or expired, please re-sign to continue",
		}, nil
	}

	// 2) Signed -> return payment data for frontend transfer
	if state.SignatureStatus == SignatureRequiredAndSigned {
		developerDID := getValidDeveloperDID(state)
		if developerDID == "" {
			return nil, fmt.Errorf("invalid developer DID in state, cannot create payment data")
		}
		userDID, err := getUserDID(userID, xForwardedHost)
		if err != nil {
			return nil, fmt.Errorf("failed to get user DID: %w", err)
		}
		paymentData := createFrontendPaymentData(ctx, httpClient, userDID, developerDID, state.ProductID, priceConfig, developerName, psm.settingsManager, state.SourceID)
		response := map[string]interface{}{
			"status":       "payment_required",
			"payment_data": paymentData,
		}
		attachFrontendPayload(response, state.FrontendData)
		return response, nil
	}

	// 2.5) Has JWS and payment notification sent -> return payment data for frontend transfer
	// This handles cases where signature status might be in error state but JWS exists and payment notification was sent
	// Note: SignatureErrorNoRecord and SignatureErrorNeedReSign cases are already handled above, so we exclude them here
	if state.JWS != "" && state.PaymentStatus == PaymentNotificationSent &&
		state.SignatureStatus != SignatureErrorNoRecord && state.SignatureStatus != SignatureErrorNeedReSign {
		developerDID := getValidDeveloperDID(state)
		if developerDID == "" {
			return nil, fmt.Errorf("invalid developer DID in state, cannot create payment data")
		}
		userDID, err := getUserDID(userID, xForwardedHost)
		if err != nil {
			return nil, fmt.Errorf("failed to get user DID: %w", err)
		}
		paymentData := createFrontendPaymentData(ctx, httpClient, userDID, developerDID, state.ProductID, priceConfig, developerName, psm.settingsManager, state.SourceID)
		response := map[string]interface{}{
			"status":       "payment_required",
			"payment_data": paymentData,
		}
		attachFrontendPayload(response, state.FrontendData)
		return response, nil
	}

	// 3) Frontend has started payment
	if state.PaymentStatus == PaymentFrontendStarted {
		response := map[string]interface{}{
			"status": "payment_frontend_started",
		}
		attachFrontendPayload(response, state.FrontendData)
		return response, nil
	}

	// PaymentFrontendCompleted is already handled above (1.7) to prioritize waiting_developer_confirmation

	// 4) VC present -> purchased (fallback check, should have been caught earlier)
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
	log.Printf("buildPurchaseResponse: JWS present=%v, VC present=%v, TxHash=%s",
		state.JWS != "", state.VC != "", state.TxHash)
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
