package settings

import (
	"context"
	"log"
	"strings"
	"time"

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

func setCachedSystemRemoteService(v string) {
	cachedSystemRemoteService = v
}

// StartSystemEnvWatcher starts a background informer to watch systemenvs and update cached OlaresRemoteService
func StartSystemEnvWatcher(ctx context.Context) {
	// Best-effort: if not running in cluster, just skip
	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("SystemEnv watcher: not running in cluster, skipping watcher: %v", err)
		return
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Printf("SystemEnv watcher: dynamic client create failed: %v", err)
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
			log.Printf("SystemEnv watcher: cache sync failed")
		} else {
			log.Printf("SystemEnv watcher: started")
		}
		// tie lifetime to ctx
		<-ctx.Done()
		log.Printf("SystemEnv watcher: context done, stopping")
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
	log.Printf("SystemEnv watcher: OlaresRemoteService updated from %s to %s", old, newValue)
	setCachedSystemRemoteService(newValue)
}

var _ = yaml.DefaultMetaFactory // keep yaml imported for future kubeconfig parsing if needed
