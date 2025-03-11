package validate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	vd "github.com/bytedance/go-tagexpr/v2/validator"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/sets"
	"github.com/golang/glog"
	"github.com/your-project/appservice"
)

func init() {
	vd.SetErrorFactory(func(failPath, msg string) error {
		return fmt.Errorf(`"validation failed: %s","msg": "%s"`, failPath, msg)
	})
}

type AppConfiguration struct {
	ConfigVersion string      `yaml:"olaresManifest.version" json:"olaresManifest.version" vd:"len($)>0;msg:sprintf('invalid parameter: %v;olaresManifest.version must satisfy the expr: len($)>0',$)"`
	Metadata      AppMetaData `yaml:"metadata" json:"metadata"`
	Entrances     []Entrance  `yaml:"entrances" json:"entrances" vd:"len($)>0 && len($)<=10;msg:sprintf('invalid parameter: %v;entrances must satisfy the expr: len($)>0 && len($)<=10',$)"`
	Spec          AppSpec     `yaml:"spec,omitempty" json:"spec,omitempty"`
	Permission    *Permission `yaml:"permission" json:"permission" vd:"?"`
	Middleware    *Middleware `yaml:"middleware,omitempty" json:"middleware,omitempty" vd:"?"`
	Options       *Options    `yaml:"options" json:"options" vd:"?"`
}

type Middleware struct {
	Postgres   *PostgresConfig   `yaml:"postgres,omitempty" json:"postgres,omitempty" vd:"?"`
	Redis      *RedisConfig      `yaml:"redis,omitempty" json:"redis,omitempty" vd:"?"`
	MongoDB    *MongodbConfig    `yaml:"mongodb,omitempty" json:"mongodb,omitempty" vd:"?"`
	ZincSearch *ZincSearchConfig `yaml:"zincSearch,omitempty" json:"zincSearch,omitempty" vd:"?"`
}

type Database struct {
	Name        string `yaml:"name" json:"name" vd:"len($)>0;msg:sprintf('invalid parameter: %v;name must satisfy the expr: len($)>0',$)"`
	Distributed bool   `yaml:"distributed,omitempty" json:"distributed,omitempty" vd:"-"`
}

type PostgresConfig struct {
	Username  string     `yaml:"username" json:"username" vd:"len($)>0;msg:sprintf('invalid parameter: %v;username must satisfy the expr: len($)>0',$)"`
	Password  string     `yaml:"password,omitempty" json:"password,omitempty" vd:"-"`
	Databases []Database `yaml:"databases" json:"databases" vd:"len($)>0;msg:sprintf('invalid parameter: %v;databases must satisfy the expr: len($)>0',$)"`
}

type RedisConfig struct {
	Password  string `yaml:"password,omitempty" json:"password,omitempty" vd:"-"`
	Namespace string `yaml:"namespace" json:"namespace" vd:"regexp('^[a-z0-9-.]{1,63}$');msg:sprintf('invalid parameter: %v;namespace must satisfy the expr: regexp(^[a-z0-9-.]{1,63}$)',$)"`
}

type MongodbConfig struct {
	Username  string     `yaml:"username" json:"username" vd:"len($)>0;msg:sprintf('invalid parameter: %v;username must satisfy the expr: len($)>0',$)"`
	Password  string     `yaml:"password,omitempty" json:"password,omitempty" vd:"-"`
	Databases []Database `yaml:"databases" json:"databases" vd:"len($)>0;msg:sprintf('invalid parameter: %v;databases must satisfy the expr: len($)>0',$)"`
}

type ZincSearchConfig struct {
	Username string  `yaml:"username" json:"username" vd:"len($)>0;msg:sprintf('invalid parameter: %v;username must satisfy the expr: len($)>0',$)"`
	Password string  `yaml:"password,omitempty" json:"password,omitempty" vd:"-"`
	Indexes  []Index `yaml:"indexes" json:"indexes" vd:"len($)>0;msg:sprintf('invalid parameter: %v;indexes must satisfy the expr: len($)>0',$)"`
}

type Index struct {
	Name string `yaml:"name" json:"name" vd:"len($)>0;msg:sprintf('invalid parameter: %v;name must satisfy the expr: len($)>0',$)"`
}
type AppMetaData struct {
	Name        string   `yaml:"name" json:"name"  vd:"len($)>0 && len($)<=30;msg:sprintf('invalid parameter: %v;name must satisfy the expr: len($)>0 && len($)<=30',$)"`
	Icon        string   `yaml:"icon" json:"icon" vd:"len($)>0;msg:sprintf('invalid parameter: %v;icon must satisfy the expr: len($)>0',$)"`
	Description string   `yaml:"description" json:"description" vd:"len($)>0;msg:sprintf('invalid parameter: %v;description must satisfy the expr: len($)>0',$)"`
	AppID       string   `yaml:"appid" json:"appid" vd:"-"`
	Title       string   `yaml:"title" json:"title" vd:"len($)>0 && len($)<=30;msg:sprintf('invalid parameter: %v;title must satisfy the expr: len($)>0 && len($)<=30',$)"`
	Version     string   `yaml:"version" json:"version" vd:"len($)>0;msg:sprintf('invalid parameter: %v;version must satisfy the expr: len($)>0',$)"`
	Categories  []string `yaml:"categories" json:"categories"`
	Rating      float32  `yaml:"rating" json:"rating" vd:"-"`
	Target      string   `yaml:"target" json:"target" vd:"-"`
}

type Entrance struct {
	Name      string `yaml:"name" json:"name" vd:"regexp('^([a-z0-9A-Z-]*)$') && len($)<=63;msg:sprintf('invalid parameter: %v;name must satisfy the expr: regexp(^([a-z0-9-]*)$)',$)"`
	Host      string `yaml:"host" json:"host" vd:"regexp('^([a-z]([-a-z0-9]*[a-z0-9]))$') && len($)<=63;msg:sprintf('invalid parameter: %v;host must satisfy the expr: regexp(^([a-z]([-a-z0-9]*[a-z0-9]))$)',$)"`
	Port      int32  `yaml:"port" json:"port" vd:"$>0;msg:sprintf('invalid parameter: %v;port must satisfy the expr: $>0',$)"`
	Icon      string `yaml:"icon" json:"icon"`
	Title     string `yaml:"title" json:"title" vd:"len($)>0 && len($)<=30 && regexp('^([a-z0-9A-Z-\\s]*)$');msg:sprintf('invalid parameter: %v;title must satisfy the expr: len($)>0 && len($)<=30 && regexp(^([a-z0-9A-Z-\\s]*)$)',$)"`
	AuthLevel string `yaml:"authLevel" json:"authLevel"`
}

type AppSpec struct {
	VersionName        string         `yaml:"versionName,omitempty" json:"versionName"`
	FullDescription    string         `yaml:"fullDescription" json:"fullDescription"`
	UpgradeDescription string         `yaml:"upgradeDescription" json:"upgradeDescription"`
	PromoteImage       []string       `yaml:"promoteImage" json:"promoteImage"`
	PromoteVideo       string         `yaml:"promoteVideo" json:"promoteVideo"`
	SubCategory        string         `yaml:"subCategory" json:"subCategory"`
	Developer          string         `yaml:"developer" json:"developer"`
	RequiredMemory     string         `yaml:"requiredMemory" json:"requiredMemory" vd:"regexp('^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$');msg:sprintf('invalid parameter: %v;requiredMemory must satisfy the expr: regexp(^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$)',$)"`
	RequiredDisk       string         `yaml:"requiredDisk" json:"requiredDisk"     vd:"regexp('^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$');msg:sprintf('invalid parameter: %v;requiredDisk must satisfy the expr: regexp(^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$)',$)"`
	SupportClient      *SupportClient `yaml:"supportClient" json:"supportClient" vd:"?"`
	SupportArch        []string       `yaml:"supportArch" json:"supportArch"`
	RequiredGPU        string         `yaml:"requiredGpu" json:"requiredGpu" vd:"len($)==0 || regexp('^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$');msg:sprintf('invalid parameter: %v;requiredGpu must satisfy the expr: len($) == 0 || regexp(^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$)',$)"`
	RequiredCPU        string         `yaml:"requiredCpu" json:"requiredCpu" vd:"regexp('^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$');msg:sprintf('invalid parameter: %v;requiredCpu must satisfy the expr: regexp(^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$)',$)"`
	LimitedMemory      string         `yaml:"limitedMemory" json:"limitedMemory" vd:"regexp('^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$');msg:sprintf('invalid parameter: %v;limitedMemory must satisfy the expr: regexp(^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$)',$)"`
	LimitedCPU         string         `yaml:"limitedCpu" json:"limitedCpu" vd:"regexp('^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$');msg:sprintf('invalid parameter: %v;limitedCpu must satisfy the expr: regexp(^(?:\\d+(?:\\.\\d+)?(?:[eE][-+]?(\\d+|i))?(?:[kKMGTP]?i?|[mMGTPE])?|[kKMGTP]i|[mMGTPE])$)',$)"`
}

type SupportClient struct {
	Edge    string `yaml:"edge" json:"edge"`
	Android string `yaml:"android" json:"android"`
	Ios     string `yaml:"ios" json:"ios"`
	Windows string `yaml:"windows" json:"windows"`
	Mac     string `yaml:"mac" json:"mac"`
	Linux   string `yaml:"linux" json:"linux"`
}

type Permission struct {
	AppData  bool         `yaml:"appData" json:"appData"`
	AppCache bool         `yaml:"appCache" json:"appCache"`
	UserData []string     `yaml:"userData" json:"userData"`
	SysData  []SysDataCfg `yaml:"sysData" json:"sysData"`
}

type SysDataCfg struct {
	Group    string   `yaml:"group" json:"group" vd:"len($)>0;msg:sprintf('invalid parameter: %v;group must satisfy the expr: len($)>0',$)"`
	DataType string   `yaml:"dataType" json:"dataType" vd:"len($)>0;msg:sprintf('invalid parameter: %v;dataType must satisfy the expr: len($)>0',$)"`
	Version  string   `yaml:"version" json:"version" vd:"len($)>0;msg:sprintf('invalid parameter: %v;version must satisfy the expr: len($)>0',$)"`
	Ops      []string `yaml:"ops" json:"ops" vd:"len($)>0;msg:sprintf('invalid parameter: %v;ops must satisfy the expr: len($)>0',$)"`
}

type Policy struct {
	Description string `yaml:"description" json:"description" vd:"-"`
	URIRegex    string `yaml:"uriRegex" json:"uriRegex" vd:"len($)>0;msg:sprintf('invalid parameter: %v;uriRegex must satisfy the expr: len($)>0',$)"`
	Level       string `yaml:"level" json:"level" vd:"len($)>0;msg:sprintf('invalid parameter: %v;level must satisfy the expr: len($)>0',$)"`
	OneTime     bool   `yaml:"oneTime" json:"oneTime"`
	Duration    string `yaml:"validDuration" json:"validDuration" vd:"len($)==0 ||regexp('^((?:[-+]?\\d+(?:\\.\\d+)?([smhdwy]|us|ns|ms))+)$');msg:sprintf('invalid parameter: %v;validDuration must satisfy the expr: regexp(^((?:[-+]?\\d+(?:\\.\\d+)?([smhdwy]|us|ns|ms))+)$)',$)"`
}

type Options struct {
	Policies     *[]Policy     `yaml:"policies" json:"policies" vd:"?"`
	Analytics    *Analytics    `yaml:"analytics" json:"analytics" vd:"?"`
	Dependencies *[]Dependency `yaml:"dependencies" json:"dependencies" vd:"?"`
}

type Analytics struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type Dependency struct {
	Name    string `yaml:"name" json:"name" vd:"len($)>0;msg:sprintf('invalid parameter: %v;name must satisfy the expr: len($)>0',$)"`
	Version string `yaml:"version" json:"version" vd:"len($)>0;msg:sprintf('invalid parameter: %v;version must satisfy the expr: len($)>0',$)"`
	// dependency type: system, application.
	Type string `yaml:"type" json:"type" vd:"$=='system' || $=='application' || $=='middleware';msg:sprintf('invalid parameter: %v;type must satisfy the expr: $==system || $==application',$)"`
}

type Mappings struct {
	Properties map[string]Property `json:"properties,omitempty"`
}

type Property struct {
	Type           string `json:"type"` // text, keyword, date, numeric, boolean, geo_point
	Analyzer       string `json:"analyzer,omitempty"`
	SearchAnalyzer string `json:"search_analyzer,omitempty"`
	Format         string `json:"format,omitempty"`    // date format yyyy-MM-dd HH:mm:ss || yyyy-MM-dd || epoch_millis
	TimeZone       string `json:"time_zone,omitempty"` // date format time_zone
	Index          bool   `json:"index"`
	Store          bool   `json:"store"`
	Sortable       bool   `json:"sortable"`
	Aggregatable   bool   `json:"aggregatable"`
	Highlightable  bool   `json:"highlightable"`

	Fields map[string]Property `json:"fields,omitempty"`
}

func checkZincSearchMappings(f io.Reader) error {
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	var m Mappings
	err = json.Unmarshal(data, &m)
	if err != nil {
		return err
	}
	return nil
}

func getAppConfigFromCfg(f io.ReadCloser, token string) (*AppConfiguration, error) {
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	
	// 添加渲染逻辑
	renderedContent, err := appservice.RenderManifest(string(data), token)
	if err != nil {
		glog.Warningf("render manifest failed: %s", err.Error())
		return nil, err
	}
	
	var cfg AppConfiguration
	if err := yaml.Unmarshal([]byte(renderedContent), &cfg); err != nil {
		glog.Warningf("YAML parsing failed, error message: %v", err)
		return nil, fmt.Errorf("configuration file parsing error: %w", err)
	}
	return &cfg, nil
}

func getAppConfigFromCfgFile(chartPath string, token string) (*AppConfiguration, error) {
	if !strings.HasSuffix(chartPath, "/") {
		chartPath += "/"
	}
	f, err := os.Open(chartPath + "OlaresManifest.yaml")
	if err != nil {
		return nil, err
	}
	return getAppConfigFromCfg(f, token)
}

func CheckAppCfg(chartPath string, token string) error {
	cfg, err := getAppConfigFromCfgFile(chartPath, token)
	if err != nil {
		return err
	}
	return checkAppCfg(cfg, chartPath)
}

func checkAppCfg(cfg *AppConfiguration, chartPath string) error {
	err := vd.Validate(cfg)
	if err != nil {
		return err
	}
	//err = CheckAppEntrances(cfg)
	//if err != nil {
	//	return err
	//}
	err = CheckSupportedArch(cfg)
	if err != nil {
		return err
	}
	err = CheckZincIndexes(cfg, chartPath)
	if err != nil {
		return err
	}
	err = CheckAppData(cfg, chartPath)
	if err != nil {
		return err
	}
	return CheckResourceLimit(chartPath, cfg)
}

func CheckSupportedArch(cfg *AppConfiguration) error {
	if len(cfg.Spec.SupportArch) == 0 {
		return errors.New("spec.SupportArch can not be empty")
	}
	// allSupportedArch := sets.String{"amd64": sets.Empty{}, "arm32v5": sets.Empty{}, "arm32v6": sets.Empty{},
	// 	"arm32v7": sets.Empty{}, "arm64v8": sets.Empty{}, "i386": sets.Empty{}, "ppc64le": sets.Empty{},
	// 	"s390x": sets.Empty{}, "mips64le": sets.Empty{}, "riscv64": sets.Empty{}, "windows-amd64": sets.Empty{}}
	allSupportedArch := sets.String{"amd64": sets.Empty{}, "arm64": sets.Empty{}}
	for _, arch := range cfg.Spec.SupportArch {
		if !allSupportedArch.Has(arch) {
			return fmt.Errorf("unsupport arch: %s", arch)
		}
	}
	return nil
}

func CheckAppEntrances(cfg *AppConfiguration) error {
	setsEntrance := sets.String{}
	setsName := sets.String{}
	for i, e := range cfg.Entrances {
		entrance := fmt.Sprintf("%s:%d", e.Host, e.Port)
		if setsEntrance.Has(entrance) {
			return fmt.Errorf("entrances:[%d] has replicated(entrance with same name and port were treat as same)", i)
		}
		setsEntrance.Insert(entrance)

		if setsName.Has(e.Name) {
			return fmt.Errorf("entrances:[%d] name has replicated", i)
		}
		setsName.Insert(e.Name)
	}
	return nil
}

func CheckZincIndexes(cfg *AppConfiguration, chartPath string) error {
	if !strings.HasSuffix(chartPath, "/") {
		chartPath += "/"
	}
	var err error
	names := sets.String{}

	// check zincSearch index name whether replicated
	if cfg.Middleware != nil && cfg.Middleware.ZincSearch != nil {
		for _, index := range cfg.Middleware.ZincSearch.Indexes {
			if names.Has(index.Name) {
				return fmt.Errorf("zinc index name: %s has replicated", index.Name)
			}
			names.Insert(index.Name)
		}
	}

	if cfg.Middleware != nil && cfg.Middleware.ZincSearch != nil {
		for _, index := range cfg.Middleware.ZincSearch.Indexes {
			err = func() error {
				f, e := os.Open(chartPath + index.Name + ".json")
				if e != nil {
					return e
				}
				defer f.Close()
				e = checkZincSearchMappings(f)
				return e
			}()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func CheckAppData(cfg *AppConfiguration, chartPath string) error {
	if cfg.Permission != nil && cfg.Permission.AppCache {
		return nil
	}
	if !strings.HasSuffix(chartPath, "/") {
		chartPath += "/"
	}
	chartPath += "templates"
	p, err := regexp.Compile(`\.Values\.userspace\.appCache`)
	if err != nil {
		return err
	}
	var rerr error
	err = filepath.Walk(chartPath, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && strings.HasSuffix(path, ".yaml") {
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			defer f.Close()
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				if p.MatchString(scanner.Text()) {
					rerr = fmt.Errorf("found .Values.userspace.appCache in %s, but not set permission.appCache in OlaresManifest.yaml", filepath.Base(path))
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return rerr
}
