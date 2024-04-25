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

package v1

import (
	"errors"
	"market/internal/models"
)

func checkAppInfo(info *models.ApplicationInfo) error {
	if info == nil {
		return errors.New("info is empty")
	}
	if info.Name == "" {
		return errors.New("name is empty")
	}

	//check name exist
	//err := checkAppExistByName(info.Name)
	//if err != nil {
	//	return err
	//}

	return nil
}

//func checkAppExistByName(appName string) error {
//	exist, err := appmgr.ExistApp(appName)
//	if err != nil {
//		return err
//	}
//	if exist {
//		return errors.New("app already exist in app store")
//	}
//
//	return nil
//}
