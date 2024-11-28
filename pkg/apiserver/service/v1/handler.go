// Copyright 2022 bytetrade
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/bfl"
	"market/internal/conf"
	"market/internal/constants"
	"market/internal/models"
	"market/internal/redisdb"
	"market/internal/watchdog"
	"market/pkg/api"
	"market/pkg/utils"
	"net/http"
	"os"
	"strconv"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

type Handler struct {
	//appServiceClient       *appservice.Client
	watchDogManager        *watchdog.Manager
	upgradeWatchDogManager *watchdog.UpgradeManager
	commonWatchDogManager  *watchdog.ModelManager
}

func newHandler() *Handler {
	return &Handler{
		//appServiceClient:       appservice.NewAppServiceClient(),
		watchDogManager:        watchdog.NewWatchDogManager(),
		upgradeWatchDogManager: watchdog.NewUpgradeWatchDogManager(),
		commonWatchDogManager:  watchdog.NewCommonWatchDogManager(),
	}
}

func getToken(req *restful.Request) string {
	if conf.GetIsPublic() {
		return "public"
	}

	cookie, err := req.Request.Cookie(constants.AuthorizationTokenCookieKey)
	if err != nil {
		token := req.Request.Header.Get(constants.AuthorizationTokenKey)
		if token == "" {
			glog.Warningf("req.Request.Cookie err:%s", err)
		}

		return token
	}

	return cookie.Value
}

func (h *Handler) handleTerminusVersion(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	resBody, err := appservice.TerminusVersion(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	respJsonWithOriginBody(resp, resBody)
}

func (h *Handler) handleTerminusNodes(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	resBody, err := appservice.TerminusNodes(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	respJsonWithOriginBody(resp, resBody)
}

func (h *Handler) handleMyApps(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	resBody, err := bfl.GetMyApps(token)
	if err != nil {
		api.HandleInternalError(resp, fmt.Errorf("list apps err:%v, resp:%s", err, resBody))
		return
	}

	respJsonWithOriginBody(resp, resBody)
}

func (h *Handler) list(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appsMap, err := getAppsMap(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	page, err := strconv.Atoi(req.QueryParameter("page"))
	if err != nil {
		glog.Infof("page error: %s", err.Error())
		page = 0
	}

	size, err := strconv.Atoi(req.QueryParameter("size"))
	if err != nil {
		glog.Infof("size error: %s", err.Error())
		size = 0
	}

	category := req.QueryParameter("category")
	ty := req.QueryParameter("type")
	if ty == "" {
		ty = defaultAppType()
	}

	apps := appmgr.ReadCacheApplications(page, size, category, ty)

	workflowMap, _ := getWorkflowsMap(token)
	middlewareMap, _ := getMiddlewaresMap(token)

	appWithStatusList := parseAppInfos(h, apps, appsMap, workflowMap, middlewareMap)

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, res.TotalCount)))
}

func (h *Handler) menuTypes(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	res := appmgr.ReadCacheAppTypes()

	types := parseAppTypes(res)

	menuTypeData := models.MenuTypeResponseData{
		MenuTypes: []models.MenuType{
			{Label: "discover", Key: "Home", Icon: "sym_r_radar"},
		},
		I18n: models.Localization{
			ZhCN: models.MainData{
				Discover: "发现",
			},
			EnUS: models.MainData{
				Discover: "Discover",
			},
		},
	}

	if utils.Contains(types, "Productivity") {
		menuTypeData.MenuTypes = append(menuTypeData.MenuTypes, models.MenuType{Label: "productivity", Key: "Productivity", Icon: "sym_r_business_center"})
		menuTypeData.I18n.ZhCN.Productivity = "效率"
		menuTypeData.I18n.EnUS.Productivity = "Productivity"
	}

	if utils.Contains(types, "Utilities") {
		menuTypeData.MenuTypes = append(menuTypeData.MenuTypes, models.MenuType{Label: "utilities", Key: "Utilities", Icon: "sym_r_extension"})
		menuTypeData.I18n.ZhCN.Utilities = "实用工具"
		menuTypeData.I18n.EnUS.Utilities = "Utilities"
	}

	if utils.Contains(types, "Entertainment") {
		menuTypeData.MenuTypes = append(menuTypeData.MenuTypes, models.MenuType{Label: "entertainment", Key: "Entertainment", Icon: "sym_r_interests"})
		menuTypeData.I18n.ZhCN.Entertainment = "娱乐"
		menuTypeData.I18n.EnUS.Entertainment = "Entertainment"
	}

	if utils.Contains(types, "Social Network") {
		menuTypeData.MenuTypes = append(menuTypeData.MenuTypes, models.MenuType{Label: "socialNetwork", Key: "Social Network", Icon: "sym_r_group"})
		menuTypeData.I18n.ZhCN.SocialNetwork = "社交网络"
		menuTypeData.I18n.EnUS.SocialNetwork = "Social network"
	}

	if utils.Contains(types, "Blockchain") {
		menuTypeData.MenuTypes = append(menuTypeData.MenuTypes, models.MenuType{Label: "blockchain", Key: "Blockchain", Icon: "sym_r_stack"})
		menuTypeData.I18n.ZhCN.Blockchain = "区块链"
		menuTypeData.I18n.EnUS.Blockchain = "Blockchain"
	}

	menuTypeData.MenuTypes = append(menuTypeData.MenuTypes, models.MenuType{Label: "recommendation", Key: "Recommend", Icon: "sym_r_featured_play_list"})
	menuTypeData.I18n.ZhCN.Recommendation = "推荐算法"
	menuTypeData.I18n.EnUS.Recommendation = "Recommendation"

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, menuTypeData))
}

func (h *Handler) listTop(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appsMap, err := getAppsMap(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	category := req.QueryParameter("category")
	ty := req.QueryParameter("type")
	if ty == "" {
		ty = defaultAppType()
	}

	size, err := strconv.Atoi(req.QueryParameter("size"))
	if err != nil {
		glog.Infof("size error: %s", err.Error())
		size = 0
	}

	apps, totalCount := appmgr.ReadCacheTopApps(category, ty, size)

	workflowMap, _ := getWorkflowsMap(token)
	middlewareMap, _ := getMiddlewaresMap(token)

	appWithStatusList := parseAppInfos(h, apps, appsMap, workflowMap, middlewareMap)

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, totalCount)))
}

func (h *Handler) listLatest(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appsMap, err := getAppsMap(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	page, err := strconv.Atoi(req.QueryParameter("page"))
	if err != nil {
		glog.Infof("page error: %s", err.Error())
		page = 0
	}

	size, err := strconv.Atoi(req.QueryParameter("size"))
	if err != nil {
		glog.Infof("size error: %s", err.Error())
		size = 0
	}

	category := req.QueryParameter("category")
	ty := req.QueryParameter("type")
	if ty == "" {
		ty = defaultAppType()
	}

	apps := appmgr.ReadCacheApplications(page, size, category, ty)

	workflowMap, _ := getWorkflowsMap(token)
	middlewareMap, _ := getMiddlewaresMap(token)

	appWithStatusList := parseAppInfos(h, apps, appsMap, workflowMap, middlewareMap)

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, res.TotalCount)))
}

func (h *Handler) openApplication(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	// osSystemServer := os.Getenv("OS_SYSTEM_SERVER")
	// if osSystemServer == nil {
	// 	log.Println("need env OS_SYSTEM_SERVER")
	// }

	httpposturl := fmt.Sprintf("http://%s/legacy/v1alpha1/api.intent/v1/server/intent/send", os.Getenv("OS_SYSTEM_SERVER"))

	fmt.Println("HTTP JSON POST URL:", httpposturl)

	appid := req.QueryParameter("id")
	fmt.Println("ID:", appid)

	var jsonData = []byte(`{
			"action": "view",
			"category": "launcher",
			"data": {
				"appid": "` + appid + `"
			}
		}`)
	request, err := http.NewRequest(http.MethodPost, httpposturl, bytes.NewBuffer(jsonData))
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	client := &http.Client{}
	response, error := client.Do(request)
	if err != nil {
		panic(error)
	}
	defer response.Body.Close()

	fmt.Println("response Status:", response.Status)
	fmt.Println("response Headers:", response.Header)
	body, _ := ioutil.ReadAll(response.Body)
	fmt.Println("response Body:", string(body))

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, nil))
}

func (h *Handler) search(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appsMap, err := getAppsMap(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	appName := req.PathParameter(ParamAppName)
	if appName == "" {
		api.HandleError(resp, errors.New("name is empty"))
		return
	}

	page := req.QueryParameter("page")
	size := req.QueryParameter("size")
	res, err := appmgr.Search(appName, page, size)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	workflowMap, _ := getWorkflowsMap(token)
	middlewareMap, _ := getMiddlewaresMap(token)

	appWithStatusList := parseAppInfos(h, res, appsMap, workflowMap, middlewareMap)

	_ = resp.WriteEntity(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, res.TotalCount)))
}

func (h *Handler) searchPost(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appsMap, err := getAppsMap(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	var appNames models.SearchAppReq
	err = req.ReadEntity(&appNames)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	appName := appNames.Name
	if appName == "" {
		api.HandleError(resp, errors.New("name is empty"))
		return
	}

	page := req.QueryParameter("page")
	size := req.QueryParameter("size")
	res, err := appmgr.Search(appName, page, size)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	workflowMap, _ := getWorkflowsMap(token)
	middlewareMap, _ := getMiddlewaresMap(token)

	appWithStatusList := parseAppInfos(h, res, appsMap, workflowMap, middlewareMap)

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, res.TotalCount)))
}

func (h *Handler) info(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appName := req.PathParameter(ParamAppName)
	if appName == "" {
		api.HandleError(resp, errors.New("name is empty"))
		return
	}

	infoLocal, _ := redisdb.GetLocalAppInfo(appName)

	infoMarket := appmgr.ReadCacheApplication(appName)
	if infoMarket == nil && infoLocal == nil {
		api.HandleError(resp, errors.New("not found"))
		return
	}

	info := infoLocal
	if info == nil {
		info = infoMarket
		info.Source = constants.AppFromMarket
	} else if infoMarket != nil {
		info.Version = infoMarket.Version
	}

	if info.CfgType == constants.AppType {
		appsMap, err := getAppsMap(token)
		if err != nil {
			api.HandleError(resp, err)
			return
		}
		getAppStatus(info, appsMap)
	}

	if info.CfgType == constants.RecommendType {
		workflowMap, err := getWorkflowsMap(token)
		if err != nil {
			api.HandleError(resp, err)
			return
		}
		getCommonStatus(info, workflowMap)
	}

	if info.CfgType == constants.ModelType {
		modelsMap, _ := getModelsMap(token)
		//if err != nil {
		//	api.HandleError(resp, err)
		//	return
		//}
		getModelStatus(info, modelsMap)
	}

	if info.CfgType == constants.MiddlewareType {
		middlewareMap, err := getMiddlewaresMap(token)
		if err != nil {
			api.HandleError(resp, err)
			return
		}
		getCommonStatus(info, middlewareMap)
	}

	if info.Status == models.AppUninstalled {
		info = infoMarket
		info.Status = models.AppUninstalled
	}

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, info))
}

func (h *Handler) readme(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appName := req.PathParameter(ParamAppName)
	if appName == "" {
		api.HandleError(resp, errors.New("name is empty"))
		return
	}

	readme, err := appmgr.GetReadMe(appName)
	if err != nil {
		resp.WriteEntity(models.NewResponse(api.InternalServerError, readme, nil))
		return
	}

	resp.WriteEntity(models.NewResponse(api.OK, readme, nil))
}
