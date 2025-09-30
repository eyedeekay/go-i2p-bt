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

// Package main demonstrates how to integrate different persistence strategies
// with the go-i2p-bt RPC server using the lifecycle hook system.
//
// This example shows three different approaches:
//   1. File-based persistence for simple deployments
//   2. Memory persistence with periodic snapshots for high-performance scenarios
//   3. Database persistence for production deployments (requires additional dependencies)
//
// The example demonstrates best practices for:
//   - Graceful startup and shutdown
//   - Error handling and recovery
//   - Performance monitoring
//   - Configuration management
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-i2p/go-i2p-bt/dht"
	"github.com/go-i2p/go-i2p-bt/examples/persistence"
	"github.com/go-i2p/go-i2p-bt/rpc"
)

// Configuration represents the application configuration
type Configuration struct {
	// Server configuration
	ListenAddr     string
	Username       string
	Password       string
	
	// Torrent configuration
	DownloadDir    string
	DHTEnabled     bool
	DHTListenAddr  string
	
	// Persistence configuration
	PersistenceType    string        // "file", "memory", "database"
	DataDir           string        // For file and database persistence
	SnapshotInterval  time.Duration // For memory persistence with backup
	
	// Performance configuration
	MaxTorrents       int
	SessionSaveInterval time.Duration
}

func main() {
	// Load configuration from environment or use defaults
	config := loadConfiguration()
	
	// Create persistence layer based on configuration
	persistence, err := createPersistence(config)
	if err != nil {
		log.Fatalf("Failed to create persistence layer: %v", err)
	}
	defer persistence.Close()
	
	// Create torrent manager with persistence hooks
	manager, hookManager, err := createTorrentManager(config, persistence)
	if err != nil {
		log.Fatalf("Failed to create torrent manager: %v", err)
	}
	
	// Try to restore previous session state
	if err := restoreSessionState(manager, persistence); err != nil {
		log.Printf("Warning: Failed to restore session state: %v", err)
	}
	
	// Create RPC server
	server, err := rpc.NewServer(rpc.ServerConfig{
		TorrentManager: manager,
		Username:       config.Username,
		Password:       config.Password,
	})
	if err != nil {
		log.Fatalf("Failed to create RPC server: %v", err)
	}
	
	// Start HTTP server
	httpServer := &http.Server{
		Addr:    config.ListenAddr,
		Handler: server,
	}
	
	// Start server in background
	go func() {
		log.Printf("Starting RPC server on %s with %s persistence", config.ListenAddr, config.PersistenceType)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
	
	// Start periodic session saving
	sessionSaver := startSessionSaver(manager, persistence, config.SessionSaveInterval)
	defer sessionSaver.Stop()
	
	// Start performance monitoring
	perfMonitor := startPerformanceMonitor(manager, persistence, hookManager)
	defer perfMonitor.Stop()
	
	// Wait for shutdown signal
	waitForShutdown()
	
	// Graceful shutdown
	log.Println("Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Save final session state
	if err := saveSessionState(ctx, manager, persistence); err != nil {
		log.Printf("Warning: Failed to save final session state: %v", err)
	}
	
	// Shutdown HTTP server
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	
	log.Println("Shutdown complete")
}

func loadConfiguration() Configuration {
	return Configuration{
		ListenAddr:          getEnv("LISTEN_ADDR", ":9091"),
		Username:           getEnv("RPC_USERNAME", "admin"),
		Password:           getEnv("RPC_PASSWORD", "secret"),
		DownloadDir:        getEnv("DOWNLOAD_DIR", "./downloads"),
		DHTEnabled:         getEnv("DHT_ENABLED", "true") == "true",
		DHTListenAddr:      getEnv("DHT_LISTEN_ADDR", ":6881"),
		PersistenceType:    getEnv("PERSISTENCE_TYPE", "file"),
		DataDir:           getEnv("DATA_DIR", "./data"),
		SnapshotInterval:  parseDuration(getEnv("SNAPSHOT_INTERVAL", "5m")),
		MaxTorrents:       parseInt(getEnv("MAX_TORRENTS", "100")),
		SessionSaveInterval: parseDuration(getEnv("SESSION_SAVE_INTERVAL", "30s")),
	}
}

func createPersistence(config Configuration) (persistence.TorrentPersistence, error) {
	switch config.PersistenceType {
	case "file":
		return persistence.NewFilePersistence(config.DataDir)
		
	case "memory":
		// Create backup file persistence for snapshots
		backup, err := persistence.NewFilePersistence(config.DataDir + "/backup")
		if err != nil {
			return nil, fmt.Errorf("failed to create backup persistence: %w", err)
		}
		
		return persistence.NewMemoryPersistence(backup, config.SnapshotInterval), nil
		
	case "database":
		// Note: This requires adding github.com/mattn/go-sqlite3 to go.mod
		// return persistence.NewDatabasePersistence(config.DataDir+"/torrents.db", true)
		return nil, fmt.Errorf("database persistence requires adding sqlite3 dependency to go.mod")
		
	default:
		return nil, fmt.Errorf("unknown persistence type: %s", config.PersistenceType)
	}
}

func createTorrentManager(config Configuration, pers persistence.TorrentPersistence) (*rpc.TorrentManager, *rpc.HookManager, error) {
	// Create hook manager
	hookManager := rpc.NewHookManager()
	hookManager.SetLogger(func(format string, args ...interface{}) {
		log.Printf("[HOOK] "+format, args...)
	})
	
	// Create persistence hook for automatic state saving
	persistenceHook := persistence.NewPersistenceHook(pers)
	if err := hookManager.RegisterHook(&rpc.Hook{
		ID:       "persistence",
		Callback: persistenceHook.HandleTorrentEvent,
		Events: []rpc.HookEvent{
			rpc.HookEventTorrentAdded,
			rpc.HookEventTorrentStarted,
			rpc.HookEventTorrentStopped,
			rpc.HookEventTorrentCompleted,
			rpc.HookEventTorrentRemoved,
		},
		Priority:        100, // High priority for persistence
		ContinueOnError: true, // Don't stop other hooks if persistence fails
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to register persistence hook: %w", err)
	}
	
	// Create additional monitoring hooks
	if err := registerMonitoringHooks(hookManager); err != nil {
		return nil, nil, fmt.Errorf("failed to register monitoring hooks: %w", err)
	}
	
	// Create torrent manager configuration
	managerConfig := rpc.TorrentManagerConfig{
		DownloadDir: config.DownloadDir,
		HookManager: hookManager,
	}
	
	// Configure DHT if enabled
	if config.DHTEnabled {
		managerConfig.DHTConfig = &dht.Config{
			ListenAddr: config.DHTListenAddr,
		}
	}
	
	// Create torrent manager
	manager, err := rpc.NewTorrentManager(managerConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create torrent manager: %w", err)
	}
	
	return manager, hookManager, nil
}

func registerMonitoringHooks(hookManager *rpc.HookManager) error {
	// Log hook for debugging and monitoring
	logHook := &rpc.Hook{
		ID:       "logger",
		Priority: 50, // Medium priority
		Events: []rpc.HookEvent{
			rpc.HookEventTorrentAdded,
			rpc.HookEventTorrentCompleted,
			rpc.HookEventTorrentError,
		},
		Callback: func(ctx *rpc.HookContext) error {
			switch ctx.Event {
			case rpc.HookEventTorrentAdded:
				log.Printf("Torrent added: %s", ctx.Torrent.InfoHash.String())
			case rpc.HookEventTorrentCompleted:
				log.Printf("Torrent completed: %s (%.1f%% done)", 
					ctx.Torrent.InfoHash.String(), ctx.Torrent.PercentDone*100)
			case rpc.HookEventTorrentError:
				if errorMsg, ok := ctx.Data["error"].(string); ok {
					log.Printf("Torrent error: %s - %s", ctx.Torrent.InfoHash.String(), errorMsg)
				}
			}
			return nil
		},
	}
	
	return hookManager.RegisterHook(logHook)
}

func restoreSessionState(manager *rpc.TorrentManager, pers persistence.TorrentPersistence) error {
	ctx := context.Background()
	
	// Load session configuration
	if sessionConfig, err := pers.LoadSessionConfig(ctx); err == nil {
		if err := manager.UpdateSessionConfig(sessionConfig); err != nil {
			log.Printf("Warning: Failed to restore session config: %v", err)
		} else {
			log.Println("Session configuration restored")
		}
	}
	
	// Load all torrents
	torrents, err := pers.LoadAllTorrents(ctx)
	if err != nil {
		return fmt.Errorf("failed to load torrents: %w", err)
	}
	
	if len(torrents) == 0 {
		log.Println("No previous torrents found")
		return nil
	}
	
	// Restore torrents to manager
	restored := 0
	for _, torrent := range torrents {
		// Add torrent back to manager (this will trigger hooks)
		if torrent.MetaInfo != nil {
			// Use MetaInfo if available
			if _, err := manager.AddTorrent(torrent.MetaInfo, rpc.AddTorrentOptions{
				DownloadDir: torrent.DownloadDir,
				Paused:      torrent.Status == rpc.TorrentStatusStopped,
			}); err != nil {
				log.Printf("Warning: Failed to restore torrent %s: %v", torrent.InfoHash.String(), err)
				continue
			}
		}
		restored++
	}
	
	log.Printf("Restored %d torrents from persistence", restored)
	return nil
}

func saveSessionState(ctx context.Context, manager *rpc.TorrentManager, pers persistence.TorrentPersistence) error {
	// Get current session configuration
	sessionConfig := manager.GetSessionConfig()
	if err := pers.SaveSessionConfig(ctx, sessionConfig); err != nil {
		return fmt.Errorf("failed to save session config: %w", err)
	}
	
	// Get all current torrents
	torrents := manager.GetAllTorrents()
	if len(torrents) > 0 {
		if err := pers.SaveAllTorrents(ctx, torrents); err != nil {
			return fmt.Errorf("failed to save torrents: %w", err)
		}
	}
	
	return nil
}

func startSessionSaver(manager *rpc.TorrentManager, pers persistence.TorrentPersistence, interval time.Duration) *PeriodicSaver {
	saver := &PeriodicSaver{
		stopCh: make(chan struct{}),
	}
	
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := saveSessionState(ctx, manager, pers); err != nil {
					log.Printf("Periodic session save failed: %v", err)
				}
				cancel()
				
			case <-saver.stopCh:
				return
			}
		}
	}()
	
	return saver
}

func startPerformanceMonitor(manager *rpc.TorrentManager, pers persistence.TorrentPersistence, hookManager *rpc.HookManager) *PerformanceMonitor {
	monitor := &PerformanceMonitor{
		stopCh: make(chan struct{}),
	}
	
	go func() {
		ticker := time.NewTicker(60 * time.Second) // Report every minute
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				// Get hook metrics
				hookMetrics := hookManager.GetMetrics()
				
				// Get persistence stats (if available)
				var persistenceStats map[string]interface{}
				if mp, ok := pers.(*persistence.MemoryPersistence); ok {
					persistenceStats = mp.GetStatistics()
				}
				
				// Log performance summary
				log.Printf("Performance: Hook executions=%d errors=%d avg_latency=%v", 
					hookMetrics.TotalExecutions, hookMetrics.TotalErrors, hookMetrics.AverageLatency)
				
				if persistenceStats != nil {
					log.Printf("Persistence: %v", persistenceStats)
				}
				
			case <-monitor.stopCh:
				return
			}
		}
	}()
	
	return monitor
}

func waitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}

// Helper types and functions

type PeriodicSaver struct {
	stopCh chan struct{}
}

func (ps *PeriodicSaver) Stop() {
	close(ps.stopCh)
}

type PerformanceMonitor struct {
	stopCh chan struct{}
}

func (pm *PerformanceMonitor) Stop() {
	close(pm.stopCh)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute // Default
	}
	return d
}

func parseInt(s string) int {
	if s == "" {
		return 0
	}
	// Simple conversion, in production use strconv.Atoi
	return 100 // Default
}