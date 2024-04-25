package watchdog

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	"market/internal/appservice"
	"market/internal/event"
	"market/internal/models"
	"sync"
	"time"
)

type UpgradeWatchDog struct {
	client  *event.Client
	uid     string
	ctx     context.Context
	cancel  context.CancelFunc
	op      string
	app     string
	from    string
	appInfo *models.ApplicationInfo
	token   string
	status  string
	clear   func(string)
}

type UpgradeManagerMap map[string]*UpgradeWatchDog

type UpgradeManager struct {
	UpgradeManagerMap
	sync.Mutex
}

func NewUpgradeWatchDogManager() *UpgradeManager {
	return &UpgradeManager{UpgradeManagerMap: make(UpgradeManagerMap)}
}

func (w *UpgradeManager) NewUpgradeWatchDog(installOp, appname, uid, token, from string, info *models.ApplicationInfo) *UpgradeWatchDog {
	w.Lock()
	defer w.Unlock()
	if _, exist := w.UpgradeManagerMap[uid]; exist {
		glog.Warningf("op:%s, app:%s, uid %s exist in map", installOp, appname, uid)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)

	eventClient := event.NewClient()

	wd := &UpgradeWatchDog{
		client:  eventClient,
		uid:     uid,
		appInfo: info,
		ctx:     ctx,
		cancel:  cancel,
		op:      installOp,
		app:     appname,
		token:   token,
		from:    from,
		//appServiceClient: client,
		clear: w.DeleteWatchDog,
	}
	w.UpgradeManagerMap[uid] = wd

	return wd
}

//func (w *UpgradeManager) CancelWatchDog(uid string) {
//	w.Lock()
//	defer w.Unlock()
//	if wd, ok := w.UpgradeManagerMap[uid]; ok {
//		wd.cancel()
//		w.DeleteWatchDog(uid)
//	}
//}

func (w *UpgradeManager) DeleteWatchDog(uid string) {
	w.Lock()
	defer w.Unlock()
	delete(w.UpgradeManagerMap, uid)
}

func (i *UpgradeWatchDog) Exec() {
	if i == nil {
		glog.Warning("watch dog empty return")
		return
	}
	ticker := time.NewTicker(1 * time.Second)
	glog.Infof("start watch app:%s, op:%s", i.app, i.op)
	defer func() {
		glog.Infof("stop watch app:%s, op:%s", i.app, i.op)
		ticker.Stop()
		i.clear(i.uid)
	}()

	s, m := i.statusDoing()
	data := i.genEvent(s)

	// start watch dog, send the first event of the action beginning
	err := i.client.CreateEvent(APP_EVENT, m, &data)
	if err != nil {
		glog.Error("send app upgrade event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
		return
	}

	for {
		select {

		case <-i.ctx.Done(): // timeout or canceled
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get upgrade status err, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
			}
			glog.Infof("watch app:%s status ctx done", i.app)
			if status != Completed && status != Failed {
				data := i.genEvent(Canceled)
				err = i.client.CreateEvent(APP_EVENT, i.statusCanceled(), &data)
				if err != nil {
					glog.Error("send app upgrade event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app upgrade event cancled success ", i.app, ", ", i.uid, ", ", i.op)
				}

				if msg != "" {
					msg = "upgrade timeout canceled"
				} else {
					msg += ", upgrade timeout canceled"
				}
				i.appUpgradeCanceled(msg)
				return
			} else if status == Completed {
				data := i.genEvent(Completed)
				err = i.client.CreateEvent(APP_EVENT, i.statusComplete(), &data)
				if err != nil {
					glog.Error("send app upgrade event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app upgrade event complete success ", i.app, ", ", i.uid, ", ", i.op)
				}
				i.appUpgradeCompleted()
				return
			} else if status == Failed {
				data := i.genEvent(Failed)
				err = i.client.CreateEvent(APP_EVENT, i.statusFail(msg), &data)
				if err != nil {
					glog.Error("send app upgrade event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app upgrade event failed success ", i.app, ", ", i.uid, ", ", i.op)
				}
				return
			} else if status == Canceled {
				if msg != "" {
					msg = "upgrade timeout and canceled"
				} else {
					msg += ", upgrade timeout and canceled"
				}
				i.appUpgradeCanceled(msg)
				return
			}
			return
		case <-ticker.C:
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get upgrade status err, ", i.app, ", ", i.uid, ", ", err)
			}
			if status == Completed {
				glog.Infof("watch app:%s op:%s status complete", i.app, i.op)
				data := i.genEvent(Completed)
				err = i.client.CreateEvent(APP_EVENT, i.statusComplete(), &data)
				if err != nil {
					glog.Error("send app upgrade event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app upgrade event complete success ", i.app, ", ", i.uid, ", ", i.op)
				}
				i.appUpgradeCompleted()
				return
			} else if status == Failed {
				glog.Infof("watch app:%s status failed", i.app)
				data := i.genEvent(Failed)
				err = i.client.CreateEvent(APP_EVENT, i.statusFail(msg), &data)
				if err != nil {
					glog.Error("send app upgrade event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app upgrade event failed success ", i.app, ", ", i.uid, ", ", i.op)
				}
				return
			} else if status == Canceled {
				if msg == "" {
					msg = "upgrade canceled"
				}
				glog.Warningf("watch app:%s status:%s msg:%s", i.app, status, msg)
				i.appUpgradeCanceled(msg)
				return
			} else {
				glog.Warningf("watch app:%s status:%s msg:%s", i.app, status, msg)
			}
		}
	}
}

func (i *UpgradeWatchDog) statusDoing() (string, string) {
	switch i.op {
	case OP_UPGRADE:
		return "upgrading", fmt.Sprintf("%s started to upgrade", i.app)
		//case OP_ROLLBACK:
		//	return "rollbacking", fmt.Sprintf("%s started to rollback", i.app)
	}

	return "unknown", ""
}

func (i *UpgradeWatchDog) statusFail(err string) string {
	switch i.op {
	case OP_UPGRADE:
		return fmt.Sprintf("%s's upgrade fail, it has been canceled. Err: %s", i.app, err)
		//case OP_ROLLBACK:
		//	return fmt.Sprintf("%s's rollback fail, it has been canceled. Err: %s", i.app, err)
	}

	return "unknown"
}

func (i *UpgradeWatchDog) statusComplete() string {
	switch i.op {
	case OP_UPGRADE:
		return fmt.Sprintf("%s's upgrade is completed", i.app)
		//case OP_ROLLBACK:
		//	return fmt.Sprintf("%s's rollback is completed", i.app)
	}

	return "unknown"
}

func (i *UpgradeWatchDog) statusCanceled() string {
	switch i.op {
	case OP_UPGRADE:
		return fmt.Sprintf("%s's upgrade has been canceled", i.app)
		//case OP_ROLLBACK:
		//	return fmt.Sprintf("%s's rollback has been canceled", i.app)
	}

	return "unknown"
}

func (i *UpgradeWatchDog) getStatus() (string, string, string, error) {
	res, err := appservice.GetOperatorResult(i.uid, i.token)
	if err != nil {
		return "", "", "", err
	}

	i.updateStatus(string(res.State), string(res.OpType), res.Message)

	return string(res.State), res.Message, string(res.OpType), nil
}

func (i *UpgradeWatchDog) updateStatus(status, op, msg string) {
	if i.status == status {
		return
	}

	GetStatusManager().UpdateUpgradeStatus(i.uid, status, op, msg)

	i.status = status
}

func (i *UpgradeWatchDog) genEvent(status string) *EventData {
	ed := EventData{
		App:    i.app,
		Op:     i.op,
		Uid:    i.uid,
		Status: status,
	}

	getInfoFromAppInfo(&ed, i.appInfo)
	return &ed
}

func (i *UpgradeWatchDog) appUpgradeCanceled(msg string) {
	data := i.genEvent(Canceled)
	err := i.client.CreateEvent(APP_EVENT, i.statusCanceled(), &data)
	if err != nil {
		glog.Error("send app upgrade event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
	} else {
		glog.Info("send app upgrade event canceled success ", i.app, ", ", i.uid, ", ", i.op)
	}

}

func (i *UpgradeWatchDog) appUpgradeCompleted() {
	uploadChartToMuseum(i.app, i.appInfo)
}
