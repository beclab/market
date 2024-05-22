package v1

import (
	"errors"
	"market/internal/appservice"
	"market/pkg/api"

	"github.com/emicklei/go-restful/v3"
)

//func (h *Handler) userResourceStatus(req *restful.Request, resp *restful.Response) {
//	token := getToken(req)
//	if token == "" {
//		api.HandleUnauthorized(resp,  errors.New("access token not found"))
//		return
//	}
//
//	username := req.PathParameter(ParamUserName)
//	if username == "" {
//		api.HandleError(resp,  errors.New("username not found"))
//		return
//	}
//
//	resBody, err := appservice.GetUserResource(username, token)
//	if err != nil {
//		api.HandleError(resp,  err)
//		return
//	}
//
//	_, err = resp.Write([]byte(resBody))
//	if err != nil {
//		glog.Warningf("err:%s", err)
//	}
//}

func (h *Handler) curUserResourceStatus(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	resBody, err := appservice.GetCurUserResource(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	respJsonWithOriginBody(resp, resBody)
}

func (h *Handler) clusterResourceStatus(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	resBody, err := appservice.GetClusterResource(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	respJsonWithOriginBody(resp, resBody)
}

func (h *Handler) userInfo(req *restful.Request, resp *restful.Response) {
	token := getToken(req)
	if token == "" {
		api.HandleUnauthorized(resp, errors.New("access token not found"))
		return
	}

	resBody, err := appservice.GetUserInfo(token)
	if err != nil {
		api.HandleError(resp, err)
		return
	}

	respJsonWithOriginBody(resp, resBody)
}
