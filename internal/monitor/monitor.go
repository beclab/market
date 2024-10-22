package monitor

import (
	"market/internal/bfl"
	"time"
)

func Start() {

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				bfl.GetMyAppsInternal()
			}
		}
	}()
}
