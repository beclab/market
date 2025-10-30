package hydrationfn

import (
	"context"
	"fmt"
	"log"

	"github.com/go-resty/resty/v2"

	"market/internal/v2/paymentnew"
)

// TaskForPaymentStep validates that AppInfo.Price matches the developer's rsaPubKey from DID, and checks purchase status
type TaskForPaymentStep struct {
	client *resty.Client
}

func NewTaskForPaymentStep() *TaskForPaymentStep {
	return &TaskForPaymentStep{client: resty.New()}
}

func (s *TaskForPaymentStep) GetStepName() string { return "TaskForPaymentStep" }

func (s *TaskForPaymentStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	pending := (&TaskForApiStep{}).findPendingDataFromCache(task)
	if pending == nil || pending.AppInfo == nil || pending.AppInfo.Price == nil {
		return true
	}
	return false
}

func (s *TaskForPaymentStep) Execute(ctx context.Context, task *HydrationTask) error {
	pending := (&TaskForApiStep{}).findPendingDataFromCache(task)
	if pending == nil || pending.AppInfo == nil {
		return fmt.Errorf("no pending app info for payment step")
	}

	// Call PreprocessAppPaymentData from paymentnew API
	pi, err := paymentnew.PreprocessAppPaymentData(
		ctx,
		pending.AppInfo,
		task.UserID,
		task.SourceID,
		task.SettingsManager,
		s.client,
	)
	if err != nil {
		return fmt.Errorf("failed to preprocess app payment data: %w", err)
	}

	// If there is no purchase info (pi is nil), it's not an error for free apps or unpaid apps
	if pi != nil {
		pending.AppInfo.PurchaseInfo = pi
		log.Printf("Successfully loaded purchase info for user=%s app=%s", task.UserID, task.AppID)
	}

	return nil
}
