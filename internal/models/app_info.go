package models

import (
	"encoding/json"
	"market/internal/constants"
	"market/internal/models/tapr"
	"market/pkg/utils"
	"path"
	"strings"

	"helm.sh/helm/v3/pkg/time"
)

const (
	AppUninstalled string = "uninstalled"
	NoInstalled    string = "no_installed"
	AppRunning     string = "running"
)

type ResponseD struct {
	Code int             `json:"code"`
	Msg  string          `json:"message"`
	Data json.RawMessage `json:"data,omitempty"`
}

type AppInfo struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Namespace      string     `json:"namespace"`
	DeploymentName string     `json:"deployment"`
	Owner          string     `json:"owner"`
	URL            string     `json:"url"`
	Icon           string     `json:"icon"`
	Title          string     `json:"title"`
	Target         string     `json:"target"`
	Entrances      []Entrance `json:"entrances"`
	State          string     `json:"state"`
}

type Entrance struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Icon  string `json:"icon"`
}

type EntranceFromCfg struct {
	Name      string `yaml:"name" json:"name"`
	Host      string `yaml:"host" json:"host"`
	Port      int32  `yaml:"port" json:"port"`
	Icon      string `yaml:"icon,omitempty" json:"icon,omitempty"`
	Title     string `yaml:"title" json:"title"`
	AuthLevel string `yaml:"authLevel,omitempty" json:"authLevel,omitempty"`
	Invisible bool   `yaml:"invisible,omitempty" json:"invisible,omitempty"`
}

type MyAppsResp struct {
	ResponseBase
	Data MyAppsListResp `json:"data,omitempty"`
}

type MyAppsListResp struct {
	Items  []AppInfo `json:"items"`
	Totals int       `json:"totals"`
}

type ListResultD struct {
	Items      []json.RawMessage `json:"items"`
	TotalItems int               `json:"totalItems"`
	TotalCount int64             `json:"totalCount,omitempty"`
}

type TopResultItem struct {
	Category string             `json:"category"`
	Apps     []*ApplicationInfo `json:"apps"`
}

type ApplicationInfo struct {
	Id string `yaml:"id" json:"id,omitempty"`

	Name      string `yaml:"name" json:"name"`
	CfgType   string `yaml:"cfgType" json:"cfgType"`
	ChartName string `yaml:"chartName" json:"chartName"`

	Icon        string   `yaml:"icon" json:"icon"`
	Description string   `yaml:"desc" json:"desc"`
	AppID       string   `yaml:"appid" json:"appid"`
	Title       string   `yaml:"title" json:"title"`
	CurVersion  string   `yaml:"curVersion" json:"curVersion"`
	Version     string   `yaml:"version" json:"version"`
	NeedUpdate  bool     `yaml:"needUpdate" json:"needUpdate"`
	Categories  []string `yaml:"categories" json:"categories"` //[]string
	VersionName string   `yaml:"versionName" json:"versionName"`

	FullDescription    string            `yaml:"fullDescription" json:"fullDescription"`
	UpgradeDescription string            `yaml:"upgradeDescription" json:"upgradeDescription"`
	PromoteImage       []string          `yaml:"promoteImage" json:"promoteImage"`
	PromoteVideo       string            `yaml:"promoteVideo" json:"promoteVideo"`
	SubCategory        string            `yaml:"subCategory" json:"subCategory"`
	Developer          string            `yaml:"developer" json:"developer"`
	RequiredMemory     string            `yaml:"requiredMemory" json:"requiredMemory"`
	RequiredDisk       string            `yaml:"requiredDisk" json:"requiredDisk"`
	SupportClient      SupportClient     `yaml:"supportClient" json:"supportClient"`
	SupportArch        []string          `yaml:"supportArch" json:"supportArch"`
	RequiredGPU        string            `yaml:"requiredGpu" json:"requiredGpu"`
	RequiredCPU        string            `yaml:"requiredCpu" json:"requiredCpu"`
	Rating             float32           `yaml:"rating" json:"rating"`
	Target             string            `yaml:"target" json:"target"`
	Permission         Permission        `yaml:"permission" json:"permission" description:"app permission request"`
	Entrances          []EntranceFromCfg `yaml:"entrances" json:"entrances"`
	Middleware         *tapr.Middleware  `yaml:"middleware" json:"middleware" description:"app middleware request"`
	Options            Options           `yaml:"options" json:"options" description:"app options"`

	LastCommitHash string    `yaml:"-" json:"lastCommitHash"`
	CreateTime     int64     `yaml:"-" json:"createTime"`
	UpdateTime     int64     `yaml:"-" json:"updateTime"`
	InstallTime    time.Time `json:"installTime"`

	Uid      string `json:"uid"`
	Status   string `yaml:"status" json:"status,omitempty"`
	Progress string `json:"progress,omitempty"`

	Locale       []string        `yaml:"locale" json:"locale"`
	Submitter    string          `yaml:"submitter" json:"submitter"`
	Doc          string          `yaml:"doc" json:"doc"`
	Website      string          `yaml:"website" json:"website"`
	FeatureImage string          `yaml:"featuredImage" json:"featuredImage"`
	SourceCode   string          `yaml:"sourceCode" json:"sourceCode"`
	License      []TextAndURL    `yaml:"license" json:"license"`
	Legal        []TextAndURL    `yaml:"legal" json:"legal"`
	I18n         map[string]I18n `yaml:"i18n" json:"i18n"`

	AppLabels []string `yaml:"appLabels" json:"appLabels"`

	Source    string `yaml:"source" json:"source"`
	ModelSize string `yaml:"modelSize" json:"modelSize,omitempty"`
	Namespace string `yaml:"namespace" json:"namespace,omitempty"`
	OnlyAdmin bool   `yaml:"onlyAdmin" json:"onlyAdmin,omitempty"`

	Variants map[string]ApplicationInfo `yaml:"variants" json:"variants,omitempty" bson:"variants"`
}

type ByInstallTime []*ApplicationInfo

func (a ByInstallTime) Len() int { return len(a) }
func (a ByInstallTime) Less(i, j int) bool {
	return a[i].InstallTime.After(a[j].InstallTime)
}
func (a ByInstallTime) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

type TextAndURL struct {
	Text string `yaml:"text" json:"text" bson:"text"`
	URL  string `yaml:"url" json:"url" bson:"url"`
}

type AppService struct {
	Name string `yaml:"name" json:"name" bson:"name"`
	Port int32  `yaml:"port" json:"port" bson:"port"`
}

type I18nEntrance struct {
	Name  string `yaml:"name" json:"name" bson:"name"`
	Title string `yaml:"title" json:"title" bson:"title"`
}

type I18nMetadata struct {
	Title       string `yaml:"title" json:"title" bson:"title"`
	Description string `yaml:"description" json:"description" bson:"description"`
}

type I18nSpec struct {
	FullDescription    string `yaml:"fullDescription" json:"fullDescription" bson:"fullDescription"`
	UpgradeDescription string `yaml:"upgradeDescription" json:"upgradeDescription" bson:"upgradeDescription"`
}

type I18n struct {
	Metadata  I18nMetadata   `yaml:"metadata" json:"metadata" bson:"metadata"`
	Entrances []I18nEntrance `yaml:"entrances" json:"entrances" bson:"entrances"`
	Spec      I18nSpec       `yaml:"spec" json:"spec" bson:"spec"`
}

type SupportClient struct {
	Chrome  string `yaml:"chrome" json:"chrome" bson:"chrome"`
	Edge    string `yaml:"edge" json:"edge" bson:"edge"`
	Android string `yaml:"android" json:"android" bson:"android"`
	Ios     string `yaml:"ios" json:"ios" bson:"ios"`
	Windows string `yaml:"windows" json:"windows" bson:"windows"`
	Mac     string `yaml:"mac" json:"mac" bson:"mac"`
	Linux   string `yaml:"linux" json:"linux" bson:"linux"`
}

type Permission struct {
	AppData  bool         `yaml:"appData" json:"appData"  description:"app data permission for writing"`
	AppCache bool         `yaml:"appCache" json:"appCache"`
	UserData []string     `yaml:"userData" json:"userData"`
	SysData  []SysDataCfg `yaml:"sysData" json:"sysData"  description:"system shared data permission for accessing"`
}

type SysDataCfg struct {
	Group    string   `yaml:"group" json:"group"`
	DataType string   `yaml:"dataType" json:"dataType"`
	Version  string   `yaml:"version" json:"version"`
	Ops      []string `yaml:"ops" json:"ops"`
}

type AppMetaData struct {
	Name        string   `yaml:"name" json:"name"`
	Icon        string   `yaml:"icon" json:"icon"`
	Description string   `yaml:"description" json:"description"`
	AppID       string   `yaml:"appid" json:"appid"`
	Title       string   `yaml:"title" json:"title"`
	Version     string   `yaml:"version" json:"version"`
	Categories  []string `yaml:"categories" json:"categories"`
	Rating      float32  `yaml:"rating" json:"rating"`
	Target      string   `yaml:"target" json:"target"`
}

type AppConfiguration struct {
	ConfigVersion string            `yaml:"olaresManifest.version" json:"olaresManifest.version"`
	ConfigType    string            `yaml:"olaresManifest.type" json:"olaresManifest.type"`
	Metadata      AppMetaData       `yaml:"metadata" json:"metadata"`
	Entrances     []EntranceFromCfg `yaml:"entrances" json:"entrances" bson:"entrance"`
	Spec          AppSpec           `yaml:"spec" json:"spec"`
	Permission    Permission        `yaml:"permission" json:"permission" description:"app permission request"`
	Middleware    *tapr.Middleware  `yaml:"middleware" json:"middleware" bson:"middleware" description:"app middleware request"`
	Options       Options           `yaml:"options" json:"options" bson:"options" description:"app options"`
}

type AppSpec struct {
	VersionName        string        `yaml:"versionName" json:"versionName"`
	FullDescription    string        `yaml:"fullDescription" json:"fullDescription"`
	UpgradeDescription string        `yaml:"upgradeDescription" json:"upgradeDescription"`
	PromoteImage       []string      `yaml:"promoteImage" json:"promoteImage"`
	PromoteVideo       string        `yaml:"promoteVideo" json:"promoteVideo"`
	SubCategory        string        `yaml:"subCategory" json:"subCategory"`
	Developer          string        `yaml:"developer" json:"developer"`
	RequiredMemory     string        `yaml:"requiredMemory" json:"requiredMemory"`
	RequiredDisk       string        `yaml:"requiredDisk" json:"requiredDisk"`
	SupportClient      SupportClient `yaml:"supportClient" json:"supportClient"`
	SupportArch        []string      `yaml:"supportArch" json:"supportArch"`
	RequiredGPU        string        `yaml:"requiredGpu" json:"requiredGpu"`
	RequiredCPU        string        `yaml:"requiredCpu" json:"requiredCpu"`

	Locale       []string     `yaml:"locale" json:"locale"`
	Submitter    string       `yaml:"submitter" json:"submitter"`
	Doc          string       `yaml:"doc" json:"doc"`
	Website      string       `yaml:"website" json:"website"`
	FeatureImage string       `yaml:"featuredImage" json:"featuredImage"`
	SourceCode   string       `yaml:"sourceCode" json:"sourceCode"`
	License      []TextAndURL `yaml:"license" json:"license"`
	Legal        []TextAndURL `yaml:"legal" json:"legal"`
	ModelSize    string       `yaml:"modelSize" json:"modelSize,omitempty"`
	Namespace    string       `yaml:"namespace" json:"namespace,omitempty"`
	OnlyAdmin    bool         `yaml:"onlyAdmin" json:"onlyAdmin,omitempty"`
}

type Policy struct {
	EntranceName string `yaml:"entranceName" json:"entranceName"`
	Description  string `yaml:"description" json:"description" bson:"description" description:"the description of the policy"`
	URIRegex     string `yaml:"uriRegex" json:"uriRegex" description:"uri regular expression"`
	Level        string `yaml:"level" json:"level"`
	OneTime      bool   `yaml:"oneTime" json:"oneTime"`
	Duration     string `yaml:"validDuration" json:"validDuration"`
}

type Analytics struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type Dependency struct {
	Name    string `yaml:"name" json:"name" bson:"name"`
	Version string `yaml:"version" json:"version" bson:"version"`
	// dependency type: system, application.
	Type      string `yaml:"type" json:"type" bson:"type"`
	Mandatory bool   `yaml:"mandatory" json:"mandatory" bson:"mandatory"`
	SelfRely  bool   `yaml:"selfRely json:"selfRely"`
}

type Conflict struct {
	Name string `yaml:"name" json:"name"`
	// conflict type: application
	Type string `yaml:"type" json:"type"`
}

type Options struct {
	Policies        []Policy     `yaml:"policies" json:"policies" bson:"policies"`
	Analytics       *Analytics   `yaml:"analytics" json:"analytics" bson:"analytics"`
	Dependencies    []Dependency `yaml:"dependencies" json:"dependencies" bson:"dependencies"`
	AppScope        *AppScope    `yaml:"appScope" json:"appScope"`
	WsConfig        *WsConfig    `yaml:"websocket" json:"websocket"`
	MobileSupported bool         `yaml:"mobileSupported" json:"mobileSupported,omitempty"`
	Conflicts       []Conflict   `yaml:"conflicts" json:"conflicts"`
}

type AppScope struct {
	ClusterScoped bool     `yaml:"clusterScoped" json:"clusterScoped"`
	AppRef        []string `yaml:"appRef" json:"appRef"`
}

type WsConfig struct {
	Port int    `yaml:"port" json:"port,omitempty"`
	URL  string `yaml:"url" json:"url,omitempty"`
}

func (ac *AppConfiguration) ToAppInfo() *ApplicationInfo {
	nameMd58 := utils.Md5String(ac.Metadata.Name)[:8]
	return &ApplicationInfo{
		Id:                 nameMd58,
		AppID:              nameMd58,
		CfgType:            ac.ConfigType,
		Name:               ac.Metadata.Name,
		Icon:               ac.Metadata.Icon,
		Description:        ac.Metadata.Description,
		Title:              ac.Metadata.Title,
		Version:            ac.Metadata.Version,
		Categories:         ac.Metadata.Categories,
		VersionName:        ac.Spec.VersionName,
		FullDescription:    ac.Spec.FullDescription,
		UpgradeDescription: ac.Spec.UpgradeDescription,
		PromoteImage:       ac.Spec.PromoteImage,
		PromoteVideo:       ac.Spec.PromoteVideo,
		SubCategory:        ac.Spec.SubCategory,
		Developer:          ac.Spec.Developer,
		RequiredMemory:     ac.Spec.RequiredMemory,
		RequiredDisk:       ac.Spec.RequiredDisk,
		SupportClient:      ac.Spec.SupportClient,
		SupportArch:        ac.Spec.SupportArch,
		RequiredGPU:        ac.Spec.RequiredGPU,
		RequiredCPU:        ac.Spec.RequiredCPU,
		Rating:             ac.Metadata.Rating,
		Target:             ac.Metadata.Target,
		Permission:         ac.Permission,
		Middleware:         ac.Middleware,
		Options:            ac.Options,
		Entrances:          ac.Entrances,

		Locale:       ac.Spec.Locale,
		Submitter:    ac.Spec.Submitter,
		Doc:          ac.Spec.Doc,
		Website:      ac.Spec.Website,
		FeatureImage: ac.Spec.FeatureImage,
		SourceCode:   ac.Spec.SourceCode,
		License:      ac.Spec.License,
		Legal:        ac.Spec.Legal,
		ModelSize:    ac.Spec.ModelSize,
		Namespace:    ac.Spec.Namespace,
		OnlyAdmin:    ac.Spec.OnlyAdmin,
	}
}

// Chart represents the structure of the Chart.yaml file
type Chart struct {
	APIVersion string `yaml:"apiVersion"`
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
}

func (info *ApplicationInfo) HasRemoveLabel() bool {
	return info.hasLabel(constants.RemoveLabel)
}

func (info *ApplicationInfo) HasSuspendLabel() bool {
	return info.hasLabel(constants.SuspendLabel)
}

func (info *ApplicationInfo) HasNsfwLabel() bool {
	return info.hasLabel(constants.NsfwLabel)
}

func (info *ApplicationInfo) hasLabel(labelName string) bool {
	for _, label := range info.AppLabels {
		if strings.EqualFold(label, labelName) {
			return true
		}
	}

	return false
}

func (info *ApplicationInfo) CheckUpdate(sameVersionUpdate bool) {
	if info.HasRemoveLabel() {
		info.NeedUpdate = false
		return
	}
	info.NeedUpdate = utils.NeedUpgrade(info.CurVersion, info.Version, sameVersionUpdate)
}

func (info *ApplicationInfo) GetChartLocalPath() string {
	if info.ChartName == "" {
		return ""
	}

	return path.Join(constants.ChartsLocalDir, info.ChartName)
}
