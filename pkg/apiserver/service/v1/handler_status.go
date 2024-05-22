package v1

import (
	"errors"
	"fmt"
	"github.com/golang/glog"
	"market/internal/appservice"
	"market/internal/constants"
	"market/internal/recommend"
	"market/pkg/api"

	"github.com/emicklei/go-restful/v3"
)

func (h *Handler) status(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	appName := req.PathParameter(ParamAppName)
	if appName == "" {
		api.HandleError(resp, fmt.Errorf("name is nil"))
		return
	}

	ty := req.PathParameter(ResourceType)
	if ty == "" {
		api.HandleError(resp, fmt.Errorf("type is nil"))
		return
	}

	if ty == constants.AppType {
		resBody, err := appservice.Status(appName, token)
		if err != nil {
			glog.Warningf("appservice.Status %s resp:%s, err:%s", appName, resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.RecommendType {
		resBody, err := recommend.Status(appName, token)
		if err != nil {
			glog.Warningf("recommend.Status %s resp:%s, err:%s", appName, resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		glog.Infof("recommend.Status%s resBody:%s", appName, resBody)
		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.ModelType {
		resBody, _, err := appservice.LlmStatus(appName, token)
		if err != nil {
			glog.Warningf("appservice.LlmStatus %s resp:%s, err:%s", appName, resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		glog.Infof("appservice.LlmStatus:%s resBody:%s", appName, resBody)
		respJsonWithOriginBody(resp, resBody)
		return
	}
}

func (h *Handler) statusList(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	ty := req.PathParameter(ResourceType)
	if ty == "" {
		api.HandleError(resp, fmt.Errorf("type is nil"))
		return
	}

	if ty == constants.AppType {
		resBody, err := appservice.StatusList(token)
		if err != nil {
			glog.Warningf("StatusList resp:%s, err:%s", resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.RecommendType {
		resBody, err := recommend.StatusList(token)
		if err != nil {
			glog.Warningf("recommend.StatusList resp:%s, err:%s", resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		glog.Infof("recommend.StatusList resBody:%s", resBody)
		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.ModelType {
		resBody, _, err := appservice.LlmStatusList(token)
		if err != nil {
			glog.Warningf("appservice.LlmStatusList resp:%s, err:%s", resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		glog.Infof("appservice.LlmStatusList resBody:%s", resBody)
		respJsonWithOriginBody(resp, resBody)
		return
	}
}
