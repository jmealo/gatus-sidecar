package resources

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
)

func TestHandler_ShouldProcess(t *testing.T) {
	cfg := &config.Config{
		EnabledAnnotation: "gatus.io/enabled",
		AutoService:       false,
	}

	def := &ResourceDefinition{
		AutoConfigFunc: func(c *config.Config) bool { return c.AutoService },
	}

	h := NewHandler(def, nil)

	tests := []struct {
		name        string
		annotations map[string]string
		autoService bool
		want        bool
	}{
		{
			name:        "auto-config enabled processes all",
			annotations: nil,
			autoService: true,
			want:        true,
		},
		{
			name:        "auto-config disabled requires annotations",
			annotations: map[string]string{"gatus.io/enabled": "true"},
			autoService: false,
			want:        true,
		},
		{
			name:        "filter function can reject objects",
			annotations: map[string]string{"gatus.io/enabled": "true"},
			autoService: false,
			want:        true,
		},
		{
			name:        "no annotations and auto disabled",
			annotations: nil,
			autoService: false,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg.AutoService = tt.autoService
			obj := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}
			if got := h.ShouldProcess(obj, cfg); got != tt.want {
				t.Errorf("ShouldProcess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandler_ExtractEndpoints(t *testing.T) {
	def := &ResourceDefinition{
		EndpointExtractor: func(obj metav1.Object) []*endpoint.Endpoint {
			return []*endpoint.Endpoint{{URL: "https://extracted.com"}}
		},
	}
	h := NewHandler(def, nil)

	t.Run("returns empty slice when no extractor", func(t *testing.T) {
		h2 := NewHandler(&ResourceDefinition{}, nil)
		if got := h2.ExtractEndpoints(&corev1.Service{}); got != nil {
			t.Errorf("ExtractEndpoints() = %v, want nil", got)
		}
	})

	t.Run("extracts endpoints using custom extractor", func(t *testing.T) {
		got := h.ExtractEndpoints(&corev1.Service{})
		if len(got) != 1 || got[0].URL != "https://extracted.com" {
			t.Errorf("ExtractEndpoints() = %v, want [{URL: https://extracted.com}]", got)
		}
	})
}

func TestHandler_ApplyTemplate(t *testing.T) {
	def := &ResourceDefinition{
		ConditionFunc: func(cfg *config.Config, obj metav1.Object, e *endpoint.Endpoint) {
			e.Conditions = append(e.Conditions, "[STATUS] == 200")
		},
		GuardedFunc: func(obj metav1.Object, e *endpoint.Endpoint) {
			e.URL = "1.1.1.1"
		},
	}
	h := NewHandler(def, nil)

	t.Run("applies condition function for non-guarded endpoint", func(t *testing.T) {
		e := &endpoint.Endpoint{}
		h.ApplyTemplate(&config.Config{}, &corev1.Service{}, e)
		if len(e.Conditions) != 1 || e.Conditions[0] != "[STATUS] == 200" {
			t.Errorf("Conditions not applied correctly: %v", e.Conditions)
		}
	})

	t.Run("applies guarded function for guarded endpoint", func(t *testing.T) {
		e := &endpoint.Endpoint{Guarded: true}
		h.ApplyTemplate(&config.Config{}, &corev1.Service{}, e)
		if e.URL != "1.1.1.1" {
			t.Errorf("Guarded function not applied correctly: %v", e.URL)
		}
	})

	t.Run("no functions defined", func(t *testing.T) {
		h2 := NewHandler(&ResourceDefinition{}, nil)
		e := &endpoint.Endpoint{}
		h2.ApplyTemplate(&config.Config{}, &corev1.Service{}, e)
		// Should not panic
	})
}

func TestHandler_GetParentAnnotations(t *testing.T) {
	def := &ResourceDefinition{
		ParentExtractor: func(ctx context.Context, obj metav1.Object, client dynamic.Interface) map[string]string {
			return map[string]string{"parent": "true"}
		},
	}
	h := NewHandler(def, nil)

	t.Run("returns nil when no parent extractor", func(t *testing.T) {
		h2 := NewHandler(&ResourceDefinition{}, nil)
		if got := h2.GetParentAnnotations(context.TODO(), &corev1.Service{}); got != nil {
			t.Errorf("GetParentAnnotations() = %v, want nil", got)
		}
	})

	t.Run("calls parent extractor when defined", func(t *testing.T) {
		got := h.GetParentAnnotations(context.TODO(), &corev1.Service{})
		if got["parent"] != "true" {
			t.Errorf("GetParentAnnotations() = %v, want {parent: true}", got)
		}
	})
}

func TestCreateConvertFunc(t *testing.T) {
	t.Run("converts valid unstructured to service", func(t *testing.T) {
		convert := CreateConvertFunc(reflect.TypeOf(corev1.Service{}))
		u := resourcesToUnstructured(&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-svc",
			},
		})
		obj, err := convert(u)
		if err != nil {
			t.Fatalf("ConvertFunc failed: %v", err)
		}
		if obj.GetName() != "test-svc" {
			t.Errorf("Converted name = %v, want test-svc", obj.GetName())
		}
	})
}

func TestHasRequiredAnnotations(t *testing.T) {
	cfg := &config.Config{
		EnabledAnnotation:  "gatus.io/enabled",
		TemplateAnnotation: "gatus.io/template",
	}

	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name:        "has enabled annotation",
			annotations: map[string]string{"gatus.io/enabled": "true"},
			want:        true,
		},
		{
			name:        "has template annotation",
			annotations: map[string]string{"gatus.io/template": "{}"},
			want:        true,
		},
		{
			name:        "has both annotations",
			annotations: map[string]string{"gatus.io/enabled": "true", "gatus.io/template": "{}"},
			want:        true,
		},
		{
			name:        "no annotations",
			annotations: map[string]string{},
			want:        false,
		},
		{
			name:        "nil annotations",
			annotations: nil,
			want:        false,
		},
		{
			name:        "unrelated annotations",
			annotations: map[string]string{"other": "value"},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasRequiredAnnotations(tt.annotations, cfg); got != tt.want {
				t.Errorf("HasRequiredAnnotations() = %v, want %v", got, tt.want)
			}
		})
	}
}

func resourcesToUnstructured(obj runtime.Object) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	_ = scheme.Scheme.Convert(obj, u, nil)
	return u
}
