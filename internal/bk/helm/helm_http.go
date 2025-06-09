package helm

import (
	"github.com/golang/glog"
	"market/internal/constants"
	"net/http"
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
