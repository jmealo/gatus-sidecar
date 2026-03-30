package httproute

import (
	"context"
	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
	"github.com/home-operations/gatus-sidecar/internal/resources"
)

const (
	dnsTestURL            = "1.1.1.1"
	dnsEmptyBodyCondition = "len([BODY]) == 0"
	dnsQueryType          = "A"
)

// Definition creates a resource definition for HTTPRoute resources
func Definition() *resources.ResourceDefinition {
	return &resources.ResourceDefinition{
		GVR: schema.GroupVersionResource{
			Group:    "gateway.networking.k8s.io",
			Version:  "v1",
			Resource: "httproutes",
		},
		TargetType:        reflect.TypeOf(gatewayv1.HTTPRoute{}),
		ConvertFunc:       resources.CreateConvertFunc(reflect.TypeOf(gatewayv1.HTTPRoute{})),
		AutoConfigFunc:    func(cfg *config.Config) bool { return cfg.AutoHTTPRoute },
		FilterFunc:        filterFunc,
		EndpointExtractor: endpointExtractor,
		ConditionFunc:     conditionFunc,
		GuardedFunc:       guardedFunc,
		ParentExtractor:   parentExtractor,
	}
}

func filterFunc(obj metav1.Object, cfg *config.Config) bool {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		return false
	}

	// Check gateway filter if configured
	if cfg.GatewayName != "" {
		return referencesGateway(route, cfg.GatewayName)
	}

	return true
}

func endpointExtractor(obj metav1.Object) []*endpoint.Endpoint {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		return nil
	}

	var endpoints []*endpoint.Endpoint

	// If no hostnames are defined, we can't really extract a URL
	if len(route.Spec.Hostnames) == 0 {
		return nil
	}

	for _, hostname := range route.Spec.Hostnames {
		h := string(hostname)
		baseURL := h
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "https://" + baseURL
		}

		for _, rule := range route.Spec.Rules {
			if len(rule.Matches) == 0 {
				// No matches means all paths (effectively /)
				endpoints = append(endpoints, &endpoint.Endpoint{
					Name: route.Name,
					URL:  baseURL + "/",
					Host: h,
					Path: "/",
				})
				continue
			}

			for _, match := range rule.Matches {
				p := "/"
				if match.Path != nil && match.Path.Value != nil {
					p = *match.Path.Value
				}
				
				endpoints = append(endpoints, &endpoint.Endpoint{
					Name: route.Name,
					URL:  baseURL + p,
					Host: h,
					Path: p,
				})
			}
		}
	}

	return endpoints
}

func conditionFunc(cfg *config.Config, obj metav1.Object, e *endpoint.Endpoint) {
	e.Conditions = []string{"[STATUS] == 200"}
}

func guardedFunc(obj metav1.Object, e *endpoint.Endpoint) {
	if _, ok := obj.(*gatewayv1.HTTPRoute); ok {
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

func parentExtractor(ctx context.Context, obj metav1.Object, client dynamic.Interface) map[string]string {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok || len(route.Spec.ParentRefs) == 0 {
		return nil
	}

	parent := route.Spec.ParentRefs[0]
	if parent.Kind != nil && *parent.Kind != "Gateway" {
		return nil
	}

	gvr := schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "gateways",
	}
	if parent.Group != nil {
		gvr.Group = string(*parent.Group)
	}

	namespace := route.GetNamespace()
	if parent.Namespace != nil {
		namespace = string(*parent.Namespace)
	}

	parentResource, err := client.Resource(gvr).Namespace(namespace).Get(ctx, string(parent.Name), metav1.GetOptions{})
	if err != nil {
		return nil
	}

	return parentResource.GetAnnotations()
}

// Helper functions

func referencesGateway(route *gatewayv1.HTTPRoute, gatewayName string) bool {
	for _, parent := range route.Spec.ParentRefs {
		if parent.Name == gatewayv1.ObjectName(gatewayName) {
			return true
		}
	}
	return false
}
