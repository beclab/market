package validate

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	vd "github.com/bytedance/go-tagexpr/v2/validator"
	"github.com/stretchr/testify/assert"
)

const metaAndEntrance = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
 
`
const metaAndEntranceWithoutMeta = `terminusManifest.version: v1
entrances:
- name: nginx-service
  port: 80
  title: nginx
`
const metaAndEntranceWithoutEntrance = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
`

const appSpec = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
`

const appCfgWithPermission = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
Permission:
 appData: true
 sysData:
 - group: apps/v1
   dataType: sys
   version: v1
   ops:
   - get
`
const appCfgWithPermissionWithEmptyOps = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
Permission:
 appData: true
 sysData:
 - group: apps/v1
   dataType: sys
   version: v1
`
const appCfgWithMiddleware = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
middleware:
 postgres:
   username: postgres
   databases:
   - name: db
     distributed: true
 redis:
   username: redis
   databases:
   - name: db0
 mongodb:
   username: mongodb
   databases:
   - name: db0
   - name: db1
 zincSearch:
   username: zinc
   indexes:
   - name: index0
   - name: index0
`

const appCfgWithMiddlewareWithMultiRedisDb = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
middleware:
 postgres:
   username: postgres
   databases:
   - name: db
     distributed: true
 redis:
   username: redis
   databases:
   - name: db0
   - name: db1
 mongodb:
   username: mongodb
   databases:
   - name: db0
   - name: db1
 zincSearch:
   username: zinc
   indexes:
   - name: index0
   - name: index0
`

const appCfgWithOptionsWithPolicy = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
options:
 policies:
 - uriRegex: "/$"
   level: "two_factor"
   oneTime: false
   validDuration: "3600s"
`
const appCfgWithOptionsWithPolicyDuration1 = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
options:
 policies:
 - uriRegex: "/$"
   level: "two_factor"
   oneTime: false
   validDuration: "1h1h"
`
const appCfgWithOptionsWithPolicyDuration2 = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
options:
 policies:
 - uriRegex: "/$"
   level: "two_factor"
   oneTime: false
   validDuration: "-1h5ms5us8ns"
`

const appCfgWithOptionsWithPolicyDuration3 = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
options:
 policies:
 - uriRegex: "/$"
   level: "two_factor"
   oneTime: false
   validDuration: "100"
`

const appCfgWithOptionsWithAnalytics = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
options:
 analytics:
   enabled: true
`

const appCfgWithOptionsWithDependencies = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
options:
 dependencies:
 - name: terminus
   version: 0.3.6
   type: system
 - name: seafile
   version: 0.0.1
   type: application
`

const appCfgWithOptionsWithDependenciesErrorType = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
options:
 dependencies:
 - name: terminus
   version: 0.3.6
   type: system
 - name: seafile
   version: 0.0.1
   type: applications
`

const appCfgWithAll = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
Permission:
 appData: true
 sysData:
 - group: apps/v1
   dataType: sys
   version: v1
   ops:
   - get
middleware:
 postgres:
   username: postgres
   databases:
   - name: db
     distributed: true
 redis:
   username: redis
   databases:
   - name: db0
 mongodb:
   username: mongodb
   databases:
   - name: db0
   - name: db1
 zincSearch:
   username: zinc
   indexes:
   - name: index0
   - name: index0
options:
 policies:
 - uriRegex: "/$"
   level: "two_factor"
   oneTime: false
   validDuration: "3600s"
 analytics:
   enabled: true
 dependencies:
 - name: terminus
   version: 0.3.6
   type: system
 - name: seafile
   version: 0.0.1
   type: application
`

const appCfgWith11Entrances = `terminusManifest.version: v1
metadata:
 name: nginx
 description: "n8n is an extendable workflow automation tool."
 icon: https://file.bttcdn.com/appstore/n8n/icon.png
 appid: nginx
 version: 0.0.1
 title: n8n
 categories:
 - utils
entrances:
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
- name: nginx-service
  port: 80
  title: nginx
spec:
  versionName: v1
  requiredMemory: 64Mi
  requiredDisk: 128Mi
  requiredCpu: 250m
  limitedMemory: 8Gi
  limitedCpu: 500m
`

func TestGetAppConfigFromCfg(t *testing.T) {

	testCases := []struct {
		cfg      string
		expected error
	}{
		{
			metaAndEntrance,
			nil,
		},
		{
			metaAndEntranceWithoutMeta,
			errors.New("invalid parameter: Metadata.Name	invalid parameter: Metadata.Icon	invalid parameter: Metadata.Description	invalid parameter: Metadata.Title	invalid parameter: Metadata.Version"),
		},
		{
			metaAndEntranceWithoutEntrance,
			errors.New("invalid parameter: Entrances: length must be great than 0 and less than 10"),
		},
		{
			appSpec,
			nil,
		},
		{
			appCfgWithPermission,
			nil,
		},
		{
			appCfgWithPermissionWithEmptyOps,
			errors.New("invalid parameter: Permission.SysData[0].Ops"),
		},
		{
			appCfgWithMiddleware,
			nil,
		},
		{
			appCfgWithMiddlewareWithMultiRedisDb,
			errors.New("invalid parameter: Middleware.Redis.Databases: length must be equal 1"),
		},
		{
			appCfgWithOptionsWithPolicy,
			nil,
		},
		{
			appCfgWithOptionsWithAnalytics,
			nil,
		},
		{
			appCfgWithOptionsWithDependencies,
			nil,
		},
		{
			appCfgWithOptionsWithDependenciesErrorType,
			errors.New("invalid parameter: Options.Dependencies[1].Type"),
		},
		{
			appCfgWithAll,
			nil,
		},
		{
			appCfgWithOptionsWithPolicyDuration1,
			nil,
		},
		{
			appCfgWithOptionsWithPolicyDuration2,
			nil,
		},
		{
			appCfgWithOptionsWithPolicyDuration3,
			errors.New("invalid parameter: Options.Policies[0].Duration"),
		},
		{
			appCfgWith11Entrances,
			errors.New("invalid parameter: Entrances: length must be great than 0 and less than 10"),
		},
	}
	for _, test := range testCases {
		f := io.NopCloser(strings.NewReader(test.cfg))
		cfg, err := getAppConfigFromCfg(f)
		if err != nil {
			t.Fatal(err)
		}
		err = vd.Validate(cfg, true)
		if err == nil {
			if err != test.expected {
				t.Errorf("want: %s, but got: %s", test.expected, err)
			}
		} else if err != nil && err.Error() != test.expected.Error() {
			t.Errorf("want: %s, but got: %s", test.expected.Error(), err.Error())
		}
	}

}

func TestChartAppCfg(t *testing.T) {
	cfg, err := getAppConfigFromCfgFile("./testdata/nextcloud2")
	if err != nil {
		t.Fatal(err)
	}
	err = vd.Validate(cfg, true)
	err = checkAppCfg(cfg, "./testdata/nextcloud2")
	if err != nil {
		t.Error("TerminusManifest.yaml validate failed", err)
	}
}

func TestCheckAppCfg(t *testing.T) {
	chartPath := "./testdata/calibre"
	cfg, err := getAppConfigFromCfgFile(chartPath)
	if err != nil {
		t.Fatal(err)
	}
	err = CheckAppData(cfg, chartPath)
	assert.NotNil(t, err)

}

func TestCheckAppEntrances(t *testing.T) {
	chartPath := "./testdata/calibre"
	cfg, err := getAppConfigFromCfgFile(chartPath)
	if err != nil {
		t.Fatal(err)
	}
	err = CheckAppEntrances(cfg)
	assert.Equal(t, fmt.Errorf("entrances:[%d] has replicated(entrance with same name and port were treat as same)", 1), err)
}

func TestCheckZincIndexes(t *testing.T) {
	chartPath := "./testdata/nextcloud2"
	cfg, err := getAppConfigFromCfgFile(chartPath)
	if err != nil {
		t.Fatal(err)
	}
	err = CheckZincIndexes(cfg, chartPath)
	assert.NotNil(t, err)
}
