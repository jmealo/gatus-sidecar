package service

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/endpoint"
	"github.com/home-operations/gatus-sidecar/internal/resources"
)

// Definition creates a resource definition for Service resources
func Definition() *resources.ResourceDefinition {
	return &resources.ResourceDefinition{
		GVR: schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "services",
		},
		TargetType:        reflect.TypeOf(corev1.Service{}),
		ConvertFunc:       resources.CreateConvertFunc(reflect.TypeOf(corev1.Service{})),
		AutoConfigFunc:    func(cfg *config.Config) bool { return cfg.AutoService },
		EndpointExtractor: endpointExtractor,
		ConditionFunc:     conditionFunc,
	}
}

func endpointExtractor(obj metav1.Object) []*endpoint.Endpoint {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return nil
	}

	if len(svc.Spec.Ports) == 0 {
		return nil
	}

	// For services, we usually just monitor the first port unless specified otherwise
	port := svc.Spec.Ports[0]
	protocol := "http"
	if port.Port == 443 || port.Name == "https" {
		protocol = "https"
	}

	url := fmt.Sprintf("%s://%s.%s.svc:%d", protocol, svc.Name, svc.Namespace, port.Port)

	return []*endpoint.Endpoint{
		{
			Name: svc.Name,
			URL:  url,
			Host: fmt.Sprintf("%s.%s.svc", svc.Name, svc.Namespace),
			Path: "/",
		},
	}
}

func conditionFunc(cfg *config.Config, obj metav1.Object, e *endpoint.Endpoint) {
	e.Conditions = []string{"[STATUS] == 200"}
}
