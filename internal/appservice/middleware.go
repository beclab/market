package appservice

import (
	"fmt"
	"market/internal/constants"
	"market/internal/models"
	"market/pkg/utils"
	"net/http"
)

func MiddlewareInstall(name, source, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceMiddlewareInstallURLTempl, appServiceHost, appServicePort, name)

	installInfo := &models.InstallOptions{
		Source:  source,
		RepoUrl: utils.GetRepoUrl(),
	}

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, installInfo)
}

func MiddlewareUninstall(name, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceMiddlewareUninstallURLTempl, appServiceHost, appServicePort, name)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, nil)
}

func MiddlewareCancel(name, t, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceMiddlewareCancelURLTempl, appServiceHost, appServicePort, name, t)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, nil)
}

func MiddlewareStatus(name, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceMiddlewareStatusURLTempl, appServiceHost, appServicePort, name)

	return utils.DoHttpRequest(http.MethodGet, urlStr, token, nil)
}

func MiddlewareOperate(name, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceMiddlewareOperateURLTempl, appServiceHost, appServicePort, name)

	return utils.DoHttpRequest(http.MethodGet, url, token, nil)
}

func GetMiddlewareOperatorResult(name, token string) (*OperateResult, error) {
	resStr, _, err := MiddlewareOperate(name, token)
	if err != nil {
		return nil, err
	}

	return ParseOperator(resStr)
}

func MiddlewareOperateList(token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceMiddlewareOperateListURLTempl, appServiceHost, appServicePort)

	return utils.DoHttpRequest(http.MethodGet, url, token, nil)
}

func MiddlewareStatusList(token string) (string, error) {
	host, port := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceMiddlewareStatusListURLTempl, host, port)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}
