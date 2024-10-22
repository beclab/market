package monitor

import (
	"market/internal/bfl"
	"time"

	"github.com/golang/glog"
)

func Start() {

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	glog.Info("monitor tick 1")
	go func() {
		for {
			glog.Info("monitor tick 2")
			<-ticker.C
			bfl.GetMyAppsInternal()
		}
	}()

	// go func() {
	// 	for {
	// 		select {
	// 		case <-ticker.C:
	// 			bfl.GetMyAppsInternal()
	// 		}
	// 	}
	// }()
}
