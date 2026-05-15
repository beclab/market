package task

import (
	"encoding/json"
)

type AppOperateInterface interface {
	String() string
}

type AppOperationMessage struct { // +
	Operation string `json:"operation"`
	AppName   string `json:"app_name"`
	User      string `json:"user"`
	CfgType   string `json:"cfgType"`

	AppSource string `json:"app_source"`
	ApiSource string `json:"api_source"`

	// install
	ExistingTaskId string `json:"existing_task_id"`

	// upgrade
	Source  string `json:"source"`
	Version string `json:"version"`

	// uninstall
	All        bool `json:"all"`
	DeleteData bool `json:"deleteData"`

	// clone
	RawAppName  string `json:"rawAppName"`
	RequestHash string `json:"requestHash"`
	Title       string `json:"title"`

	// response
	Response           any    `json:"backend_response"` // ~ 这个放的是 app-service 返回的数据
	ResponseCode       int    `json:"-"`
	ResponseData       string `json:"-"`
	ResponseHttpStatus int    `json:"-"`

	// success
	OpId string `json:"opID"`

	// error
	Error string `json:"error"`

	Status string `json:"status"`
}

// base
func BaseParamInvalid() *AppOperationMessage {
	return &AppOperationMessage{
		Operation: "check_params",
		Status:    "failed",
	}
}

func (i *AppOperationMessage) String() string {
	res, _ := json.Marshal(i)
	return string(res)
}

func (i *AppOperationMessage) SetFailed(errMsg string) {
	i.Status = "failed"
	i.Error = errMsg
}

func (i *AppOperationMessage) SetResponse(resp any, respCode int, respData string, respHttpStatus int) {
	i.Response = resp
	i.ResponseCode = respCode
	i.ResponseData = respData
	i.ResponseHttpStatus = respHttpStatus
}

func (i *AppOperationMessage) SetOpId(opId string) {
	i.OpId = opId
}

func (i *AppOperationMessage) SetSuccess() {
	i.Status = "success"
}

// install
func BaseInstallEnvelope(task *Task, cfgType, appSource, apiSrouce string) *AppOperationMessage {
	return &AppOperationMessage{
		Operation: "install",
		AppName:   task.AppName,
		User:      task.User,
		CfgType:   cfgType,
		AppSource: appSource,
		ApiSource: apiSrouce,
	}
}

func (i *AppOperationMessage) SetExistsEnvelope(errMsg string, status string, runningTaskId string) {
	i.Error = errMsg
	i.Status = status
	i.ExistingTaskId = runningTaskId
}

// cancel
func BaseCancelEnvelope(task *Task, cfgType string) *AppOperationMessage {
	return &AppOperationMessage{
		Operation: "cancel",
		AppName:   task.AppName,
		User:      task.User,
		CfgType:   cfgType,
	}
}

// uninstall
func BaseUninstallEnvelope(task *Task, cfgType string, all bool, deleteData bool) *AppOperationMessage {
	return &AppOperationMessage{
		Operation:  "uninstall",
		AppName:    task.AppName,
		User:       task.User,
		CfgType:    cfgType,
		All:        all,
		DeleteData: deleteData,
	}
}

// upgrade
func BaseUpgradeEnvelope(task *Task, source string, apiSource string, version string) *AppOperationMessage {
	return &AppOperationMessage{
		Operation: "upgrade",
		AppName:   task.AppName,
		User:      task.User,
		Source:    source,
		ApiSource: apiSource,
		Version:   version,
	}
}

// clone
func BaseCloneExistsEnvelope(task *Task, runningId string) *AppOperationMessage {
	return &AppOperationMessage{
		Operation:      "clone",
		AppName:        task.AppName,
		User:           task.User,
		Error:          "Another clone task is already running for this app",
		Status:         "failed",
		ExistingTaskId: runningId,
	}
}

func BaseCloneEnvelope(task *Task, appName, cfgType, appSource, apiSource string, rawAppName, requestHash, title string) *AppOperationMessage {
	return &AppOperationMessage{
		Operation:   "clone",
		AppName:     appName,
		User:        task.User,
		CfgType:     cfgType,
		AppSource:   appSource,
		ApiSource:   apiSource,
		RawAppName:  rawAppName,
		RequestHash: requestHash,
		Title:       title,
	}
}
