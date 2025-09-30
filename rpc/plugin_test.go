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
	"errors"
	"fmt"
	"testing"
	"time"
)

// Test plugin implementations for testing

// testBasicPlugin implements only the basic Plugin interface
type testBasicPlugin struct {
	id             string
	name           string
	version        string
	initErr        error
	shutdownErr    error
	initCalled     bool
	shutdownCalled bool
}

func (p *testBasicPlugin) ID() string      { return p.id }
func (p *testBasicPlugin) Name() string    { return p.name }
func (p *testBasicPlugin) Version() string { return p.version }

func (p *testBasicPlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
	p.initCalled = true
	return p.initErr
}

func (p *testBasicPlugin) Shutdown(ctx context.Context) error {
	p.shutdownCalled = true
	return p.shutdownErr
}

// testBehaviorPlugin implements BehaviorPlugin interface
type testBehaviorPlugin struct {
	*testBasicPlugin
	operations  []string
	behaviorErr error
	executions  map[string]int
}

func (p *testBehaviorPlugin) SupportedOperations() []string {
	return p.operations
}

func (p *testBehaviorPlugin) HandleTorrentBehavior(ctx context.Context, operation string, torrent *TorrentState, params map[string]interface{}) (map[string]interface{}, error) {
	if p.executions == nil {
		p.executions = make(map[string]int)
	}
	p.executions[operation]++

	if p.behaviorErr != nil {
		return nil, p.behaviorErr
	}

	// Modify params to show plugin executed
	result := make(map[string]interface{})
	for k, v := range params {
		result[k] = v
	}
	result["modified_by"] = p.id
	return result, nil
}

// testInterceptorPlugin implements InterceptorPlugin interface
type testInterceptorPlugin struct {
	*testBasicPlugin
	methods     []string
	requestErr  error
	responseErr error
	executions  map[string]int
}

func (p *testInterceptorPlugin) SupportedMethods() []string {
	return p.methods
}

func (p *testInterceptorPlugin) InterceptRequest(ctx context.Context, method string, params map[string]interface{}) (map[string]interface{}, error) {
	if p.executions == nil {
		p.executions = make(map[string]int)
	}
	p.executions[method+"_request"]++

	if p.requestErr != nil {
		return nil, p.requestErr
	}

	// Modify params to show interceptor executed
	result := make(map[string]interface{})
	for k, v := range params {
		result[k] = v
	}
	result["intercepted_by"] = p.id
	return result, nil
}

func (p *testInterceptorPlugin) InterceptResponse(ctx context.Context, method string, result interface{}, err error) (interface{}, error) {
	if p.executions == nil {
		p.executions = make(map[string]int)
	}
	p.executions[method+"_response"]++

	if p.responseErr != nil {
		return nil, p.responseErr
	}

	// Modify result to show interceptor executed
	if resultMap, ok := result.(map[string]interface{}); ok {
		resultMap["response_intercepted_by"] = p.id
		return resultMap, nil
	}

	return result, nil
}

// testMetricsPlugin implements MetricsPlugin interface
type testMetricsPlugin struct {
	*testBasicPlugin
	metrics map[string]interface{}
	health  PluginHealthStatus
}

func (p *testMetricsPlugin) GetMetrics() map[string]interface{} {
	return p.metrics
}

func (p *testMetricsPlugin) GetHealthStatus() PluginHealthStatus {
	return p.health
}

// testCompositePlugin implements multiple plugin interfaces
type testCompositePlugin struct {
	*testBehaviorPlugin
	*testInterceptorPlugin
	*testMetricsPlugin
}

func (p *testCompositePlugin) ID() string      { return p.testBehaviorPlugin.ID() }
func (p *testCompositePlugin) Name() string    { return p.testBehaviorPlugin.Name() }
func (p *testCompositePlugin) Version() string { return p.testBehaviorPlugin.Version() }

func (p *testCompositePlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
	return p.testBehaviorPlugin.Initialize(ctx, config)
}

func (p *testCompositePlugin) Shutdown(ctx context.Context) error {
	return p.testBehaviorPlugin.Shutdown(ctx)
}

// Test helper functions

func createTestBasicPlugin(id, name, version string) *testBasicPlugin {
	return &testBasicPlugin{
		id:      id,
		name:    name,
		version: version,
	}
}

func createTestBehaviorPlugin(id, name, version string, operations []string) *testBehaviorPlugin {
	return &testBehaviorPlugin{
		testBasicPlugin: createTestBasicPlugin(id, name, version),
		operations:      operations,
		executions:      make(map[string]int),
	}
}

func createTestInterceptorPlugin(id, name, version string, methods []string) *testInterceptorPlugin {
	return &testInterceptorPlugin{
		testBasicPlugin: createTestBasicPlugin(id, name, version),
		methods:         methods,
		executions:      make(map[string]int),
	}
}

func createTestMetricsPlugin(id, name, version string) *testMetricsPlugin {
	return &testMetricsPlugin{
		testBasicPlugin: createTestBasicPlugin(id, name, version),
		metrics: map[string]interface{}{
			"test_metric": 42,
		},
		health: PluginHealthStatus{
			Status:    "healthy",
			Message:   "All systems operational",
			LastCheck: time.Now(),
		},
	}
}

// Test cases

func TestNewPluginManager(t *testing.T) {
	pm := NewPluginManager()

	if pm == nil {
		t.Fatal("NewPluginManager returned nil")
	}

	if pm.plugins == nil {
		t.Error("plugins map not initialized")
	}

	if pm.pluginInfo == nil {
		t.Error("pluginInfo map not initialized")
	}

	if pm.behaviorPlugins == nil {
		t.Error("behaviorPlugins map not initialized")
	}

	if pm.interceptorPlugins == nil {
		t.Error("interceptorPlugins map not initialized")
	}

	if pm.metricsPlugins == nil {
		t.Error("metricsPlugins slice not initialized")
	}

	if pm.defaultTimeout != 30*time.Second {
		t.Error("default timeout not set correctly")
	}
}

func TestPluginManagerSetters(t *testing.T) {
	pm := NewPluginManager()

	// Test SetDefaultTimeout
	newTimeout := 60 * time.Second
	pm.SetDefaultTimeout(newTimeout)

	if pm.defaultTimeout != newTimeout {
		t.Errorf("SetDefaultTimeout failed: expected %v, got %v", newTimeout, pm.defaultTimeout)
	}

	// Test SetLogger
	logCalled := false
	testLogger := func(format string, args ...interface{}) {
		logCalled = true
	}
	pm.SetLogger(testLogger)

	// Test logger by registering a plugin
	plugin := createTestBasicPlugin("test", "Test Plugin", "1.0.0")
	pm.RegisterPlugin(plugin, nil)

	if !logCalled {
		t.Error("Custom logger was not called")
	}
}

func TestRegisterPlugin(t *testing.T) {
	pm := NewPluginManager()

	// Test successful registration
	plugin := createTestBasicPlugin("test-plugin", "Test Plugin", "1.0.0")
	err := pm.RegisterPlugin(plugin, map[string]interface{}{"key": "value"})

	if err != nil {
		t.Fatalf("RegisterPlugin failed: %v", err)
	}

	if !plugin.initCalled {
		t.Error("Plugin Initialize method was not called")
	}

	// Check plugin info
	info := pm.GetPluginInfo()
	if pluginInfo, exists := info["test-plugin"]; !exists {
		t.Error("Plugin info not stored")
	} else {
		if pluginInfo.ID != "test-plugin" {
			t.Error("Plugin ID not stored correctly")
		}
		if pluginInfo.Status != "loaded" {
			t.Error("Plugin status not set to loaded")
		}
		if pluginInfo.Type != "basic" {
			t.Error("Plugin type not determined correctly")
		}
	}

	// Test nil plugin
	err = pm.RegisterPlugin(nil, nil)
	if err == nil {
		t.Error("RegisterPlugin should fail with nil plugin")
	}

	// Test duplicate registration
	duplicate := createTestBasicPlugin("test-plugin", "Duplicate", "2.0.0")
	err = pm.RegisterPlugin(duplicate, nil)
	if err == nil {
		t.Error("RegisterPlugin should fail with duplicate ID")
	}

	// Test initialization failure
	failingPlugin := createTestBasicPlugin("failing", "Failing Plugin", "1.0.0")
	failingPlugin.initErr = errors.New("initialization failed")
	err = pm.RegisterPlugin(failingPlugin, nil)
	if err == nil {
		t.Error("RegisterPlugin should fail when Initialize returns error")
	}
}

func TestUnregisterPlugin(t *testing.T) {
	pm := NewPluginManager()

	// Register a plugin first
	plugin := createTestBasicPlugin("test-plugin", "Test Plugin", "1.0.0")
	pm.RegisterPlugin(plugin, nil)

	// Test successful unregistration
	err := pm.UnregisterPlugin("test-plugin")
	if err != nil {
		t.Fatalf("UnregisterPlugin failed: %v", err)
	}

	if !plugin.shutdownCalled {
		t.Error("Plugin Shutdown method was not called")
	}

	// Check plugin is removed
	info := pm.GetPluginInfo()
	if _, exists := info["test-plugin"]; exists {
		t.Error("Plugin info not removed")
	}

	// Test unregistering non-existent plugin
	err = pm.UnregisterPlugin("non-existent")
	if err == nil {
		t.Error("UnregisterPlugin should fail with non-existent plugin")
	}

	// Test empty plugin ID
	err = pm.UnregisterPlugin("")
	if err == nil {
		t.Error("UnregisterPlugin should fail with empty plugin ID")
	}
}

func TestBehaviorPluginExecution(t *testing.T) {
	pm := NewPluginManager()

	// Register behavior plugin
	plugin := createTestBehaviorPlugin("behavior-test", "Behavior Test", "1.0.0", []string{"start", "stop"})
	err := pm.RegisterPlugin(plugin, nil)
	if err != nil {
		t.Fatalf("Failed to register behavior plugin: %v", err)
	}

	// Test execution
	params := map[string]interface{}{"test": "value"}
	torrent := &TorrentState{ID: 1}

	result, err := pm.ExecuteBehaviorPlugins(context.Background(), "start", torrent, params)
	if err != nil {
		t.Fatalf("ExecuteBehaviorPlugins failed: %v", err)
	}

	// Check parameters were modified
	if result["modified_by"] != "behavior-test" {
		t.Error("Parameters were not modified by behavior plugin")
	}

	// Check execution count
	if plugin.executions["start"] != 1 {
		t.Error("Behavior plugin execution count not tracked correctly")
	}

	// Test operation not supported by plugin
	result, err = pm.ExecuteBehaviorPlugins(context.Background(), "unsupported", torrent, params)
	if err != nil {
		t.Fatalf("ExecuteBehaviorPlugins failed for unsupported operation: %v", err)
	}

	// Parameters should be unchanged
	if result["modified_by"] != nil {
		t.Error("Parameters were modified for unsupported operation")
	}
}

func TestInterceptorPluginExecution(t *testing.T) {
	pm := NewPluginManager()

	// Register interceptor plugin
	plugin := createTestInterceptorPlugin("interceptor-test", "Interceptor Test", "1.0.0", []string{"torrent-add", "torrent-get"})
	err := pm.RegisterPlugin(plugin, nil)
	if err != nil {
		t.Fatalf("Failed to register interceptor plugin: %v", err)
	}

	// Test request interception
	params := map[string]interface{}{"test": "value"}

	result, err := pm.ExecuteInterceptorPlugins(context.Background(), "torrent-add", true, params)
	if err != nil {
		t.Fatalf("ExecuteInterceptorPlugins failed for request: %v", err)
	}

	// Check parameters were modified
	if resultMap, ok := result.(map[string]interface{}); !ok {
		t.Error("Result should be a map")
	} else if resultMap["intercepted_by"] != "interceptor-test" {
		t.Error("Request was not intercepted by plugin")
	}

	// Test response interception
	response := map[string]interface{}{"response": "data"}
	result, err = pm.ExecuteInterceptorPlugins(context.Background(), "torrent-add", false, response)
	if err != nil {
		t.Fatalf("ExecuteInterceptorPlugins failed for response: %v", err)
	}

	// Check response was modified
	if resultMap, ok := result.(map[string]interface{}); !ok {
		t.Error("Result should be a map")
	} else if resultMap["response_intercepted_by"] != "interceptor-test" {
		t.Error("Response was not intercepted by plugin")
	}

	// Check execution counts
	if plugin.executions["torrent-add_request"] != 1 {
		t.Error("Request execution count not tracked correctly")
	}
	if plugin.executions["torrent-add_response"] != 1 {
		t.Error("Response execution count not tracked correctly")
	}
}

func TestMetricsPluginExecution(t *testing.T) {
	pm := NewPluginManager()

	// Register metrics plugin
	plugin := createTestMetricsPlugin("metrics-test", "Metrics Test", "1.0.0")
	err := pm.RegisterPlugin(plugin, nil)
	if err != nil {
		t.Fatalf("Failed to register metrics plugin: %v", err)
	}

	// Test metrics retrieval
	metrics := pm.GetMetrics()

	if metrics["manager"] == nil {
		t.Error("Manager metrics not included")
	}

	if metrics["metrics-test"] == nil {
		t.Error("Plugin metrics not included")
	}

	if pluginMetrics, ok := metrics["metrics-test"].(map[string]interface{}); !ok {
		t.Error("Plugin metrics should be a map")
	} else if pluginMetrics["test_metric"] != 42 {
		t.Error("Plugin metrics not retrieved correctly")
	}

	// Test health status
	health := pm.GetPluginHealthStatus()

	if pluginHealth, exists := health["metrics-test"]; !exists {
		t.Error("Plugin health status not included")
	} else {
		if pluginHealth.Status != "healthy" {
			t.Error("Plugin health status not retrieved correctly")
		}
	}
}

func TestPluginValidation(t *testing.T) {
	// Test nil plugin
	err := ValidatePlugin(nil)
	if err == nil {
		t.Error("ValidatePlugin should fail with nil plugin")
	}

	// Test plugin with empty ID
	plugin := createTestBasicPlugin("", "Test Plugin", "1.0.0")
	err = ValidatePlugin(plugin)
	if err == nil {
		t.Error("ValidatePlugin should fail with empty ID")
	}

	// Test plugin with empty name
	plugin = createTestBasicPlugin("test", "", "1.0.0")
	err = ValidatePlugin(plugin)
	if err == nil {
		t.Error("ValidatePlugin should fail with empty name")
	}

	// Test plugin with empty version
	plugin = createTestBasicPlugin("test", "Test Plugin", "")
	err = ValidatePlugin(plugin)
	if err == nil {
		t.Error("ValidatePlugin should fail with empty version")
	}

	// Test valid basic plugin
	plugin = createTestBasicPlugin("test", "Test Plugin", "1.0.0")
	err = ValidatePlugin(plugin)
	if err != nil {
		t.Errorf("ValidatePlugin should succeed with valid plugin: %v", err)
	}

	// Test behavior plugin with no operations
	behaviorPlugin := createTestBehaviorPlugin("test", "Test Plugin", "1.0.0", []string{})
	err = ValidatePlugin(behaviorPlugin)
	if err == nil {
		t.Error("ValidatePlugin should fail with behavior plugin that has no operations")
	}

	// Test interceptor plugin with no methods
	interceptorPlugin := createTestInterceptorPlugin("test", "Test Plugin", "1.0.0", []string{})
	err = ValidatePlugin(interceptorPlugin)
	if err == nil {
		t.Error("ValidatePlugin should fail with interceptor plugin that has no methods")
	}
}

func TestPluginManagerShutdown(t *testing.T) {
	pm := NewPluginManager()

	// Register multiple plugins
	plugin1 := createTestBasicPlugin("plugin1", "Plugin 1", "1.0.0")
	plugin2 := createTestBasicPlugin("plugin2", "Plugin 2", "1.0.0")

	pm.RegisterPlugin(plugin1, nil)
	pm.RegisterPlugin(plugin2, nil)

	// Test shutdown
	err := pm.Shutdown()
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Check plugins were shut down
	if !plugin1.shutdownCalled {
		t.Error("Plugin1 Shutdown method was not called")
	}
	if !plugin2.shutdownCalled {
		t.Error("Plugin2 Shutdown method was not called")
	}

	// Check plugin registries were cleared
	info := pm.GetPluginInfo()
	if len(info) != 0 {
		t.Error("Plugin info was not cleared")
	}

	metrics := pm.GetMetrics()
	if managerMetrics, ok := metrics["manager"].(PluginManagerMetrics); !ok {
		t.Error("Manager metrics should be included")
	} else if managerMetrics.ActivePlugins != 0 {
		t.Error("Active plugins count was not reset")
	}
}

func TestPluginErrors(t *testing.T) {
	pm := NewPluginManager()

	// Test behavior plugin with error
	behaviorPlugin := createTestBehaviorPlugin("behavior-error", "Behavior Error", "1.0.0", []string{"test"})
	behaviorPlugin.behaviorErr = errors.New("behavior error")
	pm.RegisterPlugin(behaviorPlugin, nil)

	params := map[string]interface{}{"test": "value"}
	torrent := &TorrentState{ID: 1}

	_, err := pm.ExecuteBehaviorPlugins(context.Background(), "test", torrent, params)
	if err == nil {
		t.Error("ExecuteBehaviorPlugins should fail when plugin returns error")
	}

	// Test interceptor plugin with error
	interceptorPlugin := createTestInterceptorPlugin("interceptor-error", "Interceptor Error", "1.0.0", []string{"test"})
	interceptorPlugin.requestErr = errors.New("request error")
	pm.RegisterPlugin(interceptorPlugin, nil)

	_, err = pm.ExecuteInterceptorPlugins(context.Background(), "test", true, params)
	if err == nil {
		t.Error("ExecuteInterceptorPlugins should fail when plugin returns error")
	}
}

func TestPluginMetrics(t *testing.T) {
	pm := NewPluginManager()

	// Register a behavior plugin
	plugin := createTestBehaviorPlugin("metrics-test", "Metrics Test", "1.0.0", []string{"test"})
	pm.RegisterPlugin(plugin, nil)

	// Execute plugin to generate metrics
	params := map[string]interface{}{"test": "value"}
	torrent := &TorrentState{ID: 1}
	pm.ExecuteBehaviorPlugins(context.Background(), "test", torrent, params)

	// Check metrics
	metrics := pm.GetMetrics()
	if managerMetrics, ok := metrics["manager"].(PluginManagerMetrics); !ok {
		t.Error("Manager metrics should be included")
	} else {
		if managerMetrics.TotalExecutions != 1 {
			t.Error("Total executions not tracked correctly")
		}
		if managerMetrics.ActivePlugins != 1 {
			t.Error("Active plugins count not correct")
		}
	}
}

// Benchmark tests

func BenchmarkPluginRegistration(b *testing.B) {
	pm := NewPluginManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plugin := createTestBasicPlugin(fmt.Sprintf("plugin-%d", i), "Test Plugin", "1.0.0")
		pm.RegisterPlugin(plugin, nil)
	}
}

func BenchmarkBehaviorPluginExecution(b *testing.B) {
	pm := NewPluginManager()
	plugin := createTestBehaviorPlugin("benchmark", "Benchmark Plugin", "1.0.0", []string{"test"})
	pm.RegisterPlugin(plugin, nil)

	params := map[string]interface{}{"test": "value"}
	torrent := &TorrentState{ID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pm.ExecuteBehaviorPlugins(context.Background(), "test", torrent, params)
	}
}

func BenchmarkInterceptorPluginExecution(b *testing.B) {
	pm := NewPluginManager()
	plugin := createTestInterceptorPlugin("benchmark", "Benchmark Plugin", "1.0.0", []string{"test"})
	pm.RegisterPlugin(plugin, nil)

	params := map[string]interface{}{"test": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pm.ExecuteInterceptorPlugins(context.Background(), "test", true, params)
	}
}
