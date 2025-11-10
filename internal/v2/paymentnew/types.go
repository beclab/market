package paymentnew

import (
	"fmt"
	"sync"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// PaymentNeed (dimension 1): whether payment is required / error states
type PaymentNeed string

const (
	PaymentNeedNotRequired               PaymentNeed = "not_required"                 // no payment required (free app)
	PaymentNeedRequired                  PaymentNeed = "required"                     // payment required
	PaymentNeedErrorMissingDeveloper     PaymentNeed = "error_missing_developer"      // developer missing in price info
	PaymentNeedErrorDeveloperFetchFailed PaymentNeed = "error_developer_fetch_failed" // DID lookup for developer failed
)

// DeveloperSyncStatus (dimension 2): whether VC info has been synced with developer
type DeveloperSyncStatus string

const (
	DeveloperSyncNotStarted DeveloperSyncStatus = "not_started"
	DeveloperSyncInProgress DeveloperSyncStatus = "in_progress"
	DeveloperSyncCompleted  DeveloperSyncStatus = "completed"
	DeveloperSyncFailed     DeveloperSyncStatus = "failed"
)

// LarePassSyncStatus (dimension 3): whether signature info has been synced with larepass
type LarePassSyncStatus string

const (
	LarePassSyncNotStarted LarePassSyncStatus = "not_started"
	LarePassSyncInProgress LarePassSyncStatus = "in_progress"
	LarePassSyncCompleted  LarePassSyncStatus = "completed"
	LarePassSyncFailed     LarePassSyncStatus = "failed"
)

// SignatureStatus (dimension 4): signature status
type SignatureStatus string

const (
	SignatureNotEvaluated       SignatureStatus = "not_evaluated"
	SignatureNotRequired        SignatureStatus = "not_required"
	SignatureRequired           SignatureStatus = "required"
	SignatureRequiredAndSigned  SignatureStatus = "required_and_signed"
	SignatureRequiredButPending SignatureStatus = "required_but_pending"
	SignatureErrorNoRecord      SignatureStatus = "error_no_record"
	SignatureErrorNeedReSign    SignatureStatus = "error_need_resign"
)

// PaymentStatus (dimension 5): payment status
type PaymentStatus string

const (
	PaymentNotEvaluated       PaymentStatus = "not_evaluated"
	PaymentNotNotified        PaymentStatus = "not_notified"
	PaymentNotificationSent   PaymentStatus = "notification_sent"
	PaymentFrontendStarted    PaymentStatus = "frontend_started"
	PaymentFrontendCompleted  PaymentStatus = "frontend_completed"
	PaymentDeveloperConfirmed PaymentStatus = "developer_confirmed"
)

// Note: Additional external-facing status strings (e.g., "not_buy") may appear in
// API response mappings for forward/backward compatibility. They are not produced
// by the state machine and are considered reserved/placeholder semantics for now.

// PaymentState is the full state of the payment state machine
type PaymentState struct {
	// Basic information
	UserID        string
	AppID         string
	AppName       string
	SourceID      string
	ProductID     string
	DeveloperName string
	// Complete developer information
	Developer DeveloperInfo `json:"developer"`

	// Five state-machine dimensions
	PaymentNeed     PaymentNeed         `json:"payment_need"`
	DeveloperSync   DeveloperSyncStatus `json:"developer_sync"`
	LarePassSync    LarePassSyncStatus  `json:"larepass_sync"`
	SignatureStatus SignatureStatus     `json:"signature_status"`
	PaymentStatus   PaymentStatus       `json:"payment_status"`

	// Associated data
	JWS            string                 `json:"jws,omitempty"`
	SignBody       string                 `json:"sign_body,omitempty"`
	VC             string                 `json:"vc,omitempty"`
	TxHash         string                 `json:"tx_hash,omitempty"`
	SystemChainID  int                    `json:"system_chain_id,omitempty"`
	XForwardedHost string                 `json:"x_forwarded_host,omitempty"`
	FrontendData   map[string]interface{} `json:"frontend_data,omitempty"`

	// Metadata
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PaymentStateMachine manages states
type PaymentStateMachine struct {
	states map[string]*PaymentState
	mu     sync.RWMutex

	dataSender      DataSenderInterface
	settingsManager *settings.SettingsManager
}

// NewPaymentStateMachine creates a new state machine instance
func NewPaymentStateMachine(dataSender DataSenderInterface, settingsManager *settings.SettingsManager) *PaymentStateMachine {
	return &PaymentStateMachine{
		states:          make(map[string]*PaymentState),
		dataSender:      dataSender,
		settingsManager: settingsManager,
	}
}

// StateTransition represents a state transition
type StateTransition struct {
	From   *PaymentState
	To     *PaymentState
	Event  string
	Reason string
	Error  error
}

// StateTransitionHandler handles a state transition
type StateTransitionHandler func(state *PaymentState, event string) (*PaymentState, error)

// DataSenderInterface defines methods to send data
type DataSenderInterface interface {
	SendSignNotificationUpdate(update types.SignNotificationUpdate) error
	SendMarketSystemUpdate(update types.MarketSystemUpdate) error
}

// GetKey returns the unique key for this state
func (ps *PaymentState) GetKey() string {
	return fmt.Sprintf("%s:%s:%s", ps.UserID, ps.AppID, ps.ProductID)
}

// IsFinalState returns whether this is a final state
func (ps *PaymentState) IsFinalState() bool {
	// Final state: either no payment required, or payment required and developer has confirmed
	if ps.PaymentNeed == PaymentNeedNotRequired {
		return true
	}
	if ps.PaymentNeed == PaymentNeedRequired &&
		ps.PaymentStatus == PaymentDeveloperConfirmed {
		return true
	}
	return false
}

// CanTransition returns whether this state can transition to target
func (ps *PaymentState) CanTransition(target *PaymentState) bool {
	// TODO: Implement transition rule checks
	return true
}

// UpdateTimestamp updates the timestamp
func (ps *PaymentState) UpdateTimestamp() {
	ps.UpdatedAt = time.Now()
}
