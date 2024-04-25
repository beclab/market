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
	"fmt"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/conf"
	"market/internal/constants"
	"market/internal/models"
	"net/http"

	restfulspec "github.com/emicklei/go-restful-openapi/v2"
	"github.com/emicklei/go-restful/v3"
)

const (
	APIRootPath   = "/app-store"
	Version       = "v1"
	ParamAppName  = "name"
	ParamAppNames = "names"
	ResourceType  = "type"
)

var (
	ModuleTags = []string{"market"}
)

func newWebService() *restful.WebService {
	webservice := restful.WebService{}

	webservice.Path(fmt.Sprintf("%s/%s", APIRootPath, Version)).
		Produces(restful.MIME_JSON)

	return &webservice
}

func AddToContainer(c *restful.Container) error {
	ws := newWebService()
	handler := newHandler()

	ws.Route(ws.GET("/applications").
		To(handler.list).
		Doc("get application list").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.QueryParameter("page", "page")).
		Param(ws.QueryParameter("size", "size")).
		Param(ws.QueryParameter("category", "category")).
		Param(ws.QueryParameter("type", "type")).
		Returns(http.StatusOK, "success to get application list", &models.ListResponse{}))

	ws.Route(ws.GET("/applications/top").
		To(handler.listTop).
		Doc("get top application list").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.QueryParameter("size", "size")).
		Param(ws.QueryParameter("category", "category")).
		Param(ws.QueryParameter("type", "type")).
		Returns(http.StatusOK, "success to get top application list", &models.ListResponse{}))

	ws.Route(ws.GET("/applications/latest").
		To(handler.listLatest).
		Doc("get latest application list").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.QueryParameter("page", "page")).
		Param(ws.QueryParameter("size", "size")).
		Param(ws.QueryParameter("category", "category")).
		Param(ws.QueryParameter("type", "type")).
		Returns(http.StatusOK, "success to get latest application list", &models.ListResponse{}))

	ws.Route(ws.GET("/applications/info/{"+ParamAppName+"}").
		To(handler.info).
		Doc("get the application info by name").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Returns(http.StatusOK, "success to get the application info", &models.InfoResponse{}))

	ws.Route(ws.GET("/readme/{"+ParamAppName+"}/README.md").
		To(handler.readme).
		Doc("get the application readme info").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ParamAppName, "the name of the application")).
		Returns(http.StatusOK, "Success to get the application readme info", nil))

	ws.Route(ws.GET("/readme/{"+ParamAppName+"}").
		To(handler.readme).
		Doc("get the application readme info").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ParamAppName, "the name of the application")).
		Returns(http.StatusOK, "Success to get the application readme info", nil))

	ws.Route(ws.GET("/applications/search/{"+ParamAppName+"}").
		To(handler.search).
		Doc("fuzzy search application list by name").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Param(ws.QueryParameter("page", "page")).
		Param(ws.QueryParameter("size", "size")).
		Returns(http.StatusOK, "success to search application list by name", &models.ListResponse{}))

	ws.Route(ws.POST("/applications/search").
		To(handler.searchPost).
		Reads(models.SearchAppReq{}).
		Doc("fuzzy search application list by name").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.QueryParameter("page", "page")).
		Param(ws.QueryParameter("size", "size")).
		Returns(http.StatusOK, "success to search application list by name", &models.ListResponse{}))

	ws.Route(ws.GET("/applications/version-history/{"+ParamAppName+"}").
		To(handler.versionHistory).
		Param(ws.PathParameter(ParamAppName, "the name of the application")).
		Doc("get application version history by name").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "success to get application version history by name", &models.ListVersionResponse{}))

	ws.Route(ws.GET("/pages/detail").
		To(handler.pagesDetail).
		Param(ws.QueryParameter("category", "category")).
		Doc("get the pages detail").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "Success to get the pages detail", &appmgr.Result{}))

	ws.Route(ws.GET("/pages/detailraw").
		To(handler.pagesDetailRaw).
		Doc("get the pages detail raw").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "Success to get the pages detail raw", &appmgr.Result{}))

	ws.Route(ws.POST("/applications/infos").
		To(handler.handleInfos).
		Param(ws.BodyParameter(ParamAppNames, "the name list of the application")).
		Doc("get app infos by names").
		Reads([]string{}).
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "success to get app infos by names", nil))

	ws.Route(ws.GET("/workflow/recommend/detail").
		To(handler.workflowRecommendsDetail).
		Param(ws.QueryParameter("category", "category")).
		Doc("get recommend infos").
		Reads([]string{}).
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "success to get recommend infos", nil))

	ws.Route(ws.GET("/model/detail").
		To(handler.modelList).
		Param(ws.QueryParameter("category", "category")).
		Doc("get model infos").
		Reads([]string{}).
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "success to get model infos", nil))

	fmt.Printf("conf.GetIsPublic():%t\n", conf.GetIsPublic())
	if conf.GetIsPublic() {
		c.Add(ws)
		return nil
	}

	ws.Route(ws.GET("/myapps").
		To(handler.handleMyApps).
		Doc("List user's apps").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "User's app list", &models.MyAppsResp{}))

	ws.Route(ws.POST("/applications/open").
		To(handler.openApplication).
		Doc("open application from App Store").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "success to check update for the application list", nil))

	ws.Route(ws.POST("/applications/dev-upload").
		To(handler.devUpload).
		Doc("upload the application for dev").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.FormParameter(ParamAppName, "the name of a application")).
		Param(ws.QueryParameter("source", "source")).
		Returns(http.StatusOK, "Success to upload the application for dev", &models.InstallationStatusResp{}))

	ws.Route(ws.POST("/applications/{"+ParamAppName+"}/dev-install").
		To(handler.devInstall).
		Doc("install the application for dev").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.FormParameter(ParamAppName, "the name of a application")).
		Returns(http.StatusOK, "Success to begin a installation of the application for dev", &models.InstallationResponse{}))

	ws.Route(ws.POST("/applications/{"+ParamAppName+"}/install").
		To(handler.install).
		Doc("install the application").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Returns(http.StatusOK, "Success to begin a installation of the application", &models.InstallationResponse{}))

	ws.Route(ws.POST("/recommends/{"+ParamAppName+"}/install").
		To(handler.installRecommend).
		Doc("install the recommend").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ParamAppName, "the name of a recommend")).
		Returns(http.StatusOK, "Success to begin a installation of the recommend", &models.InstallationResponse{}))

	ws.Route(ws.POST("/applications/{"+ParamAppName+"}/upgrade").
		To(handler.upgrade).
		Doc("upgrade the application").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ParamAppName, "the name of a application")).
		Returns(http.StatusOK, "Success to begin a upgrade of the application", &models.InstallationResponse{}))

	ws.Route(ws.POST("/application/deps").
		To(handler.checkDependencies).
		Reads(models.DepRequest{}).
		Doc("check whether specified dependencies were meet").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "return not satisfied dependencies", &models.DependenciesResp{}))

	ws.Route(ws.GET("/settings/nsfw").
		To(handler.getNsfw).
		Doc("get the nsfw setting").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "Success to get the nsfw setting", &models.Nsfw{}))

	ws.Route(ws.POST("/settings/nsfw").
		To(handler.setNsfw).
		Reads(models.Nsfw{}).
		Doc("set the nsfw setting").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "Success to set the nsfw setting", &models.ResponseBase{}))

	ws.Route(ws.GET("/user/resource").
		To(handler.curUserResourceStatus).
		Doc("get current user's resource and resource usage").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "Success to get current user's resource and resource usage", &appservice.ClusterMetrics{}))

	ws.Route(ws.GET("/cluster/resource").
		To(handler.clusterResourceStatus).
		Doc("get cluster resource and resource usage").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "Success to get cluster resource and resource usage", nil))

	ws.Route(ws.POST("/websocket/message").
		To(handler.receiveWebsocketMessage).
		Doc("receive websocket message for test").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "Success to receive websocket message", nil))

	ws.Route(ws.POST("/uninstall/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.uninstall).
		Doc("uninstall the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of a app/workflow")).
		Returns(http.StatusOK, "Success to begin a uninstallation of the application/workflow", &models.InstallationResponse{}))

	ws.Route(ws.POST("/suspend/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.suspend).
		Doc("suspend the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of a app/workflow")).
		Returns(http.StatusOK, "suspend of the application/workflow", &models.InstallationResponse{}))

	ws.Route(ws.POST("/resume/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.resume).
		Doc("resume the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of a app/workflow")).
		Returns(http.StatusOK, "resume of the application/workflow", &models.InstallationResponse{}))

	ws.Route(ws.GET("/status/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.status).
		Doc("get the status by type and name of the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of the application/workflow")).
		Returns(http.StatusOK, "Success to get status", &appservice.StatusResult{}))

	ws.Route(ws.GET("/status/{"+ResourceType+"}").
		To(handler.statusList).
		Doc("get the status list by type of application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Returns(http.StatusOK, "Success to get status list", &appservice.StatusResultList{}))

	ws.Route(ws.GET("/operate/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.operate).
		Doc("get the operate by type and name of the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of the application/workflow")).
		Returns(http.StatusOK, "Success to get operate", &appservice.OperateResult{}))

	ws.Route(ws.GET("/operate/{"+ResourceType+"}").
		To(handler.operateList).
		Doc("get the operate list by type of application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Returns(http.StatusOK, "Success to get operate list", []appservice.OperateResult{}))

	ws.Route(ws.GET("/operate_history/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.operateHistory).
		Doc("get the operate history by type and name of the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of the application/workflow")).
		Returns(http.StatusOK, "Success to get operate history", nil))

	ws.Route(ws.GET("/operate_history/{"+ResourceType+"}").
		To(handler.operateHistoryList).
		Doc("get the operate history by type the app/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Returns(http.StatusOK, "Success to get operate history", nil))

	ws.Route(ws.GET("/operate_history").
		To(handler.operateHistoryListNew).
		Doc("get the operate history by type the app/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Returns(http.StatusOK, "Success to get operate history", nil))

	ws.Route(ws.GET("/apps/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.apps).
		Doc("get the apps by type and name of the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of the application/workflow")).
		Returns(http.StatusOK, "Success to get apps", &appservice.Application{}))

	ws.Route(ws.GET("/apps/{"+ResourceType+"}").
		To(handler.appsList).
		Doc("get the apps by type and name of the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.QueryParameter("issysapp", "issysapp")).
		Param(ws.QueryParameter("state", "state")).
		Returns(http.StatusOK, "Success to get apps", []appservice.Application{}))

	ws.Route(ws.POST("/cancel/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.cancel).
		Doc("cancel the apps by type and name of the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of the application/workflow")).
		Returns(http.StatusOK, "Success to cancel the apps", &models.InstallationResponse{}))

	ws.Route(ws.POST("/apps/cancel/{"+ResourceType+"}/{"+ParamAppName+"}").
		To(handler.cancel).
		Doc("cancel the apps by type and name of the application/workflow").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.PathParameter(ResourceType, "the type of app/workflow")).
		Param(ws.PathParameter(ParamAppName, "the name of the application/workflow")).
		Returns(http.StatusOK, "Success to cancel the apps", &models.InstallationResponse{}))

	ws.Route(ws.GET("/user-info").
		To(handler.userInfo).
		Doc("get a user's role").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "Success to get user's role", &appservice.UserInfoResult{}))

	ws.Route(ws.POST("/applications/provider/installdev").
		To(handler.providerInstallDev).
		Doc("Install/upgrade the dev mode application (Only for Provider) ").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.HeaderParameter(constants.AuthorizationTokenKey, "Auth token")).
		Reads(models.InstallProviderRequest{}).
		Returns(http.StatusOK, "Success to begin a installation/upgrade of the application", &models.InstallationResponse{}))

	ws.Route(ws.POST("/applications/installdev").
		To(handler.installDev).
		Doc("Install/upgrade the dev mode application (Only for Provider) ").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.HeaderParameter(constants.AuthorizationTokenKey, "Auth token")).
		Reads(models.InstallOptions{}).
		Returns(http.StatusOK, "Success to begin a installation/upgrade of the application", &models.InstallationResponse{}))

	ws.Route(ws.POST("/applications/provider/uninstalldev").
		To(handler.providerUninstallDev).
		Doc("Uninstall the dev mode application (Only for Provider) ").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.HeaderParameter(constants.AuthorizationTokenKey, "Auth token")).
		Reads(models.InstallProviderRequest{}).
		Returns(http.StatusOK, "Success to begin a uninstallation of the application", &models.InstallationResponse{}))

	ws.Route(ws.POST("/applications/uninstalldev").
		To(handler.uninstallDev).
		Doc("Uninstall the dev mode application (Only for Provider) ").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Param(ws.HeaderParameter(constants.AuthorizationTokenKey, "Auth token")).
		Reads(models.UninstallData{}).
		Returns(http.StatusOK, "Success to begin a uninstallation of the application", &models.InstallationResponse{}))

	ws.Route(ws.GET("/myterminus").
		To(handler.myterminus).
		Doc("get myterminus apps, recommends, models...").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "success to get myterminus", nil))

	ws.Route(ws.GET("/terminus/version").
		To(handler.handleTerminusVersion).
		Doc("get terminus version").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "success to get terminus version", nil))

	ws.Route(ws.GET("/terminus/nodes").
		To(handler.handleTerminusNodes).
		Doc("get terminus nodes").
		Metadata(restfulspec.KeyOpenAPITags, ModuleTags).
		Returns(http.StatusOK, "success to get terminus nodes", nil))

	c.Add(ws)
	return nil
}
