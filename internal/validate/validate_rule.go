package validate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/thoas/go-funk"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	helmLoader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/kube"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	RoleBinding        = "RoleBinding"
	Role               = "Role"
	ClusterRole        = "ClusterRole"
	ServiceAccount     = "ServiceAccount"
	ClusterRoleBinding = "ClusterRoleBinding"
	Deployment         = "Deployment"
	StatefulSet        = "StatefulSet"
	DaemonSet          = "DaemonSet"
)

type PolicyRules struct {
	Rules []rbacv1.PolicyRule `json:"rules" yaml:"rules"`
}

func VerbMatches(rule *rbacv1.PolicyRule, requestedVerbs []string) bool {
	if len(rule.Verbs) == 0 && len(requestedVerbs) != 0 {
		return true
	}
	if len(requestedVerbs) == 0 {
		return true
	}
	for _, ruleVerb := range rule.Verbs {
		if ruleVerb == rbacv1.VerbAll {
			return true
		}
		for _, requestedVerb := range requestedVerbs {
			if funk.Contains(rule.Verbs, requestedVerb) {
				return true
			}
		}
	}
	return false
}

func APIGroupMatches(rule *rbacv1.PolicyRule, requestedGroups []string) bool {

	if len(rule.APIGroups) == 0 && len(requestedGroups) != 0 {
		return true
	}
	if len(requestedGroups) == 0 {
		return true
	}
	for _, ruleGroup := range rule.APIGroups {
		if ruleGroup == rbacv1.APIGroupAll {
			return true
		}

		for _, requestedGroup := range requestedGroups {
			if funk.Contains(rule.APIGroups, requestedGroup) {
				return true
			}
		}
	}
	return false
}

func ResourceMatches(rule *rbacv1.PolicyRule, requestedSources []string) bool {
	if len(rule.Resources) == 0 && len(requestedSources) != 0 {
		return true
	}
	if len(requestedSources) == 0 {
		return true
	}
	for _, ruleResource := range rule.Resources {
		if ruleResource == rbacv1.ResourceAll {
			return true
		}
		for _, requestedSource := range requestedSources {
			if funk.Contains(rule.Resources, requestedSource) {
				return true
			}
		}
	}
	return false
}

func ResourceNameMatches(rule *rbacv1.PolicyRule, requestedNames []string) bool {
	if len(rule.ResourceNames) == 0 {
		return true
	}
	if len(requestedNames) == 0 {
		return true
	}
	for _, requestedName := range requestedNames {
		if funk.Contains(rule.ResourceNames, requestedName) {
			return true
		}
	}
	return false
}

func NonResourceURLMatches(rule *rbacv1.PolicyRule, requestedURLs []string) bool {
	if len(rule.NonResourceURLs) == 0 && len(requestedURLs) != 0 {
		return false
	}
	for _, ruleURL := range rule.NonResourceURLs {
		if ruleURL == rbacv1.NonResourceAll {
			return true
		}
		for _, requestedURL := range requestedURLs {
			if !funk.Contains(rule.NonResourceURLs, requestedURL) {
				if !(strings.HasSuffix(ruleURL, "*") && strings.HasPrefix(requestedURL, strings.TrimRight(ruleURL, "*"))) {
					return false
				}
			}
		}

	}

	return true
}

func RuleAllows(request *rbacv1.PolicyRule, rule *rbacv1.PolicyRule) bool {
	if len(request.NonResourceURLs) == 0 {
		return VerbMatches(rule, request.Verbs) &&
			APIGroupMatches(rule, request.APIGroups) &&
			ResourceMatches(rule, request.Resources) &&
			ResourceNameMatches(rule, request.ResourceNames)
	}

	return VerbMatches(rule, request.Verbs) &&
		NonResourceURLMatches(rule, request.NonResourceURLs)
}

func RulesAllow(request *rbacv1.PolicyRule, rules ...rbacv1.PolicyRule) bool {
	for i := range rules {
		if !RuleAllows(request, &rules[i]) {
			return true
		}
	}
	return false
}

func getRulesFromFile(f io.ReadCloser) (*PolicyRules, error) {
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rules PolicyRules
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return &rules, nil
}

// checkRule if rule in attrs found in rules return false.
func checkRule(attrs []rbacv1.PolicyRule, rules []rbacv1.PolicyRule) bool {
	for _, attr := range attrs {
		if RulesAllow(&attr, rules...) {
			return true
		}
	}
	return false
}

func actionConfig() (*action.Configuration, error) {
	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, err
	}
	configuration := action.Configuration{
		Releases:       storage.Init(driver.NewMemory()),
		KubeClient:     &kubefake.FailingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: ioutil.Discard}},
		Capabilities:   chartutil.DefaultCapabilities,
		RegistryClient: registryClient,
	}
	return &configuration, nil
}

func InitAction() (*action.Install, error) {
	config, err := actionConfig()
	if err != nil {
		return nil, err
	}
	instAction := action.NewInstall(config)
	instAction.Namespace = "spaced"
	instAction.ReleaseName = "test-release"
	instAction.DryRun = true
	return instAction, nil
}

func getChart(instAction *action.Install, filepath string) (*chart.Chart, error) {
	cp, err := instAction.ChartPathOptions.LocateChart(filepath, &cli.EnvSettings{})
	if err != nil {
		return nil, err
	}
	p := getter.All(&cli.EnvSettings{})
	chartRequested, err := helmLoader.Load(cp)
	if err != nil {
		return nil, err
	}
	if req := chartRequested.Metadata.Dependencies; req != nil {
		if err := action.CheckDependencies(chartRequested, req); err != nil {
			if instAction.DependencyUpdate {
				man := &downloader.Manager{
					ChartPath:  cp,
					Keyring:    instAction.ChartPathOptions.Keyring,
					SkipUpdate: false,
					Getters:    p,
				}
				if err := man.Update(); err != nil {
					return nil, err
				}
				// Reload the chart with the updated Chart.lock file.
				if chartRequested, err = helmLoader.Load(cp); err != nil {
					return nil, err
				}
			} else {

			}
		}
	}
	return chartRequested, nil
}

func getResourceListFromChart(chartPath string) (resources kube.ResourceList, err error) {
	instAction, err := InitAction()
	if err != nil {
		return nil, err
	}
	instAction.Namespace = "app-namespace"
	chartRequested, err := getChart(instAction, chartPath)

	// fake values for helm dry run
	values := make(map[string]interface{})
	values["oidc"] = map[string]interface{}{
		"client": map[string]interface{}{},
		"issuer": "issuer",
	}
	values["nats"] = map[string]interface{}{
		"subjects": map[string]interface{}{},
		"refs":     map[string]interface{}{},
	}
	values["bfl"] = map[string]interface{}{
		"username": "bfl-username",
	}
	values["user"] = map[string]interface{}{
		"zone": "user-zone",
	}
	values["schedule"] = map[string]interface{}{
		"nodeName": "node",
	}
	values["userspace"] = map[string]interface{}{
		"appCache": "appcache",
		"userData": "userspace/Home",
	}
	values["os"] = map[string]interface{}{
		"appKey":    "appKey",
		"appSecret": "appSecret",
	}
	cfg, err := getAppConfigFromCfgFile(chartPath)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]interface{})
	for _, e := range cfg.Entrances {
		entries[e.Name] = "random-string"
	}

	values["domain"] = entries
	values["dep"] = map[string]interface{}{}
	values["postgres"] = map[string]interface{}{
		"databases": map[string]interface{}{},
	}
	values["redis"] = map[string]interface{}{}
	values["mongodb"] = map[string]interface{}{
		"databases": map[string]interface{}{},
	}
	values["zinc"] = map[string]interface{}{
		"indexes": map[string]interface{}{},
	}
	values["svcs"] = map[string]interface{}{}
	values["cluster"] = map[string]interface{}{}

	ret, err := instAction.RunWithContext(context.Background(), chartRequested, values)
	if err != nil {
		return nil, err
	}
	var metadataAccessor = meta.NewAccessor()
	d := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(ret.Manifest), 4096)
	for {
		ext := runtime.RawExtension{}
		if err := d.Decode(&ext); err != nil {
			if err == io.EOF {
				return resources, err
			}
			return nil, fmt.Errorf("error parsing")
		}
		ext.Raw = bytes.TrimSpace(ext.Raw)
		if len(ext.Raw) == 0 || bytes.Equal(ext.Raw, []byte("null")) {
			continue
		}
		obj, _, err := unstructured.UnstructuredJSONScheme.Decode(ext.Raw, nil, nil)
		if err != nil {
			return nil, err
		}
		name, _ := metadataAccessor.Name(obj)
		namespace, _ := metadataAccessor.Namespace(obj)
		info := &resource.Info{
			Namespace: namespace,
			Name:      name,
			Object:    obj,
		}
		resources = append(resources, info)
	}
}

func EnsureFileExists(filepath string) error {
	_, err := os.Stat(filepath)
	if err != nil {
		return err
	}
	return nil
}

func checkServiceAccountRule(resources kube.ResourceList, rules []rbacv1.PolicyRule) bool {
	set := make(map[string]struct{})
	for _, r := range resources {
		kind := r.Object.GetObjectKind().GroupVersionKind().Kind
		if kind == ClusterRoleBinding {
			roleBinding := rbacv1.ClusterRoleBinding{}
			err := scheme.Scheme.Convert(r.Object, &roleBinding, nil)
			if err != nil {
				fmt.Println(err)
			}
			for _, sub := range roleBinding.Subjects {
				if sub.Kind == ServiceAccount {
					set[roleBinding.RoleRef.Name] = struct{}{}
				}
			}
		}
		if kind == RoleBinding {
			roleBinding := rbacv1.RoleBinding{}
			err := scheme.Scheme.Convert(r.Object, &roleBinding, nil)
			if err != nil {
				fmt.Println(err)
			}
			for _, sub := range roleBinding.Subjects {
				if sub.Kind == ServiceAccount {
					set[roleBinding.RoleRef.Name] = struct{}{}
				}
			}
		}
	}
	var requestedRules []rbacv1.PolicyRule
	for _, r := range resources {
		kind := r.Object.GetObjectKind().GroupVersionKind().Kind
		if kind == Role {
			role := rbacv1.Role{}
			err := scheme.Scheme.Convert(r.Object, &role, nil)
			if err != nil {
				fmt.Println(err)
			}
			if _, ok := set[role.Name]; ok {
				requestedRules = append(requestedRules, role.Rules...)
			}
		}
		if kind == ClusterRole {
			role := rbacv1.ClusterRole{}
			err := scheme.Scheme.Convert(r.Object, &role, nil)
			if err != nil {
				fmt.Println(err)
			}
			if _, ok := set[role.Name]; ok {
				requestedRules = append(requestedRules, role.Rules...)
			}
		}
	}
	if len(requestedRules) == 0 {
		return true
	}
	return checkRule(requestedRules, rules)
}

func CheckServiceAccountRole(chartPath string) error {
	resources, err := getResourceListFromChart(chartPath)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	f := io.NopCloser(strings.NewReader(RULES))

	rules, err := getRulesFromFile(f)
	if err != nil {
		return err
	}
	allowed := checkServiceAccountRule(resources, rules.Rules)
	if !allowed {
		return fmt.Errorf("please check service account role rules,ensure not in %+v", rules.Rules)
	}
	return nil
}
