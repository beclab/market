package bfl

import (
	"github.com/golang/glog"
	"net/http"

	"market/pkg/utils"
)

const (
	bflMyAppsUrl = "http://bfl/bfl/app_process/v1alpha1/myapps"
)

func GetMyApps(token string) (string, error) {
	body, err := utils.SendHttpRequestWithToken(http.MethodGet, bflMyAppsUrl, token, nil)
	if err != nil {
		glog.Warning("resp:", body, "err:", err)
		return body, err
	}

	return body, err
}
