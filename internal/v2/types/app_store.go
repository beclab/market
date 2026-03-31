package types

// + AppStoreInfo
type AppStoreInfo struct {
	Data        AppStoreInfoData  `json:"data"`
	Hash        string            `json:"hash"`
	LastUpdated string            `json:"last_updated"`
	Stats       AppStoreInfoStats `json:"stats"`
	Version     string            `json:"version"`
}

type AppStoreInfoData struct {
	Apps       map[string]AppStoreInfoDataApps       `json:"apps"` // id:{}
	Latest     []string                              `json:"latest"`
	Pages      map[string]AppStoreInfoDataPages      `json:"pages"` // title:{}
	Recommends map[string]AppStoreInfoDataRecommends `json:"recommends"`
	Tags       map[string]AppStoreInfoDataTags       `json:"tags"`
	TopicLists map[string]AppStoreInfoDataTopicLists `json:"topic_lists"`
	Topics     map[string]AppStoreInfoDataTopics     `json:"topics"`
	Tops       []AppStoreInfoDataTops                `json:"tops"`
}

type AppStoreInfoDataApps struct {
	Id          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Category    string      `json:"category"`
	Description string      `json:"description"`
	Icon        string      `json:"icon"`
	Screenshots interface{} `json:"screenshots"`
	Tags        interface{} `json:"tags"`
	Metadata    interface{} `json:"metadata"`
	Source      int         `json:"source"`
	UpdatedAt   string      `json:"updated_at"`
}

type AppStoreInfoDataPages struct {
	Category  string `json:"category"`
	Content   string `json:"content"`
	Source    int    `json:"source"`
	UpdatedAt string `json:"updated_at"`
	CreatedAt string `json:"createdAt"`
}

type AppStoreInfoDataRecommends struct {
	Name        string                         `json:"name"`
	Description string                         `json:"description"`
	Content     string                         `json:"content"`
	Data        AppStoreInfoDataRecommendsData `json:"data"`
	Source      int                            `json:"source"`
	UpdatedAt   string                         `json:"updated_at"`
	CreatedAt   string                         `json:"createdAt"`
}

type AppStoreInfoDataRecommendsData struct {
	Title       map[string]string `json:"title"`
	Description map[string]string `json:"description"`
}

type AppStoreInfoDataTags struct {
	Id        string            `json:"_id"`
	Name      string            `json:"name"`
	Title     map[string]string `json:"title"`
	Icon      string            `json:"icon"`
	Sort      int               `json:"sort"`
	Source    int               `json:"source"`
	UpdatedAt string            `json:"updated_at"`
	CreatedAt string            `json:"createdAt"`
}

type AppStoreInfoDataTopicLists struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Content     string            `json:"content"`
	Title       map[string]string `json:"title"`
	Source      int               `json:"source"`
	UpdatedAt   string            `json:"updated_at"`
	CreatedAt   string            `json:"createdAt"`
}

type AppStoreInfoDataTopics struct {
	Id        string                                `json:"_id"`
	Name      string                                `json:"name"`
	Data      map[string]AppStoreInfoDataTopicsData `json:"data"`
	Source    int                                   `json:"source"`
	UpdatedAt string                                `json:"updated_at"`
	CreatedAt string                                `json:"createdAt"`
}

type AppStoreInfoDataTopicsData struct {
	Group           string `json:"group"`
	Title           string `json:"title"`
	Des             string `json:"des"`
	IconImg         string `json:"iconimg"`
	DetailImg       string `json:"detailimg"`
	RichText        string `json:"richtext"`
	Apps            string `json:"apps"`
	IsDelete        bool   `json:"isdelete"`
	MobileDetailImg string `json:"mobileDetailImg"`
	MobileRichtext  string `json:"mobileRichtext"`
	BackgroundColor string `json:"backgroundColor"`
}

type AppStoreInfoDataTops struct {
	AppId string `json:"appId"`
	Rank  int    `json:"rank"`
}

type AppStoreInfoStats struct {
	AdminData    AppStoreInfoStatsItem `json:"admin_data"`
	AppstoreData AppStoreInfoStatsItem `json:"appstore_data"`
	LastUpdated  string                `json:"last_updated"`
}

type AppStoreInfoStatsItem struct {
	Apps       int `json:"apps"`
	Pages      int `json:"pages"`
	Recommends int `json:"recommends"`
	TagFilters int `json:"tag_filters"`
	Tags       int `json:"tags"`
	TopicLists int `json:"topic_lists"`
	Topics     int `json:"topics"`
}

// + AppStoreDetail
type AppStoreDetail struct {
	Apps    map[string]AppStoreDetailApps `json:"apps"`
	Version string                        `json:"version"`
}

type AppStoreDetailApps struct {
	Id                 string                 `json:"id"`
	Name               string                 `json:"name"`
	CfgType            string                 `json:"cfgType"`
	ChartName          string                 `json:"chartName"`
	Icon               string                 `json:"icon"`
	Description        string                 `json:"description"`
	AppId              string                 `json:"appID"`
	Title              string                 `json:"title"`
	Version            string                 `json:"version"`
	Categories         []string               `json:"categories"`
	VersionName        string                 `json:"versionName"`
	FullDescription    string                 `json:"fullDescription"`
	UpgradeDescription string                 `json:"upgradeDescription"`
	PromoteImage       interface{}            `json:"promoteImage"`
	PromoteVideo       interface{}            `json:"promoteVideo"`
	SubCategory        string                 `json:"subCategory"`
	Locale             []string               `json:"locale"`
	Developer          string                 `json:"developer"`
	RequiredMemory     string                 `json:"requiredMemory"`
	RequiredDisk       string                 `json:"requiredDisk"`
	SupportClient      map[string]string      `json:"supportClient"`
	SupportArch        []string               `json:"supportArch"`
	RequiredCPU        string                 `json:"requiredCPU"`
	Rating             int                    `json:"rating"`
	Target             string                 `json:"target"`
	Permission         map[string]interface{} `json:"permission"`
	Entrances          []interface{}          `json:"entrances"`
	Middleware         map[string]interface{} `json:"middleware"`
	Options            map[string]interface{} `json:"options"`
	Submitter          string                 `json:"submitter"`
	Doc                string                 `json:"doc"`
	WebSite            string                 `json:"website"`
	FeaturedImage      string                 `json:"featuredImage"`
	SourceCode         string                 `json:"sourceCode"`
	License            []interface{}          `json:"license"`
	Legal              interface{}            `json:"legal"`
	I18n               map[string]interface{} `json:"i18n"`
	Namespace          string                 `json:"namespace"`
	OnlyAdmin          bool                   `json:"onlyAdmin"`
	LastCommitHash     string                 `json:"lastCommitHash"`
	CreateTime         int64                  `json:"createTime"`
	UpdateTime         int64                  `json:"updateTime"`
	AppLabels          []string               `json:"appLabels"`
	Count              interface{}            `json:"count"`
	VersionHistory     []interface{}          `json:"versionHistory"`
	Screenshots        interface{}            `json:"screenshots"`
	Tags               interface{}            `json:"tags"`
	Metadata           interface{}            `json:"metadata"`
	UpdatedAt          string                 `json:"updated_at"`
}
