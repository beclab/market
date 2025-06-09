package validate

import (
	"errors"
	"io"
	"testing"

	"gotest.tools/v3/assert"
	rbacv1 "k8s.io/api/rbac/v1"
)

func TestVerbMatches(t *testing.T) {
	testCases := []struct {
		rule           *rbacv1.PolicyRule
		requestedVerbs []string
		expected       bool
	}{
		{
			rule:           &rbacv1.PolicyRule{Verbs: []string{"get", "create"}},
			requestedVerbs: []string{"get", "create"},
			expected:       true,
		},
		{
			rule:           &rbacv1.PolicyRule{Verbs: []string{"get", "create"}},
			requestedVerbs: []string{"get", "create", "delete"},
			expected:       true,
		},
		{
			rule:           &rbacv1.PolicyRule{Verbs: []string{}},
			requestedVerbs: []string{"get"},
			expected:       true,
		},
		{
			rule:           &rbacv1.PolicyRule{Verbs: []string{"get"}},
			requestedVerbs: []string{},
			expected:       true,
		},
	}
	for _, test := range testCases {
		assert.Equal(t, test.expected, VerbMatches(test.rule, test.requestedVerbs))
	}
}

func TestAPIGroupMatches(t *testing.T) {
	testCases := []struct {
		rule               *rbacv1.PolicyRule
		requestedAPIGroups []string
		expected           bool
	}{
		{
			rule:               &rbacv1.PolicyRule{APIGroups: []string{"apps", "apis"}},
			requestedAPIGroups: []string{"apps", "apis"},
			expected:           true,
		},
		{
			rule:               &rbacv1.PolicyRule{APIGroups: []string{"apps", "apis"}},
			requestedAPIGroups: []string{"apps", "apis", "ext"},
			expected:           true,
		},
		{
			rule:               &rbacv1.PolicyRule{APIGroups: []string{}},
			requestedAPIGroups: []string{"apps"},
			expected:           true,
		},
		{
			rule:               &rbacv1.PolicyRule{APIGroups: []string{}},
			requestedAPIGroups: []string{},
			expected:           true,
		},
	}
	for _, test := range testCases {
		assert.Equal(t, test.expected, APIGroupMatches(test.rule, test.requestedAPIGroups))
	}
}

func TestResourceMatches(t *testing.T) {
	testCases := []struct {
		rule               *rbacv1.PolicyRule
		requestedResources []string
		expected           bool
	}{
		{
			rule:               &rbacv1.PolicyRule{Resources: []string{"pods", "deployments"}},
			requestedResources: []string{"pods", "deployments"},
			expected:           true,
		},
		{
			rule:               &rbacv1.PolicyRule{Resources: []string{"pods", "deployments"}},
			requestedResources: []string{"pods", "deployments", "daemonset"},
			expected:           true,
		},
		{
			rule:               &rbacv1.PolicyRule{Resources: []string{}},
			requestedResources: []string{"pods"},
			expected:           true,
		},
		{
			rule:               &rbacv1.PolicyRule{Resources: []string{}},
			requestedResources: []string{},
			expected:           true,
		},
	}
	for _, test := range testCases {
		assert.Equal(t, test.expected, ResourceMatches(test.rule, test.requestedResources))
	}
}

func TestResourceNameMatches(t *testing.T) {
	testCases := []struct {
		rule                   *rbacv1.PolicyRule
		requestedResourceNames []string
		expected               bool
	}{
		{
			rule:                   &rbacv1.PolicyRule{ResourceNames: []string{"pods", "deployments"}},
			requestedResourceNames: []string{"pods", "deployments"},
			expected:               true,
		},
		{
			rule:                   &rbacv1.PolicyRule{ResourceNames: []string{"pods", "deployments"}},
			requestedResourceNames: []string{"pods", "deployments", "daemonset"},
			expected:               true,
		},
		{
			rule:                   &rbacv1.PolicyRule{ResourceNames: []string{}},
			requestedResourceNames: []string{"pods"},
			expected:               true,
		},
		{
			rule:                   &rbacv1.PolicyRule{ResourceNames: []string{}},
			requestedResourceNames: []string{},
			expected:               true,
		},
	}
	for _, test := range testCases {
		assert.Equal(t, test.expected, ResourceNameMatches(test.rule, test.requestedResourceNames))
	}
}

func TestNonResourceURLMatches(t *testing.T) {
	testCases := []struct {
		rule                     *rbacv1.PolicyRule
		requestedNonResourceURLs []string
		expected                 bool
	}{
		{
			rule:                     &rbacv1.PolicyRule{NonResourceURLs: []string{"apis"}},
			requestedNonResourceURLs: []string{"apis"},
			expected:                 true,
		},
		{
			rule:                     &rbacv1.PolicyRule{NonResourceURLs: []string{"apis"}},
			requestedNonResourceURLs: []string{"apis", "healthz"},
			expected:                 false,
		},
		{
			rule:                     &rbacv1.PolicyRule{NonResourceURLs: []string{}},
			requestedNonResourceURLs: []string{"apis"},
			expected:                 false,
		},
		{
			rule:                     &rbacv1.PolicyRule{NonResourceURLs: []string{}},
			requestedNonResourceURLs: []string{},
			expected:                 true,
		},
	}
	for _, test := range testCases {
		assert.Equal(t, test.expected, NonResourceURLMatches(test.rule, test.requestedNonResourceURLs))
	}
}

func TestCheckRule(t *testing.T) {
	testCases := []struct {
		requestRules []rbacv1.PolicyRule
		rules        []rbacv1.PolicyRule
		expected     bool
	}{
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"*"},
					APIGroups: []string{"*"},
					Resources: []string{"*"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"*"},
					APIGroups: []string{"*"},
					Resources: []string{"*"},
				},
			},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"apis"},
					Resources: []string{"deployments"},
				}},
			expected: true,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"*"},
					APIGroups: []string{"*"},
					Resources: []string{"*"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				}},
			expected: true,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "create"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"core"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				}},
			expected: true,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"*"},
					APIGroups: []string{"*"},
					Resources: []string{"*"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				}},
			expected: true,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "create"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"core"},
					Resources: []string{"pods"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"apis"},
					Resources: []string{"pods"},
				}},
			expected: true,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"*"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"*"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: true,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create", "update", "patch", "deletecollection", "delete"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: true,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create", "update", "patch", "deletecollection", "delete"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"update"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create", "update", "patch", "deletecollection", "delete"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"patch"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create", "update", "patch", "deletecollection", "delete"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"deletecollection"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create", "update", "patch", "deletecollection", "delete"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"delete"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create", "update", "patch", "deletecollection", "delete"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: false,
		},
		{
			requestRules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "create"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				},
			},
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create", "update", "patch", "deletecollection", "delete"},
					APIGroups: []string{"*"},
					Resources: []string{"networkpolicies"},
				}},
			expected: false,
		},
	}
	for _, test := range testCases {
		r := checkRule(test.requestRules, test.rules)
		assert.Equal(t, r, test.expected)
	}
}

func TestGetResourceListFromChart(t *testing.T) {
	_, err := getResourceListFromChart("./testdata/calibre")
	if err != nil && !errors.Is(err, io.EOF) {
		t.Error("render chart error ", err)
	}
}

func TestCheckServiceAccountRole(t *testing.T) {
	err := CheckServiceAccountRole("./testdata/calibre")
	if err == nil {
		t.Error("check failed")
	}
}

func TestCheckServiceAccountRole2(t *testing.T) {
	err := CheckServiceAccountRole("./testdata/openllm")
	if err != nil {
		t.Error("check failed")
	}
}
