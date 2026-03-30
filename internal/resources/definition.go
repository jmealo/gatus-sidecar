package resources

import (
	"context"
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
)

// ResourceDefinition contains the configuration for a specific Kubernetes resource type
type ResourceDefinition struct {
	GVR               schema.GroupVersionResource
	TargetType        reflect.Type
	ConvertFunc       func(*unstructured.Unstructured) (metav1.Object, error)
	AutoConfigFunc    func(*config.Config) bool
	FilterFunc        func(metav1.Object, *config.Config) bool
	EndpointExtractor func(metav1.Object) []*endpoint.Endpoint
	ConditionFunc     func(*config.Config, metav1.Object, *endpoint.Endpoint)
	GuardedFunc       func(metav1.Object, *endpoint.Endpoint)
	ParentExtractor   func(context.Context, metav1.Object, dynamic.Interface) map[string]string
}

// CreateConvertFunc returns a function that converts an unstructured object to the target type
func CreateConvertFunc(targetType reflect.Type) func(*unstructured.Unstructured) (metav1.Object, error) {
	return func(u *unstructured.Unstructured) (metav1.Object, error) {
		obj := reflect.New(targetType).Interface()
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj)
		if err != nil {
			return nil, fmt.Errorf("from unstructured: %w", err)
		}
		return obj.(metav1.Object), nil
	}
}

// HasRequiredAnnotations checks if the object has the required annotations
func HasRequiredAnnotations(annotations map[string]string, cfg *config.Config) bool {
	if annotations == nil {
		return false
	}

	enabled, ok := annotations[cfg.EnabledAnnotation]
	if ok && (enabled == "true" || enabled == "1") {
		return true
	}

	_, ok = annotations[cfg.TemplateAnnotation]
	return ok
}
