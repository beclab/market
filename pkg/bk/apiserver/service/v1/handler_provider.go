package v1

import (
	"errors"
	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
	"market/internal/appservice"
	"market/internal/constants"
	"market/internal/models"
	"market/internal/watchdog"
	"market/pkg/api"
)

func (h *Handler) providerInstallDev(req *restful.Request, resp *restful.Response) {
	token := req.Request.Header.Get(constants.AuthorizationTokenKey)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	var opt models.InstallProviderRequest
	if err := req.ReadEntity(&opt); err != nil {
		api.HandleError(resp, err)
		return
	}

	appName := opt.Data.App
	appVersion := opt.Data.Version
	curVersion := getVersionByName(appName, token)
	//install, err := installOrUpgrade(appName, appVersion, token)
	//if err != nil {
	//	api.HandleError(resp, err)
	//	return
	//}

	glog.Infof("appVersion:%s, curVersion:%s", appVersion, curVersion)
	if curVersion == "" {
		// install
		h._installApp(resp, token, opt.Data)
	} else {
		// upgrade
		h._upgradeAppByInstallOpt(resp, token, opt.Data)
	}
}

func (h *Handler) installDev(req *restful.Request, resp *restful.Response) {
	token := req.Request.Header.Get(constants.AuthorizationTokenKey)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	var opt models.InstallOptions
	if err := req.ReadEntity(&opt); err != nil {
		api.HandleError(resp, err)
		return
	}

	appName := opt.App
	appVersion := opt.Version
	install, err := installOrUpgrade(appName, appVersion, token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	if install {
		// install
		h._installApp(resp, token, &opt)
	} else {
		// upgrade
		h._upgradeAppByInstallOpt(resp, token, &opt)
	}
}

func (h *Handler) _installApp(resp *restful.Response, token string, opt *models.InstallOptions) {
	body, data, err := appservice.AppInstallNew(token, opt)
	if err != nil {
		respJsonWithOriginBody(resp, body)
		return
	}

	uidData := data["data"].(map[string]interface{})
	uid := uidData["uid"].(string)

	//need parse info from chart?
	go h.watchDogManager.NewWatchDog(watchdog.OP_INSTALL,
		opt.App, uid, token, opt.Source, constants.AppType, nil).
		Exec()

	err = resp.WriteEntity(data)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
	}
}

func (h *Handler) _upgradeAppByInstallOpt(resp *restful.Response, token string, opt *models.InstallOptions) {
	body, data, err := appservice.AppUpgradeNew(opt.App, token, &models.UpgradeOptions{
		CfgURL:  opt.CfgUrl,
		RepoURL: opt.RepoUrl,
		Version: opt.Version,
		Source:  opt.Source,
	})
	if err != nil {
		respJsonWithOriginBody(resp, body)
		return
	}
	uidData := data["data"].(map[string]interface{})
	uid := uidData["uid"].(string)

	go h.upgradeWatchDogManager.NewUpgradeWatchDog(watchdog.OP_UPGRADE,
		opt.App, uid, token, watchdog.AppFromDev,
		nil).
		Exec()

	err = resp.WriteEntity(data)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
	}
}

func (h *Handler) providerUninstallDev(req *restful.Request, resp *restful.Response) {
	token := req.Request.Header.Get(constants.AuthorizationTokenKey)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	var opt models.UninstallProviderRequest
	if err := req.ReadEntity(&opt); err != nil {
		api.HandleError(resp, err)
		return
	}

	h._uninstallApp(resp, opt.Data.Name, token)
}

func (h *Handler) uninstallDev(req *restful.Request, resp *restful.Response) {
	token := req.Request.Header.Get(constants.AuthorizationTokenKey)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	var opt models.UninstallData
	if err := req.ReadEntity(&opt); err != nil {
		api.HandleError(resp, err)
		return
	}

	h._uninstallApp(resp, opt.Name, token)
}

func (h *Handler) _uninstallApp(resp *restful.Response, name, token string) {
	body, data, err := appservice.AppUninstallNew(name, token)
	if err != nil {
		respJsonWithOriginBody(resp, body)
		return
	}

	uidData := data["data"].(map[string]interface{})
	uid := uidData["uid"].(string)

	go h.watchDogManager.NewWatchDog(watchdog.OP_UNINSTALL,
		name, uid, token, "", constants.AppType, nil).
		Exec()

	err = resp.WriteEntity(data)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
	}
}
