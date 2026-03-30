package ingressroute

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
	"github.com/home-operations/gatus-sidecar/internal/resources"
)

// Definition creates a resource definition for IngressRoute resources
func Definition() *resources.ResourceDefinition {
	return &resources.ResourceDefinition{
		GVR: schema.GroupVersionResource{
			Group:    "traefik.containo.us",
			Version:  "v1alpha1",
			Resource: "ingressroutes",
		},
		TargetType:        reflect.TypeOf(unstructured.Unstructured{}),
		ConvertFunc:       convertFunc,
		AutoConfigFunc:    func(cfg *config.Config) bool { return cfg.AutoIngressRoute },
		EndpointExtractor: endpointExtractor,
		ConditionFunc:     conditionFunc,
		GuardedFunc:       guardedFunc,
	}
}

func convertFunc(u *unstructured.Unstructured) (metav1.Object, error) {
	return u, nil
}

func endpointExtractor(obj metav1.Object) []*endpoint.Endpoint {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		return nil
	}

	routes, ok := spec["routes"].([]any)
	if !ok {
		return nil
	}

	var endpoints []*endpoint.Endpoint

	protocol := "http"
	if hasTLS(u) {
		protocol = "https"
	}

	for _, r := range routes {
		route, ok := r.(map[string]any)
		if !ok {
			continue
		}

		match, ok := route["match"].(string)
		if !ok {
			continue
		}

		host := extractHostFromMatch(match)
		if host == "" {
			continue
		}

		path := "/" // Default path if not specified in match

		url := fmt.Sprintf("%s://%s%s", protocol, host, path)
		
		endpoints = append(endpoints, &endpoint.Endpoint{
			Name: u.GetName(),
			URL:  url,
			Host: host,
			Path: path,
		})
	}

	return endpoints
}

func conditionFunc(cfg *config.Config, obj metav1.Object, e *endpoint.Endpoint) {
	e.Conditions = []string{"[STATUS] == 200"}
}

func guardedFunc(obj metav1.Object, e *endpoint.Endpoint) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	host := ""
	spec, ok := u.Object["spec"].(map[string]any)
	if ok {
		routes, ok := spec["routes"].([]any)
		if ok && len(routes) > 0 {
			if route, ok := routes[0].(map[string]any); ok {
				if match, ok := route["match"].(string); ok {
					host = extractHostFromMatch(match)
				}
			}
		}
	}

	if host != "" {
		applyGuardedTemplate(host, e)
	}
}

func applyGuardedTemplate(dnsQueryName string, e *endpoint.Endpoint) {
	e.URL = "1.1.1.1"
	e.DNS = map[string]any{
		"query-name": dnsQueryName,
		"query-type": "A",
	}
	e.Conditions = []string{"len([BODY]) == 0"}
}

// Helper functions

func extractHostFromMatch(match string) string {
	re := regexp.MustCompile(`Host\(\s*['"\x60]?([^'")]*?)['"\x60]?\s*\)`)
	res := re.FindStringSubmatch(match)
	if len(res) > 1 {
		return strings.Trim(res[1], "'\"`")
	}
	return ""
}

func hasTLS(u *unstructured.Unstructured) bool {
	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = spec["tls"]
	return ok
}
