package recommend

import (
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

type Response struct {
	Code int32 `json:"code"`
}

//type StatusResp struct {
//	Response
//	Data *StatusData `json:"data"`
//}

type StatusListResp struct {
	Response
	Data []*StatusData `json:"data"`
}
