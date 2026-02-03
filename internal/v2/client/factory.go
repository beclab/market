package client

import (
	"context"
	"encoding/json"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

var Factory *factory

type factory struct {
	config *rest.Config

	client *dynamic.DynamicClient //dynamic.Interface
}

func NewFactory() error {
	if Factory == nil {
		config, err := ctrl.GetConfig()
		if err != nil {
			return errors.Errorf("new rest kubeconfig: %v", err)
		}

		config.Burst = 15
		config.QPS = 50

		client, err := dynamic.NewForConfig(config)
		if err != nil {
			return errors.Errorf("new dynamic client: %v", err)
		}

		Factory = &factory{
			config: config,
			client: client,
		}
	}

	return nil
}

func (f *factory) GetUserInfo(userId string) (map[string]string, error) {
	unstructuredUser, err := f.client.Resource(UserGVR).Get(context.Background(), userId, v1.GetOptions{})
	if err != nil {
		glog.Errorf("GetUserInfo: get user res error: %v", err)
		return nil, err
	}
	var user *User
	b, err := unstructuredUser.MarshalJSON()
	if err != nil {
		glog.Errorf("GetUserInfo: marshal unstructured user error: %v", err)
		return nil, err
	}
	if err := json.Unmarshal(b, &user); err != nil {
		glog.Errorf("GetUserInfo: unmarshal user error: %v", err)
		return nil, err
	}

	var userInfo = make(map[string]string)
	var anno = user.ObjectMeta.Annotations
	role, _ := anno["bytetrade.io/owner-role"]
	status, _ := anno["bytetrade.io/wizard-status"]
	olaresId, _ := anno["bytetrade.io/terminus-name"]
	userInfo["name"] = user.Name
	userInfo["role"] = role
	userInfo["id"] = olaresId
	userInfo["status"] = status

	return userInfo, nil

}
