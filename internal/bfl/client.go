package bfl

import (
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
var eventClient *event.Client = nil

func GetMyApps(token string) (string, error) {
	body, err := utils.SendHttpRequestWithToken(http.MethodGet, bflMyAppsUrl, token, nil)
	if err != nil {
		glog.Warning("resp:", body, "err:", err)
		return body, err
	}

	return body, err
}

func GetMyAppsInternal() (string, error) {

	glog.Info("call GetMyAppsInternal")
	bflMyAppsUrlInternal := fmt.Sprintf("http://%s/system-server/v1alpha1/app/service.bfl/v1/UserApps", os.Getenv("OS_SYSTEM_SERVER"))

	if eventClient == nil {
		eventClient = event.NewClient()
	}

	if accessToken == nil {
		token, err := eventClient.GetAccessToken()
		if err != nil {
			eventClient = nil
			return "", err
		}
		accessToken = &token
	}

	resp, err := eventClient.HttpClient.R().
		SetHeaders(map[string]string{
			restful.HEADER_ContentType: restful.MIME_JSON,
			"X-Access-Token":           *accessToken,
		}).
		SetResult(&event.Response{}).
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

	// responseData := resp.Result().(*string)

	responseData := resp.Result().(*event.Response)

	glog.Info("responseData:")
	glog.Info(responseData)

	if responseData.Code != 0 {
		glog.Warningf("url:%s post %s responseData.Code:%d not 0", bflMyAppsUrlInternal, "", responseData.Code)
		return "", errors.New(responseData.Message)
	}

	glog.Infof("url:%s post %s success responseData:%#v", bflMyAppsUrlInternal, "", *responseData)

	return getData(*responseData), err
}

func getData(responseData event.Response) string {
	if data, ok := responseData.Data.(string); ok {
		return data
	}
	return ""
}
