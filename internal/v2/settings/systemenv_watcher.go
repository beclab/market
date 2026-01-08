package settings

import (
	"context"
	"fmt"
	"market/internal/v2/utils"
	"strings"
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// GVR for systemenv CRD
var systemEnvGVR = schema.GroupVersionResource{
	Group:    "sys.bytetrade.io",
	Version:  "v1alpha1",
	Resource: "systemenvs",
}

var (
	// cached raw remote service base (without trailing /market)
	cachedSystemRemoteService string
)

func getCachedSystemRemoteService() string {
	return cachedSystemRemoteService
}

// GetCachedSystemRemoteService returns the cached SystemRemoteService base URL
func GetCachedSystemRemoteService() string {
	return cachedSystemRemoteService
}

func setCachedSystemRemoteService(v string) {
	cachedSystemRemoteService = v
}

// StartSystemEnvWatcher starts a background informer to watch systemenvs and update cached OlaresRemoteService
func StartSystemEnvWatcher(ctx context.Context) {
	// Skip in public environment (no systemenvs CRD available)
	if utils.IsPublicEnvironment() {
		glog.Info("SystemEnv watcher: public environment detected, skipping watcher")
		return
	}

	// Best-effort: if not running in cluster, just skip
	cfg, err := rest.InClusterConfig()
	if err != nil {
		glog.Errorf("SystemEnv watcher: not running in cluster, skipping watcher: %v", err)
		return
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		glog.Errorf("SystemEnv watcher: dynamic client create failed: %v", err)
		return
	}

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dyn, 2*time.Minute)
	informer := factory.ForResource(systemEnvGVR).Informer()

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { handleSystemEnv(obj) },
		UpdateFunc: func(_, newObj interface{}) { handleSystemEnv(newObj) },
		DeleteFunc: func(obj interface{}) {},
	}
	informer.AddEventHandler(handler)

	// run informer in background
	go func() {
		stopCh := make(chan struct{})
		defer close(stopCh)
		go factory.Start(stopCh)
		// wait for sync
		if !cache.WaitForCacheSync(stopCh, informer.HasSynced) {
			glog.Error("SystemEnv watcher: cache sync failed")
		} else {
			glog.V(3).Info("SystemEnv watcher: started")
		}
		// tie lifetime to ctx
		<-ctx.Done()
		glog.V(3).Info("SystemEnv watcher: context done, stopping")
	}()
}

func handleSystemEnv(obj interface{}) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok || u == nil {
		return
	}

	m := u.Object
	if m == nil {
		return
	}

	// SystemEnv inlines EnvVarSpec keys at top level: envName, value, default
	if envName, ok := m["envName"].(string); !ok || envName == "" {
		return
	} else if strings.ToUpper(envName) != "OLARES_SYSTEM_REMOTE_SERVICE" {
		return
	}

	var newValue string
	if v, ok := m["value"].(string); ok && v != "" {
		newValue = v
	} else if d, ok := m["default"].(string); ok && d != "" {
		newValue = d
	}

	if newValue == "" {
		return
	}

	// Normalize by trimming trailing slash
	newValue = strings.TrimSuffix(newValue, "/")

	old := getCachedSystemRemoteService()
	if old == newValue {
		return
	}
	glog.V(2).Infof("SystemEnv watcher: OlaresRemoteService updated from %s to %s", old, newValue)
	setCachedSystemRemoteService(newValue)
}

var _ = yaml.DefaultMetaFactory // keep yaml imported for future kubeconfig parsing if needed

// WaitForSystemRemoteService blocks until cachedSystemRemoteService is set by the watcher, or timeout occurs
func WaitForSystemRemoteService(ctx context.Context, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()

	for {
		if v := getCachedSystemRemoteService(); v != "" {
			glog.V(3).Infof("SystemEnv watcher: OlaresRemoteService ready: %s", v)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timeout waiting for OlaresRemoteService from systemenv CRD")
		case <-ticker.C:
			// keep waiting
		}
	}
}
