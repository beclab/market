package appservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"market/internal/constants"
	"market/internal/models"
	"market/pkg/utils"
	"net/http"
	"os"

	"github.com/golang/glog"
	"market/internal/conf"
)

func AppSuspend(name, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceSuspendURLTempl, appServiceHost, appServicePort, name)

	return utils.SendHttpRequestWithToken(http.MethodPost, url, token, nil)
}

func Resume(name, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceResumeURLTempl, appServiceHost, appServicePort, name)

	return utils.SendHttpRequestWithToken(http.MethodPost, url, token, nil)
}

func Status(name, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceStatusURLTempl, appServiceHost, appServicePort, name)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func StatusList(token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceStatusListURLTempl, appServiceHost, appServicePort)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func AppOperate(name, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceOperateURLTempl, appServiceHost, appServicePort, name)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func ParseOperator(str string) (*OperateResult, error) {
	var result OperateResult
	err := json.Unmarshal([]byte(str), &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func GetOperatorResult(name, token string) (*OperateResult, error) {
	resStr, err := AppOperate(name, token)
	if err != nil {
		return nil, err
	}

	return ParseOperator(resStr)
}

func OperateList(token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceOperateListURLTempl, appServiceHost, appServicePort)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func OperatorHistory(name, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceOperateHistoryURLTempl, appServiceHost, appServicePort, name)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func OperatorHistoryList(token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceOperateHistoryListURLTempl, appServiceHost, appServicePort)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func OperatorHistoryListWitRaw(token, raw string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceOperateHistoryListWithRawURLTempl, appServiceHost, appServicePort, raw)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func Apps(name, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceAppsURLTempl, appServiceHost, appServicePort, name)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func TerminusVersionValue() (string, error) {
	if conf.GetIsPublic() {
		return os.Getenv("PUBLIC_VERSION"), nil
	}

	version, err := TerminusVersion()
	if err != nil {
		return "", err
	}

	glog.Infof("version:%s", version)

	type VersionInfo struct {
		Version string `json:"version"`
	}

	var versionInfo VersionInfo

	err = json.Unmarshal([]byte(version), &versionInfo)
	if err != nil {
		fmt.Println("Error parsing JSON:", err)
		return "", err
	}

	return versionInfo.Version, nil
}

func TerminusVersion() (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceTerminusVersionURLTempl, appServiceHost, appServicePort)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, "", nil)
}

func TerminusNodes(token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceTerminusNodesURLTempl, appServiceHost, appServicePort)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func AppsList(token, isSysApp, state string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceAppsListURLTempl, appServiceHost, appServicePort, isSysApp, state)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func ParseAppsList(str string) (apps []Application, err error) {
	err = json.Unmarshal([]byte(str), &apps)
	if err != nil {
		glog.Warningf("json.Unmarshal %s, err:%s", str, err.Error())
		return
	}

	return
}

func ParseAppsListToMap(str string) (appsMap map[string]Application, err error) {
	var apps []Application
	apps, err = ParseAppsList(str)
	if err != nil {
		return
	}

	fmt.Printf("apps:%v\n", apps)

	appsMap = make(map[string]Application)
	for _, app := range apps {
		appsMap[app.Spec.Name] = app
	}

	fmt.Printf("appsMap:%v\n", appsMap)
	return
}

func ConvertAppsListToMap(apps []Application) (appsMap map[string]Application) {
	appsMap = make(map[string]Application)
	for _, app := range apps {
		appsMap[app.Spec.Name] = app
	}

	return
}

func ParsellmsList(str string) (llms []*models.ModelStatusResponse, err error) {
	err = json.Unmarshal([]byte(str), &llms)
	if err != nil {
		glog.Warningf("json.Unmarshal %s, err:%s", str, err.Error())
		return
	}

	return
}

func ParseLlmsListToMap(str string) (llmsMap map[string]*models.ModelStatusResponse, err error) {
	var llms []*models.ModelStatusResponse
	llms, err = ParsellmsList(str)
	if err != nil {
		return
	}

	fmt.Printf("llms:%v\n", llms)

	llmsMap = make(map[string]*models.ModelStatusResponse)
	for _, llm := range llms {
		llmsMap[llm.ID] = llm
	}

	fmt.Printf("appsMap:%v\n", llmsMap)
	return
}

func ConvertModelsListToMap(llms []*models.ModelStatusResponse) (llmMap map[string]*models.ModelStatusResponse) {
	llmMap = make(map[string]*models.ModelStatusResponse)
	for _, l := range llms {
		llmMap[l.ID] = l
	}

	return
}

type RenderResponse struct {
	Code int `json:"code"`
	Data struct {
		Content string `json:"content"`
	} `json:"data"`
}

func RenderManifest(content, token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceRenderManifestURLTempl, appServiceHost, appServicePort)
	
	requestBody := map[string]string{
		"content": content,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}
	
	bodyReader := bytes.NewBuffer(jsonData)
	
	responseStr, err := utils.SendHttpRequestWithToken(http.MethodPost, url, token, bodyReader)
	if err != nil {
		return "", err
	}
	
	var response RenderResponse
	if err := json.Unmarshal([]byte(responseStr), &response); err != nil {
		glog.Warningf("failed to unmarshal render response: %s, error: %v", responseStr, err)
		return "", err
	}
	
	return response.Data.Content, nil
}
