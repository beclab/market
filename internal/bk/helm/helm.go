package helm

import (
	"github.com/golang/glog"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/repo"
	"path"
	"path/filepath"
)

func IndexHelm(dir string) error {
	absPath, err := filepath.Abs(dir)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
		return err
	}

	out := filepath.Join(absPath, "index.yaml")

	i, err := repo.IndexDirectory(absPath, "")
	if err != nil {
		glog.Warningf("err:%s", err.Error())
		return err
	}

	i.SortEntries()
	err = i.WriteFile(out, 0644)
	if err != nil {
		glog.Warningf("err:%s", err.Error())
		return err
	}

	return nil
}

func PackageHelm(src, dstDir string) (string, error) {
	client := action.NewPackage()
	client.Destination = dstDir
	pathAbs, err := filepath.Abs(src)
	if err != nil {
		return "", err
	}

	var p string
	p, err = client.Run(pathAbs, nil)
	if err != nil {
		return "", err
	}
	glog.Infof("src:%s, dstDir:%s Successfully packaged chart and saved it to: %s\n", src, dstDir, p)

	return path.Base(p), nil
}
