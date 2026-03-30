package ingress

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
			name: "extracts HTTP URL from ingress without TLS",
			obj: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ingress",
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/",
										},
									},
								},
							},
						},
					},
				},
			},
			wantEndpoints: []endpoint.Endpoint{
				{Name: "test-ingress", URL: "http://example.com/", Host: "example.com", Path: "/"},
			},
		},
		{
			name: "extracts HTTPS URL from ingress with TLS",
			obj: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secure-ingress",
				},
				Spec: networkingv1.IngressSpec{
					TLS: []networkingv1.IngressTLS{
						{
							Hosts: []string{"secure.com"},
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "secure.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/api",
										},
									},
								},
							},
						},
					},
				},
			},
			wantEndpoints: []endpoint.Endpoint{
				{Name: "secure-ingress", URL: "https://secure.com/api", Host: "secure.com", Path: "/api"},
			},
		},
		{
			name: "returns empty for ingress without rules",
			obj: &networkingv1.Ingress{
				Spec: networkingv1.IngressSpec{},
			},
			wantEndpoints: nil,
		},
		{
			name: "extracts multiple paths",
			obj: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "multi-path",
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "multi.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path: "/v1",
										},
										{
											Path: "/v2",
										},
									},
								},
							},
						},
					},
				},
			},
			wantEndpoints: []endpoint.Endpoint{
				{Name: "multi-path", URL: "http://multi.com/v1", Host: "multi.com", Path: "/v1"},
				{Name: "multi-path", URL: "http://multi.com/v2", Host: "multi.com", Path: "/v2"},
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

func TestFilterFunc(t *testing.T) {
	cfg := &config.Config{}
	
	t.Run("no filter - allows all ingresses", func(t *testing.T) {
		ingress := &networkingv1.Ingress{}
		if !filterFunc(ingress, cfg) {
			t.Error("filterFunc() = false, want true")
		}
	})

	t.Run("filter by ingress class name matches", func(t *testing.T) {
		cfg.IngressClass = "nginx"
		className := "nginx"
		ingress := &networkingv1.Ingress{
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
			},
		}
		if !filterFunc(ingress, cfg) {
			t.Error("filterFunc() = false, want true")
		}
	})

	t.Run("non-ingress object returns false", func(t *testing.T) {
		if filterFunc(&metav1.ObjectMeta{}, cfg) {
			t.Error("filterFunc() = true, want false")
		}
	})
}

func TestDefinition(t *testing.T) {
	def := Definition()
	if def.GVR.Resource != "ingresses" {
		t.Errorf("Definition() resource = %v, want ingresses", def.GVR.Resource)
	}
	if def.EndpointExtractor == nil {
		t.Error("Definition() EndpointExtractor is nil")
	}
}
