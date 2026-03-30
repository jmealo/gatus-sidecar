package ingress

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
	"github.com/home-operations/gatus-sidecar/internal/resources"
)

const (
	dnsTestURL            = "1.1.1.1"
	dnsEmptyBodyCondition = "len([BODY]) == 0"
	dnsQueryType          = "A"
)

// Definition creates a resource definition for Ingress resources
func Definition() *resources.ResourceDefinition {
	return &resources.ResourceDefinition{
		GVR: schema.GroupVersionResource{
			Group:    "networking.k8s.io",
			Version:  "v1",
			Resource: "ingresses",
		},
		TargetType:        reflect.TypeOf(networkingv1.Ingress{}),
		ConvertFunc:       resources.CreateConvertFunc(reflect.TypeOf(networkingv1.Ingress{})),
		AutoConfigFunc:    func(cfg *config.Config) bool { return cfg.AutoIngress },
		FilterFunc:        filterFunc,
		EndpointExtractor: endpointExtractor,
		ConditionFunc:     conditionFunc,
		GuardedFunc:       guardedFunc,
		ParentExtractor:   parentExtractor,
	}
}

func filterFunc(obj metav1.Object, cfg *config.Config) bool {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return false
	}

	// Check ingress class filter if configured
	if cfg.IngressClass != "" {
		return hasIngressClass(ingress, cfg.IngressClass)
	}

	return true
}

func endpointExtractor(obj metav1.Object) []*endpoint.Endpoint {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return nil
	}

	var endpoints []*endpoint.Endpoint

	for _, rule := range ingress.Spec.Rules {
		if rule.Host == "" {
			continue
		}

		protocol := determineProtocol(ingress, rule.Host)
		baseURL := rule.Host
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = fmt.Sprintf("%s://%s", protocol, baseURL)
		}

		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				p := path.Path
				if p == "" {
					p = "/"
				}
				
				endpoints = append(endpoints, &endpoint.Endpoint{
					Name: ingress.Name,
					URL:  baseURL + p,
					Host: rule.Host,
					Path: p,
				})
			}
		} else {
			// No HTTP paths defined, just use the host
			endpoints = append(endpoints, &endpoint.Endpoint{
				Name: ingress.Name,
				URL:  baseURL,
				Host: rule.Host,
				Path: "/",
			})
		}
	}

	return endpoints
}

func conditionFunc(cfg *config.Config, obj metav1.Object, e *endpoint.Endpoint) {
	e.Conditions = []string{"[STATUS] == 200"}
}

func guardedFunc(obj metav1.Object, e *endpoint.Endpoint) {
	if _, ok := obj.(*networkingv1.Ingress); ok {
		applyGuardedTemplate(e.Host, e)
	}
}

func applyGuardedTemplate(dnsQueryName string, e *endpoint.Endpoint) {
	e.URL = dnsTestURL
	e.DNS = map[string]any{
		"query-name": dnsQueryName,
		"query-type": dnsQueryType,
	}
	e.Conditions = []string{dnsEmptyBodyCondition}
}

// Helper functions

func determineProtocol(ingress *networkingv1.Ingress, hostname string) string {
	if hasTLS(ingress, hostname) {
		return "https"
	}
	return "http"
}

func hasTLS(ingress *networkingv1.Ingress, hostname string) bool {
	for _, tls := range ingress.Spec.TLS {
		if slices.Contains(tls.Hosts, hostname) {
			return true
		}
	}
	return false
}

func hasIngressClass(ingress *networkingv1.Ingress, ingressClass string) bool {
	return getIngressClass(ingress) == ingressClass
}

func getIngressClass(ingress *networkingv1.Ingress) string {
	// Check spec.ingressClassName first (preferred)
	if ingress.Spec.IngressClassName != nil {
		return *ingress.Spec.IngressClassName
	}
	// Fallback to annotation (legacy)
	if ingress.Annotations != nil {
		if class, ok := ingress.Annotations["kubernetes.io/ingress.class"]; ok {
			return class
		}
	}
	return ""
}

func parentExtractor(ctx context.Context, obj metav1.Object, client dynamic.Interface) map[string]string {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return nil
	}

	className := getIngressClass(ingress)
	if className == "" {
		return nil
	}

	gvr := schema.GroupVersionResource{
		Group:    "networking.k8s.io",
		Version:  "v1",
		Resource: "ingressclasses",
	}

	parentResource, err := client.Resource(gvr).Get(ctx, className, metav1.GetOptions{})
	if err != nil {
		return nil
	}

	return parentResource.GetAnnotations()
}
