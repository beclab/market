package appservice

import (
	"fmt"
	"market/internal/constants"
	"market/pkg/utils"
	"net/http"
)

//func GetUserResource(user, token string) (string, error) {
//	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
//	url := fmt.Sprintf(constants.AppServiceUserResourceTempl, appServiceHost, appServicePort, user)
//
//	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
//}

func GetCurUserResource(token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceCurUserResourceTempl, appServiceHost, appServicePort)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func GetClusterResource(token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceClusterResourceTempl, appServiceHost, appServicePort)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

func GetUserInfo(token string) (string, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceUserInfoTempl, appServiceHost, appServicePort)

	return utils.SendHttpRequestWithToken(http.MethodGet, url, token, nil)
}
