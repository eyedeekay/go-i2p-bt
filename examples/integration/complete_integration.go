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

// Package main demonstrates complete integration of all new systems
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-i2p/go-i2p-bt/rpc"
)

// IntegratedTorrentManager demonstrates how all three systems work together
type IntegratedTorrentManager struct {
	// Core systems
	pluginManager   *rpc.PluginManager
	eventSystem     *rpc.EventNotificationSystem
	metadataManager *rpc.MetadataManager

	// Example plugins
	persistencePlugin *PersistencePlugin

	// Event subscribers
	analyticsSubscriber *AnalyticsEventSubscriber

	// Configuration
	config IntegratedConfig
}

// IntegratedConfig holds configuration for the integrated system
type IntegratedConfig struct {
	EnablePersistence bool
	EnableAnalytics   bool
	EnableLogging     bool

	PersistenceDir    string
	AnalyticsInterval time.Duration
	LogLevel          string
}

// Simple Analytics Event Subscriber for demonstration
type AnalyticsEventSubscriber struct {
	id          string
	eventCounts map[rpc.EventType]int64
	lastEvents  map[rpc.EventType]time.Time
}

func NewAnalyticsEventSubscriber() *AnalyticsEventSubscriber {
	return &AnalyticsEventSubscriber{
		id:          "analytics-subscriber",
		eventCounts: make(map[rpc.EventType]int64),
		lastEvents:  make(map[rpc.EventType]time.Time),
	}
}

func (aes *AnalyticsEventSubscriber) ID() string {
	return aes.id
}

func (aes *AnalyticsEventSubscriber) GetSubscriptionFilters() []rpc.EventType {
	return []rpc.EventType{
		rpc.EventTorrentAdded,
		rpc.EventTorrentStarted,
		rpc.EventTorrentCompleted,
		rpc.EventTorrentRemoved,
		rpc.EventSessionStarted,
		rpc.EventSessionStopped,
	}
}

func (aes *AnalyticsEventSubscriber) HandleEvent(ctx context.Context, event *rpc.Event) error {
	aes.eventCounts[event.Type]++
	aes.lastEvents[event.Type] = time.Now()

	log.Printf("[Analytics] Event: %s from %s", event.Type, event.Source)

	return nil
}

func (aes *AnalyticsEventSubscriber) GetMetrics() map[rpc.EventType]int64 {
	result := make(map[rpc.EventType]int64)
	for eventType, count := range aes.eventCounts {
		result[eventType] = count
	}
	return result
}

// NewIntegratedTorrentManager creates a fully integrated torrent manager
func NewIntegratedTorrentManager(config IntegratedConfig) *IntegratedTorrentManager {
	manager := &IntegratedTorrentManager{
		pluginManager:   rpc.NewPluginManager(),
		eventSystem:     rpc.NewEventNotificationSystem(),
		metadataManager: rpc.NewMetadataManager(),
		config:          config,
	}

	// Configure metadata manager
	constraints := rpc.MetadataConstraints{
		MaxKeyLength:      100,
		MaxValueSize:      10240, // 10KB
		MaxKeysPerTorrent: 50,
		AllowedTypes:      []string{"string", "int", "int64", "float64", "bool", "map", "slice"},
		RequiredKeys:      []string{},
		ReadOnlyKeys:      []string{"created_at"}, // Prevent modification of creation timestamp
	}
	manager.metadataManager.SetConstraints(constraints)

	// Set up event system logger
	manager.eventSystem.SetLogger(func(format string, args ...interface{}) {
		if config.LogLevel != "silent" {
			log.Printf("[EventSystem] "+format, args...)
		}
	})

	// Initialize components based on configuration
	manager.initializeComponents()

	return manager
}

// initializeComponents sets up all components based on configuration
func (itm *IntegratedTorrentManager) initializeComponents() {
	// Initialize plugins
	if itm.config.EnablePersistence {
		itm.persistencePlugin = NewPersistencePlugin(itm.config.PersistenceDir, 5*time.Second)
		if err := itm.pluginManager.RegisterPlugin(itm.persistencePlugin, map[string]interface{}{
			"enabled": true,
			"dir":     itm.config.PersistenceDir,
		}); err != nil {
			log.Printf("Failed to register persistence plugin: %v", err)
		}
	}

	// Initialize event subscribers
	if itm.config.EnableAnalytics {
		itm.analyticsSubscriber = NewAnalyticsEventSubscriber()
		if err := itm.eventSystem.Subscribe(itm.analyticsSubscriber); err != nil {
			log.Printf("Failed to subscribe analytics subscriber: %v", err)
		}
	}
}

// Torrent lifecycle methods that integrate all systems

func (itm *IntegratedTorrentManager) AddTorrent(torrentID int64, magnetLink, downloadDir string) error {
	fmt.Printf("Adding torrent %d with magnet link: %s\n", torrentID, magnetLink)

	// Set initial metadata
	request := &rpc.MetadataRequest{
		TorrentID: torrentID,
		Set: map[string]interface{}{
			"created_at":     time.Now().Unix(),
			"magnet_link":    magnetLink,
			"download_dir":   downloadDir,
			"status":         "added",
			"progress":       0.0,
			"download_speed": 0.0,
			"upload_speed":   0.0,
			"peers":          0,
		},
		Source: "torrent_manager",
		Tags:   []string{"torrent", "lifecycle"},
	}

	response := itm.metadataManager.SetMetadata(request)
	if !response.Success {
		return fmt.Errorf("failed to set initial metadata: %v", response.Errors)
	}

	// Execute behavior plugins if persistence plugin is registered
	if itm.persistencePlugin != nil {
		pluginInfo := itm.pluginManager.GetPluginInfo()
		for pluginID, info := range pluginInfo {
			if pluginID == itm.persistencePlugin.ID() {
				log.Printf("Executing persistence behavior for torrent add: %s", info.Name)
			}
		}
	}

	// Publish event
	itm.eventSystem.PublishTorrentEvent(rpc.EventTorrentAdded, &rpc.TorrentState{
		ID:          torrentID,
		Status:      rpc.TorrentStatusStopped,
		DownloadDir: downloadDir,
	}, map[string]interface{}{
		"magnet_link": magnetLink,
		"source":      "user",
	})

	return nil
}

func (itm *IntegratedTorrentManager) StartTorrent(torrentID int64) error {
	fmt.Printf("Starting torrent %d\n", torrentID)

	// Update metadata
	request := &rpc.MetadataRequest{
		TorrentID: torrentID,
		Set: map[string]interface{}{
			"status":     "downloading",
			"started_at": time.Now().Unix(),
		},
		Source: "torrent_manager",
		Tags:   []string{"torrent", "lifecycle"},
	}

	response := itm.metadataManager.SetMetadata(request)
	if !response.Success {
		log.Printf("Failed to update metadata: %v", response.Errors)
	}

	// Publish event
	itm.eventSystem.PublishTorrentEvent(rpc.EventTorrentStarted, &rpc.TorrentState{
		ID:     torrentID,
		Status: rpc.TorrentStatusDownloading,
	}, map[string]interface{}{
		"auto_start": false,
	})

	return nil
}

func (itm *IntegratedTorrentManager) UpdateTorrentProgress(torrentID int64, progress, downloadSpeed, uploadSpeed float64, peers int) error {
	// Update metadata
	request := &rpc.MetadataRequest{
		TorrentID: torrentID,
		Set: map[string]interface{}{
			"progress":       progress,
			"download_speed": downloadSpeed,
			"upload_speed":   uploadSpeed,
			"peers":          peers,
			"last_update":    time.Now().Unix(),
		},
		Source: "torrent_manager",
		Tags:   []string{"torrent", "progress"},
	}

	response := itm.metadataManager.SetMetadata(request)
	if !response.Success {
		log.Printf("Failed to update progress metadata: %v", response.Errors)
	}

	// Publish progress event periodically (every 10%)
	if progress > 0 && int(progress*100)%10 == 0 {
		itm.eventSystem.PublishTorrentEvent(rpc.EventTorrentStarted, &rpc.TorrentState{
			ID:     torrentID,
			Status: rpc.TorrentStatusDownloading,
		}, map[string]interface{}{
			"progress":       progress,
			"download_speed": downloadSpeed,
			"upload_speed":   uploadSpeed,
			"peers":          peers,
		})
	}

	return nil
}

func (itm *IntegratedTorrentManager) CompleteTorrent(torrentID int64) error {
	fmt.Printf("Completing torrent %d\n", torrentID)

	// Update metadata
	request := &rpc.MetadataRequest{
		TorrentID: torrentID,
		Set: map[string]interface{}{
			"status":       "completed",
			"progress":     1.0,
			"completed_at": time.Now().Unix(),
		},
		Source: "torrent_manager",
		Tags:   []string{"torrent", "lifecycle"},
	}

	response := itm.metadataManager.SetMetadata(request)
	if !response.Success {
		log.Printf("Failed to update completion metadata: %v", response.Errors)
	}

	// Publish event
	itm.eventSystem.PublishTorrentEvent(rpc.EventTorrentCompleted, &rpc.TorrentState{
		ID:     torrentID,
		Status: rpc.TorrentStatusSeeding,
	}, map[string]interface{}{
		"completion_time": time.Now(),
	})

	return nil
}

func (itm *IntegratedTorrentManager) RemoveTorrent(torrentID int64, deleteFiles bool) error {
	fmt.Printf("Removing torrent %d (delete files: %t)\n", torrentID, deleteFiles)

	// Publish event
	itm.eventSystem.PublishTorrentEvent(rpc.EventTorrentRemoved, &rpc.TorrentState{
		ID:     torrentID,
		Status: rpc.TorrentStatusStopped,
	}, map[string]interface{}{
		"delete_files": deleteFiles,
	})

	// Remove metadata (this should be done last)
	if !itm.metadataManager.RemoveAllMetadata(torrentID) {
		log.Printf("Failed to remove metadata for torrent %d", torrentID)
	}

	return nil
}

// Utility methods

func (itm *IntegratedTorrentManager) GetTorrentStatus(torrentID int64) (map[string]interface{}, error) {
	query := &rpc.MetadataQuery{
		TorrentIDs: []int64{torrentID},
	}

	results := itm.metadataManager.GetMetadata(query)
	if torrentMeta, exists := results[torrentID]; exists {
		status := make(map[string]interface{})
		for key, metadata := range torrentMeta.Metadata {
			status[key] = metadata.Value
		}
		return status, nil
	}

	return nil, fmt.Errorf("torrent %d not found", torrentID)
}

func (itm *IntegratedTorrentManager) ListTorrents() []int64 {
	results := itm.metadataManager.GetMetadata(&rpc.MetadataQuery{})

	var torrentIDs []int64
	for torrentID := range results {
		torrentIDs = append(torrentIDs, torrentID)
	}

	return torrentIDs
}

func (itm *IntegratedTorrentManager) GetSystemReport() string {
	report := "=== Integrated Torrent Manager System Report ===\n\n"

	// Plugin information
	pluginInfo := itm.pluginManager.GetPluginInfo()
	report += fmt.Sprintf("Registered Plugins: %d\n", len(pluginInfo))
	for pluginID, info := range pluginInfo {
		report += fmt.Sprintf("  - %s (ID: %s)\n", info.Name, pluginID)
	}
	report += "\n"

	// Event system information (simplified since GetSubscribers is not available)
	report += "Event System Status: Active\n"
	if itm.analyticsSubscriber != nil {
		report += "  - Analytics Subscriber: Active\n"
	}
	report += "\n"

	// Metadata information
	metrics := itm.metadataManager.GetMetrics()
	report += "Metadata Manager Status:\n"
	report += fmt.Sprintf("  Total Torrents: %d\n", metrics.TotalTorrents)
	report += fmt.Sprintf("  Total Keys: %d\n", metrics.TotalKeys)
	report += fmt.Sprintf("  Total Operations: %d\n", metrics.TotalOperations)
	report += fmt.Sprintf("  Validation Errors: %d\n", metrics.ValidationErrors)
	report += "\n"

	// Plugin metrics
	if itm.config.EnablePersistence && itm.persistencePlugin != nil {
		persistenceMetrics := itm.persistencePlugin.GetMetrics()
		report += "Persistence Plugin Metrics:\n"
		for key, value := range persistenceMetrics {
			report += fmt.Sprintf("  %s: %v\n", key, value)
		}
		report += "\n"
	}

	return report
}

func (itm *IntegratedTorrentManager) Shutdown() error {
	fmt.Println("Shutting down Integrated Torrent Manager...")

	// Publish shutdown event
	itm.eventSystem.PublishSessionEvent(rpc.EventSessionStopped, map[string]interface{}{
		"shutdown_time": time.Now(),
		"reason":        "user_requested",
	})

	// Shutdown event system
	if err := itm.eventSystem.Shutdown(); err != nil {
		log.Printf("Event system shutdown failed: %v", err)
	}

	// Unregister all plugins
	pluginInfo := itm.pluginManager.GetPluginInfo()
	for pluginID := range pluginInfo {
		if err := itm.pluginManager.UnregisterPlugin(pluginID); err != nil {
			log.Printf("Failed to unregister plugin %s: %v", pluginID, err)
		}
	}

	fmt.Println("Shutdown complete")
	return nil
}

// Example usage and demonstration

func demonstrateCompleteIntegration() {
	fmt.Println("=== Complete Integration Example ===")

	// Create temporary directory for persistence
	tempDir, err := os.MkdirTemp("", "torrent_manager_*")
	if err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Configure the integrated manager
	config := IntegratedConfig{
		EnablePersistence: true,
		EnableAnalytics:   true,
		EnableLogging:     true,
		PersistenceDir:    tempDir,
		AnalyticsInterval: 5 * time.Second,
		LogLevel:          "info",
	}

	// Create integrated manager
	manager := NewIntegratedTorrentManager(config)

	fmt.Println("\n--- System Initialization Complete ---")
	fmt.Println(manager.GetSystemReport())

	// Simulate torrent lifecycle
	fmt.Println("\n--- Simulating Torrent Lifecycle ---")

	// Add torrents
	manager.AddTorrent(1001, "magnet:?xt=urn:btih:example1", "/downloads/movies")
	manager.AddTorrent(1002, "magnet:?xt=urn:btih:example2", "/downloads/tv")
	manager.AddTorrent(1003, "magnet:?xt=urn:btih:example3", "/downloads/music")

	time.Sleep(500 * time.Millisecond) // Allow plugins to process

	// Start torrents
	manager.StartTorrent(1001)
	manager.StartTorrent(1002)
	manager.StartTorrent(1003)

	time.Sleep(500 * time.Millisecond)

	// Simulate progress updates
	for i := 1; i <= 10; i++ {
		progress := float64(i) * 0.1

		manager.UpdateTorrentProgress(1001, progress, 1024*1024*float64(i), 512*1024*float64(i), 10+i)
		manager.UpdateTorrentProgress(1002, progress*0.8, 512*1024*float64(i), 256*1024*float64(i), 8+i)
		manager.UpdateTorrentProgress(1003, progress*0.6, 256*1024*float64(i), 128*1024*float64(i), 5+i)

		time.Sleep(200 * time.Millisecond)
	}

	// Complete some torrents
	manager.CompleteTorrent(1001)
	time.Sleep(200 * time.Millisecond)
	manager.CompleteTorrent(1003)

	time.Sleep(500 * time.Millisecond)

	// Show torrent statuses
	fmt.Println("\n--- Torrent Statuses ---")
	for _, torrentID := range manager.ListTorrents() {
		if status, err := manager.GetTorrentStatus(torrentID); err == nil {
			fmt.Printf("Torrent %d: Status=%s, Progress=%.1f%%\n",
				torrentID, status["status"], status["progress"].(float64)*100)
		}
	}

	// Show final system report
	fmt.Println("\n--- Final System Report ---")
	fmt.Println(manager.GetSystemReport())

	// Remove a torrent
	manager.RemoveTorrent(1002, false)

	time.Sleep(500 * time.Millisecond)

	// Show metrics from event subscribers
	if manager.analyticsSubscriber != nil {
		fmt.Println("\n--- Event Analytics Metrics ---")
		metrics := manager.analyticsSubscriber.GetMetrics()
		for eventType, count := range metrics {
			fmt.Printf("  %s: %d events\n", eventType, count)
		}
	}

	// Shutdown system
	fmt.Println("\n--- Shutting Down ---")
	manager.Shutdown()

	fmt.Println("Complete integration demonstration finished!")
}

// demonstrateCompleteIntegration can be called from a main function to run the example
// func main() {
//     demonstrateCompleteIntegration()
// }
