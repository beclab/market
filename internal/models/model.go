package models

type ExistRes struct {
	Exist bool `json:"exist"`
}

type SearchAppReq struct {
	Name string `json:"name"`
}

type ClusterMetricsResp struct {
	CM    ClusterMetrics `json:"metrics"`
	Nodes []string       `json:"nodes"`
}

type ClusterMetrics struct {
	CPU    Value `json:"cpu"`
	Memory Value `json:"memory"`
	Disk   Value `json:"disk"`
	GPU    Value `json:"gpu"`
}

type Value struct {
	Total float64 `json:"total"`
	Usage float64 `json:"usage"`
	Ratio float64 `json:"ratio"`
	Unit  string  `json:"unit"`
}
