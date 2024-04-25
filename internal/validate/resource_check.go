package validate

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"helm.sh/helm/v3/pkg/kube"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/scheme"
)

func checkResourceLimit(resources kube.ResourceList, cfg *AppConfiguration) error {
	errs := make([]error, 0)
	rcpu, _ := resource.ParseQuantity(cfg.Spec.RequiredCPU)
	rmemory, _ := resource.ParseQuantity(cfg.Spec.RequiredMemory)
	lcpu, _ := resource.ParseQuantity(cfg.Spec.LimitedCPU)
	lmemory, _ := resource.ParseQuantity(cfg.Spec.LimitedMemory)

	appRequiredCPU := rcpu.AsApproximateFloat64()
	appRequiredMemory := rmemory.AsApproximateFloat64()
	appLimitedCPU := lcpu.AsApproximateFloat64()
	appLimitedMemory := lmemory.AsApproximateFloat64()

	if appRequiredCPU > appLimitedCPU {
		errs = append(errs, fmt.Errorf("spec.requiredCpu should less than spec.limitedCpu"))
	}

	if appRequiredMemory > appLimitedMemory {
		errs = append(errs, fmt.Errorf("spec.requiredMemory should less than spec.limitedMemeory"))
	}

	limitCPU, limitMemory := float64(0), float64(0)

	for _, r := range resources {
		kind := r.Object.GetObjectKind().GroupVersionKind().Kind
		if kind == Deployment {
			var deployment v1.Deployment
			err := scheme.Scheme.Convert(r.Object, &deployment, nil)
			if err != nil {
				return err
			}
			for _, c := range deployment.Spec.Template.Spec.Containers {
				requests := c.Resources.Requests

				if requests.Memory().IsZero() {
					errs = append(errs, fmt.Errorf("deployment: %s, container: %s must set memory request", deployment.Name, c.Name))
				}
				if requests.Cpu().IsZero() {
					errs = append(errs, fmt.Errorf("deployment: %s, container: %s must set cpu request", deployment.Name, c.Name))
				}
				limits := c.Resources.Limits
				if limits.Memory().IsZero() {
					errs = append(errs, fmt.Errorf("deployment: %s, container: %s must set memory limit", deployment.Name, c.Name))
				} else {
					limitMemory += limits.Memory().AsApproximateFloat64()
				}
				if limits.Cpu().IsZero() {
					errs = append(errs, fmt.Errorf("deployment: %s, container: %s must set cpu limit", deployment.Name, c.Name))
				} else {
					limitCPU += limits.Cpu().AsApproximateFloat64()
				}
			}
		}
		if kind == StatefulSet {
			var sts v1.StatefulSet
			err := scheme.Scheme.Convert(r.Object, &sts, nil)
			if err != nil {
				return err
			}
			for _, c := range sts.Spec.Template.Spec.Containers {
				requests := c.Resources.Requests
				if requests.Memory().IsZero() {
					errs = append(errs, fmt.Errorf("statefulset: %s, container: %s must set memory request", sts.Name, c.Name))
				}
				if requests.Cpu().IsZero() {
					errs = append(errs, fmt.Errorf("statefulset: %s, container: %s must set cpu request", sts.Name, c.Name))
				}
				limits := c.Resources.Limits
				if limits.Memory().IsZero() {
					errs = append(errs, fmt.Errorf("statefulset: %s, container: %s must set memory limit", sts.Name, c.Name))
				} else {
					limitMemory += limits.Memory().AsApproximateFloat64()
				}
				if limits.Cpu().IsZero() {
					errs = append(errs, fmt.Errorf("statefulset: %s, container: %s must set cpu limit", sts.Name, c.Name))
				} else {
					limitCPU += limits.Cpu().AsApproximateFloat64()
				}
			}
		}
	}
	if limitCPU > appLimitedCPU {
		errs = append(errs, fmt.Errorf("sum of all containers resources limits cpu should less than TerminusManifest.yaml spec.limitedCpu"))
	}
	if limitMemory > appLimitedMemory {
		errs = append(errs, fmt.Errorf("sum of all containers resources limits memory should less than TerminusManifest.yaml spec.limitedMemory"))
	}
	return AggregateErr(errs)
}

func CheckResourceLimit(chartPath string, cfg *AppConfiguration) error {
	resources, err := getResourceListFromChart(chartPath)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	err = checkResourceLimit(resources, cfg)
	return err
}

func checkResourceNamespace(resources kube.ResourceList) error {
	errs := make([]error, 0)
	for _, r := range resources {
		kind := r.Object.GetObjectKind().GroupVersionKind().Kind
		if kind == Deployment || kind == StatefulSet || kind == DaemonSet {
			if r.Namespace != "app-namespace" {
				err := fmt.Errorf("illegal namespace: %s for %s, name %s", r.Namespace, kind, r.Name)
				errs = append(errs, err)
			}
		} else {
			if r.Namespace != "app-namespace" && !strings.HasPrefix(r.Namespace, "user-system-") {
				err := fmt.Errorf("illegal namespace: %s for %s, name %s", r.Namespace, kind, r.Name)
				errs = append(errs, err)
			}
		}
	}
	return AggregateErr(errs)
}
