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

// 已移除本地内存缓存（paymentStateStore），统一通过状态机内存和 Redis 访问

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
// 返回 VCQueryResult，其中 Code: 0=成功, 1=没有记录, 2=签名失效
func queryVCFromDeveloper(jws string, developerName string) (*VCQueryResult, error) {
	if jws == "" {
		return nil, errors.New("jws parameter is empty")
	}
	if developerName == "" {
		return nil, errors.New("developer name is empty")
	}

	// Build base URL: https://4c94e3111.{developerName}/
	// Convert developerName to lowercase for DNS compatibility
	developerName = strings.ToLower(developerName)

	baseURL := fmt.Sprintf("https://4c94e3111.%s", developerName)
	// test code
	// baseURL := fmt.Sprintf("https://4c94e3111.%s", "tw7613781.olares.com")

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
		return nil, fmt.Errorf("failed to call AuthService: %w", err)
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
		return nil, fmt.Errorf("AuthService returned non-2xx status: %d, body: %s", resp.StatusCode(), string(resp.Body()))
	}

	// Parse response to extract verifiableCredential
	var response struct {
		VerifiableCredential string `json:"verifiableCredential"`
		Error                string `json:"error,omitempty"`
		Message              string `json:"message,omitempty"`
		Code                 int    `json:"code,omitempty"`
	}

	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return nil, fmt.Errorf("failed to parse AuthService response: %w", err)
	}

	result := &VCQueryResult{}

	// 优先使用返回的 code 字段
	if response.Code > 0 {
		result.Code = response.Code
		return result, nil
	}

	// 如果 code 不存在，根据 error 和 verifiableCredential 推断
	if response.Error != "" {
		errorLower := strings.ToLower(response.Error)
		if strings.Contains(errorLower, "no record") || strings.Contains(errorLower, "not found") ||
			strings.Contains(errorLower, "no data") {
			result.Code = 1 // 没有记录
			return result, nil
		}
		if strings.Contains(errorLower, "invalid") || strings.Contains(errorLower, "expired") ||
			(strings.Contains(errorLower, "signature") && (strings.Contains(errorLower, "fail") || strings.Contains(errorLower, "invalid"))) {
			result.Code = 2 // 签名失效
			return result, nil
		}
		// 其他错误也视为没有记录
		result.Code = 1
		return result, nil
	}

	if response.VerifiableCredential == "" {
		result.Code = 1 // 没有记录
		return result, nil
	}

	// 成功
	result.VC = response.VerifiableCredential
	result.Code = 0
	return result, nil
}

// PaymentNotReadyError represents the case when payment information is not found in developer's service
type PaymentNotReadyError struct {
	Message string
}

func (e *PaymentNotReadyError) Error() string {
	return e.Message
}

// VCQueryResult 表示查询VC的结果
type VCQueryResult struct {
	VC   string // VerifiableCredential
	Code int    // 0=成功, 1=没有记录, 2=签名失效
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

// notifyLarePassToFetchSignature 通知 larepass 客户端拉取签名（与 NotifyLarePassToSign 相同负载，Topic 不同，且不包含 txHash）
func notifyLarePassToFetchSignature(dataSender DataSenderInterface, userID, appID, productID, xForwardedHost string, systemChainID int) error {
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

	// Only add application_verifiable_credential if required fields present (omit txHash by design)
	if productID != "" && systemChainID != 0 {
		appVerifiableCredential := map[string]interface{}{
			"productId":     productID,
			"systemChainId": systemChainID,
		}
		signBody["application_verifiable_credential"] = appVerifiableCredential
		log.Printf("Including application_verifiable_credential (without txHash) for fetch-signature user %s, app %s", userID, appID)
	} else {
		log.Printf("Skipping application_verifiable_credential for fetch-signature (productID: %s, systemChainID: %d)", productID, systemChainID)
	}

	if xForwardedHost == "" {
		log.Printf("ERROR: X-Forwarded-Host is empty for user %s, cannot build callback URL (fetch-signature)", userID)
		return errors.New("X-Forwarded-Host is required but not available")
	}

	// New callback endpoint for fetch-signature
	callbackURL := fmt.Sprintf("https://%s/app-store/api/v2/payment/fetch-signature-callback", xForwardedHost)
	log.Printf("Using X-Forwarded-Host based callback URL (fetch-signature) for user %s: %s", userID, callbackURL)

	update := types.SignNotificationUpdate{
		Sign: types.SignNotificationData{
			CallbackURL: callbackURL,
			SignBody:    signBody,
		},
		User:  userID,
		Vars:  make(map[string]string),
		Topic: "fetch_payment_signature",
	}

	return dataSender.SendSignNotificationUpdate(update)
}

// notifyFrontendPaymentRequired 通知 market 前端需要进行支付（内部方法）
func notifyFrontendPaymentRequired(dataSender DataSenderInterface, userID, appID, appName, sourceID, productID, developerDID, xForwardedHost string) error {
	if dataSender == nil {
		return errors.New("data sender is nil")
	}

	// Get user DID
	userDID, err := getUserDID(userID, xForwardedHost)
	if err != nil {
		return fmt.Errorf("failed to get user DID: %w", err)
	}

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

// buildPaymentStatusFromState constructs a user-facing payment status string from PaymentState
// TODO: buildPaymentStatusFromState 需要补全映射关系与边界条件处理
func buildPaymentStatusFromState(state *PaymentState) string {
	if state == nil {
		return "not_evaluated"
	}
	// 1) 已拥有许可（与 PaymentNeed 无关）：VC 存在且开发者同步完成
	if state.VC != "" && state.DeveloperSync == DeveloperSyncCompleted {
		return "purchased"
	}
	// 2) 前端已支付，等待开发者确认
	if state.PaymentStatus == PaymentFrontendCompleted {
		return "waiting_developer_confirmation"
	}
	// 3) 已签名，需支付
	if state.SignatureStatus == SignatureRequiredAndSigned {
		return "payment_required"
	}
	// 4) 需要签名
	if state.SignatureStatus == SignatureRequired || state.SignatureStatus == SignatureRequiredButPending {
		return "signature_required"
	}
	// 5) 错误态
	if state.SignatureStatus == SignatureErrorNoRecord {
		return "signature_no_record"
	}
	if state.SignatureStatus == SignatureErrorNeedReSign {
		return "signature_need_resign"
	}
	// 6) 其他视为同步中/未评估
	return "not_evaluated"
}

// Deprecated: 旧版本地存取方法已移除；请使用状态机的 LoadState/SaveState/DeleteState

// getSystemRemoteServiceBase returns the SystemRemoteService base URL
func getSystemRemoteServiceBase() string {
	// Try to get from cached SystemRemoteService first
	if base := settings.GetCachedSystemRemoteService(); base != "" {
		return base
	}

	log.Printf("Warning: SystemRemoteService base URL not available from systemenv watcher")
	return ""
}

// ===== 从旧版 payment 包迁移的函数 =====

// fetchDeveloperInfo fetches developer information from DID service (internal function)
func fetchDidInfo(ctx context.Context, httpClient *resty.Client, didName string) (*DeveloperInfo, error) {
	if didName == "" {
		return nil, errors.New("did name is empty")
	}
	if httpClient == nil {
		httpClient = resty.New()
	}
	httpClient.SetTimeout(3 * time.Second)

	// Get SystemRemoteService base URL and append /did
	baseURL := getSystemRemoteServiceBase()
	if baseURL == "" {
		return nil, errors.New("system remote service base URL not available")
	}

	// build URL: {SystemRemoteService}/did/domain/faster_search/{did}
	baseURL = strings.TrimRight(baseURL, "/")
	escaped := url.PathEscape(didName)
	endpoint := fmt.Sprintf("%s/did/domain/faster_search/%s", baseURL, escaped)

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
// buildPurchaseInfoFromState constructs PurchaseInfo from PaymentState
func buildPurchaseInfoFromState(state *PaymentState) *types.PurchaseInfo {
	if state == nil {
		return nil
	}
	return &types.PurchaseInfo{
		VC:     state.VC,
		Status: string(state.PaymentStatus),
	}
}

// getUserDID 通过 X-Forwarded-Host 获取用户 DID
// X-Forwarded-Host 格式为 a.b.c.d，需要截取后三段 b.c.d，然后查询 DID
func getUserDID(userID, xForwardedHost string) (string, error) {
	if xForwardedHost == "" {
		return "", errors.New("X-Forwarded-Host is empty")
	}

	// 截取后三段域名
	parts := strings.Split(xForwardedHost, ".")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid X-Forwarded-Host format: %s, need at least 3 parts", xForwardedHost)
	}

	// 取后三段
	domain := strings.Join(parts[len(parts)-3:], ".")

	// 创建 HTTP 客户端并查询 DID
	httpClient := resty.New()
	httpClient.SetTimeout(3 * time.Second)

	didInfo, err := fetchDidInfo(context.Background(), httpClient, domain)
	if err != nil {
		return "", fmt.Errorf("failed to fetch DID for domain %s: %w", domain, err)
	}

	return didInfo.DID, nil
}

// parseProductIDFromSignBody 从 signBody JSON 中解析 productId
// 兼容两种位置：
// 1) application_verifiable_credential.productId
// 2) vc.credentialSubject.productId （如存在）
func parseProductIDFromSignBody(signBody string) (string, error) {
	if strings.TrimSpace(signBody) == "" {
		return "", errors.New("empty sign body")
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(signBody), &body); err != nil {
		return "", fmt.Errorf("invalid sign body json: %w", err)
	}

	// Try application_verifiable_credential.productId
	if avc, ok := body["application_verifiable_credential"].(map[string]interface{}); ok {
		if pid, ok := avc["productId"].(string); ok && pid != "" {
			return pid, nil
		}
	}

	// Try nested vc.credentialSubject.productId
	if vc, ok := body["vc"].(map[string]interface{}); ok {
		if cs, ok := vc["credentialSubject"].(map[string]interface{}); ok {
			if pid, ok := cs["productId"].(string); ok && pid != "" {
				return pid, nil
			}
		}
	}

	return "", errors.New("productId not found in sign body")
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

// getDeveloperNameFromPrice extracts developer identifier from app price info
// Preference: use DID from price.developer.did; return empty if unavailable
func getDeveloperNameFromPrice(appInfo *types.AppInfo) string {
	if appInfo == nil || appInfo.Price == nil {
		return ""
	}
	did := strings.TrimSpace(appInfo.Price.Developer.DID)
	if did != "" {
		log.Printf("GetDeveloperNameFromPrice: Found developer DID: %s", did)
		return did
	}
	log.Printf("GetDeveloperNameFromPrice: Developer DID not found in price info")
	return ""
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
