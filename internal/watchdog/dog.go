package watchdog

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	"market/internal/appservice"
	"market/internal/constants"
	"market/internal/event"
	"market/internal/models"
	"sync"
	"time"
)

type CommonWatchDog struct {
	cfgType      string
	client       *event.Client
	uid          string
	ctx          context.Context
	cancel       context.CancelFunc
	op           string
	name         string
	from         string
	appInfo      *models.ApplicationInfo
	token        string
	status       string
	progress     string
	haveProgress bool
	clear        func(string)
}

type ModelManagerMap map[string]*CommonWatchDog

type ModelManager struct {
	ModelManagerMap
	mu sync.Mutex
}

func NewCommonWatchDogManager() *ModelManager {
	return &ModelManager{ModelManagerMap: make(ModelManagerMap)}
}

func checkHaveProgress(installOp, cfgType string) bool {
	if installOp == OP_INSTALL && cfgType == constants.ModelType {
		return true
	}

	return false
}

func (w *ModelManager) NewWatchDog(installOp, appName, uid, token, from, cfgType string, info *models.ApplicationInfo) *CommonWatchDog {
	w.mu.Lock()
	defer w.mu.Unlock()
	if uid == "" {
		uid = appName
	}
	if _, exist := w.ModelManagerMap[uid]; exist {
		glog.Warningf("op:%s, appName:%s, uid %s exist in map", installOp, appName, uid)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Hour)

	eventClient := event.NewClient()

	wd := &CommonWatchDog{
		client:       eventClient,
		uid:          uid,
		appInfo:      info,
		ctx:          ctx,
		cancel:       cancel,
		op:           installOp,
		name:         appName,
		token:        token,
		from:         from,
		cfgType:      cfgType,
		haveProgress: checkHaveProgress(installOp, cfgType),
		clear:        w.DeleteWatchDog,
	}
	w.ModelManagerMap[uid] = wd

	return wd
}

func (w *ModelManager) DeleteWatchDog(uid string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.ModelManagerMap, uid)
}

func (i *CommonWatchDog) Exec() {
	if i == nil {
		glog.Warning("watch dog empty return")
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	glog.Infof("start watch model:%s, op:%s", i.name, i.op)
	defer func() {
		glog.Infof("stop watch model:%s, op:%s", i.name, i.op)
		ticker.Stop()
		i.clear(i.uid)
	}()

	pending := true
	statusBegin, _, _, _ := i.getStatus()
	if statusBegin != Pending {
		pending = false
	}
	for pending {
		select {
		case <-i.ctx.Done(): // timeout or canceled
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get installation status err, ", i.name, ", ", i.uid, ", ", i.op, ", ", err)
			}
			glog.Infof("watch model:%s status pending ctx done, uid:%s, status:%s, msg:%s", i.name, i.uid, status, msg)
			if status != Completed && status != Failed {
				err = i.cancelOp()
				if err != nil {
					glog.Error("Cancel installation err,", i.name, ", ", i.uid, ", ", i.op, ", ", err)
				}

				return
			} /*else if status == Completed {
				return
			} else if status == Failed {
				return
			} else if status == Canceled {
				return
			}*/

			return
		case <-ticker.C:
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get installation status err, ", i.name, ", ", i.uid, ", ", msg, ", ", err)
			}
			if status != Pending {
				pending = false
			}
		}
	}

	i.ctx, i.cancel = context.WithTimeout(context.Background(), 1*time.Hour)

	for {
		select {

		case <-i.ctx.Done(): // timeout or canceled
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get installation status err, ", i.name, ", ", i.uid, ", ", i.op, ", ", err)
			}
			glog.Infof("watch model:%s status ctx done, uid:%s, status:%s, msg:%s", i.name, i.uid, status, msg)
			if status != Completed && status != Failed {
				err = i.cancelOp()
				if err != nil {
					glog.Error("Cancel installation err,", i.name, ", ", i.uid, ", ", i.op, ", ", err)
				}

			} /*else if status == Completed {

			} else if status == Failed {

			} else if status == Canceled {

			}*/
			return
		case <-ticker.C:
			status, msg, _, err := i.getStatus()
			if err != nil {
				glog.Error("get installation status err, ", i.name, ", ", i.uid, ", ", err)
			}
			//glog.Infof("watch model:%s, uid:%s, status:%s, msg:%s ctx ticker", i.name, i.uid, status, msg)
			if status == Completed {
				glog.Infof("watch model:%s op:%s status complete", i.name, i.op)
				return
			} else if status == Failed {
				glog.Infof("watch model:%s status failed", i.name)

				return
			} else if status == Canceled {
				glog.Warningf("watch model:%s status:%s msg:%s", i.name, status, msg)

				return
			} else {
				glog.Warningf("watch model:%s status:%s, progress:%s, msg:%s", i.name, status, i.progress, msg)
			}
		}
	}
}

func (i *CommonWatchDog) getStatusByType() (*appservice.OperateResult, error) {
	if i.cfgType == constants.ModelType {
		return appservice.GetModelOperatorResult(i.uid, i.token)
	} else if i.cfgType == constants.RecommendType {
		return appservice.GetRecommendOperatorResult(i.uid, i.token)
	}

	return nil, fmt.Errorf("unsupport type:%s", i.cfgType)
}

func (i *CommonWatchDog) getStatus() (string, string, string, error) {
	res, err := i.getStatusByType()
	if err != nil {
		return "", "", "", err
	}

	i.updateStatus(string(res.State), res.Progress, string(res.OpType), res.Message)

	return string(res.State), res.Message, string(res.OpType), nil
}

func (i *CommonWatchDog) updateStatus(status, progress, op, msg string) {
	if i.haveProgress {
		if i.status == status && i.progress == progress {
			return
		}
	} else if i.status == status {
		return
	}

	GetStatusManager().UpdateModelInstallStatus(i.uid, status, progress, op, msg)

	i.status, i.progress = status, progress
}

func (i *CommonWatchDog) cancelOp() error {
	if i.cfgType == constants.ModelType && i.op == OP_INSTALL {
		_, _, err := appservice.LlmCancel(i.uid, constants.CancelTypeTimeout, i.token)
		return err
	}

	return nil
}
