// Copyright 2025 go-i2p
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rpc

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Plugin represents a custom behavior that can be registered with the plugin system
// Plugins provide structured extension points beyond simple event hooks
type Plugin interface {
	// ID returns a unique identifier for this plugin
	ID() string

	// Name returns a human-readable name for this plugin
	Name() string

	// Version returns the plugin version
	Version() string

	// Initialize is called when the plugin is first loaded
	// It receives a context for cancellation and configuration data
	Initialize(ctx context.Context, config map[string]interface{}) error

	// Shutdown is called when the plugin is being unloaded
	// Plugins should cleanup resources and stop goroutines
	Shutdown(ctx context.Context) error
}

// BehaviorPlugin extends the base Plugin interface for custom torrent behaviors
// This allows plugins to modify or extend torrent management logic
type BehaviorPlugin interface {
	Plugin

	// HandleTorrentBehavior is called to modify torrent behavior
	// It receives the current operation, torrent state, and parameters
	// Returns modified parameters or an error to abort the operation
	HandleTorrentBehavior(ctx context.Context, operation string, torrent *TorrentState, params map[string]interface{}) (map[string]interface{}, error)

	// SupportedOperations returns the list of operations this plugin handles
	SupportedOperations() []string
}

// InterceptorPlugin allows plugins to intercept and modify RPC requests/responses
type InterceptorPlugin interface {
	Plugin

	// InterceptRequest is called before processing an RPC request
	// Can modify the request or return an error to reject it
	InterceptRequest(ctx context.Context, method string, params map[string]interface{}) (map[string]interface{}, error)

	// InterceptResponse is called after processing an RPC request
	// Can modify the response before returning to client
	InterceptResponse(ctx context.Context, method string, result interface{}, err error) (interface{}, error)

	// SupportedMethods returns the list of RPC methods this plugin handles
	SupportedMethods() []string
}

// MetricsPlugin allows plugins to export custom metrics and monitoring data
type MetricsPlugin interface {
	Plugin

	// GetMetrics returns custom metrics data in key-value format
	GetMetrics() map[string]interface{}

	// GetHealthStatus returns plugin health information
	GetHealthStatus() PluginHealthStatus
}

// PluginHealthStatus represents the health status of a plugin
type PluginHealthStatus struct {
	Status    string                 `json:"status"`            // "healthy", "degraded", "unhealthy"
	Message   string                 `json:"message"`           // Human readable status message
	Details   map[string]interface{} `json:"details,omitempty"` // Additional health data
	LastCheck time.Time              `json:"last_check"`
}

// PluginInfo contains metadata about a loaded plugin
type PluginInfo struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Version  string                 `json:"version"`
	Type     string                 `json:"type"`   // "behavior", "interceptor", "metrics"
	Status   string                 `json:"status"` // "loaded", "error", "shutdown"
	LoadTime time.Time              `json:"load_time"`
	ErrorMsg string                 `json:"error_msg,omitempty"`
	Config   map[string]interface{} `json:"config,omitempty"`
}

// PluginManager manages the lifecycle and execution of plugins
type PluginManager struct {
	mu sync.RWMutex

	// Core plugin storage
	plugins    map[string]Plugin
	pluginInfo map[string]*PluginInfo

	// Typed plugin registries for efficient lookup
	behaviorPlugins    map[string][]BehaviorPlugin
	interceptorPlugins map[string][]InterceptorPlugin
	metricsPlugins     []MetricsPlugin

	// Configuration and lifecycle
	defaultTimeout time.Duration
	logger         func(format string, args ...interface{})
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc

	// Plugin execution metrics
	metrics PluginManagerMetrics
}

// PluginManagerMetrics tracks plugin manager performance and usage
type PluginManagerMetrics struct {
	TotalPlugins    int64         `json:"total_plugins"`
	ActivePlugins   int64         `json:"active_plugins"`
	TotalExecutions int64         `json:"total_executions"`
	TotalErrors     int64         `json:"total_errors"`
	AverageLatency  time.Duration `json:"average_latency"`
	LastExecution   time.Time     `json:"last_execution"`
}

// NewPluginManager creates a new plugin manager with default configuration
func NewPluginManager() *PluginManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &PluginManager{
		plugins:            make(map[string]Plugin),
		pluginInfo:         make(map[string]*PluginInfo),
		behaviorPlugins:    make(map[string][]BehaviorPlugin),
		interceptorPlugins: make(map[string][]InterceptorPlugin),
		metricsPlugins:     make([]MetricsPlugin, 0),
		defaultTimeout:     30 * time.Second,
		logger:             func(format string, args ...interface{}) {}, // No-op logger by default
		shutdownCtx:        ctx,
		shutdownCancel:     cancel,
	}
}

// SetLogger sets a custom logger for plugin events
func (pm *PluginManager) SetLogger(logger func(format string, args ...interface{})) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.logger = logger
}

// SetDefaultTimeout sets the default timeout for plugin operations
func (pm *PluginManager) SetDefaultTimeout(timeout time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.defaultTimeout = timeout
}

// RegisterPlugin registers a new plugin with optional configuration
func (pm *PluginManager) RegisterPlugin(plugin Plugin, config map[string]interface{}) error {
	if plugin == nil {
		return fmt.Errorf("plugin cannot be nil")
	}

	id := plugin.ID()
	if id == "" {
		return fmt.Errorf("plugin ID cannot be empty")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check for duplicate plugin IDs
	if _, exists := pm.plugins[id]; exists {
		return fmt.Errorf("plugin with ID '%s' already registered", id)
	}

	// Initialize plugin
	ctx, cancel := context.WithTimeout(pm.shutdownCtx, pm.defaultTimeout)
	defer cancel()

	if err := plugin.Initialize(ctx, config); err != nil {
		pm.logger("Failed to initialize plugin '%s': %v", id, err)
		return fmt.Errorf("failed to initialize plugin '%s': %w", id, err)
	}

	// Store plugin and metadata
	pm.plugins[id] = plugin
	pm.pluginInfo[id] = &PluginInfo{
		ID:       id,
		Name:     plugin.Name(),
		Version:  plugin.Version(),
		Type:     pm.getPluginType(plugin),
		Status:   "loaded",
		LoadTime: time.Now(),
		Config:   config,
	}

	// Register in typed registries for efficient lookup
	pm.registerTypedPlugin(plugin)

	pm.metrics.TotalPlugins++
	pm.metrics.ActivePlugins++

	pm.logger("Registered plugin '%s' (%s v%s)", id, plugin.Name(), plugin.Version())
	return nil
}

// UnregisterPlugin removes and shuts down a plugin
func (pm *PluginManager) UnregisterPlugin(pluginID string) error {
	if pluginID == "" {
		return fmt.Errorf("plugin ID cannot be empty")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	plugin, exists := pm.plugins[pluginID]
	if !exists {
		return fmt.Errorf("plugin with ID '%s' not found", pluginID)
	}

	// Shutdown plugin
	ctx, cancel := context.WithTimeout(pm.shutdownCtx, pm.defaultTimeout)
	defer cancel()

	if err := plugin.Shutdown(ctx); err != nil {
		pm.logger("Error shutting down plugin '%s': %v", pluginID, err)
		// Continue with removal even if shutdown fails
	}

	// Remove from typed registries
	pm.unregisterTypedPlugin(plugin)

	// Remove from main registry
	delete(pm.plugins, pluginID)
	if info, exists := pm.pluginInfo[pluginID]; exists {
		info.Status = "shutdown"
		delete(pm.pluginInfo, pluginID)
	}

	pm.metrics.ActivePlugins--

	pm.logger("Unregistered plugin '%s'", pluginID)
	return nil
}

// ExecuteBehaviorPlugins executes all behavior plugins for a specific operation
func (pm *PluginManager) ExecuteBehaviorPlugins(ctx context.Context, operation string, torrent *TorrentState, params map[string]interface{}) (map[string]interface{}, error) {
	pm.mu.RLock()
	plugins := pm.behaviorPlugins[operation]
	pm.mu.RUnlock()

	if len(plugins) == 0 {
		return params, nil
	}

	start := time.Now()
	defer func() {
		pm.mu.Lock()
		pm.metrics.TotalExecutions++
		pm.metrics.AverageLatency = (pm.metrics.AverageLatency + time.Since(start)) / 2
		pm.metrics.LastExecution = time.Now()
		pm.mu.Unlock()
	}()

	result := params
	for _, plugin := range plugins {
		ctx, cancel := context.WithTimeout(ctx, pm.defaultTimeout)

		modifiedParams, err := plugin.HandleTorrentBehavior(ctx, operation, torrent, result)
		cancel()

		if err != nil {
			pm.mu.Lock()
			pm.metrics.TotalErrors++
			pm.mu.Unlock()
			pm.logger("Behavior plugin '%s' failed for operation '%s': %v", plugin.ID(), operation, err)
			return nil, fmt.Errorf("behavior plugin '%s' failed: %w", plugin.ID(), err)
		}

		result = modifiedParams
	}

	return result, nil
}

// ExecuteInterceptorPlugins executes interceptor plugins for RPC requests
func (pm *PluginManager) ExecuteInterceptorPlugins(ctx context.Context, method string, isRequest bool, data interface{}) (interface{}, error) {
	pm.mu.RLock()
	plugins := pm.interceptorPlugins[method]
	pm.mu.RUnlock()

	if len(plugins) == 0 {
		return data, nil
	}

	start := time.Now()
	defer func() {
		pm.mu.Lock()
		pm.metrics.TotalExecutions++
		pm.metrics.AverageLatency = (pm.metrics.AverageLatency + time.Since(start)) / 2
		pm.metrics.LastExecution = time.Now()
		pm.mu.Unlock()
	}()

	result := data
	for _, plugin := range plugins {
		ctx, cancel := context.WithTimeout(ctx, pm.defaultTimeout)

		var err error
		if isRequest {
			if params, ok := result.(map[string]interface{}); ok {
				result, err = plugin.InterceptRequest(ctx, method, params)
			} else {
				cancel()
				return nil, fmt.Errorf("invalid request parameters type for plugin '%s'", plugin.ID())
			}
		} else {
			result, err = plugin.InterceptResponse(ctx, method, result, nil)
		}

		cancel()

		if err != nil {
			pm.mu.Lock()
			pm.metrics.TotalErrors++
			pm.mu.Unlock()
			pm.logger("Interceptor plugin '%s' failed for method '%s': %v", plugin.ID(), method, err)
			return nil, fmt.Errorf("interceptor plugin '%s' failed: %w", plugin.ID(), err)
		}
	}

	return result, nil
}

// GetMetrics returns aggregated metrics from all metrics plugins
func (pm *PluginManager) GetMetrics() map[string]interface{} {
	pm.mu.RLock()
	plugins := make([]MetricsPlugin, len(pm.metricsPlugins))
	copy(plugins, pm.metricsPlugins)
	managerMetrics := pm.metrics
	pm.mu.RUnlock()

	result := map[string]interface{}{
		"manager": managerMetrics,
	}

	for _, plugin := range plugins {
		pluginMetrics := plugin.GetMetrics()
		if pluginMetrics != nil {
			result[plugin.ID()] = pluginMetrics
		}
	}

	return result
}

// GetPluginInfo returns information about all loaded plugins
func (pm *PluginManager) GetPluginInfo() map[string]*PluginInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]*PluginInfo)
	for id, info := range pm.pluginInfo {
		// Create a copy to avoid data races
		infoCopy := *info
		result[id] = &infoCopy
	}

	return result
}

// GetPluginHealthStatus returns health status for all plugins
func (pm *PluginManager) GetPluginHealthStatus() map[string]PluginHealthStatus {
	pm.mu.RLock()
	plugins := make([]MetricsPlugin, len(pm.metricsPlugins))
	copy(plugins, pm.metricsPlugins)
	pm.mu.RUnlock()

	result := make(map[string]PluginHealthStatus)

	for _, plugin := range plugins {
		status := plugin.GetHealthStatus()
		result[plugin.ID()] = status
	}

	return result
}

// Shutdown gracefully shuts down all plugins
func (pm *PluginManager) Shutdown() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.shutdownCancel()

	var errors []error
	for id, plugin := range pm.plugins {
		ctx, cancel := context.WithTimeout(context.Background(), pm.defaultTimeout)
		if err := plugin.Shutdown(ctx); err != nil {
			errors = append(errors, fmt.Errorf("plugin '%s': %w", id, err))
		}
		cancel()

		if info, exists := pm.pluginInfo[id]; exists {
			info.Status = "shutdown"
		}
	}

	// Clear all registries
	pm.plugins = make(map[string]Plugin)
	pm.pluginInfo = make(map[string]*PluginInfo)
	pm.behaviorPlugins = make(map[string][]BehaviorPlugin)
	pm.interceptorPlugins = make(map[string][]InterceptorPlugin)
	pm.metricsPlugins = make([]MetricsPlugin, 0)
	pm.metrics.ActivePlugins = 0

	if len(errors) > 0 {
		return fmt.Errorf("errors shutting down plugins: %v", errors)
	}

	pm.logger("Plugin manager shutdown completed")
	return nil
}

// getPluginType determines the plugin type based on implemented interfaces
func (pm *PluginManager) getPluginType(plugin Plugin) string {
	types := make([]string, 0, 3)

	if _, ok := plugin.(BehaviorPlugin); ok {
		types = append(types, "behavior")
	}
	if _, ok := plugin.(InterceptorPlugin); ok {
		types = append(types, "interceptor")
	}
	if _, ok := plugin.(MetricsPlugin); ok {
		types = append(types, "metrics")
	}

	if len(types) == 0 {
		return "basic"
	}
	if len(types) == 1 {
		return types[0]
	}
	return "composite"
}

// registerTypedPlugin registers a plugin in the appropriate typed registries
func (pm *PluginManager) registerTypedPlugin(plugin Plugin) {
	// Register as behavior plugin
	if behaviorPlugin, ok := plugin.(BehaviorPlugin); ok {
		for _, operation := range behaviorPlugin.SupportedOperations() {
			pm.behaviorPlugins[operation] = append(pm.behaviorPlugins[operation], behaviorPlugin)
		}
	}

	// Register as interceptor plugin
	if interceptorPlugin, ok := plugin.(InterceptorPlugin); ok {
		for _, method := range interceptorPlugin.SupportedMethods() {
			pm.interceptorPlugins[method] = append(pm.interceptorPlugins[method], interceptorPlugin)
		}
	}

	// Register as metrics plugin
	if metricsPlugin, ok := plugin.(MetricsPlugin); ok {
		pm.metricsPlugins = append(pm.metricsPlugins, metricsPlugin)
	}
}

// unregisterTypedPlugin removes a plugin from typed registries
func (pm *PluginManager) unregisterTypedPlugin(plugin Plugin) {
	pluginID := plugin.ID()

	// Remove from behavior plugins
	if behaviorPlugin, ok := plugin.(BehaviorPlugin); ok {
		for _, operation := range behaviorPlugin.SupportedOperations() {
			plugins := pm.behaviorPlugins[operation]
			for i, p := range plugins {
				if p.ID() == pluginID {
					pm.behaviorPlugins[operation] = append(plugins[:i], plugins[i+1:]...)
					break
				}
			}
		}
	}

	// Remove from interceptor plugins
	if interceptorPlugin, ok := plugin.(InterceptorPlugin); ok {
		for _, method := range interceptorPlugin.SupportedMethods() {
			plugins := pm.interceptorPlugins[method]
			for i, p := range plugins {
				if p.ID() == pluginID {
					pm.interceptorPlugins[method] = append(plugins[:i], plugins[i+1:]...)
					break
				}
			}
		}
	}

	// Remove from metrics plugins
	if _, ok := plugin.(MetricsPlugin); ok {
		for i, p := range pm.metricsPlugins {
			if p.ID() == pluginID {
				pm.metricsPlugins = append(pm.metricsPlugins[:i], pm.metricsPlugins[i+1:]...)
				break
			}
		}
	}
}

// ListPluginsByType returns plugin IDs grouped by type
func (pm *PluginManager) ListPluginsByType() map[string][]string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string][]string)

	for id, info := range pm.pluginInfo {
		if info.Status == "loaded" {
			result[info.Type] = append(result[info.Type], id)
		}
	}

	return result
}

// ValidatePlugin performs basic validation of a plugin implementation
func ValidatePlugin(plugin Plugin) error {
	if plugin == nil {
		return fmt.Errorf("plugin cannot be nil")
	}

	if plugin.ID() == "" {
		return fmt.Errorf("plugin ID cannot be empty")
	}

	if plugin.Name() == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}

	if plugin.Version() == "" {
		return fmt.Errorf("plugin version cannot be empty")
	}

	// Validate behavior plugin interfaces
	if behaviorPlugin, ok := plugin.(BehaviorPlugin); ok {
		if len(behaviorPlugin.SupportedOperations()) == 0 {
			return fmt.Errorf("behavior plugin must support at least one operation")
		}
	}

	// Validate interceptor plugin interfaces
	if interceptorPlugin, ok := plugin.(InterceptorPlugin); ok {
		if len(interceptorPlugin.SupportedMethods()) == 0 {
			return fmt.Errorf("interceptor plugin must support at least one method")
		}
	}

	return nil
}
