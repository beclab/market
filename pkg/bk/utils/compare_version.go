package utils

import (
	"github.com/Masterminds/semver/v3"
	"github.com/golang/glog"
)

func NeedUpgrade(curVersion, latestVersion string, sameVersionUpdate bool) bool {
	vCur, err := semver.NewVersion(curVersion)
	if err != nil {
		glog.Warningf("invalid curVersion:%s %s", curVersion, err.Error())
		return false
	}

	vLatest, err := semver.NewVersion(latestVersion)
	if err != nil {
		glog.Warningf("invalid latestVersion:%s %s", latestVersion, err.Error())
		return false
	}

	//compare version
	if sameVersionUpdate {
		if vCur.LessThan(vLatest) || vCur.Equal(vLatest) {
			return true
		}
		return false
	}
	if vCur.LessThan(vLatest) {
		return true
	}
	return false
}
