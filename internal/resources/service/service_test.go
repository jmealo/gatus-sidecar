package service

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
)

func TestEndpointExtractor(t *testing.T) {
	tests := []struct {
		name     string
		obj      metav1.Object
		wantURL  string
		wantHost string
		wantPath string
		wantErr  bool
	}{
		{
			name: "extracts URL from service with TCP port",
			obj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
			wantURL:  "http://my-service.default.svc:80",
			wantHost: "my-service.default.svc",
			wantPath: "/",
		},
		{
			name: "extracts URL from service with HTTPS port",
			obj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secure-service",
					Namespace: "prod",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name: "https",
							Port: 443,
						},
					},
				},
			},
			wantURL:  "https://secure-service.prod.svc:443",
			wantHost: "secure-service.prod.svc",
			wantPath: "/",
		},
		{
			name: "returns nil for service without ports",
			obj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-port-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{},
			},
			wantURL: "",
		},
		{
			name: "uses first port when multiple ports",
			obj: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-port",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name: "metrics",
							Port: 9090,
						},
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
			wantURL:  "http://multi-port.default.svc:9090",
			wantHost: "multi-port.default.svc",
			wantPath: "/",
		},
		{
			name:    "returns nil for non-service object",
			obj:     &metav1.ObjectMeta{},
			wantURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoints := endpointExtractor(tt.obj)
			if tt.wantURL == "" {
				if len(endpoints) != 0 {
					t.Errorf("endpointExtractor() = %v, want nil", endpoints)
				}
				return
			}

			if len(endpoints) != 1 {
				t.Fatalf("endpointExtractor() returned %d endpoints, want 1", len(endpoints))
			}

			e := endpoints[0]
			if e.URL != tt.wantURL {
				t.Errorf("endpointExtractor() URL = %v, want %v", e.URL, tt.wantURL)
			}
			if e.Host != tt.wantHost {
				t.Errorf("endpointExtractor() Host = %v, want %v", e.Host, tt.wantHost)
			}
			if e.Path != tt.wantPath {
				t.Errorf("endpointExtractor() Path = %v, want %v", e.Path, tt.wantPath)
			}
		})
	}
}

func TestConditionFunc(t *testing.T) {
	def := Definition()
	cfg := &config.Config{}
	svc := &corev1.Service{}
	e := &endpoint.Endpoint{}

	def.ConditionFunc(cfg, svc, e)

	if len(e.Conditions) != 1 || e.Conditions[0] != "[STATUS] == 200" {
		t.Errorf("ConditionFunc() conditions = %v, want [[STATUS] == 200]", e.Conditions)
	}
}

func TestDefinition(t *testing.T) {
	def := Definition()
	if def.GVR.Resource != "services" {
		t.Errorf("Definition() resource = %v, want services", def.GVR.Resource)
	}
	if def.EndpointExtractor == nil {
		t.Error("Definition() EndpointExtractor is nil")
	}
}
