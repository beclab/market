package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/boltdb"
	"market/internal/constants"
	"market/internal/models"
	"market/internal/watchdog"
	"market/pkg/api"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

func checkDep(app *models.ApplicationInfo, token string) error {
	if app == nil {
		return nil
	}

	if len(app.Options.Dependencies) == 0 {
		return nil
	}

	depsList := &models.DepRequest{
		Data: app.Options.Dependencies,
	}

	depsResp, err := appservice.CheckDeps(token, depsList)
	if err != nil {
		return err
	}

	if len(depsResp.Data) <= 0 {
		return nil
	}

	notInstalled, err := json.Marshal(depsResp.Data)
	if err != nil {
		return err
	}

	return fmt.Errorf("dependency need, %s", string(notInstalled))
}

func respDepErr(resp *restful.Response, msg string) {
	//todo
	res := &models.InstallErrResponse{
		Code:     400,
		Msg:      msg,
		Resource: "dependencies",
	}
	err := resp.WriteEntity(res)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
	}
}

func installPre(appName, token string) (*models.ApplicationInfo, error) {
	info, err := appmgr.GetAppInfo(appName)
	if err != nil {
		return info, err
	}

	if info.ChartName == "" {
		return info, errors.New("get chart name failed")
	}

	err = checkDep(info, token)
	if err != nil {
		return info, err
	}

	err = appmgr.DownloadAppTgz(info.ChartName)
	if err != nil {
		return info, err
	}

	info.Source = constants.AppFromMarket
	err = boltdb.UpsertLocalAppInfo(info)
	if err != nil {
		return info, err
	}

	return info, nil
}

func (h *Handler) install(req *restful.Request, resp *restful.Response) {
	appName := req.PathParameter(ParamAppName)
	if appName == "" {
		api.HandleError(resp, errors.New("name is nil"))
		return
	}

	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	info, err := installPre(appName, token)
	if err != nil {
		respErr(resp, err)
		return
	}

	resBody, err := installByType(info, token)
	if err != nil {
		glog.Warningf("install %s resp:%s, err:%s", appName, resBody, err.Error())
		api.HandleError(resp, err)
		return
	}

	uid, _, err := appservice.GetUidFromStatus(resBody)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	glog.Infof("uid:%s", uid)
	if info.CfgType == constants.AppType {
		go h.watchDogManager.NewWatchDog(watchdog.OP_INSTALL,
			appName, uid, token, watchdog.AppFromStore,
			info).
			Exec()
	} else if info.CfgType == constants.ModelType || info.CfgType == constants.RecommendType {
		go h.commonWatchDogManager.NewWatchDog(watchdog.OP_INSTALL,
			appName, uid, token, watchdog.AppFromStore, info.CfgType,
			info).
			Exec()
	}

	dealWithInstallResp(resp, resBody)
}

func (h *Handler) uninstall(req *restful.Request, resp *restful.Response) {
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

	resBody, err := uninstallByType(appName, ty, token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	uid, _, err := appservice.GetUidFromStatus(resBody)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	glog.Infof("uid:%s, resBody:%s", uid, resBody)
	if ty == constants.AppType {
		go h.watchDogManager.NewWatchDog(watchdog.OP_UNINSTALL,
			appName, uid, token, "", nil).
			Exec()
	} else if ty == constants.ModelType || ty == constants.RecommendType {
		go h.commonWatchDogManager.NewWatchDog(watchdog.OP_UNINSTALL,
			appName, uid, token, "", ty, nil).
			Exec()
	}

	dealWithInstallResp(resp, resBody)
}

func (h *Handler) cancel(req *restful.Request, resp *restful.Response) {
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

	resBody, err := cancelByType(appName, token, constants.CancelTypeOperate, ty)
	if err != nil {
		glog.Warningf("cancel %s type:%s resp:%s, err:%s", appName, ty, resBody, err.Error())
		api.HandleError(resp, err)
		return
	}

	respJsonWithOriginBody(resp, resBody)
}

func cancelByType(name, token, cancelTy, ty string) (string, error) {
	switch ty {
	case constants.AppType:
		return appservice.AppCancel(name, cancelTy, token)
	case constants.ModelType:
		resBody, _, err := appservice.LlmCancel(name, cancelTy, token)
		return resBody, err
	//case constants.AgentType:
	//	//todo
	default:
		return "", fmt.Errorf("%s type %s invalid", name, ty)
	}
}

func (h *Handler) checkDependencies(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	depsList := &models.DepRequest{}
	err := req.ReadEntity(depsList)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	respData, err := appservice.CheckDeps(token, depsList)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	err = resp.WriteEntity(respData)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
	}
}

func dealWithInstallResp(resp *restful.Response, data string) {
	resInfo := &models.InstallationStatusResp{}
	err := json.Unmarshal([]byte(data), resInfo)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	err = resp.WriteEntity(resInfo)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
	}
}
