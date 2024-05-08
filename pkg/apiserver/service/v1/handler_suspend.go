package v1

import (
	"errors"
	"fmt"
	"github.com/golang/glog"
	"market/internal/appservice"
	"market/internal/constants"
	"market/internal/watchdog"
	"market/pkg/api"

	"github.com/emicklei/go-restful/v3"
)

func (h *Handler) suspend(req *restful.Request, resp *restful.Response) {
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

	resBody, err := suspendByType(appName, token, ty)
	if err != nil {
		glog.Warningf("suspend %s type:%s resp:%s, err:%s", appName, ty, resBody, err.Error())
		api.HandleError(resp, err)
		return
	}

	uid, _, err := appservice.GetUidFromStatus(resBody)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	if ty == constants.ModelType {
		go h.commonWatchDogManager.NewWatchDog(watchdog.OP_SUSPEND,
			appName, uid, token, "", ty,
			nil).
			Exec()
	}

	respJsonWithOriginBody(resp, resBody)
}

func suspendByType(name, token, ty string) (string, error) {
	switch ty {
	case constants.AppType:
		return appservice.AppSuspend(name, token)
	case constants.ModelType:
		resBody, _, err := appservice.LlmSuspend(name, token)
		return resBody, err
	//case constants.AgentType:
	//	//todo
	default:
		return "", fmt.Errorf("%s type %s invalid", name, ty)
	}
}

func (h *Handler) resume(req *restful.Request, resp *restful.Response) {
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

	resBody, err := resumeByType(appName, token, ty)
	if err != nil {
		glog.Warningf("suspend %s type:%s resp:%s, err:%s", appName, ty, resBody, err.Error())
		api.HandleError(resp, err)
		return
	}

	uid, _, err := appservice.GetUidFromStatus(resBody)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	if ty == constants.AppType {
		go h.watchDogManager.NewWatchDog(watchdog.OP_RESUME,
			appName, uid, token, "",
			nil).
			Exec()
	} else if ty == constants.ModelType {
		go h.commonWatchDogManager.NewWatchDog(watchdog.OP_RESUME,
			appName, uid, token, "", ty,
			nil).
			Exec()
	}

	respJsonWithOriginBody(resp, resBody)
}

func resumeByType(name, token, ty string) (string, error) {
	switch ty {
	case constants.AppType:
		return appservice.Resume(name, token)
	case constants.ModelType:
		resBody, _, err := appservice.LlmResume(name, token)
		return resBody, err
	//case constants.AgentType:
	//	//todo
	default:
		return "", fmt.Errorf("%s type %s invalid", name, ty)
	}
}
