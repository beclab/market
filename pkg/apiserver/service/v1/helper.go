package v1

import (
	"encoding/json"
	"fmt"
	"market/internal/appservice"
	"market/internal/boltdb"
	"market/internal/conf"
	"market/internal/constants"
	"market/internal/models"
	"market/internal/recommend"
	"market/pkg/api"
	"net/http"

	helmtime "helm.sh/helm/v3/pkg/time"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

func getRunningAppList(token string) ([]appservice.Application, error) {
	jsonStr, err := appservice.AppsList(token, "false", "")
	if err != nil {
		return nil, err
	}

	return appservice.ParseAppsList(jsonStr)
}

func filterMarketAndCustomApps(in []appservice.Application) (out []appservice.Application) {
	for _, app := range in {
		if source, sourceOk := app.Spec.Settings["source"]; sourceOk {
			if source != constants.AppFromMarket && source != constants.AppFromDev {
				continue
			}
			out = append(out, app)
		} else {
			out = append(out, app)
		}
	}

	return
}

func getRunningWorkflowList(token string) ([]*models.StatusData, error) {
	jsonStr, err := recommend.StatusList(token)
	if err != nil {
		return nil, err
	}

	return models.ParseStatusList(jsonStr)
}

func getRunningMiddlewareList(token string) ([]*models.StatusData, error) {
	jsonStr, err := appservice.MiddlewareStatusList(token)
	if err != nil {
		return nil, err
	}

	return models.ParseStatusList(jsonStr)
}

func hasGpu(token string) bool {
	resp, err := appservice.GetClusterResource(token)
	if err != nil {
		glog.Warningf("GetClusterResource res:%s err:%v", resp, err)
		return false
	}

	var metrics models.ClusterMetricsResp
	err = json.Unmarshal([]byte(resp), &metrics)
	if err != nil {
		glog.Warningf("json.Unmarshal %s, err:%s", resp, err.Error())
		return false
	}

	glog.Infof("resp:%s, metrics:%+v, metrics.CM.GPU.Total:%f", resp, metrics, metrics.CM.GPU.Total)
	return metrics.CM.GPU.Total > 0
}

func getRunningModelList(token string) ([]*models.ModelStatusResponse, error) {
	if !hasGpu(token) {
		return nil, nil
	}

	jsonStr, _, err := appservice.LlmStatusList(token)
	glog.Infof("LlmStatusList:res:%s", jsonStr)
	if err != nil {
		return nil, err
	}

	return appservice.ParseLlmStatusList(jsonStr)
}

func getAppsMap(token string) (map[string]appservice.Application, error) {
	if conf.GetIsPublic() {
		return nil, nil
	}

	jsonStr, err := appservice.AppsList(token, "false", "")
	if err != nil {
		return nil, err
	}

	return appservice.ParseAppsListToMap(jsonStr)
}

func getWorkflowsMap(token string) (map[string]*models.StatusData, error) {
	if conf.GetIsPublic() {
		return nil, nil
	}

	jsonStr, err := recommend.StatusList(token)
	if err != nil {
		return nil, err
	}

	return models.ParseStatusListToMap(jsonStr)
}

func getMiddlewaresMap(token string) (map[string]*models.StatusData, error) {
	if conf.GetIsPublic() {
		return nil, nil
	}

	jsonStr, err := appservice.MiddlewareStatusList(token)
	if err != nil {
		return nil, err
	}

	return models.ParseStatusListToMap(jsonStr)
}

func getModelsMap(token string) (map[string]*models.ModelStatusResponse, error) {
	if conf.GetIsPublic() {
		return nil, nil
	}
	if !hasGpu(token) {
		return nil, nil
	}

	jsonStr, _, err := appservice.LlmStatusList(token)
	if err != nil {
		return nil, err
	}

	return appservice.ParseLlmsListToMap(jsonStr)
}

func parseAppTypes(res *models.ListResultD) []string {
	var stringItems []string
	for _, rawMsg := range res.Items {
		var str string
		err := json.Unmarshal(rawMsg, &str)
		if err != nil {
			fmt.Println("Error unmarshalling:", err)
			continue
		}
		stringItems = append(stringItems, str)
	}
	return stringItems
}

func parseAppInfos(h *Handler, res *models.ListResultD, appsMap map[string]appservice.Application, workflowMap map[string]*models.StatusData, middlewareMap map[string]*models.StatusData) []*models.ApplicationInfo {

	var appWithStatusList []*models.ApplicationInfo
	for _, item := range res.Items {
		info := &models.ApplicationInfo{}
		err := json.Unmarshal(item, info)
		if err != nil {
			glog.Warningf("err:%s", err.Error())
			continue
		}

		if info.HasNsfwLabel() && boltdb.GetNsfwState() {
			glog.Warningf("app:%s has nsfw label, pass", info.Name)
			continue
		}

		getAppAndRecommendStatus(info, appsMap, workflowMap, middlewareMap)

		if (info.HasRemoveLabel() || info.HasSuspendLabel()) && info.Status == models.AppUninstalled {
			glog.Warningf("app:%s has pass label %v not installed, pass", info.Name, info.AppLabels)
			continue
		}

		// merge app info
		info.Progress = h.commonWatchDogManager.GetProgress(info.Name)

		appWithStatusList = append(appWithStatusList, info)
	}

	return appWithStatusList
}

func getAppAndRecommendStatus(info *models.ApplicationInfo, appsMap map[string]appservice.Application, workflowMap map[string]*models.StatusData, middlewareMap map[string]*models.StatusData) {
	if info == nil {
		return
	}

	if info.CfgType == constants.AppType {
		getAppStatus(info, appsMap)
		return
	}

	if info.CfgType == constants.RecommendType {
		getCommonStatus(info, workflowMap)
		return
	}

	if info.CfgType == constants.MiddlewareType {
		getCommonStatus(info, middlewareMap)
		return
	}
}

func getAppStatus(info *models.ApplicationInfo, appsMap map[string]appservice.Application) {
	if info == nil {
		return
	}

	info.Status = models.AppUninstalled
	if ia, ok := appsMap[info.Name]; ok {
		//fmt.Printf("ia:%v\n", ia)
		info.Status = ia.Status.State
		if version, versionOk := ia.Spec.Settings["version"]; versionOk {
			info.CurVersion = version
			info.InstallTime = helmtime.Time(ia.CreationTimestamp)
			info.CheckUpdate(false)
		}
	}
}

func getCommonStatus(info *models.ApplicationInfo, statusMap map[string]*models.StatusData) {
	if info == nil {
		return
	}

	//fmt.Printf("info.Name:%s, workflowMap:%v\n", info.Name, workflowMap)
	info.Status = models.AppUninstalled
	if ia, ok := statusMap[info.Name]; ok {
		info.Status = ia.ResourceStatus
		info.CurVersion = ia.Version
		info.InstallTime = helmtime.Time(ia.CreateTime)
		info.CheckUpdate(false)
	}
}

func getModelStatus(info *models.ApplicationInfo, llmsMap map[string]*models.ModelStatusResponse) {
	if info == nil {
		return
	}
	info.Status = models.AppUninstalled
	if ia, ok := llmsMap[info.Name]; ok {
		if ia.Status == models.NoInstalled {
			return
		}
		info.Status = ia.Status
	}
}

func respErr(resp *restful.Response, err error) {
	//todo code
	respErrWithCode(resp, err, 400)
}

func respErrWithCode(resp *restful.Response, err error, code int) {
	if code == 200 {
		code = 400
	}
	_ = resp.WriteHeaderAndEntity(http.StatusOK, api.Error{
		Code: code,
		Msg:  err.Error(),
	})
}

func installByType(info *models.ApplicationInfo, token string) (string, error) {
	switch info.CfgType {
	case "", constants.AppType:
		return appservice.AppInstall(info.Name, info.Source, token)
	case constants.RecommendType:
		return recommend.Install(info.Name, info.Source, token)
	case constants.ModelType:
		resBody, _, err := appservice.LlmInstall(info.Name, info.Source, token)
		return resBody, err
	case constants.MiddlewareType:
		resBody, _, err := appservice.MiddlewareInstall(info.Name, info.Source, token)
		return resBody, err
	//case constants.AgentType:
	//	//todo
	default:
		return "", fmt.Errorf("%s type %s invalid", info.Name, info.CfgType)
	}
}

func uninstallByType(name, cfgType, token string) (string, error) {
	if cfgType == "" || cfgType == constants.AppType {
		return appservice.AppUninstall(name, token)
	}

	if cfgType == constants.RecommendType {
		return recommend.Uninstall(name, token)
	}

	if cfgType == constants.ModelType {
		body, _, err := appservice.LlmUninstall(name, token)
		return body, err
	}

	if cfgType == constants.MiddlewareType {
		body, _, err := appservice.MiddlewareUninstall(name, token)
		return body, err
	}

	return "", fmt.Errorf("%s type %s invalid", name, cfgType)
}

func upgradeByType(info *models.ApplicationInfo, token string) (string, error) {
	if info.CfgType == "" || info.CfgType == constants.AppType {
		return appservice.AppUpgrade(info.Name, info.Version, info.Source, token)
	}

	if info.CfgType == constants.RecommendType {
		return recommend.Upgrade(info.Name, info.Version, info.Source, token)
	}

	if info.CfgType == constants.ModelType {

	}

	return "", fmt.Errorf("%s type %s invalid", info.Name, info.CfgType)
}

func respJsonWithOriginBody(resp *restful.Response, body string) {
	glog.Info("body:", body)
	info := make(map[string]interface{})
	err := json.Unmarshal([]byte(body), &info)
	if err != nil {
		resInfo := map[string]any{
			"code":    500,
			"message": body,
		}
		resp.WriteAsJson(resInfo)
		return
	}

	resp.Header().Set(restful.HEADER_ContentType, restful.MIME_JSON)
	_, err = resp.Write([]byte(body))
	if err != nil {
		glog.Warningf("err:%s", err)
	}
	return
}

func defaultAppType() string {
	return fmt.Sprintf("%s,%s", constants.AppType, constants.MiddlewareType)
}
