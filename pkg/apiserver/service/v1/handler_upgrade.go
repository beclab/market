package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/constants"
	"market/internal/models"
	"market/internal/redisdb"
	"market/internal/watchdog"
	"market/pkg/api"
	"market/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

func getVersionAndInfo(appName, token string) (curVersion string, latestInfo *models.ApplicationInfo, err error) {
	var resBody string
	resBody, err = appservice.AppCurVersion(appName, token)
	if err != nil {
		return
	}

	curVersionRes := &models.VersionResponse{}
	err = json.Unmarshal([]byte(resBody), curVersionRes)
	if err != nil {
		return
	}
	curVersion = curVersionRes.Data.Version

	latestInfo = appmgr.ReadCacheApplication(appName)
	if latestInfo == nil {
		return
	}

	return
}

func (h *Handler) upgrade(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appName := req.PathParameter(ParamAppName)

	//get and compare version
	curVersion, latestInfo, err := getVersionAndInfo(appName, token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	needUpdate := utils.NeedUpgrade(curVersion, latestInfo.Version, false)
	glog.Infof("appName:%s, curVersion:%s, latestVersion:%s, need_update:%v", appName, curVersion, latestInfo.Version, needUpdate)
	if !needUpdate {
		api.HandleError(resp, fmt.Errorf("curVersion:%s, latestVersion:%s do not need update", curVersion, latestInfo.Version))
		return
	}
	//get newest chart.tgz and update helm repo
	if latestInfo.ChartName == "" {
		api.HandleError(resp, errors.New("get chart name failed"))
		return
	}

	err = appmgr.DownloadAppTgz(latestInfo.ChartName)
	if err != nil {
		api.HandleError(resp, err)
		return
	}
	i18n := appmgr.ReadCacheI18n(latestInfo.ChartName, latestInfo.Locale)
	if len(i18n) > 0 {
		latestInfo.I18n = i18n
	}

	latestInfo.Source = constants.AppFromMarket
	err = redisdb.UpsertLocalAppInfo(latestInfo)
	if err != nil {
		glog.Warningf("UpsertLocalAppInfo err:%s", err.Error())
		api.HandleError(resp, err)
		return
	}

	resBody, err := upgradeByType(latestInfo, token)
	if err != nil {
		return
	}

	uid, _, err := appservice.GetUidFromStatus(resBody)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	glog.Infof("uid:%s", uid)

	if latestInfo.CfgType == constants.AppType {
		go h.upgradeWatchDogManager.NewUpgradeWatchDog(watchdog.OP_UPGRADE,
			appName, uid, token, watchdog.AppFromStore,
			latestInfo).
			Exec()
	} else if latestInfo.CfgType == constants.RecommendType {
		go h.commonWatchDogManager.NewWatchDog(watchdog.OP_UPGRADE,
			appName, uid, token, watchdog.AppFromStore, latestInfo.CfgType,
			latestInfo).
			Exec()
	}

	dealWithInstallResp(resp, resBody)
}
