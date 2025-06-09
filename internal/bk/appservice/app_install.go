package appservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"market/internal/constants"
	"market/internal/models"
	"market/pkg/utils"
	"net/http"
	"os"
	"strings"

	"github.com/golang/glog"
)

func AppInstallNew(token string, options *models.InstallOptions) (string, map[string]interface{}, error) {
	if options == nil {
		return "", nil, errors.New("empty options")
	}

	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceInstallURLTempl, appServiceHost, appServicePort, options.App)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, options)
}

func AppInstall(appName, source, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceInstallURLTempl, appServiceHost, appServicePort, appName)

	installInfo := &models.InstallOptions{
		RepoUrl: utils.GetRepoUrl(),
		Source:  source,
	}
	ms, err := json.Marshal(installInfo)
	if err != nil {
		return "", err
	}
	glog.Infof("installUrl:%s, installInfo:%s, token:%s\n", urlStr, string(ms), token)

	return utils.SendHttpRequestWithToken(http.MethodPost, urlStr, token, strings.NewReader(string(ms)))
}

func AppUninstallNew(appName, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceUninstallURLTempl, appServiceHost, appServicePort, appName)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, nil)
}

func AppUninstall(appName, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceUninstallURLTempl, appServiceHost, appServicePort, appName)

	return utils.SendHttpRequestWithToken(http.MethodPost, url, token, nil)
}

func AppCancel(uid, t, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceInstallCancelURLTempl, appServiceHost, appServicePort, uid, t)

	return utils.SendHttpRequestWithToken(http.MethodPost, url, token, nil)
}

func CheckDeps(token string, deps *models.DepRequest) (*models.DependenciesResp, error) {
	appServiceHost := os.Getenv(constants.AppServiceHostEnv)
	appServicePort := os.Getenv(constants.AppServicePortEnv)
	url := fmt.Sprintf(constants.AppServiceCheckDependencies, appServiceHost, appServicePort)

	ms, err := json.Marshal(deps)
	if err != nil {
		return nil, err
	}
	glog.Infof("installUrl:%s, installInfo:%s, token:%s\n", url, string(ms), token)

	httpReq, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(ms)))
	if err != nil {
		glog.Warningf("err:%s", err.Error())
		return nil, err
	}

	httpReq.Header.Set(constants.AuthorizationTokenKey, token)
	httpReq.Header.Set("Content-Type", "application/json")

	body, err := utils.SendHttpRequest(httpReq)
	if err != nil {
		glog.Warningf("body:%s, err:%s", body, err.Error())
		return nil, err
	}

	resp := &models.DependenciesResp{}
	err = json.Unmarshal([]byte(body), resp)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
		return nil, err
	}

	return resp, nil
}

func GetUidFromStatus(resBody string) (uid string, info models.InstallationStatusResp, err error) {
	err = json.Unmarshal([]byte(resBody), &info)
	if err != nil {
		return
	}
	uid = info.Data.UID
	if info.Code != http.StatusOK {
		err = errors.New(info.Data.Msg)
		return
	}

	return
}
