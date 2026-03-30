package resources

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
)

// ResourceHandler defines the interface for processing specific Kubernetes resources
type ResourceHandler interface {
	ShouldProcess(obj metav1.Object, cfg *config.Config) bool
	ExtractEndpoints(obj metav1.Object) []*endpoint.Endpoint
	ApplyTemplate(cfg *config.Config, obj metav1.Object, e *endpoint.Endpoint)
	GetParentAnnotations(ctx context.Context, obj metav1.Object) map[string]string
}

type resourceHandler struct {
	definition    *ResourceDefinition
	dynamicClient dynamic.Interface
}

// NewHandler creates a new resource handler
func NewHandler(definition *ResourceDefinition, dynamicClient dynamic.Interface) ResourceHandler {
	return &resourceHandler{
		definition:    definition,
		dynamicClient: dynamicClient,
	}
}

func (h *resourceHandler) ShouldProcess(obj metav1.Object, cfg *config.Config) bool {
	// Check if auto-config is enabled for this resource type
	if h.definition.AutoConfigFunc != nil && h.definition.AutoConfigFunc(cfg) {
		// If auto-config is enabled, still allow the filter function to reject objects
		if h.definition.FilterFunc != nil {
			return h.definition.FilterFunc(obj, cfg)
		}
		return true
	}

	// Otherwise, check if the required annotations are present
	if !HasRequiredAnnotations(obj.GetAnnotations(), cfg) {
		return false
	}

	// Still apply filter function if present
	if h.definition.FilterFunc != nil {
		return h.definition.FilterFunc(obj, cfg)
	}

	return true
}

func (h *resourceHandler) ExtractEndpoints(obj metav1.Object) []*endpoint.Endpoint {
	if h.definition.EndpointExtractor != nil {
		return h.definition.EndpointExtractor(obj)
	}
	return nil
}

func (h *resourceHandler) ApplyTemplate(cfg *config.Config, obj metav1.Object, e *endpoint.Endpoint) {
	// Apply base conditions
	if h.definition.ConditionFunc != nil {
		h.definition.ConditionFunc(cfg, obj, e)
	}

	// Apply guarded settings if applicable
	if e.Guarded && h.definition.GuardedFunc != nil {
		h.definition.GuardedFunc(obj, e)
	}
}

func (h *resourceHandler) GetParentAnnotations(ctx context.Context, obj metav1.Object) map[string]string {
	if h.definition.ParentExtractor != nil {
		return h.definition.ParentExtractor(ctx, obj, h.dynamicClient)
	}
	return nil
}
