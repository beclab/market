package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if pi.Status == "purchased" {
		return true
	}
	return false
}
