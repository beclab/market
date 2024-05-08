package v1

import (
	"errors"
	"fmt"
	"github.com/golang/glog"
	"market/internal/appservice"
	"market/internal/constants"
	"market/pkg/api"

	"github.com/emicklei/go-restful/v3"
)

func (h *Handler) operate(req *restful.Request, resp *restful.Response) {
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
		resBody, err := appservice.AppOperate(appName, token)
		if err != nil {
			glog.Warningf("appservice.Operate %s resp:%s, err:%s", appName, resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.ModelType {
		resBody, _, err := appservice.LlmOperate(appName, token)
		if err != nil {
			glog.Warningf("appservice.LlmOperate %s resp:%s, err:%s", appName, resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.RecommendType {
		return
	}
}

func (h *Handler) operateList(req *restful.Request, resp *restful.Response) {
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
		resBody, err := appservice.OperateList(token)
		if err != nil {
			glog.Warningf("appservice.OperateList resp:%s, err:%s", resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		_, _ = resp.Write([]byte(resBody))
		return
	}

	if ty == constants.ModelType {
		resBody, _, err := appservice.LlmOperateList(token)
		if err != nil {
			glog.Warningf("appservice.LlmOperateList resp:%s, err:%s", resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.RecommendType {
		return
	}
}

func (h *Handler) operateHistoryListNew(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	resBody, err := appservice.OperatorHistoryListWitRaw(token, req.Request.URL.RawQuery)
	if err != nil {
		glog.Warningf("appservice.OperatorHistoryListWitRaw resp:%s, err:%s", resBody, err.Error())
		api.HandleError(resp, err)
		return
	}

	respJsonWithOriginBody(resp, resBody)
	return
}

// OperatorHistoryList
func (h *Handler) operateHistoryList(req *restful.Request, resp *restful.Response) {
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
		resBody, err := appservice.OperatorHistoryList(token)
		if err != nil {
			glog.Warningf("appservice.OperatorHistoryList resp:%s, err:%s", resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.ModelType {
		resBody, _, err := appservice.LlmOperateHistoryList(token)
		if err != nil {
			glog.Warningf("appservice.LlmOperateHistoryList resp:%s, err:%s", resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.RecommendType {
		return
	}
}

func (h *Handler) operateHistory(req *restful.Request, resp *restful.Response) {
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
		resBody, err := appservice.OperatorHistory(appName, token)
		if err != nil {
			glog.Warningf("appservice.OperatorHistory %s resp:%s, err:%s", appName, resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.ModelType {
		resBody, _, err := appservice.LlmOperateHistory(appName, token)
		if err != nil {
			glog.Warningf("appservice.LlmOperateHistory %s resp:%s, err:%s", appName, resBody, err.Error())
			api.HandleError(resp, err)
			return
		}

		respJsonWithOriginBody(resp, resBody)
		return
	}

	if ty == constants.RecommendType {
		return
	}
}
