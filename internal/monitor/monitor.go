package monitor

import (
	"market/internal/bfl"
	"time"

	"github.com/golang/glog"
)

func Start() {

	interval := 5 * time.Second
	ticker := time.NewTicker(interval)

	defer ticker.Stop()

	glog.Info("monitor tick 1")
	for range ticker.C {
		glog.Info("monitor tick 2")
		bfl.GetMyAppsInternal()
	}
}
