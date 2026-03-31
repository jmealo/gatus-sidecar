package state

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/home-operations/gatus-sidecar/internal/endpoint"
)

const (
	defaultDebounceDuration = 500 * time.Millisecond
)

// Manager maintains the global state of all endpoints
type Manager struct {
	mu         sync.Mutex
	endpoints  map[string]*endpoint.Endpoint // keyed by resource key (name-namespace)
	outputFile string
	writeChan  chan struct{}
}

// NewManager creates a new state manager
func NewManager(outputFile string) *Manager {
	return &Manager{
		endpoints:  make(map[string]*endpoint.Endpoint),
		outputFile: outputFile,
		writeChan:  make(chan struct{}, 1),
	}
}

// Start starts the background writer with debouncing
func (m *Manager) Start(ctx context.Context) {
	slog.Info("starting state manager background writer", "file", m.outputFile)
	
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		select {
		case <-ctx.Done():
			// Final write before exiting
			m.ForceWrite()
			return
		case <-m.writeChan:
			// Reset timer on new write request
			timer.Reset(defaultDebounceDuration)
		case <-timer.C:
			// Timer expired, perform the write
			m.mu.Lock()
			m.writeState()
			m.mu.Unlock()
		}
	}
}

// AddOrUpdate adds or updates an endpoint and signals a write if changed
func (m *Manager) AddOrUpdate(key string, e *endpoint.Endpoint, write bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if this is actually a change
	existing, exists := m.endpoints[key]
	if exists && reflect.DeepEqual(existing, e) {
		return false // No change
	}

	m.endpoints[key] = e

	// Signal write if requested
	if write {
		m.signalWrite()
	}

	return true // Change detected
}

// Remove removes an endpoint and signals a write if changed
func (m *Manager) Remove(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.endpoints[key]
	if !exists {
		return false // No change
	}

	delete(m.endpoints, key)
	m.signalWrite()
	return true // Change detected
}

// ForceWrite forces an immediate write of the current state to disk
func (m *Manager) ForceWrite() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeState()
}

func (m *Manager) signalWrite() {
	select {
	case m.writeChan <- struct{}{}:
	default:
		// Channel full, write already signaled
	}
}

func (m *Manager) GetCurrentState() map[string]*endpoint.Endpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Return a copy to avoid race conditions
	result := make(map[string]*endpoint.Endpoint)
	for k, v := range m.endpoints {
		result[k] = v
	}
	return result
}

// writeState writes the current state to disk (must be called with mutex held)
func (m *Manager) writeState() {
	endpoints := m.getUniqueEndpoints()

	yamlData, err := yaml.Marshal(map[string]any{"endpoints": endpoints})
	if err != nil {
		slog.Error("failed to marshal state to yaml", "error", err)
		return
	}

	if err := os.WriteFile(m.outputFile, yamlData, 0o644); err != nil {
		slog.Error("failed to write state to file", "error", err)
		return
	}

	slog.Info("wrote consolidated state file", "file", m.outputFile, "endpoints", len(endpoints))
}

// getUniqueEndpoints returns a sorted slice of endpoints with guaranteed unique Name+Group combinations
func (m *Manager) getUniqueEndpoints() []*endpoint.Endpoint {
	// Convert to slice
	all := make([]*endpoint.Endpoint, 0, len(m.endpoints))
	for _, e := range m.endpoints {
		// Clone to avoid modifying the original in the map
		clone := *e
		all = append(all, &clone)
	}

	// Sort by Name and Group for deterministic suffixing
	sort.Slice(all, func(i, j int) bool {
		if all[i].Group != all[j].Group {
			return all[i].Group < all[j].Group
		}
		return all[i].Name < all[j].Name
	})

	seen := make(map[string]int)
	result := make([]*endpoint.Endpoint, 0, len(all))

	for _, e := range all {
		id := e.Group + "/" + e.Name
		if count, exists := seen[id]; exists {
			count++
			seen[id] = count
			originalName := e.Name
			e.Name = fmt.Sprintf("%s-%d", e.Name, count)
			slog.Warn("duplicate name+group detected, appended suffix", 
				"group", e.Group, 
				"originalName", originalName, 
				"newName", e.Name,
				"url", e.URL)
		} else {
			seen[id] = 0
		}
		result = append(result, e)
	}

	return result
}

// getCurrentState returns the current state as a map suitable for YAML generation
// (must be called with mutex held)
func (m *Manager) getCurrentState() map[string]any {
	return map[string]any{
		"endpoints": m.getUniqueEndpoints(),
	}
}
