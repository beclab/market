package conf

import (
	"k8s.io/klog/v2"
	"os"
	"strconv"
)

const (
	isPublicEnv = "isPublic"
)

type config struct {
	IsPublic bool
}

var Config config

func Init() {
	initFromEnv()
}

func initFromEnv() {
	Config.IsPublic, _ = strconv.ParseBool(os.Getenv(isPublicEnv))
	klog.Infof("Config.IsPublic:%t", Config.IsPublic)
}

func GetIsPublic() bool {
	return Config.IsPublic
}
