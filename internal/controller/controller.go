package controller

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"text/template"

	"gopkg.in/yaml.v3"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/resources"
	"github.com/home-operations/gatus-sidecar/internal/state"
)

type Controller struct {
	gvr           schema.GroupVersionResource
	handler       resources.ResourceHandler
	convert       func(*unstructured.Unstructured) (metav1.Object, error)
	stateManager  *state.Manager
	dynamicClient dynamic.Interface
	informer      cache.SharedIndexInformer
}

type TemplateContext struct {
	Name      string
	Namespace string
	Resource  string
	Host      string
	Path      string
}

// New creates a controller using a ResourceDefinition
func New(definition *resources.ResourceDefinition, stateManager *state.Manager, dynamicClient dynamic.Interface, informerFactory dynamicinformer.DynamicSharedInformerFactory, namespace string) *Controller {
	informer := informerFactory.ForResource(definition.GVR).Informer()

	return &Controller{
		gvr:           definition.GVR,
		handler:       resources.NewHandler(definition, dynamicClient),
		stateManager:  stateManager,
		dynamicClient: dynamicClient,
		convert:       definition.ConvertFunc,
		informer:      informer,
	}
}

func (c *Controller) GetResource() string {
	return c.gvr.Resource
}

func (c *Controller) Run(ctx context.Context, cfg *config.Config) error {
	if !c.informer.HasSynced() {
		return fmt.Errorf("informer for %s failed to sync, skipping execution", c.gvr.Resource)
	}

	slog.Info("starting informer for resource", "resource", c.gvr.Resource)

	c.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			c.processObject(ctx, cfg, obj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			c.processObject(ctx, cfg, newObj)
		},
		DeleteFunc: func(obj any) {
			c.processDelete(obj)
		},
	})

	c.informer.Run(ctx.Done())
	return nil
}

func (c *Controller) processObject(ctx context.Context, cfg *config.Config, obj any) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		slog.Error("expected unstructured object", "type", fmt.Sprintf("%T", obj))
		return
	}

	metaObj, err := c.convert(unstructuredObj)
	if err != nil {
		slog.Error("failed to convert object", "error", err)
		return
	}

	c.handleEvent(ctx, cfg, metaObj)
}

func (c *Controller) processDelete(obj any) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			slog.Error("unexpected object type in delete", "type", fmt.Sprintf("%T", obj))
			return
		}
		unstructuredObj, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			slog.Error("expected unstructured object in tombstone", "type", fmt.Sprintf("%T", tombstone.Obj))
			return
		}
	}

	name := unstructuredObj.GetName()
	namespace := unstructuredObj.GetNamespace()
	
	// Since we don't know the paths of the deleted object easily from the tombstone without re-extracting,
	// and the state manager uses a key that might include the path, we might need a more robust way to 
	// cleanup all endpoints related to this resource.
	// For now, we'll prefix-match in the state manager if we implement that, or just handle root.
	
	key := fmt.Sprintf("%s.%s.%s", name, namespace, c.gvr.Resource)
	c.removeFromState(key, c.gvr.Resource, name, namespace)
}

func (c *Controller) handleEvent(ctx context.Context, cfg *config.Config, obj metav1.Object) {
	name := obj.GetName()
	namespace := obj.GetNamespace()
	annotations := obj.GetAnnotations()
	resource := c.gvr.Resource

	// Early returns for non-processable resources
	if !c.handler.ShouldProcess(obj, cfg) {
		// Cleanup logic here needs to be path-aware
		return
	}

	// Extract multiple endpoints
	endpoints := c.handler.ExtractEndpoints(obj)
	if len(endpoints) == 0 {
		return
	}

	// Check if explicitly disabled
	if c.isEndpointDisabled(annotations, cfg) {
		return
	}

	// Process template data from annotations
	annotationTemplateData, err := c.buildTemplateData(ctx, obj, cfg)
	if err != nil {
		slog.Error("failed to build template data", "resource", resource, "name", name, "namespace", namespace, "error", err)
		return
	}

	for i, e := range endpoints {
		// Prepare template context
		tmplCtx := TemplateContext{
			Name:      name,
			Namespace: namespace,
			Resource:  resource,
			Host:      e.Host,
			Path:      e.Path,
		}

		// Apply naming and grouping templates
		// Use a more descriptive default name that includes path to prevent Gatus panics
		defaultName := name
		if e.Path != "" && e.Path != "/" {
			defaultName = fmt.Sprintf("%s (%s)", name, e.Path)
		}

		e.Name = c.renderTemplate(cfg.DefaultNameTemplate, tmplCtx, defaultName)
		if groupTemplate, ok := annotations["gatus.io/group-template"]; ok {
			e.Group = c.renderTemplate(groupTemplate, tmplCtx, namespace)
		} else {
			e.Group = c.renderTemplate(cfg.DefaultGroupTemplate, tmplCtx, namespace)
		}
		
		if nameTemplate, ok := annotations["gatus.io/name-template"]; ok {
			e.Name = c.renderTemplate(nameTemplate, tmplCtx, e.Name)
		}

		e.Interval = cfg.DefaultInterval.String()
		e.Guarded = c.isGuardedEndpoint(annotationTemplateData)

		c.handler.ApplyTemplate(cfg, obj, e)
		if annotationTemplateData != nil {
			e.ApplyTemplate(annotationTemplateData)
		}

		// Unique key including path to prevent collisions
		key := fmt.Sprintf("%s.%s.%s.%d", name, namespace, resource, i)
		if e.Path != "" && e.Path != "/" {
			key = fmt.Sprintf("%s.%s.%s.%s", name, namespace, resource, e.Path)
		}

		changed := c.stateManager.AddOrUpdate(key, e, true)
		if changed {
			slog.Info("updated endpoint in state", "resource", resource, "name", e.Name, "url", e.URL)
		}
	}
}

func (c *Controller) renderTemplate(tmplStr string, ctx TemplateContext, fallback string) string {
	if tmplStr == "" {
		return fallback
	}

	tmpl, err := template.New("gatus").Parse(tmplStr)
	if err != nil {
		slog.Error("failed to parse template", "template", tmplStr, "error", err)
		return fallback
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		slog.Error("failed to execute template", "template", tmplStr, "error", err)
		return fallback
	}

	return buf.String()
}

func (c *Controller) isEndpointDisabled(annotations map[string]string, cfg *config.Config) bool {
	enabledValue, ok := annotations[cfg.EnabledAnnotation]
	return ok && enabledValue != "true" && enabledValue != "1"
}

func (c *Controller) removeFromState(key, resource, name, namespace string) {
	if removed := c.stateManager.Remove(key); removed {
		slog.Info("removed endpoint from state", "resource", resource, "name", name, "namespace", namespace)
	}
}

func (c *Controller) isGuardedEndpoint(templateData map[string]any) bool {
	if templateData == nil {
		return false
	}
	_, exists := templateData["guarded"]
	return exists
}

func (c *Controller) buildTemplateData(ctx context.Context, obj metav1.Object, cfg *config.Config) (map[string]any, error) {
	annotations := obj.GetAnnotations()

	parentAnnotations := c.handler.GetParentAnnotations(ctx, obj)
	if parentAnnotations == nil {
		parentAnnotations = make(map[string]string)
	}

	parentTemplateData, err := c.parseTemplateData(parentAnnotations, cfg.TemplateAnnotation)
	if err != nil {
		return nil, fmt.Errorf("failed to parse parent template: %w", err)
	}

	objectTemplateData, err := c.parseTemplateData(annotations, cfg.TemplateAnnotation)
	if err != nil {
		return nil, fmt.Errorf("failed to parse object template: %w", err)
	}

	return c.deepMergeTemplates(parentTemplateData, objectTemplateData), nil
}

func (c *Controller) parseTemplateData(annotations map[string]string, annotationKey string) (map[string]any, error) {
	templateStr, ok := annotations[annotationKey]
	if !ok || templateStr == "" {
		return nil, nil
	}

	var templateData map[string]any
	if err := yaml.Unmarshal([]byte(templateStr), &templateData); err != nil {
		return nil, err
	}
	return templateData, nil
}

func (c *Controller) deepMergeTemplates(parent, child map[string]any) map[string]any {
	if parent == nil {
		return child
	}
	if child == nil {
		return parent
	}

	result := make(map[string]any)
	maps.Copy(result, parent)

	for key, childValue := range child {
		if parentValue, exists := result[key]; exists {
			if parentMap, ok := parentValue.(map[string]any); ok {
				if childMap, ok := childValue.(map[string]any); ok {
					result[key] = c.deepMergeTemplates(parentMap, childMap)
					continue
				}
			}
		}
		result[key] = childValue
	}

	return result
}
