package v1

import (
	"errors"
	"fmt"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/constants"
	"market/internal/models"
	"market/internal/redisdb"
	"market/pkg/api"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
	"k8s.io/klog/v2"
)

func (h *Handler) handleInfos(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	var names []string
	err := req.ReadEntity(&names)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	infosMarket := appmgr.ReadCacheApplicationsWithMap(names, token)
	if infosMarket == nil {
		api.HandleError(resp, errors.New("get apps failed"))
		return
	}

	infosLocal, err := redisdb.GetLocalAppInfoMap()
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	//var namesNotFoundInMarket []string
	var infoList []*models.ApplicationInfo
	for _, name := range names {
		if _, okLocal := infosLocal[name]; okLocal {
			infoList = append(infoList, infosLocal[name])
		} else if _, ok := infosMarket[name]; ok {
			infoList = append(infoList, infosMarket[name])
		}
		//namesNotFoundInMarket = append(namesNotFoundInMarket, name)
	}

	//localInfos := redisdb.GetDevAppInfos(namesNotFoundInMarket)
	//infoList = append(infoList, localInfos...)

	appsMap, err := getAppsMap(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}
	workflowMap, _ := getWorkflowsMap(token)
	middlewareMap, _ := getMiddlewaresMap(token)

	for i := range infoList {
		if _, exist := infosLocal[infoList[i].Name]; exist {
			infoList[i].Source = infosLocal[infoList[i].Name].Source
		} else {
			infoList[i].Source = constants.AppFromMarket
		}

		if _, exist := infosMarket[infoList[i].Name]; exist {
			infoList[i].Version = infosMarket[infoList[i].Name].Version
		}

		getAppAndRecommendStatus(infoList[i], appsMap, workflowMap, middlewareMap)
	}

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, infoList))
}

func (h *Handler) apps(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appName := req.PathParameter(ParamAppName)
	if appName == "" {
		api.HandleError(resp, fmt.Errorf("name is nil"))
		return
	}

	ty := req.PathParameter(ResourceType)
	if ty == "" {
		api.HandleError(resp, fmt.Errorf("type is nil"))
		return
	}

	if ty == constants.AppType {
		resBody, err := appservice.Apps(appName, token)
		if err != nil {
			glog.Warningf("appservice.Apps %s resp:%s, err:%s", appName, resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		_, _ = resp.Write([]byte(resBody))
		return
	}

	if ty == constants.RecommendType {
		return
	}
}

func (h *Handler) appsList(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	ty := req.PathParameter(ResourceType)
	if ty == "" {
		api.HandleError(resp, fmt.Errorf("type is nil"))
		return
	}

	isSysApp := req.QueryParameter("issysapp")
	state := req.QueryParameter("state")
	if ty == constants.AppType {
		resBody, err := appservice.AppsList(token, isSysApp, state)
		if err != nil {
			glog.Warningf("appservice.AppsList resp:%s, err:%s", resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		_, _ = resp.Write([]byte(resBody))
		return
	}

	if ty == constants.RecommendType {
		return
	}
}

func (h *Handler) handleI18ns(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}
	//names := []string{"astral", "market", "firefox"}
	names := []string{}
	infosMarket := appmgr.ReadCacheApplicationsWithMap(names, token)
	if infosMarket == nil {
		api.HandleError(resp, errors.New("get apps failed"))
		return
	}
	//klog.Infof("infosMarket: %#v", infosMarket)

	infosLocal, err := redisdb.GetLocalAppInfoMap()
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	//klog.Infof("infosLocal: %#v", infosLocal["firefox"])

	i18nList := make([]map[string]map[string]models.I18n, 0)

	i18nMap := make(map[string]map[string]models.I18n)
	for name, info := range infosMarket {
		klog.Infof("market xxxxx: name: %v, i18n: %v", info.Name, info.I18n)
		i18nMap[name] = info.I18n
	}
	for name, info := range infosLocal {
		klog.Infof("local xxxxx: name: %v, i18n: %v", info.Name, info.I18n)

		i18nMap[name] = info.I18n
	}

	klog.Infof("i18nMap: %#v", i18nMap)

	for name, i18n := range i18nMap {
		i18nList = append(i18nList, map[string]map[string]models.I18n{
			name: i18n,
		})
	}
	klog.Infof("i18nList: %#v", i18nList)

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, i18nList))
}

func (h *Handler) handleI18n(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}
	name := req.PathParameter(ParamAppName)
	names := []string{name}
	infosMarket := appmgr.ReadCacheApplicationsWithMap(names, token)
	if infosMarket == nil {
		api.HandleError(resp, errors.New("get apps failed"))
		return
	}
	klog.Infof("infosMarket: %#v", infosMarket)

	infosLocal, err := redisdb.GetLocalAppInfoMap()
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	i18nMap := make(map[string]map[string]models.I18n)
	for name, info := range infosMarket {
		i18nMap[name] = info.I18n
	}
	for name, info := range infosLocal {
		i18nMap[name] = info.I18n
	}

	resp.WriteEntity(models.NewResponse(api.OK, api.Success, i18nMap))
}
