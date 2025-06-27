package watchdog

import (
	"sync"

	"github.com/golang/glog"
)

type StatusManager struct {
}

type InstallOrUpgradeResp struct {
	Code int         `json:"code"`
	Data interface{} `json:"data"`
}

type InstallOrUpgradeStatus struct {
	Uid      string `json:"uid"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Msg      string `json:"message"`
	Progress string `json:"progress,omitempty"`
	From     string `json:"from"`
}

var once sync.Once
var StatusManagerSvc *StatusManager

func init() {
	GetStatusManager()
}

func GetStatusManager() *StatusManager {
	once.Do(func() {
		StatusManagerSvc = &StatusManager{}
	})

	return StatusManagerSvc
}

func (s *StatusManager) UpdateInstallStatus(uid, status, progress, ty, msg string, from string) {
	s.broadcastStatus(uid, status, progress, msg, ty, from)
}

func (s *StatusManager) UpdateUpgradeStatus(uid, status, ty, msg string, from string) {
	s.broadcastStatus(uid, status, "", msg, ty, from)
}

func (s *StatusManager) UpdateModelInstallStatus(uid, status, progress, ty, msg string, from string) {
	s.broadcastStatus(uid, status, progress, msg, ty, from)
}

// call when status changed
func (s *StatusManager) broadcastStatus(uid, status, progress, msg, t string, from string) {
	resp := InstallOrUpgradeResp{
		Code: 200,
		Data: InstallOrUpgradeStatus{
			Uid:      uid,
			Type:     t,
			Status:   status,
			Progress: progress,
			Msg:      msg,
			From:     from,
		},
	}

	glog.Infof("broadcast, uid:%s, status:%s, progress:%s, msg:%s, t:%s", uid, status, progress, msg, t)
	//broadcast status
	err := BroadcastMessage(resp)
	if err != nil {
		glog.Warning(err)
	}
}
