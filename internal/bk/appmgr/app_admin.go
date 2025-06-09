package appmgr

import (
	"fmt"
	"market/internal/appservice"
	"market/internal/constants"
	"net/http"
	"sync"
	"time"

	"github.com/golang/glog"
)

const (
	pageKey = "Page"
)

var (
	cache sync.Map
)

func init() {
	go getPagesDetailLoop()
}

func getPagesDetailLoop() {
	callLoop(getPagesDetailFromAdmin)
}

func callLoop(f func() error) {
	_ = f()
	ticker := time.NewTicker(time.Minute * 30)
	defer ticker.Stop()

	for range ticker.C {
		err := f()
		if err != nil {
			glog.Warningf("err:%s", err.Error())
		}
	}
}

func getPagesDetailFromAdmin() error {
	version, err := appservice.TerminusVersionValue()
	if err != nil {
		return err
	}

	url := fmt.Sprintf(constants.AppStoreServicePagesDetailURLTempl, getAppStoreServiceHost(), getAppStoreServicePort(), version)
	bodyStr, err := sendHttpRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	cache.Store(pageKey, bodyStr)
	return nil
}

func GetPagesDetail() interface{} {
	value, _ := cache.Load(pageKey)
	if value == nil {
		_ = getPagesDetailFromAdmin()
		value, _ = cache.Load(pageKey)
	}

	return value
}
