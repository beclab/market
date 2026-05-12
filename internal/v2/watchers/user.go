package watchers

import (
	"sync"

	"market/internal/v2/client"

	"github.com/golang/glog"
	"k8s.io/client-go/tools/cache"
)

type UserDataInfo struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Id     string `json:"id"`
	Zone   string `json:"zone"`
	Status string `json:"status"`
	Exists bool   `json:"exists"`
}

var Users sync.Map

func UserHandlerEvent() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handleUserEvent(obj, false, "Add")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			handleUserEvent(newObj, false, "Update")
		},
		DeleteFunc: func(obj interface{}) {
			handleUserEvent(obj, true, "Delete")
		},
	}
}

func handleUserEvent(obj interface{}, isDelete bool, opType string) {
	if isDelete {
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			obj = tombstone.Obj
		}
	}

	user, ok := obj.(*client.User)
	if !ok {
		glog.Errorf("Watchers: expected *client.User, got %T", obj)
		return
	}

	userName := user.Name

	glog.Infof("Watcher users, opType: %s, isDelete: %v, user: %s", opType, isDelete, userName)

	if isDelete {
		Users.Delete(userName)
		return
	}

	var role, id, zone, status string
	annotations := user.ObjectMeta.Annotations
	if annotations != nil {
		role = annotations["bytetrade.io/owner-role"]
		id = annotations["bytetrade.io/terminus-name"]
		status = annotations["bytetrade.io/wizard-status"]
		zone = annotations["bytetrade.io/zone"]
	}

	userInfo := UserDataInfo{
		Name:   userName,
		Role:   role,
		Id:     id,
		Zone:   zone,
		Status: status,
		Exists: true,
	}

	Users.Store(userName, userInfo)
}

func GetUserFromCache(name string) (UserDataInfo, bool) {
	val, ok := Users.Load(name)
	if !ok {
		return UserDataInfo{}, false
	}

	userInfo, ok := val.(UserDataInfo)
	if !ok {
		return UserDataInfo{}, false
	}

	return userInfo, true
}

func GetUserIDs() []string {
	var ids []string

	Users.Range(func(key, value interface{}) bool {
		if userInfo, ok := value.(UserDataInfo); ok {
			if userInfo.Name != "" {
				ids = append(ids, userInfo.Name)
			}
		}
		return true
	})

	return ids
}
