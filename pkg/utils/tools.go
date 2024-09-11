// Copyright 2022 bytetrade
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"market/internal/constants"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/mholt/archiver/v3"
)

func ToJSON(v any) string {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		panic(err)
	}
	return buf.String()
}

func PrettyJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return buf.String()
}

func RandomIntWithRange(small, large int) int {
	if small > large {
		small, large = large, small
	}

	rand.Seed(time.Now().UnixNano())
	r := rand.Intn(large - small)

	return small + r
}

func ExistDir(dirname string) bool {
	fi, err := os.Stat(dirname)
	return (err == nil || os.IsExist(err)) && fi.IsDir()
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}

	if os.IsNotExist(err) {
		return false
	}
	return false
}

func CheckDir(dirname string) error {
	fi, err := os.Stat(dirname)
	if (err == nil || os.IsExist(err)) && fi.IsDir() {
		return nil
	}
	if os.IsExist(err) {
		return err
	}

	err = os.MkdirAll(dirname, 0755)
	return err
}

func CheckParentDir(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	subDir := filepath.Dir(absPath)

	exist := PathExists(subDir)
	if exist {
		return nil
	}

	err = os.MkdirAll(subDir, 0755)
	return err
}

func GetRepoUrl() string {
	repoServiceHost := os.Getenv(constants.RepoUrlHostEnv)
	repoStoreServicePort := os.Getenv(constants.RepoUrlPortEnv)
	return fmt.Sprintf(constants.RepoUrlTempl, repoServiceHost, repoStoreServicePort)
}

func UnArchive(src, dstDir string) error {
	err := CheckDir(dstDir)
	if err != nil {
		glog.Warningf("err:%v\n", err)
		return err
	}

	err = archiver.Unarchive(src, dstDir)
	if err != nil {
		glog.Warningf("err:%v\n", err)
		return err
	}

	return nil
}

func VerifyPageAndSize(page, size string) (int, int) {
	pageN, err := strconv.Atoi(page)
	if pageN < 1 || err != nil {
		pageN = constants.DefaultPage
	}

	sizeN, err := strconv.Atoi(size)
	if sizeN < 1 || err != nil {
		sizeN = constants.DefaultPageSize
	}

	return pageN, sizeN
}

func VerifyFromAndSize(page, size string) (int, int) {
	pageN, err := strconv.Atoi(page)
	if pageN < 1 || err != nil {
		pageN = constants.DefaultPage
	}

	sizeN, err := strconv.Atoi(size)
	if sizeN < 1 || err != nil {
		sizeN = constants.DefaultPageSize
	}

	from := (pageN - 1) * sizeN
	if from < 0 {
		from = constants.DefaultFrom
	}

	return from, sizeN
}

func Md5String(s string) string {
	hash := md5.Sum([]byte(s))
	hashString := hex.EncodeToString(hash[:])
	return hashString
}

func FindChartPath(chartDirPath string) string {
	charts, err := os.ReadDir(chartDirPath)
	if err != nil {
		glog.Warningf("read dir %s error: %s", chartDirPath, err.Error())
		return ""
	}

	for _, c := range charts {
		if !c.IsDir() || strings.HasPrefix(c.Name(), ".") {
			continue
		}
		appCfgFullName := path.Join(chartDirPath, c.Name(), constants.AppCfgFileName)
		if PathExists(appCfgFullName) {
			return path.Join(chartDirPath, c.Name())
		}
	}

	return ""
}
