package hydrationfn

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-resty/resty/v2"

	"market/internal/v2/payment"
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

	// 3.1 Check if there is a payment section in AppInfo
	if pending.AppInfo.Price == nil {
		log.Printf("INFO: %s no payment section, skip", task.AppID)
		return nil
	}

	// 3.2 Call DID interface to get developer information
	didBase := os.Getenv("DID_GATE_BASE")
	if didBase == "" {
		didBase = "https://did-gate.mdogs.me"
	}
	developerName := pending.AppInfo.AppEntry.Developer
	s.client.SetTimeout(3 * time.Second)
	dev, err := payment.FetchDeveloperInfo(ctx, s.client, didBase, developerName)
	if err != nil {
		return fmt.Errorf("fetch developer info failed: %w", err)
	}

	// 3.3 Compare rsaPubKey
	ok, err := payment.VerifyPaymentConsistency(pending.AppInfo, dev)
	if err != nil {
		return fmt.Errorf("verify payment failed: %w", err)
	}
	if !ok {
		return fmt.Errorf("rsa public key not matched for developer=%s app=%s", developerName, task.AppID)
	}

	// 3.4 Get VC and status from redis
	devNameForKey := developerName
	if devNameForKey == "" {
		devNameForKey = dev.Name
	}
	appNameForKey := pending.AppInfo.AppEntry.Name
	if appNameForKey == "" {
		appNameForKey = task.AppName
	}
	key := payment.RedisPurchaseKey(task.UserID, devNameForKey, appNameForKey, "NonConsumable")
	pi, err := payment.GetPurchaseInfoFromRedis(task.SettingsManager, key)
	if err != nil {
		// If there is no purchase record, it is not considered an error, only recorded
		log.Printf("INFO: purchase info not found for key=%s: %v", key, err)
		return nil
	}

	// Write purchase information to Task.ChartData, for subsequent steps or storage

	if pi.Status == "purchased" {
		ok := payment.VerifyPurchaseInfo(pi)
		if !ok {
			return fmt.Errorf("purchase info not verified for key=%s", key)
		}
	}

	// Write purchase info to task.ChartData and attach to pending.AppInfo.PurchaseInfo
	pending.AppInfo.PurchaseInfo = pi
	return nil
}
