package appmgr

import (
	"io"
	"io/ioutil"
	"market/internal/appservice"
	"market/internal/constants"
	"market/internal/models"
	"os"
	"path"

	"github.com/golang/glog"
	"gopkg.in/yaml.v3"
)

func ReadAppInfo(cfgFileName string, token string) (*models.ApplicationInfo, error) {
	f, err := os.Open(cfgFileName)
	if err != nil {
		glog.Warningf("%s", err.Error())
		return nil, err
	}

	info, err := io.ReadAll(f)
	if err != nil {
		glog.Warningf("%s", err.Error())
		return nil, err
	}
	
	renderedContent, err := appservice.RenderManifest(string(info), token)
	if err != nil {
		glog.Warningf("render manifest failed: %s", err.Error())
		return nil, err
	}

	var appCfg models.AppConfiguration
	if err = yaml.Unmarshal([]byte(renderedContent), &appCfg); err != nil {
		glog.Warningf("cfg:%s %s", cfgFileName, err.Error())
		return nil, err
	}
	
	appInfo := appCfg.ToAppInfo()
	dir := path.Dir(cfgFileName)
	i18nMap := make(map[string]models.I18n)
	for _, lang := range appInfo.Locale {
		data, err := ioutil.ReadFile(path.Join(dir, "i18n", lang, constants.AppCfgFileName))
		if err != nil {
			glog.Warningf("failed to get file %s,err=%v", path.Join("i18n", lang, constants.AppCfgFileName), err)
			continue
		}
		var i18n models.I18n
		i18n.Entrances = make([]models.I18nEntrance, 0)
		err = yaml.Unmarshal(data, &i18n)
		if err != nil {
			glog.Warningf("unmarshal to I18n failed err=%v", err)
			continue
		}

		i18nMap[lang] = i18n
	}
	appInfo.I18n = i18nMap

	return appInfo, nil
}
