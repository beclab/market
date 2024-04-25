package appservice

import (
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	"market/internal/constants"
	"market/internal/models"
	"market/pkg/utils"
	"net/http"
)

func LlmInstall(modelID, source, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceModelInstallURLTempl, appServiceHost, appServicePort, modelID)

	installInfo := &models.InstallOptions{
		Source:  source,
		RepoUrl: utils.GetRepoUrl(),
	}

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, installInfo)
}

func LlmUninstall(modelID, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceModelUninstallURLTempl, appServiceHost, appServicePort, modelID)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, nil)
}

func LlmCancel(modelID, t, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceModelCancelURLTempl, appServiceHost, appServicePort, modelID, t)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, nil)
}

func LlmSuspend(modelID, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceModelSuspendURLTempl, appServiceHost, appServicePort, modelID)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, nil)
}

func LlmResume(modelID, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceModelResumeURLTempl, appServiceHost, appServicePort, modelID)

	return utils.DoHttpRequest(http.MethodPost, urlStr, token, nil)
}

func LlmStatus(modelID, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceModelStatusURLTempl, appServiceHost, appServicePort, modelID)

	return utils.DoHttpRequest(http.MethodGet, urlStr, token, nil)
}

func ParseLlmStatusList(str string) (llms []*models.ModelStatusResponse, err error) {
	var resp []*models.ModelStatusResponse
	err = json.Unmarshal([]byte(str), &resp)
	if err != nil {
		glog.Warningf("json.Unmarshal %s, err:%s", str, err.Error())
		return
	}

	for _, model := range resp {
		if model.Status == models.NoInstalled {
			continue
		}
		llms = append(llms, model)
	}

	return
}

func LlmStatusList(token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	urlStr := fmt.Sprintf(constants.AppServiceModelStatusListURLTempl, appServiceHost, appServicePort)

	return utils.DoHttpRequest(http.MethodGet, urlStr, token, nil)
}

func LlmOperate(modelID, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceModelOperateURLTempl, appServiceHost, appServicePort, modelID)

	return utils.DoHttpRequest(http.MethodGet, url, token, nil)
}

func GetModelOperatorResult(name, token string) (*OperateResult, error) {
	resStr, _, err := LlmOperate(name, token)
	if err != nil {
		return nil, err
	}

	return ParseOperator(resStr)
}

func LlmOperateList(token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceModelOperateListURLTempl, appServiceHost, appServicePort)

	return utils.DoHttpRequest(http.MethodGet, url, token, nil)
}

func LlmOperateHistory(modelID, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceModelOperateHistoryURLTempl, appServiceHost, appServicePort, modelID)

	return utils.DoHttpRequest(http.MethodGet, url, token, nil)
}

func LlmOperateHistoryList(token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceModelOperateHistoryListURLTempl, appServiceHost, appServicePort)

	return utils.DoHttpRequest(http.MethodGet, url, token, nil)
}
