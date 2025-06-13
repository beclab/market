package helm

import (
	"market/internal/constants"
	"net/http"

	"github.com/golang/glog"
)

func init() {
	go startHelmHttp()
}

func startHelmHttp() {
	file := http.FileServer(http.Dir(constants.ChartsLocalDir))
	http.Handle("/charts/", http.StripPrefix("/charts/", file))
	err := http.ListenAndServe(constants.HelmServerListenAddress, nil)
	if err != nil {
		glog.Fatalf("start helm http err:%s", err)
	}
}
