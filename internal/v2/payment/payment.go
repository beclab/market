package payment

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

// DataSenderInterface defines the interface for sending NATS messages
type DataSenderInterface interface {
	SendSignNotificationUpdate(update types.SignNotificationUpdate) error
	SendMarketSystemUpdate(update types.MarketSystemUpdate) error
}

// DataSenderWrapper wraps DataSenderInterface to provide the interface needed by TaskManager
type DataSenderWrapper struct {
	dataSender DataSenderInterface
}

// SendSignNotificationUpdate implements the interface
func (w *DataSenderWrapper) SendSignNotificationUpdate(update types.SignNotificationUpdate) error {
	return w.dataSender.SendSignNotificationUpdate(update)
}

// SendMarketSystemUpdate implements the interface
func (w *DataSenderWrapper) SendMarketSystemUpdate(update types.MarketSystemUpdate) error {
	return w.dataSender.SendMarketSystemUpdate(update)
}

// DeveloperInfo represents subset of DID search response used by payment
type DeveloperInfo struct {
	Name      string
	DID       string
	RSAPubKey string
}

// didGateResponse is used for JSON decoding of DID search API
type didGateResponse struct {
	Code    int          `json:"code"`
	Message *string      `json:"message"`
	Data    *didGateData `json:"data"`
}

type didGateData struct {
	ID   string       `json:"id"`
	Name string       `json:"name"`
	DID  string       `json:"did"`
	Tags []didGateTag `json:"tags"`
}

type didGateTag struct {
	Name          string `json:"name"`
	ValueFormated string `json:"valueFormated"`
}

// FetchDeveloperInfo queries DID gate and extracts RSA public key
func FetchDeveloperInfo(ctx context.Context, httpClient *resty.Client, base string, developerName string) (*DeveloperInfo, error) {
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

	dev := &DeveloperInfo{
		Name: dg.Data.Name,
		DID:  dg.Data.DID,
	}
	for _, t := range dg.Data.Tags {
		if strings.EqualFold(t.Name, "rsaPubKey") {
			dev.RSAPubKey = strings.Trim(t.ValueFormated, "\"")
			break
		}
	}
	return dev, nil
}

// VerifyPaymentConsistency checks payment section and compares RSA key
func VerifyPaymentConsistency(appInfo *types.AppInfo, dev *DeveloperInfo) (bool, error) {
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

// RedisPurchaseKey builds redis key for purchase receipt
func RedisPurchaseKey(userID, developerName, appName, priceProductID string) string {
	// key convention: payment:receipt:{user}:{developer}:{app}:{product}
	return fmt.Sprintf("payment:receipt:%s:%s:%s:%s", userID, developerName, appName, priceProductID)
}

// PurchaseInfo represents VC and purchase state stored in Redis
// GetPurchaseInfoFromRedis loads purchase info JSON from Redis
func GetPurchaseInfoFromRedis(sm *settings.SettingsManager, key string) (*types.PurchaseInfo, error) {
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

func VerifyPurchaseInfo(pi *types.PurchaseInfo) bool {
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

// merchantProductLicenseCredentialManifestJSON is the hardcoded schema for Merchant Product License Credential Manifest
var merchantProductLicenseCredentialManifestJSON = `{"id":"c544214b-be43-6ba8-1618-c80de084aa62","spec_version":"https://identity.foundation/credential-manifest/spec/v1.0.0/","name":"Merchant Product License Credential Manifest","description":"Credential manifest for issuing merchant product license based on payment proof","issuer":{"id":"did:key:z6MktdEpjYpYocHibuZqMsjXmaVusyUHckMnkzM3xUCxpfa4#z6MktdEpjYpYocHibuZqMsjXmaVusyUHckMnkzM3xUCxpfa4","name":"default-merchant"},"output_descriptors":[{"id":"ff9561a9-607f-5e7b-cad7-01be4e3ae457","schema":"c333229e-f82b-d66c-2e16-b7ff701378cf","name":"Merchant Product License Credential","description":"Product license credential with complete payment and product information","display":{"title":{"path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"],"schema":{"type":"string"}},"properties":[{"label":"Product ID","path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"],"schema":{"type":"string"}},{"label":"systemChainId","path":["$.credentialSubject.systemChainId","$.vc.credentialSubject.systemChainId"],"schema":{"type":"string"}},{"label":"txHash","path":["$.credentialSubject.txHash","$.vc.credentialSubject.txHash"],"schema":{"type":"string"}}]},"styles":{"background":{"color":"#1e40af"},"text":{"color":"#ffffff"}}}],"format":{"jwt_vc":{"alg":["EdDSA"]}},"presentation_definition":{"id":"de434d03-052c-027a-d4cc-887be81aa941","name":"Merchant Product License Application Presentation Manifest","purpose":"Request presentation of application credentials for merchant product license","input_descriptors":[{"id":"productId","name":"Product ID","purpose":"Provide your product ID to activate from payment transaction","format":{"jwt_vc":{"alg":["EdDSA"]}},"constraints":{"fields":[{"path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"]}],"subject_is_issuer":"preferred"}}]}}`

// GetMerchantProductLicenseCredentialManifest returns the hardcoded Merchant Product License Credential Manifest JSON
func GetMerchantProductLicenseCredentialManifest() string {
	return merchantProductLicenseCredentialManifestJSON
}

// CreateFrontendPaymentData creates payment data for frontend payment process
func CreateFrontendPaymentData(userDID, developerDID, productID string) *types.FrontendPaymentData {
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

// verifyVCAgainstManifest validates VC using the manifest's declared JSONPath fields (no crypto verification).
// It supports JWT VC (compact) and JSON VC, and dynamically reads required field paths from the manifest.
func verifyVCAgainstManifest(vc string, manifest string) (bool, error) {
	if strings.TrimSpace(vc) == "" {
		return false, errors.New("empty vc")
	}

	// 1) Parse manifest to collect paths for required fields
	req := manifestRequiredPaths{}
	if err := req.parseFromManifest(manifest); err != nil {
		return false, fmt.Errorf("invalid manifest: %w", err)
	}

	// 2) Extract VC payload as JSON object (support JWT and JSON)
	var payload map[string]interface{}
	var header map[string]interface{}
	if parts := strings.Split(vc, "."); len(parts) == 3 {
		// JWT VC
		var err error
		payload, err = base64urlDecodeToJSON(parts[1])
		if err != nil {
			return false, err
		}
		header, err = base64urlDecodeToJSON(parts[0])
		if err != nil {
			return false, err
		}
		// Check alg if provided
		if alg, _ := header["alg"].(string); alg != "" && !strings.EqualFold(alg, "EdDSA") {
			return false, fmt.Errorf("jwt alg not EdDSA: %s", alg)
		}
	} else {
		// JSON VC
		if err := json.Unmarshal([]byte(vc), &payload); err != nil {
			return false, fmt.Errorf("vc is neither JWT nor JSON: %w", err)
		}
	}

	// The VC object may be at root or under "vc"
	candidates := []map[string]interface{}{payload}
	if vcObj, ok := payload["vc"].(map[string]interface{}); ok {
		candidates = append(candidates, vcObj)
	}

	// 3) Validate all required fields by resolving their JSONPath arrays against candidates
	// Required: productId; Recommended: systemChainId, txHash (treat as required if present in manifest)
	for _, cand := range candidates {
		okProd := resolveAnyPathNonEmptyString(cand, req.ProductIDPaths)
		okSys := true
		if len(req.SystemChainIDPaths) > 0 {
			okSys = resolveAnyPathNonEmptyString(cand, req.SystemChainIDPaths)
		}
		okTx := true
		if len(req.TxHashPaths) > 0 {
			okTx = resolveAnyPathNonEmptyString(cand, req.TxHashPaths)
		}
		if okProd && okSys && okTx {
			return true, nil
		}
	}

	return false, errors.New("required fields not satisfied by manifest paths")
}

type manifestRequiredPaths struct {
	ProductIDPaths     [][]string
	SystemChainIDPaths [][]string
	TxHashPaths        [][]string
}

// parseFromManifest extracts JSONPath arrays from manifest for productId/systemChainId/txHash
func (m *manifestRequiredPaths) parseFromManifest(manifest string) error {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(manifest), &obj); err != nil {
		return err
	}
	// Preferred source: presentation_definition.input_descriptors[*].constraints.fields[*].path
	if pd, ok := obj["presentation_definition"].(map[string]interface{}); ok {
		if ids, ok := pd["input_descriptors"].([]interface{}); ok {
			for _, idesc := range ids {
				if idm, ok := idesc.(map[string]interface{}); ok {
					if cons, ok := idm["constraints"].(map[string]interface{}); ok {
						if fields, ok := cons["fields"].([]interface{}); ok {
							for _, f := range fields {
								if fm, ok := f.(map[string]interface{}); ok {
									if paths, ok := fm["path"].([]interface{}); ok {
										// Classify by suffix keys we care about
										m.collectBySuffix(paths)
									}
								}
							}
						}
					}
				}
			}
		}
	}
	// Fallback: display.properties[*].path to discover additional keys (systemChainId, txHash)
	if od, ok := obj["output_descriptors"].([]interface{}); ok {
		for _, o := range od {
			if om, ok := o.(map[string]interface{}); ok {
				if display, ok := om["display"].(map[string]interface{}); ok {
					if props, ok := display["properties"].([]interface{}); ok {
						for _, p := range props {
							if pm, ok := p.(map[string]interface{}); ok {
								if paths, ok := pm["path"].([]interface{}); ok {
									m.collectBySuffix(paths)
								}
							}
						}
					}
				}
			}
		}
	}
	if len(m.ProductIDPaths) == 0 {
		// Provide sensible defaults matching our manifest
		m.ProductIDPaths = append(m.ProductIDPaths, []string{"credentialSubject", "productId"})
		m.ProductIDPaths = append(m.ProductIDPaths, []string{"vc", "credentialSubject", "productId"})
	}
	if len(m.SystemChainIDPaths) == 0 {
		m.SystemChainIDPaths = append(m.SystemChainIDPaths, []string{"credentialSubject", "systemChainId"})
		m.SystemChainIDPaths = append(m.SystemChainIDPaths, []string{"vc", "credentialSubject", "systemChainId"})
	}
	if len(m.TxHashPaths) == 0 {
		m.TxHashPaths = append(m.TxHashPaths, []string{"credentialSubject", "txHash"})
		m.TxHashPaths = append(m.TxHashPaths, []string{"vc", "credentialSubject", "txHash"})
	}
	return nil
}

func (m *manifestRequiredPaths) collectBySuffix(paths []interface{}) {
	for _, p := range paths {
		if ps, ok := p.(string); ok {
			// Accept two forms: $.credentialSubject.x and $.vc.credentialSubject.x
			segs := strings.Split(strings.TrimPrefix(ps, "$."), ".")
			if len(segs) >= 2 {
				last := segs[len(segs)-1]
				switch strings.ToLower(last) {
				case "productid":
					m.ProductIDPaths = append(m.ProductIDPaths, normalizePath(segs))
				case "systemchainid":
					m.SystemChainIDPaths = append(m.SystemChainIDPaths, normalizePath(segs))
				case "txhash":
					m.TxHashPaths = append(m.TxHashPaths, normalizePath(segs))
				}
			}
		}
	}
}

func normalizePath(segs []string) []string {
	// Drop leading "$" if present and return keys from either root or from "vc"
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		s = strings.TrimSpace(strings.TrimPrefix(s, "$"))
		if s == "" || s == "." {
			continue
		}
		if s == "vc" || s == "credentialSubject" || s == "productId" || s == "systemChainId" || s == "txHash" {
			out = append(out, s)
		}
	}
	// If path starts with vc, keep as-is; otherwise treat as from root
	if len(out) >= 2 && out[0] == "vc" {
		return out
	}
	return out
}

// resolveAnyPathNonEmptyString tries multiple key-paths and returns true if any yields a non-empty string
func resolveAnyPathNonEmptyString(root map[string]interface{}, paths [][]string) bool {
	for _, p := range paths {
		if s, ok := resolvePathString(root, p); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

func resolvePathString(root map[string]interface{}, path []string) (string, bool) {
	cur := interface{}(root)
	for _, key := range path {
		if m, ok := cur.(map[string]interface{}); ok {
			cur = m[key]
		} else {
			return "", false
		}
	}
	if s, ok := cur.(string); ok {
		return s, true
	}
	return "", false
}

// base64urlDecodeToJSON decodes a base64url string and parses JSON object
func base64urlDecodeToJSON(segment string) (map[string]interface{}, error) {
	// base64url decode without padding
	// Add padding if needed
	s := segment
	if m := len(s) % 4; m != 0 {
		s = s + strings.Repeat("=", 4-m)
	}
	dec, err := urlSafeBase64Decode(s)
	if err != nil {
		return nil, err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(dec, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// urlSafeBase64Decode decodes base64url to bytes
func urlSafeBase64Decode(s string) ([]byte, error) {
	// Use standard library encoding with RawURLEncoding
	// but we avoid adding a new import alias; call directly here
	return base64RawURLDecode(s)
}

// base64RawURLDecode uses encoding/base64 RawURLEncoding
func base64RawURLDecode(s string) ([]byte, error) {
	// local helper to avoid repeating import references
	return base64RawURLDecodeImpl(s)
}

// base64RawURLDecodeImpl performs base64.RawURLEncoding decoding
func base64RawURLDecodeImpl(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// PaymentNotReadyError represents the case when payment information is not found in developer's service
type PaymentNotReadyError struct {
	Message string
}

func (e *PaymentNotReadyError) Error() string {
	return e.Message
}

// getVCFromDeveloper calls the developer's AuthService to get verifiable credential
// baseURL format: https://4c94e3111.{developerName}/
func getVCFromDeveloper(jws string, developerName string) (string, error) {
	if jws == "" {
		return "", errors.New("jws parameter is empty")
	}
	if developerName == "" {
		return "", errors.New("developer name is empty")
	}

	// Build base URL: https://4c94e3111.{developerName}/
	baseURL := fmt.Sprintf("https://4c94e3111.%s/", developerName)
	endpoint := fmt.Sprintf("%s/api/grpc/AuthService/ActivateAndGrant", baseURL)

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

// Global task manager instance
var globalTaskManager *TaskManager

// InitTaskManager initializes the global task manager
func InitTaskManager(dataSender DataSenderInterface, settingsManager *settings.SettingsManager) {
	// Create a wrapper that implements the required interface
	wrapper := &DataSenderWrapper{dataSender: dataSender}
	globalTaskManager = NewTaskManager(wrapper, settingsManager)
	log.Println("Task manager initialized")
}

// GetTaskManager returns the global task manager
func GetTaskManager() *TaskManager {
	return globalTaskManager
}

// CreatePaymentTask creates a new payment task or returns existing one
func CreatePaymentTask(userID, appID, productID, developerName, appName string) (*PaymentTask, error) {
	if globalTaskManager == nil {
		return nil, errors.New("task manager not initialized")
	}
	return globalTaskManager.CreateOrGetTask(userID, appID, productID, developerName, appName)
}

// GetPaymentTaskStatus returns the status of a payment task
func GetPaymentTaskStatus(userID, appID, productID string) (TaskStatus, error) {
	if globalTaskManager == nil {
		return "", errors.New("task manager not initialized")
	}
	return globalTaskManager.GetTaskStatus(userID, appID, productID)
}

// ProcessSignatureSubmission handles the business logic for signature submission
// Now integrated with TaskManager workflow
func ProcessSignatureSubmission(jws, signBody, user string) error {
	log.Printf("=== Payment Module Processing Signature Submission ===")
	log.Printf("JWS: %s", jws)
	log.Printf("SignBody: %s", signBody)
	log.Printf("User: %s", user)

	if globalTaskManager == nil {
		log.Printf("Task manager not initialized, falling back to basic processing")
		log.Printf("=== End of Payment Module Processing ===")
		return nil
	}

	// Process signature submission through task manager
	if err := globalTaskManager.ProcessSignatureSubmission(jws, signBody, user); err != nil {
		log.Printf("Error processing signature submission: %v", err)
		return err
	}

	log.Printf("=== End of Payment Module Processing ===")
	return nil
}

// NotifyLarePassToSign sends a sign notification to the client via NATS
func NotifyLarePassToSign(dataSender DataSenderInterface, signBody, user string) error {
	if dataSender == nil {
		return errors.New("data sender is nil")
	}

	// Parse the merchantProductLicenseCredentialManifestJSON
	var manifestData map[string]interface{}
	if err := json.Unmarshal([]byte(merchantProductLicenseCredentialManifestJSON), &manifestData); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	// Create the sign notification update
	update := types.SignNotificationUpdate{
		Sign: types.SignNotificationData{
			CallbackURL: fmt.Sprintf("https://market.%s/app-store/api/v2/payment/submit-signature", user),
			SignBody: map[string]interface{}{
				"ProductCredentialManifest": manifestData,
			},
		},
		User: user,
		Vars: make(map[string]string),
	}

	// Send the notification via DataSender
	return dataSender.SendSignNotificationUpdate(update)
}

// CheckIfAppIsPaid checks if an app is a paid app by examining its Price configuration
func CheckIfAppIsPaid(appInfo *types.AppInfo) (bool, error) {
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

// GetPaymentStatus gets the payment status from AppInfo
func GetPaymentStatus(appInfo *types.AppInfo) (string, error) {
	if appInfo == nil {
		return "", errors.New("app info is nil")
	}

	if appInfo.PurchaseInfo == nil {
		return "not_buy", nil // No purchase info means not bought
	}

	return appInfo.PurchaseInfo.Status, nil
}

// PaymentStatusResult represents the result of payment status check
type PaymentStatusResult struct {
	IsPaid       bool   `json:"is_paid"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	PaymentError string `json:"payment_error,omitempty"`
}

// ProcessAppPaymentStatus handles the complete payment status check and processing
func ProcessAppPaymentStatus(userID, appID string, appInfo *types.AppInfo) (*PaymentStatusResult, error) {
	log.Printf("Processing payment status for user: %s, app: %s", userID, appID)

	// Step 1: Check if app is paid app
	isPaidApp, err := CheckIfAppIsPaid(appInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to check if app is paid: %w", err)
	}

	if !isPaidApp {
		log.Printf("App %s is not a paid app", appID)
		return &PaymentStatusResult{
			IsPaid:  false,
			Status:  "free",
			Message: "This is a free app",
		}, nil
	}

	// Step 2: Get payment status
	paymentStatus, err := GetPaymentStatus(appInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment status: %w", err)
	}

	// Step 3: Handle different payment statuses
	result := &PaymentStatusResult{
		IsPaid: true,
		Status: paymentStatus,
	}

	switch paymentStatus {
	case "purchased":
		log.Printf("App %s is already purchased by user %s", appID, userID)
		result.Message = "App is already purchased"

	case "paying":
		log.Printf("App %s payment is in progress for user %s", appID, userID)
		result.Message = "Payment is in progress"

	case "not_buy":
		log.Printf("App %s is not purchased by user %s, starting payment flow", appID, userID)
		result.Message = "Starting payment flow"

		// Start payment process directly
		if err := StartPaymentProcess(userID, appID, appInfo); err != nil {
			log.Printf("Failed to start payment process: %v", err)
			result.PaymentError = err.Error()
		}

	case "not_sign":
		log.Printf("App %s payment signature is missing for user %s, retrying payment flow", appID, userID)
		result.Message = "Payment signature missing, retrying payment flow"

		// Retry payment process directly
		if err := RetryPaymentProcess(userID, appID, appInfo); err != nil {
			log.Printf("Failed to retry payment process: %v", err)
			result.PaymentError = err.Error()
		}

	case "not_pay":
		log.Printf("App %s payment is not completed for user %s, retrying payment flow", appID, userID)
		result.Message = "Payment not completed, retrying payment flow"

		// Retry payment process directly
		if err := RetryPaymentProcess(userID, appID, appInfo); err != nil {
			log.Printf("Failed to retry payment process: %v", err)
			result.PaymentError = err.Error()
		}

	default:
		log.Printf("Unknown payment status %s for app %s and user %s", paymentStatus, appID, userID)
		result.Message = "Unknown payment status"
	}

	log.Printf("Payment status processing completed for app: %s, user: %s, status: %s", appID, userID, paymentStatus)
	return result, nil
}
