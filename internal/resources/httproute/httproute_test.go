package httproute

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
)

func TestEndpointExtractor(t *testing.T) {
	tests := []struct {
		name          string
		obj           metav1.Object
		wantEndpoints []endpoint.Endpoint
	}{
		{
			name: "extracts HTTPS URL from HTTPRoute with hostname",
			obj: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-route",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					Hostnames: []gatewayv1.Hostname{"example.com"},
					Rules: []gatewayv1.HTTPRouteRule{
						{
							Matches: []gatewayv1.HTTPRouteMatch{
								{
									Path: &gatewayv1.HTTPPathMatch{
										Value: ptr("/api"),
									},
								},
							},
						},
					},
				},
			},
			wantEndpoints: []endpoint.Endpoint{
				{Name: "test-route", URL: "https://example.com/api", Host: "example.com", Path: "/api"},
			},
		},
		{
			name: "returns nil for HTTPRoute without hostnames",
			obj: &gatewayv1.HTTPRoute{
				Spec: gatewayv1.HTTPRouteSpec{
					Hostnames: []gatewayv1.Hostname{},
				},
			},
			wantEndpoints: nil,
		},
		{
			name: "extracts multiple hostnames and paths",
			obj: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "multi-host",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					Hostnames: []gatewayv1.Hostname{"a.com", "b.com"},
					Rules: []gatewayv1.HTTPRouteRule{
						{
							Matches: []gatewayv1.HTTPRouteMatch{
								{
									Path: &gatewayv1.HTTPPathMatch{
										Value: ptr("/v1"),
									},
								},
							},
						},
					},
				},
			},
			wantEndpoints: []endpoint.Endpoint{
				{Name: "multi-host", URL: "https://a.com/v1", Host: "a.com", Path: "/v1"},
				{Name: "multi-host", URL: "https://b.com/v1", Host: "b.com", Path: "/v1"},
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

func ptr(s string) *string {
	return &s
}

func TestFilterFunc(t *testing.T) {
	cfg := &config.Config{}
	
	t.Run("no filter - allows all routes", func(t *testing.T) {
		route := &gatewayv1.HTTPRoute{}
		if !filterFunc(route, cfg) {
			t.Error("filterFunc() = false, want true")
		}
	})

	t.Run("non-HTTPRoute object returns false", func(t *testing.T) {
		if filterFunc(&metav1.ObjectMeta{}, cfg) {
			t.Error("filterFunc() = true, want false")
		}
	})
}

func TestDefinition(t *testing.T) {
	def := Definition()
	if def.GVR.Resource != "httproutes" {
		t.Errorf("Definition() resource = %v, want httproutes", def.GVR.Resource)
	}
	if def.EndpointExtractor == nil {
		t.Error("Definition() EndpointExtractor is nil")
	}
}
