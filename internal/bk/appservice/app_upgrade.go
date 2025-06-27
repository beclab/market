package appservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"market/internal/constants"
	"market/internal/models"
	"market/pkg/utils"
	"net/http"
	"strings"

	"github.com/golang/glog"
)

func AppUpgradeNew(appname, token string, options *models.UpgradeOptions) (string, map[string]interface{}, error) {
	if options == nil {
		return "", nil, errors.New("empty options")
	}

	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceUpgradeURLTempl, appServiceHost, appServicePort, appname)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, options)
}

func AppUpgrade(appName, version, source, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceUpgradeURLTempl, appServiceHost, appServicePort, appName)

	upgradeInfo := &models.UpgradeOptions{
		RepoURL: utils.GetRepoUrl(),
		Version: version,
		Source:  source,
	}
	ms, err := json.Marshal(upgradeInfo)
	if err != nil {
		return "", err
	}
	glog.Infof("url:%s, upgradeInfo:%s, token:%s\n", url, string(ms), token)

	return utils.SendHttpRequestWithToken(http.MethodPost, url, token, strings.NewReader(string(ms)))
}

func AppCurVersion(appName, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceVersionURLTempl, appServiceHost, appServicePort, appName)
	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}
