package v1

//
//import (
//	"errors"
//	"github.com/emicklei/go-restful/v3"
//	"github.com/golang/glog"
//	"market/internal/appmgr"
//	"market/internal/appservice"
//	"market/internal/boltdb"
//	"market/internal/constants"
//	"market/pkg/api"
//)

//func (h *Handler) installRecommend(req *restful.Request, resp *restful.Response) {
//	appName := req.PathParameter(ParamAppName)
//	token := getToken(req)
//	if token == "" {
//		api.HandleUnauthorized(resp, errors.New("access token not found"))
//		return
//	}
//
//	info, err := appmgr.GetAppInfo(appName)
//	if err != nil {
//		respErr(resp, err)
//		return
//	}
//	if info.ChartName == "" {
//		api.HandleError(resp, errors.New("get chart name failed"))
//		return
//	}
//
//	err = checkDep(info, token)
//	if err != nil {
//		respDepErr(resp, err.Error())
//		return
//	}
//
//	//todo cache chart
//	err = appmgr.DownloadAppTgz(info.ChartName)
//	if err != nil {
//		respErr(resp, err)
//		return
//	}
//
//	info.Source = constants.AppFromMarket
//	err = boltdb.UpsertLocalAppInfo(info)
//	if err != nil {
//		glog.Warningf("UpsertLocalAppInfo err:%s", err.Error())
//		api.HandleError(resp, err)
//		return
//	}
//
//	resBody, err := installByType(info, token)
//	if err != nil {
//		api.HandleError(resp, err)
//		glog.Warningf("install %s resp:%s, err:%s", appName, resBody, err.Error())
//		return
//	}
//
//	uid, _, err := appservice.GetUidFromStatus(resBody)
//	if err != nil {
//		api.HandleError(resp, err)
//		return
//	}
//
//	glog.Infof("uid:%s", uid)
//
//	dealWithInstallResp(resp, resBody)
//}
