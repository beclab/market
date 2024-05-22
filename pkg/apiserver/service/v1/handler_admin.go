package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
	helmtime "helm.sh/helm/v3/pkg/time"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/boltdb"
	"market/internal/constants"
	"market/internal/models"
	"market/internal/recommend"
	"market/pkg/api"
	"reflect"
	"sort"
	"strings"
)

func (h *Handler) myterminus(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	apps, err := getRunningAppList(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	apps = filterMarketAndCustomApps(apps)
	appsMap := appservice.ConvertAppsListToMap(apps)

	workflows, err := getRunningWorkflowList(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}
	workflowsMap := recommend.ConvertWorkflowsListToMap(workflows)

	llms, err := getRunningModelList(token)
	llmsMap := appservice.ConvertModelsListToMap(llms)

	var names []string
	for _, app := range apps {
		names = append(names, app.Spec.Name)
	}

	for _, workflow := range workflows {
		names = append(names, workflow.Metadata.Name)
	}

	for _, llm := range llms {
		names = append(names, llm.ID)
	}

	if len(names) == 0 {
		resp.WriteEntity(models.NewResponse(api.OK, api.Success, nil))
		return
	}

	infosLocal, err := boltdb.GetLocalAppInfoMap()
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	infosMarket, err := appmgr.GetAppInfos(names)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	var infoList []*models.ApplicationInfo
	for _, name := range names {
		if info, ok := infosLocal[name]; ok {
			if infoMarket, ok2 := infosMarket[name]; ok2 {
				info.Version = infoMarket.Version
			}

			if info.CfgType == constants.AppType {
				//todo
				info.CurVersion = appsMap[info.Name].Spec.Settings["version"]
				info.InstallTime = helmtime.Time(appsMap[info.Name].CreationTimestamp)
				info.Status = appsMap[info.Name].Status.State
			} else if info.CfgType == constants.RecommendType {
				info.CurVersion = workflowsMap[info.Name].Version
				info.InstallTime = helmtime.Time(workflowsMap[info.Name].CreateTime)
				info.Status = workflowsMap[info.Name].ResourceStatus
			}
			if info.CfgType == constants.AppType || info.CfgType == constants.RecommendType {
				if info.Source == constants.AppFromMarket {
					info.CheckUpdate(false)
				}
			} else if info.CfgType == constants.ModelType {
				info.Status = llmsMap[info.Name].Status
				info.Progress = fmt.Sprintf("%.2f", llmsMap[info.Name].Progress)
			}

			infoList = append(infoList, info)
			continue
		}

		if info, ok := infosMarket[name]; ok {
			if info.CfgType == constants.AppType {
				//todo
				info.CurVersion = appsMap[info.Name].Spec.Settings["version"]
				info.InstallTime = helmtime.Time(appsMap[info.Name].CreationTimestamp)
				info.Status = appsMap[info.Name].Status.State
			} else if info.CfgType == constants.RecommendType {
				info.CurVersion = workflowsMap[info.Name].Version
				info.InstallTime = helmtime.Time(workflowsMap[info.Name].CreateTime)
				info.Status = workflowsMap[info.Name].ResourceStatus
			} else if info.CfgType == constants.ModelType {
				info.Status = llmsMap[info.Name].Status
			}

			if info.CfgType == constants.AppType || info.CfgType == constants.RecommendType {
				info.Status = models.AppRunning
				info.CheckUpdate(false)
			}

			infoList = append(infoList, info)
		}
	}

	sort.Sort(models.ByInstallTime(infoList))
	//sort.Slice(infoList, func(i, j int) bool {
	//	return infoList[i].InstallTime.After(infoList[j].InstallTime)
	//})

	resp.WriteAsJson(models.NewResponse(api.OK, api.Success, infoList))
}

func (h *Handler) workflowRecommendsDetail(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	category := req.QueryParameter("category")

	res, err := appmgr.GetApps("0", "0", category, constants.RecommendType)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	workflowMap, _ := getWorkflowsMap(token)

	var appWithStatusList []*models.ApplicationInfo
	for _, item := range res.Items {
		info := &models.ApplicationInfo{}
		err := json.Unmarshal(item, info)
		if err != nil {
			glog.Warningf("err:%s", err.Error())
			continue
		}

		getWorkflowStatus(info, workflowMap)
		if (info.HasRemoveLabel() || info.HasSuspendLabel()) && info.Status == models.AppUninstalled {
			glog.Warningf("workflow:%s has pass label %v not installed, pass", info.Name, info.AppLabels)
			continue
		}

		appWithStatusList = append(appWithStatusList, info)
	}

	resp.WriteAsJson(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, res.TotalCount)))
}

func (h *Handler) modelList(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	category := req.QueryParameter("category")

	res, err := appmgr.GetApps("0", "0", category, constants.ModelType)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	//todo get running models status
	modelsMap, _ := getModelsMap(token)

	var appWithStatusList []*models.ApplicationInfo
	for _, item := range res.Items {
		info := &models.ApplicationInfo{}
		err := json.Unmarshal(item, info)
		if err != nil {
			glog.Warningf("err:%s", err.Error())
			continue
		}

		getModelStatus(info, modelsMap)
		if (info.HasRemoveLabel() || info.HasSuspendLabel()) && info.Status == models.AppUninstalled {
			glog.Warningf("model:%s has pass label %v not installed, pass", info.Name, info.AppLabels)
			continue
		}

		appWithStatusList = append(appWithStatusList, info)
	}

	resp.WriteAsJson(models.NewResponse(api.OK, api.Success, models.NewListResultWithCount(appWithStatusList, res.TotalCount)))
}

func deleteElement(slice []models.ApplicationInfo, index int) []models.ApplicationInfo {
	return append(slice[:index], slice[index+1:]...)
}

func (h *Handler) pagesDetailRaw(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	detail := appmgr.GetPagesDetail()
	//todo deal with error
	if detail == nil {
		api.HandleError(resp, errors.New("get empty detail"))
		return
	}

	respJsonWithOriginBody(resp, detail.(string))
}

func (h *Handler) pagesDetail(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	category := req.QueryParameter("category")
	detail := appmgr.GetPagesDetail()
	//todo deal with error
	if detail == nil {
		api.HandleError(resp, errors.New("get empty detail"))
		return
	}

	var response appmgr.Result

	err := json.Unmarshal([]byte(detail.(string)), &response)
	if err != nil {
		glog.Warning("err:", err)
		_, err = resp.Write([]byte(detail.(string)))
		if err != nil {
			glog.Warningf("err:%s", err)
		}
		return
	}

	appsMap, err := getAppsMap(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}
	workflowMap, _ := getWorkflowsMap(token)
	responseNew := appmgr.Result{
		Code:    response.Code,
		Message: response.Message,
	}
	for _, categoryData := range response.Data {
		if category != "" && !strings.EqualFold(categoryData.Category, category) {
			continue
		}
		resultDataNew := appmgr.ResultData{
			Category: categoryData.Category,
		}

		for _, data := range categoryData.Data {
			cateDataNew := appmgr.CategoryData{
				Type:        data.Type,
				Id:          data.Id,
				Name:        data.Name,
				Description: data.Description,
				TopicType:   data.TopicType,
			}
			if data.Type == "Topic" {
				for _, content := range data.Content {
					topic := &appmgr.Topic{}
					err := json.Unmarshal(content, topic)
					if err != nil {
						glog.Warning(err)
					}
				Label1:
					for i := range topic.Apps {
						if topic.Apps[i].HasNsfwLabel() && boltdb.GetNsfwState() {
							glog.Warningf("app:%s has nsfw label, pass", topic.Apps[i].Name)
							topic.Apps = deleteElement(topic.Apps, i)
							goto Label1
						}

						getAppAndRecommendStatus(&topic.Apps[i], appsMap, workflowMap)
						if reflect.TypeOf(topic.Apps[i].Categories).Kind() == reflect.String {
							oldCate := topic.Apps[i].Categories.(string)
							topic.Apps[i].Categories = strings.Split(oldCate, ",")
						}

						if (topic.Apps[i].HasRemoveLabel() || topic.Apps[i].HasSuspendLabel()) && topic.Apps[i].Status == string(models.AppUninstalled) {
							glog.Warningf("app:%s has pass label %v not installed, pass", topic.Apps[i].Name, topic.Apps[i].AppLabels)
							topic.Apps = deleteElement(topic.Apps, i)
							goto Label1
						}
					}
					jsonR, _ := json.Marshal(topic)
					cateDataNew.Content = append(cateDataNew.Content, jsonR)

				}
			} else if data.Type == "Recommends" {
				for _, content := range data.Content {
					app := &models.ApplicationInfo{}
					err := json.Unmarshal(content, app)
					if err != nil {
						glog.Warning(err)
					}

					if app.HasNsfwLabel() && boltdb.GetNsfwState() {
						glog.Warningf("app:%s has nsfw label, pass", app.Name)
						continue
					}
					getAppAndRecommendStatus(app, appsMap, workflowMap)
					if reflect.TypeOf(app.Categories).Kind() == reflect.String {
						oldCate := app.Categories.(string)
						app.Categories = strings.Split(oldCate, ",")
					}
					if (app.HasRemoveLabel() || app.HasSuspendLabel()) && app.Status == models.AppUninstalled {
						glog.Warningf("app:%s has pass label %v not installed, pass", app.Name, app.AppLabels)
						continue
					}

					jsonR, _ := json.Marshal(app)
					cateDataNew.Content = append(cateDataNew.Content, jsonR)
				}
			}
			resultDataNew.Data = append(resultDataNew.Data, cateDataNew)
		}

		responseNew.Data = append(responseNew.Data, resultDataNew)
	}

	err = resp.WriteAsJson(responseNew)
	if err != nil {
		glog.Warningf("err:%s", err)
	}
}

func (h *Handler) versionHistory(req *restful.Request, resp *restful.Response) {
	appName := req.PathParameter(ParamAppName)
	if appName == "" {
		api.HandleError(resp, errors.New("param invalid"))
		return
	}

	respStr, err := appmgr.GetVersionHistory(appName)
	if err != nil {
		glog.Warningf("err:%s", err)
		return
	}

	appVersionHistory := &models.ListVersionResponse{}
	err = json.Unmarshal([]byte(respStr), &appVersionHistory)
	if err != nil {
		glog.Warning("err:", err)
		_, err = resp.Write([]byte(respStr))
		if err != nil {
			glog.Warningf("err:%s", err)
		}
		return
	}

	err = resp.WriteAsJson(appVersionHistory)
	if err != nil {
		glog.Warningf("err:%s", err)
		return
	}
}
