package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/home-operations/gatus-sidecar/internal/config"
	"github.com/home-operations/gatus-sidecar/internal/controller"
	"github.com/home-operations/gatus-sidecar/internal/resources/httproute"
	"github.com/home-operations/gatus-sidecar/internal/resources/ingress"
	"github.com/home-operations/gatus-sidecar/internal/resources/ingressroute"
	"github.com/home-operations/gatus-sidecar/internal/resources/service"
	"github.com/home-operations/gatus-sidecar/internal/state"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	cfg := config.Load()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	restCfg, err := getKubeConfig()
	if err != nil {
		slog.Error("get kubernetes config", "error", err)
		os.Exit(1)
	}

	dc, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		slog.Error("create dynamic client", "error", err)
		os.Exit(1)
	}

	// Create a single shared state manager
	stateManager := state.NewManager(cfg.Output)

	// Start state manager background writer
	go stateManager.Start(ctx)

	// Create dynamic informer factory
	informerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dc, 0, cfg.Namespace, nil)

	// Initialize controllers slice
	controllers := []*controller.Controller{}

	// Determine if default controllers (Ingress/Service) should be enabled
	// If any specific resource is enabled, we don't use the "default all" logic
	defaultControllers := !cfg.EnableHTTPRoute && !cfg.EnableIngress && !cfg.EnableService && !cfg.EnableIngressRoute

	// Ingress and Service are considered "standard" and enabled by default
	if cfg.EnableIngress || cfg.AutoIngress || defaultControllers {
		controllers = append(controllers, controller.New(ingress.Definition(), stateManager, dc, informerFactory, cfg.Namespace))
	}
	if cfg.EnableService || cfg.AutoService || defaultControllers {
		controllers = append(controllers, controller.New(service.Definition(), stateManager, dc, informerFactory, cfg.Namespace))
	}

	// Gateway API and Traefik are optional and require explicit opt-in or auto-discovery flags
	if cfg.EnableHTTPRoute || cfg.AutoHTTPRoute {
		controllers = append(controllers, controller.New(httproute.Definition(), stateManager, dc, informerFactory, cfg.Namespace))
	}
	if cfg.EnableIngressRoute || cfg.AutoIngressRoute {
		controllers = append(controllers, controller.New(ingressroute.Definition(), stateManager, dc, informerFactory, cfg.Namespace))
	}

	// If no controllers are enabled, log a warning and exit
	if len(controllers) == 0 {
		slog.Warn("No controllers enabled. Exiting.")
		return
	}

	// Start informer factory
	informerFactory.Start(ctx.Done())

	// Wait for all informers to sync
	slog.Info("Waiting for informers to sync")
	syncCtx, syncCancel := context.WithTimeout(ctx, 30*time.Second)
	defer syncCancel()
	
	for gvr, synced := range informerFactory.WaitForCacheSync(syncCtx.Done()) {
		if !synced {
			slog.Warn("Failed to sync informer cache", "resource", gvr.Resource)
		}
	}
	slog.Info("Informers sync completed (some might have failed)")

	// Run all controllers concurrently
	runControllers(ctx, cfg, controllers)

	// Keep main running until context is cancelled
	<-ctx.Done()
	slog.Info("All controllers have finished successfully")
}

func runControllers(ctx context.Context, cfg *config.Config, controllers []*controller.Controller) {
	for _, c := range controllers {
		go func(ctrl *controller.Controller) {
			slog.Info("Starting controller", "controller", ctrl.GetResource())

			if err := ctrl.Run(ctx, cfg); err != nil {
				slog.Error("Controller error", "controller", ctrl.GetResource(), "error", err)
			}
		}(c)
	}
}

func getKubeConfig() (*rest.Config, error) {
	// Check if we're running in a cluster by looking for the service host env var
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
		slog.Info("using in-cluster kubernetes config")
		return cfg, nil
	}

	// Fall back to kubeconfig for local development
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	cfg, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("kubeconfig: %w", err)
	}

	slog.Info("using kubeconfig")
	return cfg, nil
}
