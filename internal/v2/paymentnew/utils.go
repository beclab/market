package paymentnew

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
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

// manifestIdCache caches manifestId by productId:developerName to avoid repeated API calls
var (
	manifestIdCache       sync.Map // map[string]string: "productId:developerName" -> manifestId
	manifestIdFetching    sync.Map // map[string]*sync.Mutex: tracks ongoing fetches to prevent duplicate requests
	manifestIdFetchingMux sync.Mutex
)

// GetApplicationSchemaIdResponse represents the API response structure
type GetApplicationSchemaIdResponse struct {
	Code int `json:"code"`
	Data struct {
		ManifestId                        string `json:"manifestId"`
		PresentationDefinitionId          string `json:"presentationDefinitionId"`
		ApplicationVerifiableCredentialId string `json:"applicationVerifiableCredentialId"`
	} `json:"data"`
}

// merchantProductLicenseCredentialManifestJSON is the hardcoded schema for Merchant Product License Credential Manifest
var merchantProductLicenseCredentialManifestJSON = `{"id":"","spec_version":"https://identity.foundation/credential-manifest/spec/v1.0.0/","name":"Merchant Product License Credential Manifest","description":"Credential manifest for issuing merchant product license based on payment proof","issuer":{"id":"did:key:z6Mkp53XYSGCVFu4sKin6QVKPjiqfUCauTqxW8QpoYq5nUZ9","name":"did:key:z6Mkp53XYSGCVFu4sKin6QVKPjiqfUCauTqxW8QpoYq5nUZ9"},"output_descriptors":[{"id":"70c236a7-7dbb-cf15-6d5c-5ceffb6caf24","schema":"3a8adbf4-a083-ea50-ecee-b5d55107e2a3","name":"Merchant Product License Credential","description":"Product license credential with complete payment and product information","display":{"title":{"path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"],"schema":{"type":"string"}},"properties":[{"label":"Product ID","path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"],"schema":{"type":"string"}},{"label":"systemChainId","path":["$.credentialSubject.systemChainId","$.vc.credentialSubject.systemChainId"],"schema":{"type":"string"}},{"label":"txHash","path":["$.credentialSubject.txHash","$.vc.credentialSubject.txHash"],"schema":{"type":"string"}}]},"styles":{"background":{"color":"#1e40af"},"text":{"color":"#ffffff"}}}],"format":{"jwt_vc":{"alg":["EdDSA"]}},"presentation_definition":{"id":"de434d03-052c-027a-d4cc-887be81aa941","name":"Merchant Product License Application Presentation Manifest","purpose":"Request presentation of application credentials for merchant product license","input_descriptors":[{"id":"productId","name":"Product ID","purpose":"Provide your product ID to activate from payment transaction","format":{"jwt_vc":{"alg":["EdDSA"]}},"constraints":{"fields":[{"path":["$.credentialSubject.productId","$.vc.credentialSubject.productId"]}],"subject_is_issuer":"preferred"}}]}}`

// Temporarily unusable data
var merchantProductLicenseApplicationVerifiableCredentialJSON = `{"id":"6ad3d05c-ccf2-90df-32e9-aea5d5cf3115","type":"CredentialSchema2023","credentialSchema":"eyJhbGciOiJFZERTQSIsImtpZCI6ImRpZDprZXk6ejZNa3A1M1hZU0dDVkZ1NHNLaW42UVZLUGppcWZVQ2F1VHF4VzhRcG9ZcTVuVVo5I3o2TWtwNTNYWVNHQ1ZGdTRzS2luNlFWS1BqaXFmVUNhdVRxeFc4UXBvWXE1blVaOSIsInR5cCI6IkpXVCJ9.eyJpYXQiOjE3NjI3NzIyMTEsImlzcyI6ImRpZDprZXk6ejZNa3A1M1hZU0dDVkZ1NHNLaW42UVZLUGppcWZVQ2F1VHF4VzhRcG9ZcTVuVVo5IiwianRpIjoiaHR0cDovL2xvY2FsaG9zdDo2MDAzL3YxL3NjaGVtYXMvNmFkM2QwNWMtY2NmMi05MGRmLTMyZTktYWVhNWQ1Y2YzMTE1IiwibmJmIjoxNzYyNzcyMjExLCJub25jZSI6IjlmY2U0ZjMwLTdjNWItNGRkOC04MjAzLTc5Yjk2MDg5ZGQ0ZiIsInZjIjp7IkBjb250ZXh0IjpbImh0dHBzOi8vd3d3LnczLm9yZy8yMDE4L2NyZWRlbnRpYWxzL3YxIl0sInR5cGUiOlsiVmVyaWZpYWJsZUNyZWRlbnRpYWwiXSwiY3JlZGVudGlhbFN1YmplY3QiOnsiJGlkIjoiNmFkM2QwNWMtY2NmMi05MGRmLTMyZTktYWVhNWQ1Y2YzMTE1IiwiJHNjaGVtYSI6Imh0dHBzOi8vanNvbi1zY2hlbWEub3JnL2RyYWZ0LzIwMjAtMTIvc2NoZW1hIiwiZGVzY3JpcHRpb24iOiJNZXJjaGFudCBQcm9kdWN0IExpY2Vuc2UgQXBwbGljYXRpb24gQ3JlZGVudGlhbCBTY2hlbWEiLCJuYW1lIjoiTWVyY2hhbnQgUHJvZHVjdCBMaWNlbnNlIEFwcGxpY2F0aW9uIENyZWRlbnRpYWwgU2NoZW1hIiwicHJvcGVydGllcyI6eyJjcmVkZW50aWFsU3ViamVjdCI6eyJhZGRpdGlvbmFsUHJvcGVydGllcyI6dHJ1ZSwicHJvcGVydGllcyI6eyJwcm9kdWN0SWQiOnsiZGVzY3JpcHRpb24iOiJQcm9kdWN0IElEIHRvIGFjdGl2YXRlIChzaW5jZSBwYXltZW50IG1heSBjb250YWluIG11bHRpcGxlIHByb2R1Y3RzKSIsInR5cGUiOiJzdHJpbmcifX0sInJlcXVpcmVkIjpbInByb2R1Y3RJZCJdLCJ0eXBlIjoib2JqZWN0In19LCJ0eXBlIjoib2JqZWN0In19fQ.ACqzHn9ADQyzZ3B3moprQ6ADRTRkuxo07Zu72uYP2r2BmHcHvhpFUZ3lcJgCeJ17Sx_cFpOdgLJCDZqGrDC7Cg","schema":{"@context":["https://www.w3.org/2018/credentials/v1"],"type":["VerifiableCredential"],"credentialSubject":{"$id":"6ad3d05c-ccf2-90df-32e9-aea5d5cf3115","$schema":"https://json-schema.org/draft/2020-12/schema","description":"Merchant Product License Application Credential Schema","name":"Merchant Product License Application Credential Schema","properties":{"credentialSubject":{"additionalProperties":true,"properties":{"productId":{"description":"Product ID to activate (since payment may contain multiple products)","type":"string"}},"required":["productId"],"type":"object"}},"type":"object"}}}`

// Local in-memory cache (paymentStateStore) removed; use state machine memory and Redis uniformly

// getStateKey generates the unique key for a state
func getStateKey(userID, appID, productID string) string {
	return fmt.Sprintf("%s:%s:%s", userID, appID, productID)
}

// getRedisStateKey generates the Redis key
func getRedisStateKey(userID, appID, productID string) string {
	return fmt.Sprintf("payment:state:%s:%s:%s", userID, appID, productID)
}

// queryVCFromDeveloper queries VC via developer endpoint (internal)
// Equivalent to the original getVCFromDeveloper
// Returns VCQueryResult, where Code: 0=success, 1=no record, 2=signature invalid
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

	// Prefer using returned code field when available
	switch response.Code {
	case 0:
		if response.VerifiableCredential == "" {
			return nil, errors.New("developer ActivateAndGrant returned code 0 but verifiableCredential is empty")
		}
		result.VC = response.VerifiableCredential
		result.Code = 0
		return result, nil
	case 1100, 1101, 1604:
		// Various JWS validation failures (expired, missing fields, etc.)
		result.Code = 2
		return result, nil
	case 1502:
		// No payment records found for user
		result.Code = 1
		return result, nil
	case 1501:
		// Treat as no record according to current backend design
		result.Code = 1
		return result, nil
	}

	if response.Code > 0 {
		// All other positive codes fall back to "no record" semantics
		result.Code = 1
		return result, nil
	}

	// If code is absent or unrecognized, fall back to inferring from error/message fields
	if response.Error != "" {
		errorLower := strings.ToLower(response.Error)
		if strings.Contains(errorLower, "no record") || strings.Contains(errorLower, "not found") ||
			strings.Contains(errorLower, "no data") {
			result.Code = 1 // no record
			return result, nil
		}
		if strings.Contains(errorLower, "invalid") || strings.Contains(errorLower, "expired") ||
			(strings.Contains(errorLower, "signature") && (strings.Contains(errorLower, "fail") || strings.Contains(errorLower, "invalid"))) {
			result.Code = 2 // signature invalid
			return result, nil
		}
		// Other errors are also treated as no record
		result.Code = 1
		return result, nil
	}

	if response.VerifiableCredential == "" {
		result.Code = 1 // no record
		return result, nil
	}

	// Success
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

// getManifestIdCacheKey generates cache key from productID and developerName
func getManifestIdCacheKey(productID, developerName string) string {
	return fmt.Sprintf("%s:%s", productID, developerName)
}

// getManifestId fetches manifestId from developer service API with caching
// Returns manifestId and error
func getManifestId(productID, developerName string) (string, error) {
	if productID == "" {
		return "", errors.New("productID is required")
	}
	if developerName == "" {
		return "", errors.New("developerName is required")
	}

	cacheKey := getManifestIdCacheKey(productID, developerName)

	// Check cache first
	if cached, ok := manifestIdCache.Load(cacheKey); ok {
		if manifestId, ok := cached.(string); ok && manifestId != "" {
			log.Printf("getManifestId: Using cached manifestId for productID=%s, developerName=%s: %s", productID, developerName, manifestId)
			return manifestId, nil
		}
	}

	// Get or create mutex for this cache key to prevent duplicate concurrent requests
	manifestIdFetchingMux.Lock()
	muxInterface, _ := manifestIdFetching.LoadOrStore(cacheKey, &sync.Mutex{})
	fetchMux := muxInterface.(*sync.Mutex)
	manifestIdFetchingMux.Unlock()

	// Lock to prevent concurrent fetches for the same key
	fetchMux.Lock()
	defer fetchMux.Unlock()

	// Double-check cache after acquiring lock (another goroutine might have fetched it)
	if cached, ok := manifestIdCache.Load(cacheKey); ok {
		if manifestId, ok := cached.(string); ok && manifestId != "" {
			log.Printf("getManifestId: Using cached manifestId (after lock) for productID=%s, developerName=%s: %s", productID, developerName, manifestId)
			return manifestId, nil
		}
	}

	// Cache miss, fetch from API
	manifestId, err := fetchManifestIdFromAPI(productID, developerName)
	if err != nil {
		// Clean up fetching mutex on error
		manifestIdFetching.Delete(cacheKey)
		return "", fmt.Errorf("failed to fetch manifestId: %w", err)
	}

	// Store in cache
	if manifestId != "" {
		manifestIdCache.Store(cacheKey, manifestId)
		log.Printf("getManifestId: Cached manifestId for productID=%s, developerName=%s: %s", productID, developerName, manifestId)
	}

	// Clean up fetching mutex
	manifestIdFetching.Delete(cacheKey)

	return manifestId, nil
}

// fetchManifestIdFromAPI calls the GetApplicationSchemaId API
func fetchManifestIdFromAPI(productID, developerName string) (string, error) {
	// Convert developerName to lowercase for DNS compatibility
	developerName = strings.ToLower(developerName)
	baseURL := fmt.Sprintf("https://4c94e3111.%s", developerName)
	endpoint := fmt.Sprintf("%s/api/grpc/AuthService/GetApplicationSchemaId", baseURL)

	log.Printf("fetchManifestIdFromAPI: Requesting manifestId for productID=%s, developerName=%s, endpoint=%s", productID, developerName, endpoint)

	// Create HTTP client with timeout
	httpClient := resty.New()
	httpClient.SetTimeout(10 * time.Second)

	// Make POST request with productId
	resp, err := httpClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"productId": productID}).
		Post(endpoint)

	if err != nil {
		return "", fmt.Errorf("failed to call GetApplicationSchemaId API: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return "", fmt.Errorf("GetApplicationSchemaId API returned non-2xx status: %d, body: %s", resp.StatusCode(), string(resp.Body()))
	}

	// Parse response
	var apiResponse GetApplicationSchemaIdResponse
	if err := json.Unmarshal(resp.Body(), &apiResponse); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	if apiResponse.Code != 0 {
		return "", fmt.Errorf("API returned error code: %d", apiResponse.Code)
	}

	if apiResponse.Data.ManifestId == "" {
		return "", errors.New("manifestId is empty in API response")
	}

	log.Printf("fetchManifestIdFromAPI: Successfully fetched manifestId=%s for productID=%s", apiResponse.Data.ManifestId, productID)
	return apiResponse.Data.ManifestId, nil
}

// getMerchantProductLicenseCredentialManifestJSON returns the manifest JSON with manifestId injected
// This function replaces direct usage of merchantProductLicenseCredentialManifestJSON variable
func getMerchantProductLicenseCredentialManifestJSON(productID, developerName string) (string, error) {
	// Get manifestId (with caching)
	manifestId, err := getManifestId(productID, developerName)
	if err != nil {
		return "", fmt.Errorf("failed to get manifestId: %w", err)
	}

	// Parse the base manifest JSON
	var manifestData map[string]interface{}
	if err := json.Unmarshal([]byte(merchantProductLicenseCredentialManifestJSON), &manifestData); err != nil {
		return "", fmt.Errorf("failed to parse base manifest JSON: %w", err)
	}

	// Inject manifestId into the JSON
	manifestData["id"] = manifestId

	// Marshal back to JSON string
	manifestJSONBytes, err := json.Marshal(manifestData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal manifest JSON: %w", err)
	}

	return string(manifestJSONBytes), nil
}

// VCQueryResult represents the result of querying VC
type VCQueryResult struct {
	VC   string // VerifiableCredential
	Code int    // 0=成功, 1=没有记录, 2=签名失效
}

// notifyLarePassToSign notifies larepass client to sign (internal)
// Equivalent to the original NotifyLarePassToSign
func notifyLarePassToSign(dataSender DataSenderInterface, userID, appID, productID, txHash, xForwardedHost string, developerName string, isReSign bool) error {
	if dataSender == nil {
		return errors.New("data sender is nil")
	}

	// Get manifest JSON with manifestId injected
	manifestJSON, err := getMerchantProductLicenseCredentialManifestJSON(productID, developerName)
	if err != nil {
		return fmt.Errorf("failed to get manifest JSON: %w", err)
	}

	var manifestData map[string]interface{}
	if err := json.Unmarshal([]byte(manifestJSON), &manifestData); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	// Build SignBody - start with ProductCredentialManifest
	signBody := map[string]interface{}{
		"product_credential_manifest": manifestData,
	}

	// Include application_verifiable_credential_schema for larepass
	var applicationCredentialSchema map[string]interface{}
	if err := json.Unmarshal([]byte(merchantProductLicenseApplicationVerifiableCredentialJSON), &applicationCredentialSchema); err != nil {
		return fmt.Errorf("failed to parse application VC schema JSON: %w", err)
	}
	signBody["application_verifiable_credential_schema"] = applicationCredentialSchema
	log.Printf("Including application_verifiable_credential_schema in sign notification for user %s, app %s, productID: %s",
		userID, appID, productID)

	// Include product_id and application_verifiable_credential if productID is available
	if productID != "" {
		// Always include product_id at top level (needed for signature request identification)
		signBody["product_id"] = productID

		// Build application_verifiable_credential (txHash is optional)
		appVerifiableCredential := map[string]interface{}{
			"productId": productID,
		}
		// Only include txHash if available (payment may not have completed yet)
		if txHash != "" {
			appVerifiableCredential["txHash"] = txHash
		}
		signBody["application_verifiable_credential"] = appVerifiableCredential

		log.Printf("Including product_id and application_verifiable_credential in sign notification for user %s, app %s, productID: %s, txHash: %s",
			userID, appID, productID, txHash)
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
			IsReSign:    isReSign,
		},
		User:  userID,
		Vars:  make(map[string]string),
		Topic: "market_payment",
	}

	// Send the notification via DataSender
	return dataSender.SendSignNotificationUpdate(update)
}

// notifyLarePassToFetchSignature notifies larepass client to fetch signature (same payload as NotifyLarePassToSign, different topic, omits txHash)
func notifyLarePassToFetchSignature(dataSender DataSenderInterface, userID, appID, productID, xForwardedHost string, developerName string) error {
	log.Printf("notifyLarePassToFetchSignature: Starting notification for user=%s app=%s productID=%s", userID, appID, productID)

	if dataSender == nil {
		log.Printf("notifyLarePassToFetchSignature: ERROR - data sender is nil")
		return errors.New("data sender is nil")
	}

	// Get manifest JSON with manifestId injected
	manifestJSON, err := getMerchantProductLicenseCredentialManifestJSON(productID, developerName)
	if err != nil {
		log.Printf("notifyLarePassToFetchSignature: ERROR - failed to get manifest JSON: %v", err)
		return fmt.Errorf("failed to get manifest JSON: %w", err)
	}

	var manifestData map[string]interface{}
	if err := json.Unmarshal([]byte(manifestJSON), &manifestData); err != nil {
		log.Printf("notifyLarePassToFetchSignature: ERROR - failed to parse manifest JSON: %v", err)
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	// Build SignBody - start with ProductCredentialManifest
	signBody := map[string]interface{}{
		"product_credential_manifest": manifestData,
	}

	// Include application_verifiable_credential_schema for fetch-signature flow
	var applicationCredentialSchema map[string]interface{}
	if err := json.Unmarshal([]byte(merchantProductLicenseApplicationVerifiableCredentialJSON), &applicationCredentialSchema); err != nil {
		log.Printf("notifyLarePassToFetchSignature: ERROR - failed to parse application VC schema JSON: %v", err)
		return fmt.Errorf("failed to parse application VC schema JSON: %w", err)
	}
	signBody["application_verifiable_credential_schema"] = applicationCredentialSchema
	log.Printf("notifyLarePassToFetchSignature: Including application_verifiable_credential_schema for user %s, app %s, productID: %s",
		userID, appID, productID)

	// productID is required for application_verifiable_credential (omit txHash by design)
	if productID == "" {
		log.Printf("notifyLarePassToFetchSignature: ERROR - productID is empty for user %s, app %s, cannot build application_verifiable_credential", userID, appID)
		return errors.New("productID is required but not available")
	}

	// Always include product_id at top level for consistency and easy identification
	signBody["product_id"] = productID
	log.Printf("notifyLarePassToFetchSignature: Including product_id in sign notification for user %s, app %s, productID: %s", userID, appID, productID)

	appVerifiableCredential := map[string]interface{}{
		"productId": productID,
	}
	signBody["application_verifiable_credential"] = appVerifiableCredential
	log.Printf("notifyLarePassToFetchSignature: Including application_verifiable_credential (without txHash) for fetch-signature user %s, app %s, productID: %s", userID, appID, productID)

	if xForwardedHost == "" {
		log.Printf("notifyLarePassToFetchSignature: ERROR - X-Forwarded-Host is empty for user %s, cannot build callback URL (fetch-signature)", userID)
		return errors.New("X-Forwarded-Host is required but not available")
	}

	// New callback endpoint for fetch-signature
	callbackURL := fmt.Sprintf("https://%s/app-store/api/v2/payment/fetch-signature-callback", xForwardedHost)
	log.Printf("notifyLarePassToFetchSignature: Using X-Forwarded-Host based callback URL (fetch-signature) for user %s: %s", userID, callbackURL)

	update := types.SignNotificationUpdate{
		Sign: types.SignNotificationData{
			CallbackURL: callbackURL,
			SignBody:    signBody,
		},
		User:  userID,
		Vars:  make(map[string]string),
		Topic: "fetch_payment_signature",
	}

	log.Printf("notifyLarePassToFetchSignature: Sending notification to LarePass for user=%s app=%s productID=%s topic=fetch_payment_signature", userID, appID, productID)
	err = dataSender.SendSignNotificationUpdate(update)
	if err != nil {
		log.Printf("notifyLarePassToFetchSignature: ERROR - failed to send notification: %v", err)
		return err
	}
	log.Printf("notifyLarePassToFetchSignature: Successfully sent notification to LarePass for user=%s app=%s productID=%s", userID, appID, productID)
	return nil
}

// notifyFrontendPaymentRequired notifies market frontend that payment is required (internal)
func notifyFrontendPaymentRequired(dataSender DataSenderInterface, userID, appID, appName, sourceID, productID, developerDID, xForwardedHost string, appInfo *types.AppInfo) error {
	if dataSender == nil {
		return errors.New("data sender is nil")
	}

	// Get user DID
	userDID, err := getUserDID(userID, xForwardedHost)
	if err != nil {
		return fmt.Errorf("failed to get user DID: %w", err)
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
	}

	// Get settingsManager from global state machine if available
	var settingsManager *settings.SettingsManager
	if globalStateMachine != nil {
		settingsManager = globalStateMachine.settingsManager
	}
	paymentData := createFrontendPaymentData(ctx, httpClient, userDID, developerDID, productID, priceConfig, developerName, settingsManager, sourceID)

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

// notifyFrontendPurchaseCompleted notifies frontend that purchase is completed (internal)
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

// DeveloperAPIResponse represents the response from appstore-git-bot developer API
type DeveloperAPIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Apps          map[string]interface{} `json:"apps"`
		Developer     string                 `json:"developer"`
		RSAPublicKeys []struct {
			IsCurrent bool   `json:"isCurrent"`
			Key       string `json:"key"`
		} `json:"rsa_public_keys"`
	} `json:"data"`
}

// fetchDeveloperInfoFromAPI fetches developer information from appstore-git-bot API (internal)
func fetchDeveloperInfoFromAPI(ctx context.Context, httpClient *resty.Client, developerName string, settingsManager *settings.SettingsManager, sourceID string) (*DeveloperAPIResponse, error) {
	if developerName == "" {
		return nil, errors.New("developer name is empty")
	}
	if httpClient == nil {
		httpClient = resty.New()
	}
	httpClient.SetTimeout(10 * time.Second)

	// Get appstore base URL from market source (same approach as data_fetch_step.go and detail_fetch_step.go)
	var appstoreBase string
	if settingsManager != nil && sourceID != "" {
		// Try to get market source by sourceID
		marketSources := settingsManager.GetMarketSources()
		if marketSources != nil && marketSources.Sources != nil {
			for _, source := range marketSources.Sources {
				if source.ID == sourceID || source.Name == sourceID {
					appstoreBase = source.BaseURL
					log.Printf("Found market source for sourceID=%s: %s", sourceID, appstoreBase)
					break
				}
			}
		}
		// If not found by sourceID, try to get default or first active source
		if appstoreBase == "" {
			activeSources := settingsManager.GetActiveMarketSources()
			if len(activeSources) > 0 {
				appstoreBase = activeSources[0].BaseURL
				log.Printf("Using first active market source: %s", appstoreBase)
			}
		}
	}

	// Fallback to environment or default
	if appstoreBase == "" {
		appstoreBase = os.Getenv("APPSTORE_BASE_URL")
		if appstoreBase == "" {
			// Try to get from SystemRemoteService
			if base := getSystemRemoteServiceBase(); base != "" {
				appstoreBase = base
			}
		}
		if appstoreBase == "" {
			appstoreBase = "https://appstore-server-prod.bttcdn.com"
			log.Printf("Using default appstore base URL: %s", appstoreBase)
		}
	}

	// Build URL using SettingsManager.BuildAPIURL (same as data_fetch_step.go)
	var endpoint string
	if settingsManager != nil {
		endpoint = settingsManager.BuildAPIURL(appstoreBase, "/appstore-git-bot/v1/developer/"+url.PathEscape(developerName))
	} else {
		// Fallback manual URL building
		appstoreBase = strings.TrimRight(appstoreBase, "/")
		escaped := url.PathEscape(developerName)
		endpoint = fmt.Sprintf("%s/appstore-git-bot/v1/developer/%s", appstoreBase, escaped)
	}

	log.Printf("Fetching developer info from API: %s", endpoint)

	resp, err := httpClient.R().
		SetContext(ctx).
		Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to call developer API: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, fmt.Errorf("developer API returned non-2xx status: %d", resp.StatusCode())
	}

	var apiResp DeveloperAPIResponse
	if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse developer API response: %w", err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("developer API returned error code: %d, message: %s", apiResp.Code, apiResp.Message)
	}

	return &apiResp, nil
}

// extractTokenInfoFromPriceConfig extracts token information from PriceConfig and API response
func extractTokenInfoFromPriceConfig(priceConfig *types.PriceConfig, apiResp *DeveloperAPIResponse, productID string) []types.TokenInfo {
	if priceConfig == nil {
		log.Printf("extractTokenInfoFromPriceConfig: priceConfig is nil")
		return nil
	}

	log.Printf("extractTokenInfoFromPriceConfig: Starting extraction for productID=%s", productID)
	log.Printf("extractTokenInfoFromPriceConfig: apiResp=%v, apiResp.Data.Apps=%v", apiResp != nil, apiResp != nil && apiResp.Data.Apps != nil)

	var tokenInfos []types.TokenInfo

	// Extract from Paid section
	if priceConfig.Paid != nil && len(priceConfig.Paid.Price) > 0 {
		log.Printf("extractTokenInfoFromPriceConfig: Processing Paid section with %d price entries", len(priceConfig.Paid.Price))
		for _, priceEntry := range priceConfig.Paid.Price {
			tokenInfo := types.TokenInfo{
				Chain:         priceEntry.Chain,
				TokenSymbol:   priceEntry.TokenSymbol,
				ReceiveWallet: priceEntry.ReceiveWallet,
			}
			log.Printf("extractTokenInfoFromPriceConfig: Processing price entry: chain=%s, symbol=%s", priceEntry.Chain, priceEntry.TokenSymbol)

			// Try to get token decimals from API response
			if apiResp != nil && apiResp.Data.Apps != nil {
				log.Printf("extractTokenInfoFromPriceConfig: API response has Apps")
				// Log the structure of Apps for debugging
				if appsJSON, err := json.Marshal(apiResp.Data.Apps); err == nil {
					log.Printf("extractTokenInfoFromPriceConfig: Apps structure: %s", string(appsJSON))
				}
				// apps is a map[string]interface{} that represents a JSON object
				// It contains "app_name" and "products" fields directly
				// So we should access apps["products"] directly, not iterate through it
				log.Printf("extractTokenInfoFromPriceConfig: Apps is a map, checking for products...")
				if products, ok := apiResp.Data.Apps["products"].([]interface{}); ok {
					log.Printf("extractTokenInfoFromPriceConfig: Found %d products", len(products))
					for _, p := range products {
						if product, ok := p.(map[string]interface{}); ok {
							pid, _ := product["product_id"].(string)
							log.Printf("extractTokenInfoFromPriceConfig: Checking product_id=%s (expected=%s)", pid, productID)
							if pid == productID {
								log.Printf("extractTokenInfoFromPriceConfig: Product ID matched! Checking prices...")
								if prices, ok := product["price"].([]interface{}); ok {
									log.Printf("extractTokenInfoFromPriceConfig: Found %d price entries", len(prices))
									for _, pr := range prices {
										if price, ok := pr.(map[string]interface{}); ok {
											priceChain, _ := price["chain"].(string)
											log.Printf("Checking price entry: chain=%s, expected=%s", priceChain, priceEntry.Chain)
											if chain, ok := price["chain"].(string); ok && chain == priceEntry.Chain {
												log.Printf("Chain matched! Extracting token info from price entry: %+v", price)
												// Extract token_decimals (handle multiple number types from JSON)
												if decimals, ok := price["token_decimals"].(float64); ok {
													tokenInfo.TokenDecimals = int(decimals)
													log.Printf("Extracted token_decimals as float64: %f -> %d", decimals, tokenInfo.TokenDecimals)
												} else if decimals, ok := price["token_decimals"].(int); ok {
													tokenInfo.TokenDecimals = decimals
													log.Printf("Extracted token_decimals as int: %d", tokenInfo.TokenDecimals)
												} else if decimals, ok := price["token_decimals"].(int64); ok {
													tokenInfo.TokenDecimals = int(decimals)
													log.Printf("Extracted token_decimals as int64: %d -> %d", decimals, tokenInfo.TokenDecimals)
												} else {
													log.Printf("WARNING: token_decimals not found or wrong type in price entry, available keys: %v", getMapKeys(price))
												}
												if contract, ok := price["token_contract"].(string); ok {
													tokenInfo.TokenContract = contract
													log.Printf("Extracted token_contract: %s", contract)
												} else {
													log.Printf("WARNING: token_contract not found in price entry")
												}
												if amount, ok := price["token_amount"].(string); ok {
													tokenInfo.TokenAmount = amount
													log.Printf("Extracted token_amount: %s", amount)
												}
												if icon, ok := price["token_icon"].(string); ok {
													tokenInfo.TokenIcon = icon
													log.Printf("Extracted token_icon: %s", icon)
												} else {
													log.Printf("WARNING: token_icon not found in price entry")
												}
												log.Printf("Final extracted token info for product %s, chain %s: decimals=%d, contract=%s, amount=%s, icon=%s",
													productID, chain, tokenInfo.TokenDecimals, tokenInfo.TokenContract, tokenInfo.TokenAmount, tokenInfo.TokenIcon)
												break
											}
										}
									}
								} else {
									log.Printf("extractTokenInfoFromPriceConfig: Product price field is not an array")
								}
								log.Printf("Found product %s", productID)
								break
							}
						}
					}
				} else {
					log.Printf("extractTokenInfoFromPriceConfig: No products array found in apps")
				}
			}

			tokenInfos = append(tokenInfos, tokenInfo)
		}
	}

	// Extract from Products section
	if len(priceConfig.Products) > 0 {
		for _, product := range priceConfig.Products {
			if product.ProductID == productID && len(product.Price) > 0 {
				for _, priceEntry := range product.Price {
					tokenInfo := types.TokenInfo{
						Chain:         priceEntry.Chain,
						TokenSymbol:   priceEntry.TokenSymbol,
						ReceiveWallet: priceEntry.ReceiveWallet,
					}

					// Try to get token decimals from API response
					if apiResp != nil && apiResp.Data.Apps != nil {
						// apps is a map[string]interface{} that represents a JSON object
						// It contains "app_name" and "products" fields directly
						if products, ok := apiResp.Data.Apps["products"].([]interface{}); ok {
							for _, p := range products {
								if prod, ok := p.(map[string]interface{}); ok {
									if pid, ok := prod["product_id"].(string); ok && pid == productID {
										if prices, ok := prod["price"].([]interface{}); ok {
											for _, pr := range prices {
												if price, ok := pr.(map[string]interface{}); ok {
													priceChain, _ := price["chain"].(string)
													log.Printf("Checking price entry (Products): chain=%s, expected=%s", priceChain, priceEntry.Chain)
													if chain, ok := price["chain"].(string); ok && chain == priceEntry.Chain {
														log.Printf("Chain matched! Extracting token info from price entry (Products): %+v", price)
														// Extract token_decimals (handle multiple number types from JSON)
														if decimals, ok := price["token_decimals"].(float64); ok {
															tokenInfo.TokenDecimals = int(decimals)
															log.Printf("Extracted token_decimals as float64: %f -> %d", decimals, tokenInfo.TokenDecimals)
														} else if decimals, ok := price["token_decimals"].(int); ok {
															tokenInfo.TokenDecimals = decimals
															log.Printf("Extracted token_decimals as int: %d", tokenInfo.TokenDecimals)
														} else if decimals, ok := price["token_decimals"].(int64); ok {
															tokenInfo.TokenDecimals = int(decimals)
															log.Printf("Extracted token_decimals as int64: %d -> %d", decimals, tokenInfo.TokenDecimals)
														} else {
															log.Printf("WARNING: token_decimals not found or wrong type in price entry, available keys: %v", getMapKeys(price))
														}
														if contract, ok := price["token_contract"].(string); ok {
															tokenInfo.TokenContract = contract
															log.Printf("Extracted token_contract: %s", contract)
														} else {
															log.Printf("WARNING: token_contract not found in price entry")
														}
														if amount, ok := price["token_amount"].(string); ok {
															tokenInfo.TokenAmount = amount
															log.Printf("Extracted token_amount: %s", amount)
														}
														if icon, ok := price["token_icon"].(string); ok {
															tokenInfo.TokenIcon = icon
															log.Printf("Extracted token_icon: %s", icon)
														} else {
															log.Printf("WARNING: token_icon not found in price entry")
														}
														log.Printf("Final extracted token info (Products) for product %s, chain %s: decimals=%d, contract=%s, amount=%s, icon=%s",
															productID, chain, tokenInfo.TokenDecimals, tokenInfo.TokenContract, tokenInfo.TokenAmount, tokenInfo.TokenIcon)
														break
													}
												}
											}
										}
										log.Printf("Found product %s", productID)
										break
									}
								}
							}
						}
					}

					tokenInfos = append(tokenInfos, tokenInfo)
				}
				break
			}
		}
	}

	return tokenInfos
}

// createFrontendPaymentData creates frontend payment data with enhanced information (internal)
func createFrontendPaymentData(ctx context.Context, httpClient *resty.Client, userDID, developerDID, productID string, priceConfig *types.PriceConfig, developerName string, settingsManager *settings.SettingsManager, sourceID string) *types.FrontendPaymentData {
	paymentData := &types.FrontendPaymentData{
		From: userDID,
		To:   developerDID,
		Product: []struct {
			ProductID string `json:"product_id"`
		}{
			{ProductID: productID},
		},
		PriceConfig: priceConfig,
	}

	log.Printf("createFrontendPaymentData: developerName=%s, httpClient=%v, priceConfig=%v", developerName, httpClient != nil, priceConfig != nil)

	// Fetch developer info from API to get RSA public key and token info
	if developerName != "" {
		if httpClient == nil {
			httpClient = resty.New()
			httpClient.SetTimeout(10 * time.Second)
			log.Printf("createFrontendPaymentData: Created new HTTP client")
		}
		if ctx == nil {
			ctx = context.Background()
		}

		log.Printf("createFrontendPaymentData: Calling fetchDeveloperInfoFromAPI for developer=%s, sourceID=%s", developerName, sourceID)
		apiResp, err := fetchDeveloperInfoFromAPI(ctx, httpClient, developerName, settingsManager, sourceID)
		if err != nil {
			log.Printf("createFrontendPaymentData: Failed to fetch developer info from API: %v, continuing without RSA key and token info", err)
		} else {
			log.Printf("createFrontendPaymentData: Successfully fetched developer info, RSAPublicKeys count=%d", len(apiResp.Data.RSAPublicKeys))
			// Extract RSA public key (use current key if available)
			if len(apiResp.Data.RSAPublicKeys) > 0 {
				var rawKey string
				for _, key := range apiResp.Data.RSAPublicKeys {
					if key.IsCurrent {
						// Remove quotes if present
						rawKey = strings.Trim(key.Key, "\"")
						log.Printf("createFrontendPaymentData: Found current RSA public key, length=%d", len(rawKey))
						break
					}
				}
				// If no current key found, use the first one
				if rawKey == "" && len(apiResp.Data.RSAPublicKeys) > 0 {
					rawKey = strings.Trim(apiResp.Data.RSAPublicKeys[0].Key, "\"")
					log.Printf("createFrontendPaymentData: Using first RSA public key (no current key), length=%d", len(rawKey))
				}
				// Convert RSA key from hex to PEM format
				if rawKey != "" {
					paymentData.RSAPublicKey = convertRSAKeyToPEM(rawKey)
					log.Printf("createFrontendPaymentData: Converted RSA key to PEM format, length=%d", len(paymentData.RSAPublicKey))
				}
			} else {
				log.Printf("createFrontendPaymentData: No RSA public keys found in API response")
			}

			// Extract token info from price config and API response
			paymentData.TokenInfo = extractTokenInfoFromPriceConfig(priceConfig, apiResp, productID)
			log.Printf("createFrontendPaymentData: Extracted token info count=%d", len(paymentData.TokenInfo))
		}
	} else {
		log.Printf("createFrontendPaymentData: developerName is empty, skipping API call")
	}

	return paymentData
}

// getMapKeys returns the keys of a map as a slice of strings (helper function)
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// convertRSAKeyToPEM converts RSA public key from hex format (0x...) to PEM format
func convertRSAKeyToPEM(hexKey string) string {
	if hexKey == "" || hexKey == "0x" {
		return ""
	}

	// Remove 0x prefix if present
	hexKey = strings.TrimPrefix(hexKey, "0x")
	hexKey = strings.TrimSpace(hexKey)

	// Decode hex string to bytes
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		log.Printf("convertRSAKeyToPEM: Failed to decode hex key: %v", err)
		return hexKey // Return original if conversion fails
	}

	// Encode to base64
	base64Key := base64.StdEncoding.EncodeToString(keyBytes)

	// Format as PEM (64 characters per line)
	var pemLines []string
	pemLines = append(pemLines, "-----BEGIN RSA PUBLIC KEY-----")
	for i := 0; i < len(base64Key); i += 64 {
		end := i + 64
		if end > len(base64Key) {
			end = len(base64Key)
		}
		pemLines = append(pemLines, base64Key[i:end])
	}
	pemLines = append(pemLines, "-----END RSA PUBLIC KEY-----")

	return strings.Join(pemLines, "\n")
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

	log.Printf("CheckIfAppIsPaid: Price configuration found, examining paid section and products")
	// Priority: Paid buyout (main feature). Products are for in-app items (reserved), not a paid app flag.
	if appInfo.Price.Paid != nil {
		if len(appInfo.Price.Paid.Price) > 0 {
			log.Printf("CheckIfAppIsPaid: Paid section with price entries found -> PAID app")
			return true, nil
		}
		log.Printf("CheckIfAppIsPaid: Paid section present but no price entries")
	}

	// Products alone do NOT make the app a paid app in this phase
	if len(appInfo.Price.Products) > 0 {
		log.Printf("CheckIfAppIsPaid: Products exist but treated as in-app purchases (reserved); not a paid app in this phase")
	}

	log.Printf("CheckIfAppIsPaid: App %s is not a paid app - no valid paid section", getAppID())
	return false, nil // Not a paid app
}

// buildPaymentStatusFromState constructs a user-facing payment status string from PaymentState
// TODO: Complete mappings and edge-case handling
func buildPaymentStatusFromState(state *PaymentState) string {
	if state == nil {
		return "not_evaluated"
	}
	// 1) Already licensed (independent of PaymentNeed): VC exists and developer sync completed
	if state.VC != "" && state.DeveloperSync == DeveloperSyncCompleted {
		return "purchased"
	}
	// 2) Frontend paid, waiting for developer confirmation
	if state.PaymentStatus == PaymentFrontendCompleted {
		return "waiting_developer_confirmation"
	}
	// 2.5) Frontend started payment preparation
	if state.PaymentStatus == PaymentFrontendStarted {
		return "payment_frontend_started"
	}
	// 3) Signed, payment required
	if state.SignatureStatus == SignatureRequiredAndSigned {
		return "payment_required"
	}
	// 4) Signature required
	if state.SignatureStatus == SignatureRequired || state.SignatureStatus == SignatureRequiredButPending {
		return "signature_required"
	}
	// 5) Error states
	if state.SignatureStatus == SignatureErrorNoRecord {
		return "signature_no_record"
	}
	if state.SignatureStatus == SignatureErrorNeedReSign {
		return "signature_need_resign"
	}
	// 6) Others: if ProductID exists (requires purchase) but not in signature/payment flow yet, mark as not_buy; otherwise not_evaluated
	if strings.TrimSpace(state.ProductID) != "" {
		return "not_buy"
	}
	return "not_evaluated"
}

// Deprecated: old local storage methods removed; use state machine LoadState/SaveState/DeleteState

// getSystemRemoteServiceBase returns the SystemRemoteService base URL
func getSystemRemoteServiceBase() string {
	// Try to get from cached SystemRemoteService first
	if base := settings.GetCachedSystemRemoteService(); base != "" {
		return base
	}

	log.Printf("Warning: SystemRemoteService base URL not available from systemenv watcher")
	return ""
}

// ===== Functions migrated from legacy payment package =====

// fetchDidInfo fetches developer information from DID service (internal function)
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
// Note: With new price.yaml format, RSA public key is no longer stored in price config.
// This function now only checks developer identifier consistency.
func verifyPaymentConsistency(appInfo *types.AppInfo, dev *DeveloperInfo) (bool, error) {
	if appInfo == nil || appInfo.Price == nil {
		return false, errors.New("no payment info in app")
	}
	// In new format, developer is a string (email/identifier)
	developerInApp := strings.TrimSpace(appInfo.Price.Developer)
	if developerInApp == "" {
		return false, errors.New("no developer in app price.developer")
	}
	if dev == nil || dev.Name == "" {
		return false, errors.New("no developer name from developer info")
	}
	// Compare developer identifiers (normalize by converting to lowercase)
	devInApp := strings.ToLower(strings.TrimSpace(developerInApp))
	devFromDid := strings.ToLower(strings.TrimSpace(dev.Name))
	// For email format, compare the part before @
	devInAppParts := strings.Split(devInApp, "@")
	devFromDidParts := strings.Split(devFromDid, "@")
	if len(devInAppParts) > 0 && len(devFromDidParts) > 0 {
		return devInAppParts[0] == devFromDidParts[0], nil
	}
	return devInApp == devFromDid, nil
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

// getUserDID obtains user's DID via X-Forwarded-Host
// X-Forwarded-Host format is a.b.c.d; take the last three segments b.c.d, then query DID
func getUserDID(userID, xForwardedHost string) (string, error) {
	if xForwardedHost == "" {
		return "", errors.New("X-Forwarded-Host is empty")
	}

	// Take the last three domain segments
	parts := strings.Split(xForwardedHost, ".")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid X-Forwarded-Host format: %s, need at least 3 parts", xForwardedHost)
	}

	// Join the last three parts
	domain := strings.Join(parts[len(parts)-3:], ".")

	// Create HTTP client and query DID
	httpClient := resty.New()
	httpClient.SetTimeout(3 * time.Second)

	didInfo, err := fetchDidInfo(context.Background(), httpClient, domain)
	if err != nil {
		return "", fmt.Errorf("failed to fetch DID for domain %s: %w", domain, err)
	}

	return didInfo.DID, nil
}

// parseProductIDFromSignBody parses productId from signBody JSON
// Compatible with two locations:
// 1) application_verifiable_credential.productId
// 2) vc.credentialSubject.productId (if present)
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
func verifyPurchaseInfo(pi *types.PurchaseInfo, productID, developerName string) bool {
	if pi == nil {
		return false
	}
	// Must have a VC when purchased
	if strings.EqualFold(pi.Status, "purchased") {
		// Get manifest JSON with manifestId injected
		manifestJSON, err := getMerchantProductLicenseCredentialManifestJSON(productID, developerName)
		if err != nil {
			log.Printf("verifyPurchaseInfo: ERROR - failed to get manifest JSON: %v", err)
			return false
		}
		ok, _ := verifyVCAgainstManifest(pi.VC, manifestJSON)
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

	// Get product ID from first product in products array
	if len(appInfo.Price.Products) > 0 {
		productID := appInfo.Price.Products[0].ProductID
		if productID != "" {
			log.Printf("GetProductIDFromAppInfo: Found product ID: %s", productID)
			return productID
		}
		log.Printf("GetProductIDFromAppInfo: First product has empty product_id")
	}

	log.Printf("GetProductIDFromAppInfo: No products found, returning empty string")
	return ""
}

// getDeveloperNameFromPrice extracts developer identifier from app price info
// Returns developer email/identifier string directly
func getDeveloperNameFromPrice(appInfo *types.AppInfo) string {
	if appInfo == nil || appInfo.Price == nil {
		return ""
	}
	developer := strings.TrimSpace(appInfo.Price.Developer)
	if developer != "" {
		log.Printf("GetDeveloperNameFromPrice: Found developer: %s", developer)
		return developer
	}
	log.Printf("GetDeveloperNameFromPrice: Developer not found in price info")
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
