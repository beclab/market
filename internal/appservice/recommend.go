package appservice

import (
	"fmt"
	"market/internal/constants"
	"market/pkg/utils"
	"net/http"
)

func RecommendOperate(name, token string) (string, map[string]interface{}, error) {
	appServiceHost, appServicePort := constants.GetAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceRecommendOperateURLTempl, appServiceHost, appServicePort, name)

	return utils.DoHttpRequest(http.MethodGet, url, token, nil)
}

func GetRecommendOperatorResult(name, token string) (*OperateResult, error) {
	resStr, _, err := RecommendOperate(name, token)
	if err != nil {
		return nil, err
	}

	return ParseOperator(resStr)
}
