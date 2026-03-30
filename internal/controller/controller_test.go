package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/dynamic/dynamicinformer"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/resources/service"
	"github.com/home-operations/gatus-sidecar/internal/state"
)

func TestController_Informer(t *testing.T) {
	scheme := runtime.NewScheme()
	gvr := service.Definition().GVR
	dc := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		gvr: "ServiceList",
	})

	// Create a fake service
	svc := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]any{
				"name":      "test-svc",
				"namespace": "default",
				"annotations": map[string]any{
					"gatus.io/enabled": "true",
				},
			},
			"spec": map[string]any{
				"ports": []any{
					map[string]any{
						"port": int64(80),
					},
				},
			},
		},
	}

	_, err := dc.Resource(service.Definition().GVR).Namespace("default").Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create fake service: %v", err)
	}

	tempFile := t.TempDir() + "/gatus.yaml"
	sm := state.NewManager(tempFile)
	cfg := &config.Config{
		Namespace:           "default",
		EnabledAnnotation:   "gatus.io/enabled",
		DefaultInterval:     time.Minute,
		DefaultNameTemplate: "{{.Name}}",
	}

	informerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dc, 0)
	c := New(service.Definition(), sm, dc, informerFactory, "default")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	go c.Run(ctx, cfg)

	// Give it a moment to process the initial list from the informer
	time.Sleep(200 * time.Millisecond)

	// Force a write to verify state
	sm.ForceWrite()

	// Verify state manager has the endpoint
	if len(sm.GetCurrentState()) == 0 {
		t.Errorf("Expected 1 endpoint in state, got 0")
	}
}
