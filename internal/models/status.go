package models

import (
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Metadata struct {
	Name string `json:"name"`
}

type StatusData struct {
	UUID           string      `json:"uuid"`
	Namespace      string      `json:"namespace"`
	User           string      `json:"user"`
	ResourceStatus string      `json:"resourceStatus"`
	ResourceType   string      `json:"resourceType"`
	CreateTime     metav1.Time `json:"createTime"`
	UpdateTime     metav1.Time `json:"updateTime"`
	Metadata       Metadata    `json:"metadata"`
	Version        string      `json:"version"`
}

type StatusResponse struct {
	Code int32 `json:"code"`
}

//type StatusResp struct {
//	Response
//	Data *StatusData `json:"data"`
//}

type StatusListResponse struct {
	StatusResponse
	Data []*StatusData `json:"data"`
}

func ParseStatusList(str string) (apps []*StatusData, err error) {
	var resp StatusListResponse
	err = json.Unmarshal([]byte(str), &resp)
	if err != nil {
		glog.Warningf("json.Unmarshal %s, err:%s", str, err.Error())
		return
	}

	for _, app := range resp.Data {
		if app.ResourceStatus == "notfound" {
			continue
		}
		apps = append(apps, app)
	}

	return
}

func ParseStatusListToMap(str string) (statusMap map[string]*StatusData, err error) {
	var status []*StatusData
	status, err = ParseStatusList(str)
	if err != nil {
		return
	}

	fmt.Printf("status:%v\n", status)

	statusMap = make(map[string]*StatusData)
	for _, w := range status {
		statusMap[w.Metadata.Name] = w
	}

	fmt.Printf("statusMap:%v\n", statusMap)
	return
}

func ConvertStatusListToMap(statuss []*StatusData) (statusMap map[string]*StatusData) {
	statusMap = make(map[string]*StatusData)
	for _, w := range statuss {
		statusMap[w.Metadata.Name] = w
	}

	return
}
