package appmgr

import (
	"io"
	"market/internal/models"
	"os"

	"github.com/golang/glog"
	"gopkg.in/yaml.v3"
)

func ReadAppInfo(cfgFileName string) (*models.ApplicationInfo, error) {
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

	var appCfg models.AppConfiguration
	if err = yaml.Unmarshal(info, &appCfg); err != nil {
		glog.Warningf("cfg:%s %s", cfgFileName, err.Error())
		return nil, err
	}

	return appCfg.ToAppInfo(), nil
}
