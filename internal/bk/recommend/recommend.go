package recommend

import (
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	"market/internal/constants"
	"market/internal/models"
	"market/pkg/utils"
	"net/http"
	"strings"
)

func Install(name, source, token string) (string, error) {
	host, port := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.RecommendServiceInstallURLTempl, host, port, name)

	installInfo := &models.InstallOptions{
		RepoUrl: utils.GetRepoUrl(),
		Source:  source,
	}
	ms, err := json.Marshal(installInfo)
	if err != nil {
		return "", err
	}
	glog.Infof("installUrl:%s, installInfo:%s, token:%s\n", url, string(ms), token)

	return utils.SendHttpRequestWithToken(http.MethodPost, url, token, strings.NewReader(string(ms)))
}

func Uninstall(name, token string) (string, error) {
	host, port := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.RecommendServiceUninstallURLTempl, host, port, name)

	return utils.SendHttpRequestWithToken(http.MethodPost, url, token, nil)
}

func Upgrade(name, version, source, token string) (string, error) {
	host, port := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.RecommendServiceUpgradeURLTempl, host, port, name)

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

func Status(name, token string) (string, error) {
	host, port := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.RecommendServiceUpgradeStatusURLTempl, host, port, name)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func StatusList(token string) (string, error) {
	host, port := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.RecommendServiceUpgradeStatusListURLTempl, host, port)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}
