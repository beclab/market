package bfl

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"

	"market/internal/event"
	"market/pkg/utils"
)

const (
	bflMyAppsUrl = "http://bfl/bfl/app_process/v1alpha1/myapps"
)

var accessToken *string = nil

func GetMyApps(token string) (string, error) {
	body, err := utils.SendHttpRequestWithToken(http.MethodGet, bflMyAppsUrl, token, nil)
	if err != nil {
		glog.Warning("resp:", body, "err:", err)
		return body, err
	}

	return body, err
}

type MyAppsRequest struct {
	IsLocal bool `json:"isLocal"`
}

func GetMyAppsInternal(eventClient *event.Client) (string, error) {

	glog.Info("call GetMyAppsInternal")
	bflMyAppsUrlInternal := fmt.Sprintf("http://%s/system-server/v1alpha1/app/service.bfl/v1/UserApps", os.Getenv("OS_SYSTEM_SERVER"))

	if accessToken == nil {
		glog.Info("accessToken is nil, call GetMyAppsInternal")
		token, err := eventClient.GetMyAppsAccessToken()
		if err != nil {
			eventClient = nil
			glog.Warningf("url:%s post %s err:%s", "GetMyAppsAccessToken", "", err.Error())
			return "", err
		}
		accessToken = &token
	}
	glog.Infof("accessToken:", *accessToken)

	var responseBody map[string]interface{}

	perm := MyAppsRequest{
		IsLocal: true,
	}

	postData, err := json.Marshal(perm)
	if err != nil {
		return "", err
	}

	resp, err := eventClient.HttpClient.R().
		SetHeaders(map[string]string{
			restful.HEADER_ContentType: restful.MIME_JSON,
			"X-Access-Token":           *accessToken,
		}).
		SetBody(postData).
		SetResult(&responseBody).
		Post(bflMyAppsUrlInternal)

	if err != nil {
		glog.Warningf("url:%s post %s err:%s", bflMyAppsUrlInternal, "", err.Error())
		eventClient = nil
		accessToken = nil
		return "", err
	}

	if resp.StatusCode() != http.StatusOK {
		glog.Warningf("url:%s post %s resp.StatusCode():%d not 200", bflMyAppsUrlInternal, "", resp.StatusCode())
		eventClient = nil
		accessToken = nil
		return "", errors.New(string(resp.Body()))
	}

	responseJSON, err := json.MarshalIndent(responseBody, "", "  ")
	if err != nil {
		glog.Warningf("Error marshaling response:", err)
		return "", err
	}
	glog.Infof("Response Body:", string(responseJSON))

	// responseData := resp.Result().(*event.Response)

	// glog.Info("responseData:")
	// glog.Info(responseData)

	// if responseData.Code != 0 {
	// 	glog.Warningf("url:%s post %s responseData.Code:%d not 0", bflMyAppsUrlInternal, "", responseData.Code)
	// 	return "", errors.New(responseData.Message)
	// }

	// glog.Infof("url:%s post %s success responseData:%#v", bflMyAppsUrlInternal, "", *responseData)

	return string(responseJSON), err
}

func getData(responseData event.Response) string {
	if data, ok := responseData.Data.(string); ok {
		return data
	}
	return ""
}
