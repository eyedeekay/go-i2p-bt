// Copyright 2025 go-i2ppackage integration

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

// Package main demonstrates plugin-based persistence integration
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-i2p/go-i2p-bt/rpc"
)

// PersistencePlugin implements plugin-based persistence for torrents and metadata
type PersistencePlugin struct {
	id           string
	name         string
	version      string
	dataDir      string
	saveInterval time.Duration

	// Internal state
	mu          sync.RWMutex
	initialized bool
	shutdown    chan struct{}

	// External dependencies
	metadataManager *rpc.MetadataManager

	// Performance metrics
	saveCount int64
	loadCount int64
	lastSave  time.Time
	lastLoad  time.Time
}

// NewPersistencePlugin creates a new persistence plugin
func NewPersistencePlugin(dataDir string, saveInterval time.Duration) *PersistencePlugin {
	return &PersistencePlugin{
		id:           "persistence-plugin",
		name:         "Automatic Persistence Plugin",
		version:      "1.0.0",
		dataDir:      dataDir,
		saveInterval: saveInterval,
		shutdown:     make(chan struct{}),
	}
}

// Plugin interface implementation

func (p *PersistencePlugin) ID() string {
	return p.id
}

func (p *PersistencePlugin) Name() string {
	return p.name
}

func (p *PersistencePlugin) Version() string {
	return p.version
}

func (p *PersistencePlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(p.dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Extract configuration
	if interval, ok := config["save_interval"]; ok {
		if intervalStr, ok := interval.(string); ok {
			if parsed, err := time.ParseDuration(intervalStr); err == nil {
				p.saveInterval = parsed
			}
		}
	}

	// Initialize metadata manager from config
	if mm, ok := config["metadata_manager"]; ok {
		if metadataManager, ok := mm.(*rpc.MetadataManager); ok {
			p.metadataManager = metadataManager
		}
	}

	p.initialized = true

	// Start periodic saving
	go p.periodicSave()

	log.Printf("PersistencePlugin initialized with save interval: %v", p.saveInterval)
	return nil
}

func (p *PersistencePlugin) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.initialized {
		return nil
	}

	// Signal shutdown
	close(p.shutdown)

	// Perform final save
	if err := p.saveState(); err != nil {
		log.Printf("Error during final save: %v", err)
	}

	p.initialized = false
	log.Printf("PersistencePlugin shutdown completed")
	return nil
}

// BehaviorPlugin interface implementation

func (p *PersistencePlugin) SupportedOperations() []string {
	return []string{"torrent-add", "torrent-remove", "metadata-set"}
}

func (p *PersistencePlugin) HandleTorrentBehavior(ctx context.Context, operation string, torrent *rpc.TorrentState, params map[string]interface{}) (map[string]interface{}, error) {
	switch operation {
	case "torrent-add":
		return p.handleTorrentAdd(ctx, torrent, params)
	case "torrent-remove":
		return p.handleTorrentRemove(ctx, torrent, params)
	case "metadata-set":
		return p.handleMetadataSet(ctx, torrent, params)
	default:
		return params, nil
	}
}

// MetricsPlugin interface implementation

func (p *PersistencePlugin) GetMetrics() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"save_count":    p.saveCount,
		"load_count":    p.loadCount,
		"last_save":     p.lastSave,
		"last_load":     p.lastLoad,
		"data_dir":      p.dataDir,
		"save_interval": p.saveInterval.String(),
	}
}

func (p *PersistencePlugin) GetHealthStatus() rpc.PluginHealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := "healthy"
	message := "Persistence plugin operating normally"

	// Check if data directory is accessible
	if _, err := os.Stat(p.dataDir); os.IsNotExist(err) {
		status = "unhealthy"
		message = "Data directory not accessible"
	}

	// Check if saves are happening regularly
	if p.initialized && time.Since(p.lastSave) > p.saveInterval*2 {
		status = "degraded"
		message = "Saves appear to be delayed"
	}

	return rpc.PluginHealthStatus{
		Status:    status,
		Message:   message,
		LastCheck: time.Now(),
		Details: map[string]interface{}{
			"save_count": p.saveCount,
			"last_save":  p.lastSave,
		},
	}
}

// Behavior handlers

func (p *PersistencePlugin) handleTorrentAdd(ctx context.Context, torrent *rpc.TorrentState, params map[string]interface{}) (map[string]interface{}, error) {
	// Add persistence metadata
	if torrent != nil {
		params["persistence_added"] = time.Now()
		params["persistence_plugin"] = p.id

		// Save torrent state immediately
		if err := p.saveTorrentState(torrent); err != nil {
			log.Printf("Failed to save torrent state: %v", err)
		}
	}

	return params, nil
}

func (p *PersistencePlugin) handleTorrentRemove(ctx context.Context, torrent *rpc.TorrentState, params map[string]interface{}) (map[string]interface{}, error) {
	// Clean up persisted data
	if torrent != nil {
		if err := p.removeTorrentState(torrent.ID); err != nil {
			log.Printf("Failed to remove persisted torrent state: %v", err)
		}
	}

	return params, nil
}

func (p *PersistencePlugin) handleMetadataSet(ctx context.Context, torrent *rpc.TorrentState, params map[string]interface{}) (map[string]interface{}, error) {
	// Save metadata changes immediately
	if torrent != nil && p.metadataManager != nil {
		if err := p.saveMetadata(torrent.ID); err != nil {
			log.Printf("Failed to save metadata: %v", err)
		}
	}

	return params, nil
}

// Persistence operations

func (p *PersistencePlugin) periodicSave() {
	ticker := time.NewTicker(p.saveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := p.saveState(); err != nil {
				log.Printf("Periodic save failed: %v", err)
			}
		case <-p.shutdown:
			return
		}
	}
}

func (p *PersistencePlugin) saveState() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.saveCount++
	p.lastSave = time.Now()

	// In a real implementation, this would save all torrent states
	// For this example, we'll just create a marker file
	markerPath := filepath.Join(p.dataDir, "last_save.json")
	data := map[string]interface{}{
		"timestamp":  time.Now(),
		"save_count": p.saveCount,
		"plugin_id":  p.id,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal save data: %w", err)
	}

	if err := os.WriteFile(markerPath, jsonData, 0o644); err != nil {
		return fmt.Errorf("failed to write save marker: %w", err)
	}

	return nil
}

func (p *PersistencePlugin) saveTorrentState(torrent *rpc.TorrentState) error {
	if torrent == nil {
		return fmt.Errorf("torrent state is nil")
	}

	torrentDir := filepath.Join(p.dataDir, "torrents")
	if err := os.MkdirAll(torrentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create torrents directory: %w", err)
	}

	filename := fmt.Sprintf("torrent_%d.json", torrent.ID)
	filePath := filepath.Join(torrentDir, filename)

	jsonData, err := json.Marshal(torrent)
	if err != nil {
		return fmt.Errorf("failed to marshal torrent state: %w", err)
	}

	if err := os.WriteFile(filePath, jsonData, 0o644); err != nil {
		return fmt.Errorf("failed to write torrent state: %w", err)
	}

	return nil
}

func (p *PersistencePlugin) removeTorrentState(torrentID int64) error {
	torrentDir := filepath.Join(p.dataDir, "torrents")
	filename := fmt.Sprintf("torrent_%d.json", torrentID)
	filePath := filepath.Join(torrentDir, filename)

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove torrent state file: %w", err)
	}

	return nil
}

func (p *PersistencePlugin) saveMetadata(torrentID int64) error {
	if p.metadataManager == nil {
		return fmt.Errorf("metadata manager not available")
	}

	// Get metadata for this torrent
	query := &rpc.MetadataQuery{TorrentIDs: []int64{torrentID}}
	metadata := p.metadataManager.GetMetadata(query)

	if torrentMeta, exists := metadata[torrentID]; exists {
		metadataDir := filepath.Join(p.dataDir, "metadata")
		if err := os.MkdirAll(metadataDir, 0o755); err != nil {
			return fmt.Errorf("failed to create metadata directory: %w", err)
		}

		filename := fmt.Sprintf("metadata_%d.json", torrentID)
		filePath := filepath.Join(metadataDir, filename)

		jsonData, err := json.Marshal(torrentMeta)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		if err := os.WriteFile(filePath, jsonData, 0o644); err != nil {
			return fmt.Errorf("failed to write metadata: %w", err)
		}
	}

	return nil
}

func (p *PersistencePlugin) LoadTorrentState(torrentID int64) (*rpc.TorrentState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.loadCount++
	p.lastLoad = time.Now()

	torrentDir := filepath.Join(p.dataDir, "torrents")
	filename := fmt.Sprintf("torrent_%d.json", torrentID)
	filePath := filepath.Join(torrentDir, filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Not found, not an error
		}
		return nil, fmt.Errorf("failed to read torrent state file: %w", err)
	}

	var torrent rpc.TorrentState
	if err := json.Unmarshal(data, &torrent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal torrent state: %w", err)
	}

	return &torrent, nil
}

// Example usage and demonstration

// demonstratePersistencePlugin demonstrates the complete persistence plugin integration workflow.
// This function coordinates the setup, execution, monitoring, and cleanup of persistence plugin features.
func demonstratePersistencePlugin() {
	fmt.Println("=== Plugin-Based Persistence Integration Example ===")

	pluginManager, metadataManager, persistencePlugin, tempDir := setupPluginEnvironment()
	
	torrent := createSampleTorrent()
	params := map[string]interface{}{"source": "user_add"}
	ctx := context.Background()

	demonstrateBehaviorExecution(ctx, pluginManager, torrent, params)
	demonstrateMetadataIntegration(pluginManager, metadataManager, torrent, params, ctx)
	displayPluginMetricsAndHealth(pluginManager, persistencePlugin)
	waitForPeriodicSave(pluginManager, persistencePlugin)
	demonstrateStateLoading(persistencePlugin)
	demonstrateTorrentRemoval(pluginManager, torrent, params, ctx)
	cleanupResources(pluginManager, tempDir)

	fmt.Println("Plugin-based persistence demonstration completed!")
}

// setupPluginEnvironment initializes and configures the plugin manager, metadata manager, and persistence plugin.
// Returns the configured components and the temporary directory path for cleanup.
func setupPluginEnvironment() (*rpc.PluginManager, *rpc.MetadataManager, *PersistencePlugin, string) {
	pluginManager := rpc.NewPluginManager()
	pluginManager.SetLogger(func(format string, args ...interface{}) {
		log.Printf("[PluginManager] "+format, args...)
	})

	metadataManager := rpc.NewMetadataManager()
	tempDir := "/tmp/torrent_persistence_example"
	persistencePlugin := NewPersistencePlugin(tempDir, 5*time.Second)

	config := map[string]interface{}{
		"save_interval":    "3s",
		"metadata_manager": metadataManager,
	}

	if err := pluginManager.RegisterPlugin(persistencePlugin, config); err != nil {
		log.Fatalf("Failed to register persistence plugin: %v", err)
	}

	return pluginManager, metadataManager, persistencePlugin, tempDir
}

// createSampleTorrent creates a sample torrent state for demonstration purposes.
// Returns a configured TorrentState with typical download parameters.
func createSampleTorrent() *rpc.TorrentState {
	return &rpc.TorrentState{
		ID:          1,
		Status:      rpc.TorrentStatusDownloading,
		DownloadDir: "/downloads",
		Downloaded:  1024,
		Uploaded:    512,
	}
}

// demonstrateBehaviorExecution shows how the plugin manager executes behavior plugins for torrent operations.
// Executes a torrent-add behavior and reports the results.
func demonstrateBehaviorExecution(ctx context.Context, pluginManager *rpc.PluginManager, torrent *rpc.TorrentState, params map[string]interface{}) {
	fmt.Println("\n--- Demonstrating Plugin Behavior Execution ---")

	result, err := pluginManager.ExecuteBehaviorPlugins(ctx, "torrent-add", torrent, params)
	if err != nil {
		log.Printf("Behavior execution failed: %v", err)
	} else {
		fmt.Printf("Torrent add behavior result: %+v\n", result)
	}
}

// demonstrateMetadataIntegration shows how metadata operations integrate with the persistence plugin.
// Sets metadata for a torrent and triggers the metadata-set behavior.
func demonstrateMetadataIntegration(pluginManager *rpc.PluginManager, metadataManager *rpc.MetadataManager, torrent *rpc.TorrentState, params map[string]interface{}, ctx context.Context) {
	fmt.Println("\n--- Demonstrating Metadata Integration ---")

	metadataRequest := &rpc.MetadataRequest{
		TorrentID: 1,
		Set: map[string]interface{}{
			"category":    "movies",
			"priority":    "high",
			"custom_tags": []string{"example", "demo"},
		},
		Source: "user",
		Tags:   []string{"demo"},
	}

	metadataResponse := metadataManager.SetMetadata(metadataRequest)
	if metadataResponse.Success {
		fmt.Printf("Metadata set successfully, version: %d\n", metadataResponse.NewVersion)

		_, err := pluginManager.ExecuteBehaviorPlugins(ctx, "metadata-set", torrent, params)
		if err != nil {
			log.Printf("Metadata behavior execution failed: %v", err)
		}
	} else {
		fmt.Printf("Metadata set failed: %v\n", metadataResponse.Errors)
	}
}

// displayPluginMetricsAndHealth retrieves and displays current plugin metrics and health status.
// Shows performance data and operational status for the persistence plugin.
func displayPluginMetricsAndHealth(pluginManager *rpc.PluginManager, persistencePlugin *PersistencePlugin) {
	fmt.Println("\n--- Plugin Metrics ---")

	metrics := pluginManager.GetMetrics()
	if pluginMetrics, exists := metrics[persistencePlugin.ID()]; exists {
		fmt.Printf("Persistence Plugin Metrics:\n")
		for key, value := range pluginMetrics.(map[string]interface{}) {
			fmt.Printf("  %s: %v\n", key, value)
		}
	}

	health := pluginManager.GetPluginHealthStatus()
	if pluginHealth, exists := health[persistencePlugin.ID()]; exists {
		fmt.Printf("\nPersistence Plugin Health:\n")
		fmt.Printf("  Status: %s\n", pluginHealth.Status)
		fmt.Printf("  Message: %s\n", pluginHealth.Message)
	}
}

// waitForPeriodicSave pauses execution to allow the persistence plugin to complete a save cycle.
// Displays updated metrics after the save operation completes.
func waitForPeriodicSave(pluginManager *rpc.PluginManager, persistencePlugin *PersistencePlugin) {
	fmt.Println("\n--- Waiting for Periodic Save ---")
	time.Sleep(4 * time.Second)

	metrics := pluginManager.GetMetrics()
	if pluginMetrics, exists := metrics[persistencePlugin.ID()]; exists {
		fmt.Printf("Updated Plugin Metrics:\n")
		for key, value := range pluginMetrics.(map[string]interface{}) {
			fmt.Printf("  %s: %v\n", key, value)
		}
	}
}

// demonstrateStateLoading shows how to retrieve previously saved torrent state data.
// Attempts to load torrent state and displays the results or error messages.
func demonstrateStateLoading(persistencePlugin *PersistencePlugin) {
	fmt.Println("\n--- Demonstrating State Loading ---")

	loadedTorrent, err := persistencePlugin.LoadTorrentState(1)
	if err != nil {
		log.Printf("Failed to load torrent state: %v", err)
	} else if loadedTorrent != nil {
		fmt.Printf("Loaded torrent state: ID=%d, Status=%d, Downloaded=%d\n",
			loadedTorrent.ID, loadedTorrent.Status, loadedTorrent.Downloaded)
	} else {
		fmt.Println("No saved state found for torrent 1")
	}
}

// demonstrateTorrentRemoval executes the torrent removal behavior through the plugin system.
// Shows how torrent removal operations are handled by behavior plugins.
func demonstrateTorrentRemoval(pluginManager *rpc.PluginManager, torrent *rpc.TorrentState, params map[string]interface{}, ctx context.Context) {
	fmt.Println("\n--- Demonstrating Torrent Removal ---")

	_, err := pluginManager.ExecuteBehaviorPlugins(ctx, "torrent-remove", torrent, params)
	if err != nil {
		log.Printf("Remove behavior execution failed: %v", err)
	} else {
		fmt.Printf("Torrent remove behavior completed\n")
	}
}

// cleanupResources performs cleanup operations for the plugin demonstration.
// Shuts down the plugin manager and removes temporary directories.
func cleanupResources(pluginManager *rpc.PluginManager, tempDir string) {
	fmt.Println("\n--- Cleanup ---")

	if err := pluginManager.Shutdown(); err != nil {
		log.Printf("Plugin manager shutdown failed: %v", err)
	}

	if err := os.RemoveAll(tempDir); err != nil {
		log.Printf("Failed to cleanup temp directory: %v", err)
	}
}

func main() {
	demonstratePersistencePlugin()
}
