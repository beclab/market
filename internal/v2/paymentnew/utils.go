package paymentnew

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// DeveloperInfo represents subset of DID search response used by payment
type DeveloperInfo struct {
	Name      string
	DID       string
	RSAPubKey string
}

// didGateResponse is used for JSON decoding of DID search API
type didGateResponse struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    *didGateData `json:"data"`
}

type didGateData struct {
	Name      string `json:"name"`
	DID       string `json:"did"`
	RSAPubKey string `json:"rsaPubKey"`
}

// merchantProductLicenseCredentialManifestJSON is the hardcoded schema for Merchant Product License Credential Manifest
var merchantProductLicenseCredentialManifestJSON = `{"id":"c544214b-be43-6ba8-1618-c80de084aa62","spec_version":"https://identity.foundation/credential-manifest/spec/v1.0.0/","name":"Merchant Product License Credential Manifest","description":"Credential manifest for issuing merchant product license based on payment proof","issuer":{"id":"did:key:z6MktdEpjYpYocHibuZqMsjXmaVusyUHckMnkzM3xUCxpfa4#z6MktdEpjYpYocHibuZqMsjXmaVusyUHckMnkzM3xUCxpfa4","name":"default-merchant"},"output_descriptors":[{"id":"ff9561a9-607f-5e7b-cad7-01be4e3ae457","schema":"c333229e-f82b-d66c-2e16-b7ff701378cf","name":"Merchant Product License Credential","description":"Product license credential with complete payment and product information","display":{"title":{"path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"],"schema":{"type":"string"}},"properties":[{"label":"Product ID","path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"],"schema":{"type":"string"}},{"label":"systemChainId","path":["$.credentialSubject.systemChainId","$.vc.credentialSubject.systemChainId"],"schema":{"type":"string"}},{"label":"txHash","path":["$.credentialSubject.txHash","$.vc.credentialSubject.txHash"],"schema":{"type":"string"}}]},"styles":{"background":{"color":"#1e40af"},"text":{"color":"#ffffff"}}}],"format":{"jwt_vc":{"alg":["EdDSA"]}},"presentation_definition":{"id":"de434d03-052c-027a-d4cc-887be81aa941","name":"Merchant Product License Application Presentation Manifest","purpose":"Request presentation of application credentials for merchant product license","input_descriptors":[{"id":"productId","name":"Product ID","purpose":"Provide your product ID to activate from payment transaction","format":{"jwt_vc":{"alg":["EdDSA"]}},"constraints":{"fields":[{"path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"]}],"subject_is_issuer":"preferred"}}]}}`

// paymentStateStore 全局的 PaymentState 存储（内存缓存）
var (
	paymentStateStore = struct {
		mu     sync.RWMutex
		states map[string]*PaymentState
	}{
		states: make(map[string]*PaymentState),
	}
)

// getStateKey 生成状态的唯一键
func getStateKey(userID, appID, productID string) string {
	return fmt.Sprintf("%s:%s:%s", userID, appID, productID)
}

// getRedisStateKey 生成 Redis key
func getRedisStateKey(userID, appID, productID string) string {
	return fmt.Sprintf("payment:state:%s:%s:%s", userID, appID, productID)
}

// queryVCFromDeveloper 通过开发者接口查询VC（内部方法）
// 对应原来的 getVCFromDeveloper
func queryVCFromDeveloper(jws string, developerName string) (string, error) {
	if jws == "" {
		return "", errors.New("jws parameter is empty")
	}
	if developerName == "" {
		return "", errors.New("developer name is empty")
	}

	// Build base URL: https://4c94e3111.{developerName}/
	// Convert developerName to lowercase for DNS compatibility
	developerName = strings.ToLower(developerName)

	// baseURL := fmt.Sprintf("https://4c94e3111.%s", developerName)
	// test code
	baseURL := fmt.Sprintf("https://4c94e3111.%s", "tw7613781.olares.com")

	endpoint := fmt.Sprintf("%s/api/grpc/AuthService/ActivateAndGrant", baseURL)

	// Log the endpoint being called
	log.Printf("=== QueryVCFromDeveloper Request ===")
	log.Printf("Developer Name: %s", developerName)
	log.Printf("Request Endpoint: %s", endpoint)
	log.Printf("===================================")

	// Create HTTP client with timeout
	httpClient := resty.New()
	httpClient.SetTimeout(10 * time.Second)

	// Make POST request with JWS as parameter
	resp, err := httpClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"jws": jws}).
		Post(endpoint)

	if err != nil {
		return "", fmt.Errorf("failed to call AuthService: %w", err)
	}

	// Log complete response for debugging
	log.Printf("=== QueryVCFromDeveloper Response ===")
	log.Printf("Status Code: %d", resp.StatusCode())
	log.Printf("Status: %s", resp.Status())
	log.Printf("Response Headers: %+v", resp.Header())
	log.Printf("Response Body: %s", string(resp.Body()))
	log.Printf("Response Time: %v", resp.Time())
	log.Printf("===================================")

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return "", fmt.Errorf("AuthService returned non-2xx status: %d, body: %s", resp.StatusCode(), string(resp.Body()))
	}

	// Parse response to extract verifiableCredential
	var response struct {
		VerifiableCredential string `json:"verifiableCredential"`
		Error                string `json:"error,omitempty"`
		Message              string `json:"message,omitempty"`
	}

	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return "", fmt.Errorf("failed to parse AuthService response: %w", err)
	}

	if response.Error != "" {
		// Check if it's a payment not ready error
		if strings.Contains(strings.ToLower(response.Error), "payment") &&
			(strings.Contains(strings.ToLower(response.Error), "not") ||
				strings.Contains(strings.ToLower(response.Error), "no")) {
			return "", &PaymentNotReadyError{Message: response.Error}
		}
		return "", fmt.Errorf("AuthService returned error: %s", response.Error)
	}

	if response.VerifiableCredential == "" {
		return "", errors.New("AuthService returned empty verifiableCredential")
	}

	return response.VerifiableCredential, nil
}

// PaymentNotReadyError represents the case when payment information is not found in developer's service
type PaymentNotReadyError struct {
	Message string
}

func (e *PaymentNotReadyError) Error() string {
	return e.Message
}

// notifyLarePassToSign 通知 larepass 客户端进行签名（内部方法）
// 对应原来的 NotifyLarePassToSign
func notifyLarePassToSign(dataSender DataSenderInterface, userID, appID, productID, txHash, xForwardedHost string, systemChainID int) error {
	if dataSender == nil {
		return errors.New("data sender is nil")
	}

	var manifestData map[string]interface{}
	if err := json.Unmarshal([]byte(merchantProductLicenseCredentialManifestJSON), &manifestData); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	// Build SignBody - start with ProductCredentialManifest
	signBody := map[string]interface{}{
		"product_credential_manifest": manifestData,
	}

	// Only add application_verifiable_credential if all required fields are present
	if productID != "" && txHash != "" && systemChainID != 0 {
		appVerifiableCredential := map[string]interface{}{
			"productId":     productID,
			"systemChainId": systemChainID,
			"txHash":        txHash,
		}
		signBody["application_verifiable_credential"] = appVerifiableCredential
		log.Printf("Including application_verifiable_credential in sign notification for user %s, app %s", userID, appID)
	} else {
		log.Printf("Skipping application_verifiable_credential for user %s, app %s (productID: %s, txHash: %s, systemChainID: %d)",
			userID, appID, productID, txHash, systemChainID)
	}

	// Build callback URL using X-Forwarded-Host from request
	if xForwardedHost == "" {
		log.Printf("ERROR: X-Forwarded-Host is empty for user %s, cannot build callback URL", userID)
		return errors.New("X-Forwarded-Host is required but not available")
	}

	callbackURL := fmt.Sprintf("https://%s/app-store/api/v2/payment/submit-signature", xForwardedHost)
	log.Printf("Using X-Forwarded-Host based callback URL for user %s: %s", userID, callbackURL)

	// Create the sign notification update
	update := types.SignNotificationUpdate{
		Sign: types.SignNotificationData{
			CallbackURL: callbackURL,
			SignBody:    signBody,
		},
		User:  userID,
		Vars:  make(map[string]string),
		Topic: "market_payment",
	}

	// Send the notification via DataSender
	return dataSender.SendSignNotificationUpdate(update)
}

// notifyFrontendPaymentRequired 通知 market 前端需要进行支付（内部方法）
func notifyFrontendPaymentRequired(dataSender DataSenderInterface, userID, appID, appName, sourceID, productID string) error {
	if dataSender == nil {
		return errors.New("data sender is nil")
	}

	// Create frontend payment data
	// Note: In a real implementation, you would need to get user DID and developer DID
	// For now, we'll use placeholder values
	userDID := "did:key:z6Mkgk8SGZQqXahNc9RumbX8LvUdCLWZskY7LFNMULdT5Z6r" // This should come from user info
	developerDID := fmt.Sprintf("did:key:%s", productID)                  // This should come from app price info

	paymentData := createFrontendPaymentData(userDID, developerDID, productID)

	// Create notification with payment data
	update := types.MarketSystemUpdate{
		User:       userID,
		Timestamp:  time.Now().Unix(),
		NotifyType: "payment_required",
		Extensions: map[string]string{
			"app_id":    appID,
			"app_name":  appName,
			"source_id": sourceID,
		},
		ExtensionsObj: map[string]interface{}{
			"payment_data": paymentData,
		},
	}

	// Send notification via DataSender
	return dataSender.SendMarketSystemUpdate(update)
}

// notifyFrontendPurchaseCompleted 通知前端购买完成（内部方法）
func notifyFrontendPurchaseCompleted(dataSender DataSenderInterface, userID, appID, appName, sourceID string) error {
	if dataSender == nil {
		return errors.New("data sender is nil")
	}

	// Create empty message format notification
	update := types.MarketSystemUpdate{
		User:       userID,
		Timestamp:  time.Now().Unix(),
		NotifyType: "purchase_completed",
		Extensions: map[string]string{
			"app_id":    appID,
			"app_name":  appName,
			"source_id": sourceID,
		},
	}

	// Send notification via DataSender
	return dataSender.SendMarketSystemUpdate(update)
}

// createFrontendPaymentData 创建前端支付数据（内部方法）
func createFrontendPaymentData(userDID, developerDID, productID string) *types.FrontendPaymentData {
	return &types.FrontendPaymentData{
		From: userDID,
		To:   developerDID,
		Product: []struct {
			ProductID string `json:"product_id"`
		}{
			{ProductID: productID},
		},
	}
}

// checkIfAppIsPaid checks if an app is a paid app by examining its Price configuration
// This is an internal function, use CheckIfAppIsPaid from api.go instead
func checkIfAppIsPaid(appInfo *types.AppInfo) (bool, error) {
	log.Printf("CheckIfAppIsPaid: Starting payment check for app")

	if appInfo == nil {
		log.Printf("CheckIfAppIsPaid: ERROR - app info is nil")
		return false, errors.New("app info is nil")
	}

	// Helper function to get app ID safely
	getAppID := func() string {
		if appInfo.AppEntry != nil {
			return appInfo.AppEntry.ID
		}
		return "unknown"
	}

	log.Printf("CheckIfAppIsPaid: App info received, checking price configuration")
	if appInfo.AppEntry != nil {
		log.Printf("CheckIfAppIsPaid: App ID: %s, App Name: %s", appInfo.AppEntry.ID, appInfo.AppEntry.Name)
	} else {
		log.Printf("CheckIfAppIsPaid: App entry is nil")
	}

	if appInfo.Price == nil {
		log.Printf("CheckIfAppIsPaid: App %s is not a paid app - no price configuration", getAppID())
		return false, nil // Not a paid app
	}

	log.Printf("CheckIfAppIsPaid: Price configuration found, examining products")
	log.Printf("CheckIfAppIsPaid: Price structure - Products: %+v", appInfo.Price.Products)

	// Check if there's a NonConsumable product with AcceptedCurrencies
	if appInfo.Price.Products.NonConsumable.Price.AcceptedCurrencies != nil {
		log.Printf("CheckIfAppIsPaid: NonConsumable product found with AcceptedCurrencies")
		log.Printf("CheckIfAppIsPaid: AcceptedCurrencies count: %d", len(appInfo.Price.Products.NonConsumable.Price.AcceptedCurrencies))
		log.Printf("CheckIfAppIsPaid: AcceptedCurrencies: %+v", appInfo.Price.Products.NonConsumable.Price.AcceptedCurrencies)

		if len(appInfo.Price.Products.NonConsumable.Price.AcceptedCurrencies) > 0 {
			log.Printf("CheckIfAppIsPaid: App %s is a PAID app - has valid accepted currencies", getAppID())
			return true, nil // This is a paid app
		} else {
			log.Printf("CheckIfAppIsPaid: App %s is not a paid app - accepted currencies list is empty", getAppID())
		}
	} else {
		log.Printf("CheckIfAppIsPaid: App %s is not a paid app - no accepted currencies configured", getAppID())
	}

	log.Printf("CheckIfAppIsPaid: App %s is not a paid app - no valid payment configuration", getAppID())
	return false, nil // Not a paid app
}

// getPaymentStatus gets the payment status from AppInfo
// This is an internal function, use GetPaymentStatus from api.go instead
func getPaymentStatus(appInfo *types.AppInfo) (string, error) {
	if appInfo == nil {
		return "", errors.New("app info is nil")
	}

	if appInfo.PurchaseInfo == nil {
		return "not_buy", nil // No purchase info means not bought
	}

	return appInfo.PurchaseInfo.Status, nil
}

// getPaymentStateFromStore 获取 PaymentState（内部函数）
// 先从内存中查找，如果找不到则从 Redis 读取，如果都没有则返回错误
func getPaymentStateFromStore(userID, appID, productID string, settingsManager *settings.SettingsManager) (*PaymentState, error) {
	key := getStateKey(userID, appID, productID)

	// Step 1: Try to get from memory cache
	paymentStateStore.mu.RLock()
	cachedState, exists := paymentStateStore.states[key]
	paymentStateStore.mu.RUnlock()

	if exists && cachedState != nil {
		log.Printf("PaymentState found in memory cache for key: %s", key)
		return cachedState, nil
	}

	// Step 2: Try to get from Redis
	if settingsManager == nil {
		return nil, fmt.Errorf("settings manager is nil")
	}

	rc := settingsManager.GetRedisClient()
	if rc == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	redisKey := getRedisStateKey(userID, appID, productID)
	val, err := rc.Get(redisKey)
	if err != nil {
		log.Printf("Failed to get PaymentState from Redis: %v", err)
		return nil, fmt.Errorf("payment state not found in memory or Redis: %w", err)
	}

	if val == "" {
		return nil, fmt.Errorf("payment state not found in memory or Redis")
	}

	// Parse PaymentState from JSON
	var state PaymentState
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		log.Printf("Failed to unmarshal PaymentState from Redis: %v", err)
		return nil, fmt.Errorf("failed to parse payment state from Redis: %w", err)
	}

	// Store in memory cache for next time
	paymentStateStore.mu.Lock()
	paymentStateStore.states[key] = &state
	paymentStateStore.mu.Unlock()

	log.Printf("PaymentState loaded from Redis and cached in memory for key: %s", key)
	return &state, nil
}

// savePaymentStateToStore 保存 PaymentState 到内存和 Redis（内部函数）
func savePaymentStateToStore(state *PaymentState, settingsManager *settings.SettingsManager) error {
	if state == nil {
		return fmt.Errorf("payment state is nil")
	}

	key := getStateKey(state.UserID, state.AppID, state.ProductID)
	redisKey := getRedisStateKey(state.UserID, state.AppID, state.ProductID)

	// Step 1: Save to memory cache
	paymentStateStore.mu.Lock()
	paymentStateStore.states[key] = state
	paymentStateStore.mu.Unlock()
	log.Printf("PaymentState saved to memory cache for key: %s", key)

	// Step 2: Save to Redis
	if settingsManager == nil {
		log.Printf("Warning: settings manager is nil, cannot save to Redis")
		return nil // Don't fail if settings manager is nil
	}

	rc := settingsManager.GetRedisClient()
	if rc == nil {
		log.Printf("Warning: redis client is nil, cannot save to Redis")
		return nil // Don't fail if redis client is nil
	}

	// Convert PaymentState to JSON
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal payment state: %w", err)
	}

	// Save to Redis with no expiration (0 means no expiration)
	if err := rc.Set(redisKey, string(data), 0); err != nil {
		log.Printf("Failed to save PaymentState to Redis: %v", err)
		return fmt.Errorf("failed to save payment state to Redis: %w", err)
	}

	log.Printf("PaymentState saved to Redis with key: %s", redisKey)
	return nil
}

// deletePaymentStateFromStore 从内存和 Redis 删除 PaymentState（内部函数）
func deletePaymentStateFromStore(userID, appID, productID string, settingsManager *settings.SettingsManager) error {
	key := getStateKey(userID, appID, productID)
	redisKey := getRedisStateKey(userID, appID, productID)

	// Delete from memory cache
	paymentStateStore.mu.Lock()
	delete(paymentStateStore.states, key)
	paymentStateStore.mu.Unlock()
	log.Printf("PaymentState deleted from memory cache for key: %s", key)

	// Delete from Redis
	if settingsManager != nil {
		rc := settingsManager.GetRedisClient()
		if rc != nil {
			if err := rc.Del(redisKey); err != nil {
				log.Printf("Failed to delete PaymentState from Redis: %v", err)
				return fmt.Errorf("failed to delete payment state from Redis: %w", err)
			}
			log.Printf("PaymentState deleted from Redis with key: %s", redisKey)
		}
	}

	return nil
}

// getAllPaymentStates 获取所有内存中的 PaymentState（用于调试，内部方法）
func getAllPaymentStates() map[string]*PaymentState {
	paymentStateStore.mu.RLock()
	defer paymentStateStore.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]*PaymentState)
	for key, state := range paymentStateStore.states {
		result[key] = state
	}
	return result
}

// ===== 从旧版 payment 包迁移的函数 =====

// fetchDeveloperInfo fetches developer information from DID gate (internal function)
func fetchDeveloperInfo(ctx context.Context, httpClient *resty.Client, base string, developerName string) (*DeveloperInfo, error) {
	if developerName == "" {
		return nil, errors.New("developer name is empty")
	}
	if httpClient == nil {
		httpClient = resty.New()
	}
	httpClient.SetTimeout(3 * time.Second)

	// build URL: base + /domain/faster_search/{did}
	base = strings.TrimRight(base, "/")
	escaped := url.PathEscape(developerName)
	endpoint := fmt.Sprintf("%s/domain/faster_search/%s", base, escaped)

	resp, err := httpClient.R().
		SetContext(ctx).
		Get(endpoint)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, fmt.Errorf("did gate non-2xx: %d", resp.StatusCode())
	}

	var dg didGateResponse
	if err := json.Unmarshal(resp.Body(), &dg); err != nil {
		return nil, err
	}
	if dg.Code != 0 || dg.Data == nil {
		return nil, fmt.Errorf("did gate bad response code=%d", dg.Code)
	}

	return &DeveloperInfo{
		Name:      dg.Data.Name,
		DID:       dg.Data.DID,
		RSAPubKey: dg.Data.RSAPubKey,
	}, nil
}

// verifyPaymentConsistency verifies that the RSA public key in app info matches the developer info (internal function)
func verifyPaymentConsistency(appInfo *types.AppInfo, dev *DeveloperInfo) (bool, error) {
	if appInfo == nil || appInfo.Price == nil {
		return false, errors.New("no payment info in app")
	}
	rsaInApp := strings.TrimSpace(appInfo.Price.Developer.RSAPublic)
	if rsaInApp == "" {
		return false, errors.New("no rsa public in app price.developer")
	}
	if dev == nil || dev.RSAPubKey == "" {
		return false, errors.New("no rsa public from developer info")
	}
	// normalize surrounding quotes from DID gate value
	rsaFromDid := strings.TrimSpace(dev.RSAPubKey)
	return rsaInApp == rsaFromDid, nil
}

// redisPurchaseKey builds redis key for purchase receipt (internal function)
func redisPurchaseKey(userID, developerName, appName, priceProductID string) string {
	// key convention: payment:receipt:{user}:{developer}:{app}:{product}
	return fmt.Sprintf("payment:receipt:%s:%s:%s:%s", userID, developerName, appName, priceProductID)
}

// getPurchaseInfoFromRedis loads purchase info JSON from Redis (internal function)
func getPurchaseInfoFromRedis(sm *settings.SettingsManager, key string) (*types.PurchaseInfo, error) {
	if sm == nil {
		return nil, errors.New("settings manager is nil")
	}
	rc := sm.GetRedisClient()
	if rc == nil {
		return nil, errors.New("redis client is nil")
	}
	val, err := rc.Get(key)
	if err != nil {
		return nil, err
	}
	if val == "" {
		return nil, errors.New("empty purchase info")
	}
	var pi types.PurchaseInfo
	if err := json.Unmarshal([]byte(val), &pi); err != nil {
		// if not JSON, assume raw VC string stored
		pi = types.PurchaseInfo{VC: val, Status: "unknown"}
	}
	return &pi, nil
}

// verifyPurchaseInfo verifies purchase info against manifest (internal function)
func verifyPurchaseInfo(pi *types.PurchaseInfo) bool {
	if pi == nil {
		return false
	}
	// Must have a VC when purchased
	if strings.EqualFold(pi.Status, "purchased") {
		ok, _ := verifyVCAgainstManifest(pi.VC, merchantProductLicenseCredentialManifestJSON)
		return ok
	}
	return false
}

// getProductIDFromAppInfo extracts product ID from app info (internal function)
func getProductIDFromAppInfo(appInfo *types.AppInfo) string {
	// Check if price info exists
	if appInfo == nil || appInfo.Price == nil {
		log.Printf("GetProductIDFromAppInfo: No price info available, returning empty string")
		return ""
	}

	// Check if AcceptedCurrencies exists and is not empty
	acceptedCurrencies := appInfo.Price.Products.NonConsumable.Price.AcceptedCurrencies
	if len(acceptedCurrencies) == 0 {
		log.Printf("GetProductIDFromAppInfo: No accepted currencies found, returning empty string")
		return ""
	}

	// Get product ID from the first accepted currency
	firstCurrency := acceptedCurrencies[0]
	if firstCurrency.ProductID == "" {
		log.Printf("GetProductIDFromAppInfo: Product ID is empty in first currency, returning empty string")
		return ""
	}

	log.Printf("GetProductIDFromAppInfo: Found product ID: %s", firstCurrency.ProductID)
	return firstCurrency.ProductID
}

// verifyVCAgainstManifest verifies VC against manifest using JSONPath
func verifyVCAgainstManifest(vc, manifestJSON string) (bool, error) {
	if vc == "" || manifestJSON == "" {
		return false, errors.New("empty VC or manifest")
	}

	// Decode JWT VC
	parts := strings.Split(vc, ".")
	if len(parts) != 3 {
		return false, errors.New("invalid JWT format")
	}

	// Decode payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var vcData map[string]interface{}
	if err := json.Unmarshal(payload, &vcData); err != nil {
		return false, fmt.Errorf("failed to unmarshal VC data: %w", err)
	}

	// Parse manifest
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return false, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	// TODO: Implement JSONPath validation logic
	// For now, just return true if VC has required fields
	if vcData["vc"] != nil {
		return true, nil
	}

	return false, nil
}
