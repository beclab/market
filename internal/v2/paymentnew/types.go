package paymentnew

import (
	"fmt"
	"sync"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// PaymentNeed 维度一：是否需要支付
type PaymentNeed bool

const (
	PaymentNeedNotRequired PaymentNeed = false
	PaymentNeedRequired    PaymentNeed = true
)

// DeveloperSyncStatus 维度二：是否已跟开发者同步过vc信息
type DeveloperSyncStatus string

const (
	DeveloperSyncNotStarted DeveloperSyncStatus = "not_started"
	DeveloperSyncInProgress DeveloperSyncStatus = "in_progress"
	DeveloperSyncCompleted  DeveloperSyncStatus = "completed"
	DeveloperSyncFailed     DeveloperSyncStatus = "failed"
)

// LarePassSyncStatus 维度三：是否跟larepass同步过签名信息
type LarePassSyncStatus string

const (
	LarePassSyncNotStarted LarePassSyncStatus = "not_started"
	LarePassSyncInProgress LarePassSyncStatus = "in_progress"
	LarePassSyncCompleted  LarePassSyncStatus = "completed"
	LarePassSyncFailed     LarePassSyncStatus = "failed"
)

// SignatureStatus 维度四：签名状态
type SignatureStatus string

const (
	SignatureNotRequired        SignatureStatus = "not_required"
	SignatureRequired           SignatureStatus = "required"
	SignatureRequiredAndSigned  SignatureStatus = "required_and_signed"
	SignatureRequiredButPending SignatureStatus = "required_but_pending"
)

// PaymentStatus 维度五：支付状态
type PaymentStatus string

const (
	PaymentNotNotified        PaymentStatus = "not_notified"
	PaymentNotificationSent   PaymentStatus = "notification_sent"
	PaymentFrontendCompleted  PaymentStatus = "frontend_completed"
	PaymentDeveloperConfirmed PaymentStatus = "developer_confirmed"
)

// PaymentState 支付状态机的完整状态
type PaymentState struct {
	// 基础信息
	UserID        string
	AppID         string
	AppName       string
	SourceID      string
	ProductID     string
	DeveloperName string

	// 状态机五个维度
	PaymentNeed     PaymentNeed         `json:"payment_need"`
	DeveloperSync   DeveloperSyncStatus `json:"developer_sync"`
	LarePassSync    LarePassSyncStatus  `json:"larepass_sync"`
	SignatureStatus SignatureStatus     `json:"signature_status"`
	PaymentStatus   PaymentStatus       `json:"payment_status"`

	// 关联数据
	JWS            string `json:"jws,omitempty"`
	SignBody       string `json:"sign_body,omitempty"`
	VC             string `json:"vc,omitempty"`
	TxHash         string `json:"tx_hash,omitempty"`
	SystemChainID  int    `json:"system_chain_id,omitempty"`
	XForwardedHost string `json:"x_forwarded_host,omitempty"`

	// 元数据
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PaymentStateMachine 状态机管理
type PaymentStateMachine struct {
	states map[string]*PaymentState
	mu     sync.RWMutex

	dataSender      DataSenderInterface
	settingsManager *settings.SettingsManager
}

// NewPaymentStateMachine 创建新的状态机实例
func NewPaymentStateMachine(dataSender DataSenderInterface, settingsManager *settings.SettingsManager) *PaymentStateMachine {
	return &PaymentStateMachine{
		states:          make(map[string]*PaymentState),
		dataSender:      dataSender,
		settingsManager: settingsManager,
	}
}

// StateTransition 状态转换
type StateTransition struct {
	From   *PaymentState
	To     *PaymentState
	Event  string
	Reason string
	Error  error
}

// StateTransitionHandler 状态转换处理器
type StateTransitionHandler func(state *PaymentState, event string) (*PaymentState, error)

// DataSenderInterface 定义发送数据的接口
type DataSenderInterface interface {
	SendSignNotificationUpdate(update types.SignNotificationUpdate) error
	SendMarketSystemUpdate(update types.MarketSystemUpdate) error
}

// GetKey 获取状态机的唯一键
func (ps *PaymentState) GetKey() string {
	return fmt.Sprintf("%s:%s:%s", ps.UserID, ps.AppID, ps.ProductID)
}

// IsFinalState 判断是否为终态
func (ps *PaymentState) IsFinalState() bool {
	// 终态：支付不需要，或者需要支付且已获得开发者确认
	if ps.PaymentNeed == PaymentNeedNotRequired {
		return true
	}
	if ps.PaymentNeed == PaymentNeedRequired &&
		ps.PaymentStatus == PaymentDeveloperConfirmed {
		return true
	}
	return false
}

// CanTransition 判断是否可以转换到目标状态
func (ps *PaymentState) CanTransition(target *PaymentState) bool {
	// TODO: 实现状态转换规则检查
	return true
}

// UpdateTimestamp 更新时间戳
func (ps *PaymentState) UpdateTimestamp() {
	ps.UpdatedAt = time.Now()
}
