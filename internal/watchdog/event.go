package watchdog

import (
	"context"
	"fmt"
	"market/internal/appmgr"
	"market/internal/appservice"
	"market/internal/constants"
	"market/internal/event"

	//"market/internal/helm"
	"market/internal/models"
	"sync"
	"time"

	"github.com/golang/glog"
)

type InstallationWatchDog struct {
	cfgType  string
	client   *event.Client
	uid      string
	ctx      context.Context
	cancel   context.CancelFunc
	op       string
	app      string
	from     string
	progress string
	appInfo  *models.ApplicationInfo
	token    string
	status   string
	clear    func(string)
}

type EventData struct {
	Op string `json:"op"`

	App       string     `json:"app"`
	ID        string     `json:"id"` //same with appid
	AppID     string     `json:"appid"`
	Icon      string     `json:"icon"`
	Title     string     `json:"title"`
	Entrances []Entrance `json:"entrances"`

	Uid    string `json:"uid"`
	Status string `json:"status"`
}

type Entrance struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
}

const (
	APP_EVENT = "app-installation-event"

	OP_INSTALL   = "install"
	OP_UNINSTALL = "uninstall"
	OP_SUSPEND   = "suspend"
	OP_RESUME    = "resume"

	OP_UPGRADE = "upgrade"
	//OP_ROLLBACK = "rollback"

	//Installing   = "installing"
	//Uninstalling = "uninstalling"
	//Upgrading    = "upgrading"
	//Upgraded  = "upgraded"
	Pending     = "pending"
	Downloading = "downloading"
	Processing  = "processing"
	Completed   = "completed"
	Canceled    = "canceled"
	Failed      = "failed"
	Stopping    = "stopping"

	AppFromDev   = "dev"
	AppFromStore = "store"
)

type ManagerMap map[string]*InstallationWatchDog

type Manager struct {
	ManagerMap
	mu sync.Mutex
}

func NewWatchDogManager() *Manager {
	return &Manager{ManagerMap: make(ManagerMap)}
}

func (w *Manager) GetProgress(uid string) string {

	manager, exists := w.ManagerMap[uid]
	if !exists || manager == nil {
		glog.Info("GetProgress-Event:", uid, "not found or nil")
		return ""
	}

	glog.Info("GetProgress-Event:", uid, manager.progress)
	return manager.progress
}

func (w *Manager) NewWatchDog(installOp, appname, uid, token, from, cfgType string, info *models.ApplicationInfo) *InstallationWatchDog {
	w.mu.Lock()
	defer w.mu.Unlock()
	if uid == "" {
		uid = appname
	}
	if _, exist := w.ManagerMap[uid]; exist {
		glog.Warningf("op:%s, app:%s, uid %s exist in map", installOp, appname, uid)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Hour)

	eventClient := event.NewClient()

	wd := &InstallationWatchDog{
		client:  eventClient,
		uid:     uid,
		appInfo: info,
		ctx:     ctx,
		cancel:  cancel,
		op:      installOp,
		app:     appname,
		token:   token,
		from:    from,
		cfgType: cfgType,
		clear:   w.DeleteWatchDog,
	}
	w.ManagerMap[uid] = wd
	glog.Info("NewWatchDog-Event:", uid)

	return wd
}

//func (w *Manager) CancelWatchDog(uid string) {
//	w.mu.Lock()
//	defer w.mu.Unlock()
//	if wd, ok := w.ManagerMap[uid]; ok {
//		wd.cancel()
//		w.DeleteWatchDog(uid)
//	}
//}

func (w *Manager) DeleteWatchDog(uid string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.ManagerMap, uid)
}

func isStatusWaiting(status string) bool {
	if status != Pending {
		return false
	}

	return true
}

func (i *InstallationWatchDog) Exec() {
	if i == nil {
		glog.Warning("watch dog empty return")
		return
	}
	//if i.appInfo.CfgType != constants.AppType {
	//	return
	//}
	ticker := time.NewTicker(1 * time.Second)
	glog.Infof("start watch app:%s, op:%s", i.app, i.op)
	defer func() {
		glog.Infof("stop watch app:%s, op:%s", i.app, i.op)
		ticker.Stop()
		i.clear(i.uid)
	}()

	statusBegin, _, _, _ := i.getStatus()

	waiting := isStatusWaiting(statusBegin)
	for waiting {
		select {
		case <-i.ctx.Done(): // timeout or canceled
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get installation status err, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
			}
			glog.Infof("watch app:%s status waiting ctx done, uid:%s, status:%s, msg:%s", i.app, i.uid, status, msg)
			if status != Completed && status != Failed {
				err = i.cancelOp()
				if err != nil {
					glog.Error("Cancel installation err,", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				}
				i.appInstallCanceled()
				return
			} else if status == Completed {
				data := i.genEvent(Completed)
				err = i.client.CreateEvent(APP_EVENT, i.statusComplete(), &data)
				if err != nil {
					glog.Error("send app installation event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app installation event complete success ", i.app, ", ", i.uid, ", ", i.op)
				}

				i.appInstallFinished(Completed)
				return
			} else if status == Failed {
				return
			} else if status == Canceled {
				i.appInstallCanceled()
				return
			}

			return
		case <-ticker.C:
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get installation status err, ", i.app, ", ", i.uid, ", ", msg, ", ", err)
			}
			waiting = isStatusWaiting(status)
		}
	}

	s, m := i.statusDoing()
	data := i.genEvent(s)

	// start watch dog, send the first event of the action beginning
	err := i.client.CreateEvent(APP_EVENT, m, &data)
	if err != nil {
		glog.Error("send app installation event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
		return
	}

	i.ctx, i.cancel = context.WithTimeout(context.Background(), 15*time.Minute)

	for {
		select {

		case <-i.ctx.Done(): // timeout or canceled
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get installation status err, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
			}
			glog.Infof("watch app:%s status ctx done", i.app)
			if status != Completed && status != Failed {
				err = i.cancelOp()
				if err != nil {
					glog.Error("Cancel installation err,", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				}

				i.appInstallCanceled()
			} else if status == Completed {
				data := i.genEvent(Completed)
				err = i.client.CreateEvent(APP_EVENT, i.statusComplete(), &data)
				if err != nil {
					glog.Error("send app installation event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app installation event complete success ", i.app, ", ", i.uid, ", ", i.op)
				}

				i.appInstallFinished(Completed)
			} else if status == Failed {
				data := i.genEvent(Failed)
				err = i.client.CreateEvent(APP_EVENT, i.statusFail(msg), &data)
				if err != nil {
					glog.Error("send app installation event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app installation event failed success ", i.app, ", ", i.uid, ", ", i.op)
				}

			} else if status == Canceled {
				i.appInstallCanceled()
			}
			return
		case <-ticker.C:
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get installation status err, ", i.app, ", ", i.uid, ", ", err)
			}
			//glog.Infof("watch app:%s, uid:%s, status:%s, msg:%s ctx ticker", i.app, i.uid, status, msg)
			if status == Completed {
				glog.Infof("watch app:%s op:%s status complete", i.app, i.op)
				data := i.genEvent(Completed)
				err = i.client.CreateEvent(APP_EVENT, i.statusComplete(), &data)
				if err != nil {
					glog.Error("send app installation event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app installation event complete success ", i.app, ", ", i.uid, ", ", i.op)
				}

				i.appInstallFinished(Completed)
				return
			} else if status == Failed {
				glog.Infof("watch app:%s status failed", i.app)
				data := i.genEvent(Failed)
				err = i.client.CreateEvent(APP_EVENT, i.statusFail(msg), &data)
				if err != nil {
					glog.Error("send app installation event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
				} else {
					glog.Info("send app installation event failed success ", i.app, ", ", i.uid, ", ", i.op)
				}

				return
			} else if status == Canceled {
				glog.Warningf("watch app:%s status:%s msg:%s", i.app, status, msg)
				i.appInstallCanceled()
				return
			} else {
				glog.Warningf("watch app:%s status:%s msg:%s", i.app, status, msg)
			}
		}
	}
}

func (i *InstallationWatchDog) appInstallCanceled() {
	data := i.genEvent(Canceled)
	err := i.client.CreateEvent(APP_EVENT, i.statusCanceled(), &data)
	if err != nil {
		glog.Error("send app installation event error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err)
	} else {
		glog.Info("send app installation event canceled success ", i.app, ", ", i.uid, ", ", i.op)
	}
}

func (i *InstallationWatchDog) appUninstallFinished(status string) {
	if status != Completed {
		return
	}
	//_ = helm.DeleteChartByName(i.app)
}

func (i *InstallationWatchDog) appInstallFinished(status string) {
	if i.op == OP_UNINSTALL {
		i.appUninstallFinished(status)
	}

	if i.op != OP_INSTALL || i.appInfo == nil {
		return
	}

	i.countAppFromStoreCompleted(status)
}

func (i *InstallationWatchDog) uploadChart() {
	uploadChartToMuseum(i.app, i.appInfo)
}

func uploadChartToMuseum(name string, info *models.ApplicationInfo) {
	if info == nil {
		return
	}

	localChart := info.GetChartLocalPath()
	if localChart == "" {
		glog.Warningf("app:%s info:%+v has no local chart", name, info)
	}

	//_ = helm.DeleteChartByName(name)
	//err := helm.UploadChart(localChart)
	//if err != nil {
	//	glog.Warningf("UploadChart err %v", err)
	//	return
	//}

	glog.Infof("UploadChart success, name %s, chart:%s", name, localChart)
}

func (i *InstallationWatchDog) countAppFromStoreCompleted(status string) {
	if status != Completed {
		return
	}

	glog.Infof("app %s install success, will upload chart to museum", i.app)

	i.uploadChart()

	if i.from != AppFromStore {
		return
	}

	res, err := appmgr.CountAppInstalled(i.app)
	if err != nil {
		glog.Error("CountApp error, ", i.app, ", ", i.uid, ", ", i.op, ", ", err, ", ", res)
	}
}

func (i *InstallationWatchDog) statusDoing() (string, string) {
	switch i.op {
	case OP_INSTALL:
		return "installing", fmt.Sprintf("%s started to install", i.app)
	case OP_UNINSTALL:
		return "uninstalling", fmt.Sprintf("%s started to uninstall", i.app)
	case OP_RESUME:
		return "resuming", fmt.Sprintf("%s started to resuming", i.app)
	}

	return "unknown", ""
}

func (i *InstallationWatchDog) statusFail(err string) string {
	switch i.op {
	case OP_INSTALL:
		return fmt.Sprintf("%s's installation fail, it has been canceled. Err: %s", i.app, err)
	case OP_UNINSTALL:
		return fmt.Sprintf("%s's uninstallation fail, it has been canceled. Err: %s", i.app, err)
	case OP_RESUME:
		return fmt.Sprintf("%s's resume fail, it has been canceled. Err: %s", i.app, err)
	}

	return "unknown"
}

func (i *InstallationWatchDog) statusComplete() string {
	switch i.op {
	case OP_INSTALL:
		return fmt.Sprintf("%s's installation is completed", i.app)
	case OP_UNINSTALL:
		return fmt.Sprintf("%s's uninstallation is completed", i.app)
	case OP_RESUME:
		return fmt.Sprintf("%s's resume is completed", i.app)
	}

	return "unknown"
}

func (i *InstallationWatchDog) statusCanceled() string {
	switch i.op {
	case OP_INSTALL:
		return fmt.Sprintf("%s's installation has been canceled", i.app)
	case OP_UNINSTALL:
		return fmt.Sprintf("%s's uninstallation has been canceled", i.app)
	case OP_RESUME:
		return fmt.Sprintf("%s's resume has been canceled", i.app)
	}

	return "unknown"
}

func (i *InstallationWatchDog) getStatusByType() (*appservice.OperateResult, error) {
	if i.cfgType == constants.AppType {
		return appservice.GetOperatorResult(i.uid, i.token)
	} else if i.cfgType == constants.MiddlewareType {
		return appservice.GetMiddlewareOperatorResult(i.uid, i.token)
	}

	return nil, fmt.Errorf("unsupport type:%s", i.cfgType)
}

func (i *InstallationWatchDog) getStatus() (string, string, string, error) {
	res, err := i.getStatusByType()
	if err != nil {
		return "", "", "", err
	}

	i.updateStatus(string(res.State), res.Progress, string(res.OpType), res.Message)

	return string(res.State), res.Message, string(res.OpType), nil
}

func (i *InstallationWatchDog) updateStatus(status, progress, op, msg string) {
	if i.status == Downloading {
		if i.status == status && compareFloatStrings(i.progress, progress) >= 0 {
			return
		}
	} else if i.status == status {
		return
	}

	GetStatusManager().UpdateInstallStatus(i.uid, status, progress, op, msg, i.from)

	i.status, i.progress = status, progress
}

func (i *InstallationWatchDog) cancelOp() error {
	if i.cfgType == constants.AppType {
		_, err := appservice.AppCancel(i.uid, constants.CancelTypeTimeout, i.token)
		return err
	}
	if i.cfgType == constants.MiddlewareType {
		_, _, err := appservice.MiddlewareCancel(i.uid, constants.CancelTypeTimeout, i.token)
		return err
	}

	return nil
}

func (i *InstallationWatchDog) genEvent(status string) *EventData {
	ed := EventData{
		App:    i.app,
		Op:     i.op,
		Uid:    i.uid,
		Status: status,
	}

	getInfoFromAppInfo(&ed, i.appInfo)
	return &ed
}

func getInfoFromAppInfo(ed *EventData, info *models.ApplicationInfo) {
	if info == nil || ed == nil {
		return
	}

	ed.ID = info.AppID
	ed.AppID = info.AppID
	ed.Icon = info.Icon
	ed.Title = info.Title

	if len(info.Entrances) == 0 {
		return
	}

	ed.Entrances = make([]Entrance, len(info.Entrances))

	if len(info.Entrances) == 1 {
		ed.Entrances[0].ID = ed.ID
		ed.Entrances[0].Name = info.Entrances[0].Name
		ed.Entrances[0].Title = info.Entrances[0].Title
		ed.Entrances[0].Icon = info.Entrances[0].Icon
		if ed.Entrances[0].Icon == "" {
			ed.Entrances[0].Icon = info.Icon
		}

		return
	}

	for j := range info.Entrances {
		ed.Entrances[j].ID = fmt.Sprintf("%s%d", ed.ID, j)
		ed.Entrances[j].Name = info.Entrances[j].Name
		ed.Entrances[j].Title = info.Entrances[j].Title
		ed.Entrances[j].Icon = info.Entrances[j].Icon

		if ed.Entrances[j].Icon == "" {
			ed.Entrances[j].Icon = info.Icon
		}
	}

}
