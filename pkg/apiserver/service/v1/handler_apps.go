package v1

import (
	"errors"
	"fmt"
	"github.com/golang/glog"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/boltdb"
	"market/internal/constants"
	"market/internal/models"
	"market/pkg/api"

	"github.com/emicklei/go-restful/v3"
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

	infosMarket, err := appmgr.GetAppInfos(names)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	infosLocal, err := boltdb.GetLocalAppInfoMap()
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

	//localInfos := boltdb.GetDevAppInfos(namesNotFoundInMarket)
	//infoList = append(infoList, localInfos...)

	appsMap, err := getAppsMap(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}
	workflowMap, _ := getWorkflowsMap(token)

	for i := range infoList {
		if _, exist := infosLocal[infoList[i].Name]; exist {
			infoList[i].Source = infosLocal[infoList[i].Name].Source
		} else {
			infoList[i].Source = constants.AppFromMarket
		}

		if _, exist := infosMarket[infoList[i].Name]; exist {
			infoList[i].Version = infosMarket[infoList[i].Name].Version
		}

		getAppAndRecommendStatus(infoList[i], appsMap, workflowMap)
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
