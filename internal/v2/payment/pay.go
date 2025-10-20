package payment

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// TaskStatus represents the status of a payment task
type TaskStatus string

const (
	TaskStatusNotSign   TaskStatus = "not_sign"
	TaskStatusNotPay    TaskStatus = "not_pay"
	TaskStatusPurchased TaskStatus = "purchased"
)

// PaymentTask represents a payment task with unique key
type PaymentTask struct {
	UserID        string     `json:"user_id"`
	AppID         string     `json:"app_id"`
	AppName       string     `json:"app_name,omitempty"`
	SourceID      string     `json:"source_id,omitempty"` // Add source_id field
	ProductID     string     `json:"product_id"`
	Status        TaskStatus `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	JWS           string     `json:"jws,omitempty"`
	SignBody      string     `json:"sign_body,omitempty"`
	DeveloperName string     `json:"developer_name,omitempty"`
	VC            string     `json:"vc,omitempty"`
}

// TaskManager manages payment tasks with unique key constraint
type TaskManager struct {
	mu              sync.RWMutex
	tasks           map[string]*PaymentTask // key: userid:appid:productid
	dataSender      DataSenderInterface
	settingsManager *settings.SettingsManager
}

// NewTaskManager creates a new task manager
func NewTaskManager(dataSender DataSenderInterface, settingsManager *settings.SettingsManager) *TaskManager {
	return &TaskManager{
		tasks:           make(map[string]*PaymentTask),
		dataSender:      dataSender,
		settingsManager: settingsManager,
	}
}

// generateKey creates a unique key for the task
func (tm *TaskManager) generateKey(userID, appID, productID string) string {
	return fmt.Sprintf("%s:%s:%s", userID, appID, productID)
}

// CreateOrGetTask creates a new task or returns existing one
func (tm *TaskManager) CreateOrGetTask(userID, appID, productID, developerName, appName, sourceID string) (*PaymentTask, error) {
	// Try to acquire lock with timeout
	if !tm.mu.TryLock() {
		return nil, fmt.Errorf("failed to acquire lock, task manager is busy")
	}
	defer tm.mu.Unlock()

	key := tm.generateKey(userID, appID, productID)

	// Check if task already exists
	if task, exists := tm.tasks[key]; exists {
		log.Printf("Task already exists for key %s, status: %s", key, task.Status)
		return task, nil
	}

	// Create new task
	task := &PaymentTask{
		UserID:        userID,
		AppID:         appID,
		AppName:       appName,
		SourceID:      sourceID, // Add source_id
		ProductID:     productID,
		Status:        TaskStatusNotSign,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		DeveloperName: developerName,
	}

	tm.tasks[key] = task
	log.Printf("Created new payment task for key %s", key)

	// Start the task workflow
	// go tm.executeTaskWorkflow(task)

	return task, nil
}

// GetTask retrieves a task by key
func (tm *TaskManager) GetTask(userID, appID, productID string) (*PaymentTask, bool) {
	if !tm.mu.TryRLock() {
		return nil, false
	}
	defer tm.mu.RUnlock()

	key := tm.generateKey(userID, appID, productID)
	task, exists := tm.tasks[key]
	return task, exists
}

// findTaskByUserApp finds a task by user and app ignoring product id (first match)
func (tm *TaskManager) findTaskByUserApp(userID, appID string) *PaymentTask {
	if !tm.mu.TryRLock() {
		return nil
	}
	defer tm.mu.RUnlock()
	for _, t := range tm.tasks {
		if t.UserID == userID && t.AppID == appID {
			return t
		}
	}
	return nil
}

// UpdateTaskStatus updates the status of a task
func (tm *TaskManager) UpdateTaskStatus(userID, appID, productID string, status TaskStatus) error {
	if !tm.mu.TryLock() {
		return fmt.Errorf("failed to acquire lock for status update, task manager is busy")
	}
	defer tm.mu.Unlock()

	key := tm.generateKey(userID, appID, productID)
	task, exists := tm.tasks[key]
	if !exists {
		return fmt.Errorf("task not found for key %s", key)
	}

	task.Status = status
	task.UpdatedAt = time.Now()

	log.Printf("Updated task status for key %s to %s", key, status)
	return nil
}

// executeTaskWorkflow executes the payment task workflow
func (tm *TaskManager) executeTaskWorkflow(task *PaymentTask) {
	log.Printf("Starting payment workflow for task %s:%s:%s", task.UserID, task.AppID, task.ProductID)

	// Step 2.1: Change status to not_sign and send notification
	// if err := tm.stepNotifySign(task); err != nil {
	// 	log.Printf("Error in step 2.1 (notify sign): %v", err)
	// 	return
	// }

	globalTaskManager.stepNotifyPaymentRequired(task)

	// Wait for signature submission (this will be handled by ProcessSignatureSubmission)
	log.Printf("Task workflow started, waiting for signature submission for task %s:%s:%s",
		task.UserID, task.AppID, task.ProductID)
}

// stepNotifySign implements step 2.1: Change status to not_sign and call NotifyLarePassToSign
func (tm *TaskManager) stepNotifySign(task *PaymentTask) error {
	// Update status to not_sign
	if err := tm.UpdateTaskStatus(task.UserID, task.AppID, task.ProductID, TaskStatusNotSign); err != nil {
		return fmt.Errorf("failed to update task status to not_sign: %w", err)
	}

	// Call NotifyLarePassToSign
	signBody := fmt.Sprintf(`{"user_id":"%s","app_id":"%s","product_id":"%s"}`,
		task.UserID, task.AppID, task.ProductID)

	if err := NotifyLarePassToSign(tm.dataSender, signBody, task.UserID); err != nil {
		return fmt.Errorf("failed to notify LarePass to sign: %w", err)
	}

	log.Printf("Step 2.1 completed: Status changed to not_sign and notification sent for task %s:%s:%s",
		task.UserID, task.AppID, task.ProductID)
	return nil
}

// ProcessSignatureSubmission processes signature submission from LarePass
func (tm *TaskManager) ProcessSignatureSubmission(jws, signBody, userID string) error {
	if !tm.mu.TryLock() {
		return fmt.Errorf("failed to acquire lock for signature processing, task manager is busy")
	}
	defer tm.mu.Unlock()

	// Find the task for this user (we need to find by userID since we don't have appID/productID in the signature)
	var targetTask *PaymentTask
	for _, task := range tm.tasks {
		if task.UserID == userID && task.Status == TaskStatusNotSign {
			targetTask = task
			break
		}
	}

	if targetTask == nil {
		return fmt.Errorf("no pending task found for user %s", userID)
	}

	// Update task with signature information
	targetTask.JWS = jws
	targetTask.SignBody = signBody
	targetTask.Status = TaskStatusNotPay
	targetTask.UpdatedAt = time.Now()

	log.Printf("Step 2.2 completed: Signature received, status changed to not_pay for task %s:%s:%s",
		targetTask.UserID, targetTask.AppID, targetTask.ProductID)

	// Continue with step 2.3: Get VC from developer
	go tm.stepGetVC(targetTask)

	return nil
}

// stepGetVC implements step 2.3: Get VC from developer using signature
func (tm *TaskManager) stepGetVC(task *PaymentTask) {
	log.Printf("Step 2.3: Getting VC from developer for task %s:%s:%s",
		task.UserID, task.AppID, task.ProductID)

	// Call getVCFromDeveloper
	vc, err := getVCFromDeveloper(task.JWS, task.DeveloperName)
	if err != nil {
		log.Printf("Error getting VC from developer: %v", err)
		// Step 2.4.1: No VC found, notify frontend for payment with payment data
		tm.stepNotifyPaymentRequired(task)
		return
	}

	// Step 2.4.2: VC found, update status to purchased and store in Redis
	task.VC = vc
	task.Status = TaskStatusPurchased
	task.UpdatedAt = time.Now()

	// Store in Redis
	if err := tm.storePurchaseInfo(task); err != nil {
		log.Printf("Error storing purchase info in Redis: %v", err)
	}

	// Notify frontend with empty message format
	tm.stepNotifyPurchased(task)

	log.Printf("Step 2.4.2 completed: VC obtained, status changed to purchased for task %s:%s:%s",
		task.UserID, task.AppID, task.ProductID)
}

// stepNotifyPaymentRequired implements step 2.4.1: Notify frontend for payment
func (tm *TaskManager) stepNotifyPaymentRequired(task *PaymentTask) {
	log.Printf("Step 2.4.1: Notifying frontend for payment required for task %s:%s:%s",
		task.UserID, task.AppID, task.ProductID)

	// Create frontend payment data
	// Note: In a real implementation, you would need to get user DID and developer DID
	// For now, we'll use placeholder values
	userDID := "did:key:z6Mkgk8SGZQqXahNc9RumbX8LvUdCLWZskY7LFNMULdT5Z6r" // This should come from user info
	developerDID := fmt.Sprintf("did:key:%s", task.DeveloperName)         // This should come from app info

	paymentData := CreateFrontendPaymentData(userDID, developerDID, task.ProductID)

	// Create notification with payment data
	update := types.MarketSystemUpdate{
		User:       task.UserID,
		Timestamp:  time.Now().Unix(),
		NotifyType: "payment_required",
		Extensions: map[string]string{
			"app_id":    task.AppID,
			"app_name":  task.AppName,
			"source_id": task.SourceID, // Add source_id to notification
		},
		ExtensionsObj: map[string]interface{}{
			"payment_data": paymentData,
		},
	}

	// Send notification via DataSender
	if err := tm.dataSender.SendMarketSystemUpdate(update); err != nil {
		log.Printf("Error sending payment required notification: %v", err)
	}

	log.Printf("Payment required notification sent for task %s:%s:%s",
		task.UserID, task.AppID, task.ProductID)
}

// stepNotifyPurchased implements step 2.4.2: Notify frontend of purchase completion
func (tm *TaskManager) stepNotifyPurchased(task *PaymentTask) {
	log.Printf("Step 2.4.2: Notifying frontend of purchase completion for task %s:%s:%s",
		task.UserID, task.AppID, task.ProductID)

	// Create empty message format notification
	update := types.MarketSystemUpdate{
		User:       task.UserID,
		Timestamp:  time.Now().Unix(),
		NotifyType: "purchase_completed",
		Extensions: map[string]string{
			"app_id":    task.AppID,
			"app_name":  task.AppName,
			"source_id": task.SourceID, // Add source_id to notification
		},
	}

	// Send notification via DataSender
	if err := tm.dataSender.SendMarketSystemUpdate(update); err != nil {
		log.Printf("Error sending purchase completion notification: %v", err)
	}

	log.Printf("Purchase completion notification sent for task %s:%s:%s",
		task.UserID, task.AppID, task.ProductID)
}

// storePurchaseInfo stores purchase information in Redis
func (tm *TaskManager) storePurchaseInfo(task *PaymentTask) error {
	if tm.settingsManager == nil {
		return fmt.Errorf("settings manager is nil")
	}

	// Create purchase info
	purchaseInfo := &types.PurchaseInfo{
		VC:     task.VC,
		Status: string(task.Status),
	}

	// Convert to JSON
	data, err := json.Marshal(purchaseInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal purchase info: %w", err)
	}

	// Generate Redis key
	key := RedisPurchaseKey(task.UserID, task.DeveloperName, task.AppID, task.ProductID)

	// Store in Redis
	rc := tm.settingsManager.GetRedisClient()
	if rc == nil {
		return fmt.Errorf("redis client is nil")
	}

	if err := rc.Set(key, string(data), 0); err != nil {
		return fmt.Errorf("failed to store purchase info in Redis: %w", err)
	}

	log.Printf("Purchase info stored in Redis with key: %s", key)
	return nil
}

// GetTaskStatus returns the current status of a task
func (tm *TaskManager) GetTaskStatus(userID, appID, productID string) (TaskStatus, error) {
	if !tm.mu.TryRLock() {
		return "", fmt.Errorf("failed to acquire lock for status query, task manager is busy")
	}
	defer tm.mu.RUnlock()

	key := tm.generateKey(userID, appID, productID)
	task, exists := tm.tasks[key]
	if !exists {
		return "", fmt.Errorf("task not found for key %s", key)
	}

	return task.Status, nil
}

// ListTasks returns all current tasks (for debugging/monitoring)
func (tm *TaskManager) ListTasks() map[string]*PaymentTask {
	if !tm.mu.TryRLock() {
		return nil
	}
	defer tm.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]*PaymentTask)
	for key, task := range tm.tasks {
		result[key] = task
	}
	return result
}

// CleanupCompletedTasks removes completed tasks older than specified duration
func (tm *TaskManager) CleanupCompletedTasks(olderThan time.Duration) {
	if !tm.mu.TryLock() {
		log.Printf("Failed to acquire lock for cleanup, skipping this round")
		return
	}
	defer tm.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	for key, task := range tm.tasks {
		if task.Status == TaskStatusPurchased && task.UpdatedAt.Before(cutoff) {
			delete(tm.tasks, key)
			log.Printf("Cleaned up completed task: %s", key)
		}
	}
}

// StartPaymentProcess starts the payment process for a new purchase
func StartPaymentProcess(userID, appID, sourceID string, appInfo *types.AppInfo) error {
	log.Printf("=== Pay Module Starting Payment Process ===")
	log.Printf("User ID: %s", userID)
	log.Printf("App ID: %s", appID)
	log.Printf("App Info: %+v", appInfo)

	// Check if task manager is initialized
	if globalTaskManager == nil {
		log.Printf("Task manager not initialized, cannot start payment process")
		return fmt.Errorf("task manager not initialized")
	}

	// Extract required information from appInfo
	if appInfo == nil {
		return fmt.Errorf("app info is nil")
	}

	// Get developer name from app info
	developerName := ""
	if appInfo.AppEntry != nil {
		developerName = appInfo.AppEntry.Developer
	}
	if developerName == "" {
		return fmt.Errorf("developer name not found in app info")
	}

	// Get app name from app info
	appName := ""
	if appInfo.AppEntry != nil {
		appName = appInfo.AppEntry.Name
	}

	// Use a default product ID if not specified
	productID := "NonConsumable" // Default product ID
	// Note: PriceConfig doesn't have ProductID field, using default

	// Create or get existing payment task
	task, err := globalTaskManager.CreateOrGetTask(userID, appID, productID, developerName, appName, sourceID)
	if err != nil {
		log.Printf("Error creating payment task: %v", err)
		return fmt.Errorf("failed to create payment task: %w", err)
	}

	go globalTaskManager.executeTaskWorkflow(task)

	log.Printf("Payment task created/retrieved: %s:%s:%s, status: %s",
		task.UserID, task.AppID, task.ProductID, task.Status)
	log.Printf("=== End of Pay Module Starting Payment Process ===")

	return nil
}

// RetryPaymentProcess retries the payment process for existing purchase attempts
func RetryPaymentProcess(userID, appID, sourceID string, appInfo *types.AppInfo) error {
	log.Printf("=== Pay Module Retrying Payment Process ===")
	log.Printf("User ID: %s", userID)
	log.Printf("App ID: %s", appID)
	log.Printf("App Info: %+v", appInfo)

	// Check if task manager is initialized
	if globalTaskManager == nil {
		log.Printf("Task manager not initialized, cannot retry payment process")
		return fmt.Errorf("task manager not initialized")
	}

	// Extract required information from appInfo
	if appInfo == nil {
		return fmt.Errorf("app info is nil")
	}

	// Get developer name from app info
	developerName := ""
	if appInfo.AppEntry != nil {
		developerName = appInfo.AppEntry.Developer
	}
	if developerName == "" {
		return fmt.Errorf("developer name not found in app info")
	}

	// Use a default product ID if not specified
	productID := "NonConsumable" // Default product ID
	// Note: PriceConfig doesn't have ProductID field, using default

	// Check if task exists
	task, exists := globalTaskManager.GetTask(userID, appID, productID)
	if !exists {
		log.Printf("No existing task found for retry, creating new task")
		// Create new task if none exists
		return StartPaymentProcess(userID, appID, sourceID, appInfo)
	}

	// Check task status and decide retry strategy
	switch task.Status {
	case TaskStatusNotSign:
		log.Printf("Task is in not_sign status, restarting workflow")
		// Restart the workflow
		go globalTaskManager.executeTaskWorkflow(task)

	case TaskStatusNotPay:
		log.Printf("Task is in not_pay status, waiting for signature")
		// Task is waiting for signature, no action needed

	case TaskStatusPurchased:
		log.Printf("Task is already purchased, no retry needed")
		// Task is already completed, no retry needed

	default:
		log.Printf("Unknown task status: %s, restarting workflow", task.Status)
		// Restart the workflow for unknown status
		go globalTaskManager.executeTaskWorkflow(task)
	}

	log.Printf("Retry payment process completed for task %s:%s:%s, status: %s",
		task.UserID, task.AppID, task.ProductID, task.Status)
	log.Printf("=== End of Pay Module Retrying Payment Process ===")

	return nil
}

// PaymentPollingRequest represents the request for payment polling
type PaymentPollingRequest struct {
	UserID string `json:"user_id"`
	AppID  string `json:"app_id"`
}

// PaymentPollingResponse represents the response for payment polling
type PaymentPollingResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Status  string `json:"status,omitempty"`
}

// StartPaymentPolling starts polling for VC after payment completion
// appName and developerName are provided by caller (queried from cache in API layer)
func StartPaymentPolling(userID, appID string, appInfoLatest *types.AppInfoLatestData) error {
	log.Printf("=== Starting Payment Polling ===")
	log.Printf("User ID: %s", userID)
	log.Printf("App ID: %s", appID)
	if appInfoLatest != nil && appInfoLatest.RawData != nil {
		if appInfoLatest.RawData.Name != "" {
			log.Printf("App Name: %s", appInfoLatest.RawData.Name)
		}
		if appInfoLatest.RawData.Developer != "" {
			log.Printf("Developer: %s", appInfoLatest.RawData.Developer)
		}
	}

	// Check if task manager is initialized
	if globalTaskManager == nil {
		log.Printf("Task manager not initialized, cannot start payment polling")
		return fmt.Errorf("task manager not initialized")
	}

	// Locate existing task (by user/app)
	task := globalTaskManager.findTaskByUserApp(userID, appID)
	if task == nil {
		log.Printf("No existing task found for user %s app %s, creating new task from provided appInfoLatest", userID, appID)

		// Try to create a new task using provided app info
		newTask, err := createTaskFromCache(userID, appID, appInfoLatest)
		if err != nil {
			log.Printf("Failed to create task from cache: %v", err)
			return fmt.Errorf("no payment task found and failed to create from cache: %w", err)
		}
		task = newTask
	}

	// Snapshot needed fields to avoid race
	snapshot := *task

	// Start polling in background
	go func(t PaymentTask) {
		pollingResult := pollForVC(&t)
		log.Printf("Payment polling completed for user %s, app %s: %v", t.UserID, t.AppID, pollingResult)
	}(snapshot)

	log.Printf("Payment polling started for user %s, app %s", userID, appID)
	return nil
}

// pollForVC polls getVCFromDeveloper with 30s intervals for up to 10 minutes
func pollForVC(task *PaymentTask) error {
	// Check if JWS is empty (e.g., after program restart)
	if task.JWS == "" {
		log.Printf("JWS is empty for user %s, app %s - cannot poll for VC, notifying signature required", task.UserID, task.AppID)
		// JWS is empty, which means no signature is present. Need to re-trigger the payment process.
		notifySignatureRequired(task.UserID, task.AppID, task.ProductID, task.DeveloperName)
		return fmt.Errorf("JWS is empty, cannot poll for VC")
	}

	maxAttempts := 20 // 10 minutes / 30 seconds = 20 attempts
	interval := 30 * time.Second

	log.Printf("Starting VC polling for user %s, app %s, developer %s", task.UserID, task.AppID, task.DeveloperName)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		log.Printf("VC polling attempt %d/%d for user %s, app %s", attempt, maxAttempts, task.UserID, task.AppID)

		// Try to get VC from developer
		vc, err := getVCFromDeveloper(task.JWS, task.DeveloperName)
		if err != nil {
			log.Printf("VC polling attempt %d failed: %v", attempt, err)

			// If this is the last attempt, notify frontend about timeout error
			if attempt == maxAttempts {
				log.Printf("All VC polling attempts failed for user %s, app %s", task.UserID, task.AppID)
				notifyPollingTimeout(task.UserID, task.AppID, task.ProductID, task.DeveloperName)
				return fmt.Errorf("failed to get VC after %d attempts: %w", maxAttempts, err)
			}

			// Wait before next attempt
			time.Sleep(interval)
			continue
		}

		// VC obtained successfully
		log.Printf("VC obtained successfully on attempt %d for user %s, app %s", attempt, task.UserID, task.AppID)

		// Update task status to purchased and store in Redis
		if err := completePurchase(task.UserID, task.AppID, task.AppName, task.ProductID, task.DeveloperName, vc); err != nil {
			log.Printf("Failed to complete purchase for user %s, app %s: %v", task.UserID, task.AppID, err)
			return err
		}

		log.Printf("Payment completed successfully for user %s, app %s", task.UserID, task.AppID)
		return nil
	}

	return fmt.Errorf("polling timeout after %d attempts", maxAttempts)
}

// completePurchase completes the purchase by updating status and storing in Redis
func completePurchase(userID, appID, appName, productID, developerName, vc string) error {
	if globalTaskManager == nil {
		return fmt.Errorf("task manager not initialized")
	}

	// Create a temporary task for completion
	task := &PaymentTask{
		UserID:        userID,
		AppID:         appID,
		AppName:       appName,
		ProductID:     productID,
		Status:        TaskStatusPurchased,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		DeveloperName: developerName,
		VC:            vc,
	}

	// Store purchase info in Redis
	if err := globalTaskManager.storePurchaseInfo(task); err != nil {
		return fmt.Errorf("failed to store purchase info: %w", err)
	}

	// Notify frontend of purchase completion
	globalTaskManager.stepNotifyPurchased(task)

	log.Printf("Purchase completed and stored for user %s, app %s", userID, appID)
	return nil
}

// notifyPaymentRequired notifies frontend that payment is required
func notifyPaymentRequired(userID, appID, productID, developerName string) {
	if globalTaskManager == nil {
		log.Printf("Task manager not initialized, cannot send payment required notification")
		return
	}

	// Create a temporary task for notification with provided info
	task := &PaymentTask{
		UserID:        userID,
		AppID:         appID,
		ProductID:     productID,
		DeveloperName: developerName,
		Status:        TaskStatusNotPay,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Send payment required notification
	globalTaskManager.stepNotifyPaymentRequired(task)
}

// notifySignatureRequired notifies frontend that signature is required (no JWS found)
func notifySignatureRequired(userID, appID, productID, developerName string) {
	if globalTaskManager == nil {
		log.Printf("Task manager not initialized, cannot send signature required notification")
		return
	}

	// Create a temporary task for notification
	task := &PaymentTask{
		UserID:        userID,
		AppID:         appID,
		ProductID:     productID,
		DeveloperName: developerName,
		Status:        TaskStatusNotSign,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Send signature required notification (trigger payment flow again)
	globalTaskManager.stepNotifySign(task)
}

// notifyPollingTimeout notifies frontend about polling timeout error
func notifyPollingTimeout(userID, appID, productID, developerName string) {
	if globalTaskManager == nil {
		log.Printf("Task manager not initialized, cannot send polling timeout notification")
		return
	}

	// Create timeout error notification
	update := types.MarketSystemUpdate{
		User:       userID,
		Timestamp:  time.Now().Unix(),
		NotifyType: "payment_error",
		Extensions: map[string]string{
			"app_id": appID,
			"error":  "Sync payment status timeout, please try again",
		},
	}

	// Send notification via DataSender
	if err := globalTaskManager.dataSender.SendMarketSystemUpdate(update); err != nil {
		log.Printf("Error sending polling timeout notification: %v", err)
	}

	log.Printf("Polling timeout notification sent for user %s, app %s", userID, appID)
}

// createTaskFromCache creates a new payment task from cache data
func createTaskFromCache(userID, appID string, appInfoLatest *types.AppInfoLatestData) (*PaymentTask, error) {
	if globalTaskManager == nil {
		return nil, fmt.Errorf("task manager not initialized")
	}

	// Try to derive app basic info using Redis-stored app info if available later (not modifying cache manager)
	productID := "NonConsumable" // Default product ID
	appName := appID
	developerName := ""

	// Prefer provided info from caller (queried via cache in API layer)
	// Prefer fields from provided AppInfoLatest
	if appInfoLatest != nil {
		if appInfoLatest.RawData != nil {
			if appInfoLatest.RawData.Name != "" {
				appName = appInfoLatest.RawData.Name
			}
			// Developer priority per payment expectation: prefer RawData.Developer (domain-like), fallback AppEntry.Developer
			if appInfoLatest.RawData.Developer != "" {
				developerName = appInfoLatest.RawData.Developer
			}
		}
		if appName == "" && appInfoLatest.AppSimpleInfo != nil && appInfoLatest.AppSimpleInfo.AppID != "" {
			appName = appInfoLatest.AppSimpleInfo.AppID
		}
		if developerName == "" && appInfoLatest.AppInfo != nil && appInfoLatest.AppInfo.AppEntry != nil && appInfoLatest.AppInfo.AppEntry.Developer != "" {
			developerName = appInfoLatest.AppInfo.AppEntry.Developer
		}
	}

	// Read app info directly from Redis cache to rebuild fields
	// No direct cache access here; app info should be provided by caller (API layer)
	if developerName == "" {
		developerName = "unknown"
	}

	// Create a new task with minimal information
	// The task will be in not_pay status since we're assuming payment was completed
	task := &PaymentTask{
		UserID:        userID,
		AppID:         appID,
		AppName:       appName,
		ProductID:     productID,
		Status:        TaskStatusNotPay, // Assume payment was completed, waiting for VC
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		DeveloperName: developerName,
		JWS:           "", // Will try to load from Redis below
	}

	// Try loading JWS from Redis if available
	if globalTaskManager != nil && globalTaskManager.settingsManager != nil {
		if rc := globalTaskManager.settingsManager.GetRedisClient(); rc != nil {
			jwsKey := fmt.Sprintf("payment:jws:%s:%s", userID, appID)
			if jwsVal, err := rc.Get(jwsKey); err == nil && jwsVal != "" {
				task.JWS = jwsVal
				log.Printf("Loaded JWS from Redis for user %s app %s", userID, appID)
			}
		}
	}

	log.Printf("Created new task from cache for user %s, app %s", userID, appID)
	return task, nil
}
