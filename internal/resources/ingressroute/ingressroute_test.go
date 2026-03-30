package ingressroute

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/home-operations/gatus-sidecar/internal/endpoint"
)

func TestEndpointExtractor(t *testing.T) {
	tests := []struct {
		name          string
		obj           metav1.Object
		wantEndpoints []endpoint.Endpoint
	}{
		{
			name: "extracts HTTP URL from IngressRoute without TLS",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"name": "test-ingressroute",
					},
					"spec": map[string]any{
						"routes": []any{
							map[string]any{
								"match": "Host(`example.com`)",
							},
						},
					},
				},
			},
			wantEndpoints: []endpoint.Endpoint{
				{Name: "test-ingressroute", URL: "http://example.com/", Host: "example.com", Path: "/"},
			},
		},
		{
			name: "extracts HTTPS URL from IngressRoute with TLS",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"name": "secure-ingressroute",
					},
					"spec": map[string]any{
						"tls": map[string]any{},
						"routes": []any{
							map[string]any{
								"match": "Host(`secure.com`)",
							},
						},
					},
				},
			},
			wantEndpoints: []endpoint.Endpoint{
				{Name: "secure-ingressroute", URL: "https://secure.com/", Host: "secure.com", Path: "/"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointExtractor(tt.obj)
			if len(got) != len(tt.wantEndpoints) {
				t.Fatalf("endpointExtractor() returned %d endpoints, want %d", len(got), len(tt.wantEndpoints))
			}
			for i, e := range got {
				if e.URL != tt.wantEndpoints[i].URL {
					t.Errorf("Endpoint[%d] URL = %v, want %v", i, e.URL, tt.wantEndpoints[i].URL)
				}
				if e.Host != tt.wantEndpoints[i].Host {
					t.Errorf("Endpoint[%d] Host = %v, want %v", i, e.Host, tt.wantEndpoints[i].Host)
				}
				if e.Path != tt.wantEndpoints[i].Path {
					t.Errorf("Endpoint[%d] Path = %v, want %v", i, e.Path, tt.wantEndpoints[i].Path)
				}
			}
		})
	}
}

func TestDefinition(t *testing.T) {
	def := Definition()
	if def.GVR.Resource != "ingressroutes" {
		t.Errorf("Definition() resource = %v, want ingressroutes", def.GVR.Resource)
	}
	if def.EndpointExtractor == nil {
		t.Error("Definition() EndpointExtractor is nil")
	}
}
