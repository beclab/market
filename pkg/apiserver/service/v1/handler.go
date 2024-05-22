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
	"market/internal/boltdb"
	"market/internal/conf"
	"market/internal/constants"
	"market/internal/models"
	"market/internal/watchdog"
	"market/pkg/api"
	"net/http"
	"os"

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

	page := req.QueryParameter("page")
	size := req.QueryParameter("size")
	category := req.QueryParameter("category")
	ty := req.QueryParameter("type")
	if ty == "" {
		ty = "app"
	}

	res, err := appmgr.GetApps(page, size, category, ty)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	workflowMap, _ := getWorkflowsMap(token)
	appWithStatusList := parseAppInfos(res, appsMap, workflowMap)

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, res.TotalCount)))
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
		ty = "app"
	}
	size := req.QueryParameter("size")
	res, err := appmgr.GetTopApps(category, ty, size)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	workflowMap, _ := getWorkflowsMap(token)
	appWithStatusList := parseAppInfos(res, appsMap, workflowMap)
	resp.WriteEntity(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, res.TotalCount)))
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

	page := req.QueryParameter("page")
	size := req.QueryParameter("size")
	category := req.QueryParameter("category")
	ty := req.QueryParameter("type")
	if ty == "" {
		ty = "app"
	}

	res, err := appmgr.GetApps(page, size, category, ty)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	workflowMap, _ := getWorkflowsMap(token)
	appWithStatusList := parseAppInfos(res, appsMap, workflowMap)

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
	appWithStatusList := parseAppInfos(res, appsMap, workflowMap)

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
	appWithStatusList := parseAppInfos(res, appsMap, workflowMap)

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

	infoLocal, _ := boltdb.GetLocalAppInfo(appName)

	infoMarket, err := appmgr.GetAppInfo(appName)
	if err != nil && infoLocal == nil {
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
		getWorkflowStatus(info, workflowMap)
	}

	if info.CfgType == constants.ModelType {
		modelsMap, _ := getModelsMap(token)
		//if err != nil {
		//	api.HandleError(resp, err)
		//	return
		//}
		getModelStatus(info, modelsMap)
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
		api.HandleError(resp, err)
		return
	}

	resp.Write([]byte(readme))
}
