package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/boltdb"
	"market/internal/constants"
	"market/internal/helm"
	"market/internal/models"
	"market/internal/validate"
	"market/internal/watchdog"
	"market/pkg/api"
	"market/pkg/utils"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

//var appSourceMap = map[string]bool{
//	"market":  true,
//	"custom":  true,
//	"devbox":  true,
//	"system":  true,
//	"unknown": true,
//}

func getChartsTempPath(filename string) (string, error) {
	chartTmpPath := path.Join(constants.ChartsLocalTempDir, filename)
	os.RemoveAll(chartTmpPath)
	err := utils.CheckParentDir(chartTmpPath)
	if err != nil {
		return "", err
	}

	return chartTmpPath, nil
}

func readInfoFromCfg(chartDirPath string) (info *models.ApplicationInfo, cfgFile string, err error) {
	cfgFile = findAppCfgFile(chartDirPath)
	if cfgFile == "" {
		return nil, "", fmt.Errorf("%s not found", constants.AppCfgFileName)
	}
	info, err = appmgr.ReadAppInfo(cfgFile)
	glog.Infof("chartDirPath:%s, cfgFile:%s, info:%#v", chartDirPath, cfgFile, info)

	return
}

func getVersionByName(name, token string) string {
	resBody, err := appservice.AppCurVersion(name, token)
	glog.Infof("name:%s, version res:%s", name, resBody)
	if err == nil {
		curVersionRes := &models.VersionResponse{}
		err = json.Unmarshal([]byte(resBody), curVersionRes)
		if err == nil {
			return curVersionRes.Data.Version
		}
	}

	return ""
}

func installOrUpgrade(name, version, token string) (bool, error) {
	curVersion := getVersionByName(name, token)

	//get version failed, install
	if curVersion == "" {
		return true, nil
	}

	//version get, upgrade
	if !utils.NeedUpgrade(curVersion, version, true) {
		return false, fmt.Errorf("installed version:%s, chart version:%s, not need upgrade", curVersion, version)
	}

	return false, nil
}

func validChart(chartDir string, info *models.ApplicationInfo) (err error) {
	switch info.CfgType {
	case constants.AppType:
	case constants.RecommendType, constants.AgentType, constants.ModelType: //todo
		glog.Infof("terminusManifest.type:%s do not check chart", info.CfgType)
		return
	default:
		err = fmt.Errorf("terminusManifest.type %s must in %v", info.CfgType, constants.ValidTypes)
		return
	}

	err = validate.CheckChartFolder(chartDir)
	if err != nil {
		return
	}
	err = validate.CheckAppCfg(chartDir)
	if err != nil {
		return
	}
	err = validate.CheckServiceAccountRole(chartDir)
	return
}

func (h *Handler) devUpload(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	//get file info
	file, header, err := req.Request.FormFile(ParamAppName)
	if err != nil || header == nil {
		api.HandleError(resp, fmt.Errorf("get name failed %w", err))
		return
	}

	source := req.QueryParameter("source")

	//get file bytes
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	//write bytes to tmp file
	chartTmpPath, err := getChartsTempPath(header.Filename)
	if err != nil {
		api.HandleError(resp, err)
		return
	}
	err = os.WriteFile(chartTmpPath, fileBytes, 0755)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	//unzip temp file
	chartDirTmpPath := path.Join(constants.ChartsLocalTempDir, fmt.Sprintf("%s_%d", header.Filename, time.Now().UnixNano()))
	err = utils.UnArchive(chartTmpPath, chartDirTmpPath)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	//read info from cfg
	info, cfgFile, err := readInfoFromCfg(chartDirTmpPath)
	if err != nil {
		respErr(resp, err)
		return
	}

	//check chart
	realChartPath := findChartPath(chartDirTmpPath)
	err = validChart(realChartPath, info)
	if err != nil {
		respErr(resp, err)
		return
	}

	//for test, later delete
	defer os.RemoveAll(chartTmpPath)
	defer os.RemoveAll(chartDirTmpPath)

	//check app info
	err = checkAppInfo(info)
	if err != nil {
		respErr(resp, err)
		return
	}

	//remove old package if exists
	if exist := boltdb.ExistLocalAppInfo(info.Name); exist {
		_ = deleteLocalCharts(info.Name)
	}

	//helm package
	chartTmpDir := filepath.Dir(cfgFile)
	info.ChartName, err = helm.PackageHelm(chartTmpDir, constants.ChartsLocalDir)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	//helm index
	err = helm.IndexHelm(constants.ChartsLocalDir)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	//insert info to local db
	if source != "" {
		info.Source = source
	} else {
		info.Source = constants.AppFromDev
	}

	err = boltdb.UpsertLocalAppInfo(info)
	if err != nil {
		glog.Warningf("UpsertLocalAppInfo err:%s", err.Error())
		api.HandleError(resp, err)
		return
	}

	install, err := installOrUpgrade(info.Name, info.Version, token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	if install {
		err = checkDep(info, token)
		if err != nil {
			respDepErr(resp, err.Error())
			return
		}

		resBody, err := installByType(info, token)
		if err != nil {
			api.HandleError(resp, err)
			glog.Warningf("install %s resp:%s, err:%s", info.Name, resBody, err.Error())
			return
		}

		uid, _, err := appservice.GetUidFromStatus(resBody)
		if err != nil {
			api.HandleError(resp, err)
			return
		}
		if info.CfgType == constants.AppType {
			go h.watchDogManager.NewWatchDog(watchdog.OP_INSTALL,
				info.Name, uid, token, watchdog.AppFromDev, info).
				Exec()
		} else if info.CfgType == constants.ModelType || info.CfgType == constants.RecommendType {
			go h.commonWatchDogManager.NewWatchDog(watchdog.OP_INSTALL,
				info.Name, uid, token, watchdog.AppFromDev, info.CfgType,
				info).
				Exec()
		}

		dealWithInstallResp(resp, resBody)
		return
	}

	//dev upgrade mode not need version
	resBody, err := upgradeByType(info, token)
	if err != nil {
		glog.Warningf("install %s resp:%s, err:%s", info.Name, resBody, err.Error())
		return
	}
	//todo
	uid, _, err := appservice.GetUidFromStatus(resBody)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	glog.Infof("uid:%s", uid)

	if info.CfgType == constants.AppType {
		go h.upgradeWatchDogManager.NewUpgradeWatchDog(watchdog.OP_UPGRADE,
			info.Name, uid, token, watchdog.AppFromDev,
			info).
			Exec()
	}

	dealWithInstallResp(resp, resBody)
}

func findAppCfgFile(chartDirPath string) string {
	charts, err := os.ReadDir(chartDirPath)
	if err != nil {
		glog.Warningf("read dir %s error: %s", chartDirPath, err.Error())
		return ""
	}

	for _, c := range charts {
		if !c.IsDir() || strings.HasPrefix(c.Name(), ".") {
			continue
		}
		appCfgFullName := path.Join(chartDirPath, c.Name(), constants.AppCfgFileName)
		if utils.PathExists(appCfgFullName) {
			return appCfgFullName
		}
	}

	return ""
}

func findChartPath(chartDirPath string) string {
	charts, err := os.ReadDir(chartDirPath)
	if err != nil {
		glog.Warningf("read dir %s error: %s", chartDirPath, err.Error())
		return ""
	}

	for _, c := range charts {
		if !c.IsDir() || strings.HasPrefix(c.Name(), ".") {
			continue
		}
		appCfgFullName := path.Join(chartDirPath, c.Name(), constants.AppCfgFileName)
		if utils.PathExists(appCfgFullName) {
			return path.Join(chartDirPath, c.Name())
		}
	}

	return ""
}

func deleteLocalCharts(name string) error {
	info, err := boltdb.GetLocalAppInfo(name)
	if err != nil {
		glog.Warningf("GetLocalAppInfo err:%s", err.Error())
		return err
	}

	chartPath := path.Join(constants.ChartsLocalDir, info.ChartName)
	if !utils.PathExists(chartPath) {
		glog.Warningf("chart %s not found", chartPath)
		//return errors.New("local chart not found")
	}

	err = os.Remove(chartPath)
	if err != nil {
		glog.Warningf("os.Remove %s err:%s", chartPath, err.Error())
		//return err
	}

	err = boltdb.DelLocalAppInfo(name)
	if err != nil {
		glog.Warningf("boltdb.DelLocalAppInfo %s err:%s", name, err.Error())
		//return err
	}

	return nil
}
