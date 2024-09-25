package constants

import "os"

const (
	AuthorizationTokenCookieKey = "auth_token"
	AuthorizationTokenKey       = "X-Authorization"
	APIServerListenAddress      = ":81"
	HelmServerListenAddress     = ":82"

	DataPath           = "./data"
	ChartsLocalDir     = "./data/charts"
	ChartsLocalTempDir = "./data/chartsTmp"
	AppCfgFileName     = "TerminusManifest.yaml"
)

const (
	CancelTypeOperate = "operate"
	CancelTypeTimeout = "timeout"
)

const (
	AppServiceInstallURLTempl                   = "http://%s:%s/app-service/v1/apps/%s/install"
	AppServiceUninstallURLTempl                 = "http://%s:%s/app-service/v1/apps/%s/uninstall"
	AppServiceUpgradeURLTempl                   = "http://%s:%s/app-service/v1/apps/%s/upgrade"
	AppServiceVersionURLTempl                   = "http://%s:%s/app-service/v1/applications/%s/version"
	AppServiceSuspendURLTempl                   = "http://%s:%s/app-service/v1/apps/%s/suspend"
	AppServiceResumeURLTempl                    = "http://%s:%s/app-service/v1/apps/%s/resume"
	AppServiceStatusURLTempl                    = "http://%s:%s/app-service/v1/apps/%s/status"
	AppServiceStatusListURLTempl                = "http://%s:%s/app-service/v1/apps/status"
	AppServiceOperateURLTempl                   = "http://%s:%s/app-service/v1/apps/%s/operate"
	AppServiceOperateListURLTempl               = "http://%s:%s/app-service/v1/apps/operate"
	AppServiceOperateHistoryURLTempl            = "http://%s:%s/app-service/v1/apps/%s/operate_history"
	AppServiceOperateHistoryListURLTempl        = "http://%s:%s/app-service/v1/apps/operate_history"
	AppServiceOperateHistoryListWithRawURLTempl = "http://%s:%s/app-service/v1/apps/operate_history?%s"
	AppServiceAppsURLTempl                      = "http://%s:%s/app-service/v1/apps/%s"
	AppServiceAppsListURLTempl                  = "http://%s:%s/app-service/v1/apps?issysapp=%s&state=%s"

	AppServiceCheckDependencies = "http://%s:%s/app-service/v1/application/deps"

	AppServiceInstallCancelURLTempl = "http://%s:%s/app-service/v1/apps/%s/cancel?type=%s"

	AppServiceTerminusVersionURLTempl = "http://%s:%s/app-service/v1/terminus/version"
	AppServiceTerminusNodesURLTempl   = "http://%s:%s/app-service/v1/terminus/nodes"

	AppServiceCurUserResourceTempl = "http://%s:%s/app-service/v1/user/resource"
	AppServiceClusterResourceTempl = "http://%s:%s/app-service/v1/cluster/resource"
	AppServiceUserInfoTempl        = "http://%s:%s/app-service/v1/user-info"

	AppStoreServiceAppURLTempl        = "https://%s:%s/app-store-server/v1/application/%s"
	AppStoreServiceAppInfoURLTempl    = "https://%s:%s/app-store-server/v1/applications/info/%s"
	AppStoreServiceAppInfosURLTempl   = "https://%s:%s/app-store-server/v1/applications/infos"
	AppStoreServiceAppTypesURLTempl   = "https://%s:%s/app-store-server/v1/applications/types"
	AppStoreServiceAppExistURLTempl   = "https://%s:%s/app-store-server/v1/applications/exist/%s"
	AppStoreServiceAppListURLTempl    = "https://%s:%s/app-store-server/v1/applications?page=%s&size=%s"
	AppStoreServiceAppTopURLTempl     = "https://%s:%s/app-store-server/v1/applications/top"
	AppStoreServiceAppSearchURLTempl  = "https://%s:%s/app-store-server/v1/applications/search/%s?page=%s&size=%s"
	AppStoreServiceAppCounterURLTempl = "https://%s:%s/app-store-server/v1/counter/%s"
	AppStoreAppVersionURLTempl        = "https://%s:%s/app-store-server/v1/applications/version-history/%s"
	AppStoreServiceReadMeURLTempl     = "https://%s:%s/app-store-server/v1/applications/%s/README.md"

	AppStoreServicePagesDetailURLTempl = "https://%s:%s/app-store-server/v1/pages/detail"

	AppServiceHostEnv      = "APP_SERVICE_SERVICE_HOST"
	AppServicePortEnv      = "APP_SERVICE_SERVICE_PORT"
	AppStoreServiceHostEnv = "APP_SOTRE_SERVICE_SERVICE_HOST"
	AppStoreServicePortEnv = "APP_SOTRE_SERVICE_SERVICE_PORT"
	MarketProvider         = "MARKET_PROVIDER"

	RecommendServiceInstallURLTempl           = "http://%s:%s/app-service/v1/recommends/%s/install"
	RecommendServiceUninstallURLTempl         = "http://%s:%s/app-service/v1/recommends/%s/uninstall"
	RecommendServiceUpgradeURLTempl           = "http://%s:%s/app-service/v1/recommends/%s/upgrade" //post
	RecommendServiceUpgradeStatusURLTempl     = "http://%s:%s/app-service/v1/recommends/%s/status"  //get
	RecommendServiceUpgradeStatusListURLTempl = "http://%s:%s/app-service/v1/recommends/status"     //get
	AppServiceRecommendOperateURLTempl        = "http://%s:%s/app-service/v1/recommends/%s/operate"

	AppServiceModelInstallURLTempl            = "http://%s:%s/app-service/v1/models/%s/install"
	AppServiceModelUninstallURLTempl          = "http://%s:%s/app-service/v1/models/%s/uninstall"
	AppServiceModelCancelURLTempl             = "http://%s:%s/app-service/v1/models/%s/cancel?type=%s"
	AppServiceModelSuspendURLTempl            = "http://%s:%s/app-service/v1/models/%s/suspend"
	AppServiceModelResumeURLTempl             = "http://%s:%s/app-service/v1/models/%s/resume"
	AppServiceModelStatusURLTempl             = "http://%s:%s/app-service/v1/models/%s/status"
	AppServiceModelStatusListURLTempl         = "http://%s:%s/app-service/v1/models/status"
	AppServiceModelOperateURLTempl            = "http://%s:%s/app-service/v1/models/%s/operate"
	AppServiceModelOperateListURLTempl        = "http://%s:%s/app-service/v1/models/operate"
	AppServiceModelOperateHistoryURLTempl     = "http://%s:%s/app-service/v1/models/%s/operate_history"
	AppServiceModelOperateHistoryListURLTempl = "http://%s:%s/app-service/v1/models/operate_history"

	AppServiceMiddlewareInstallURLTempl     = "http://%s:%s/app-service/v1/middlewares/%s/install"
	AppServiceMiddlewareUninstallURLTempl   = "http://%s:%s/app-service/v1/middlewares/%s/uninstall"
	AppServiceMiddlewareStatusURLTempl      = "http://%s:%s/app-service/v1/middlewares/%s/status"
	AppServiceMiddlewareOperateURLTempl     = "http://%s:%s/app-service/v1/middlewares/%s/operate"
	AppServiceMiddlewareOperateListURLTempl = "http://%s:%s/app-service/v1/middlewares/operate"
	AppServiceMiddlewareCancelURLTempl      = "http://%s:%s/app-service/v1/middlewares/%s/cancel?type=%s"
	AppServiceMiddlewareStatusListURLTempl  = "http://%s:%s/app-service/v1/middlewares/status"

	RepoUrlTempl = "http://%s:%s/charts"

	RepoUrlHostEnv = "REPO_URL_HOST"
	RepoUrlPortEnv = "REPO_URL_PORT"

	DefaultPage     = 1
	DefaultPageSize = 100
	DefaultFrom     = 0
)

const (
	RemoveLabel  = "remove"
	SuspendLabel = "suspend"
	NsfwLabel    = "nsfw"
)

const (
	AppType        = "app"
	RecommendType  = "recommend"
	AgentType      = "agent"
	ModelType      = "model"
	MiddlewareType = "middleware"
)

const (
	AppFromMarket = "market"
	AppFromDev    = "custom"
)

var (
	ValidTypes = []string{AppType, RecommendType, AgentType, ModelType, MiddlewareType}
)

func GetAppServiceHostAndPort() (string, string) {
	return os.Getenv(AppServiceHostEnv), os.Getenv(AppServicePortEnv)
}
