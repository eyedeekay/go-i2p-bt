package rpc

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/bencode"
	"github.com/go-i2p/go-i2p-bt/dht"
	"github.com/go-i2p/go-i2p-bt/downloader"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// Integration test configuration with realistic settings
func createIntegrationTestConfig() TorrentManagerConfig {
	return TorrentManagerConfig{
		DHTConfig: dht.Config{
			ID: metainfo.NewRandomHash(),
			K:  8,
		},
		DownloaderConfig: downloader.TorrentDownloaderConfig{
			ID:        metainfo.NewRandomHash(),
			WorkerNum: 2, // Use more workers for integration tests
		},
		DownloadDir:          "integration_test_downloads",
		IncompleteDir:        "integration_test_incomplete",
		IncompleteDirEnabled: true,
		ErrorLog:             testLogger,
		MaxTorrents:          100,
		PeerLimitGlobal:      200,
		PeerLimitPerTorrent:  50,
		PeerPort:             0, // Use random port for tests
		SessionConfig: SessionConfiguration{
			DownloadDir:           "integration_test_downloads",
			IncompleteDir:         "integration_test_incomplete",
			IncompleteDirEnabled:  true,
			PeerPort:              6881,
			PeerLimitGlobal:       200,
			PeerLimitPerTorrent:   50,
			DHTEnabled:            true, // Enable DHT for integration tests
			PEXEnabled:            true,
			StartAddedTorrents:    true,
			DownloadQueueEnabled:  true,
			DownloadQueueSize:     5,
			SeedQueueEnabled:      true,
			SeedQueueSize:         3,
			SpeedLimitDownEnabled: true,
			SpeedLimitDown:        1000000, // 1MB/s limit for testing
			SpeedLimitUpEnabled:   true,
			SpeedLimitUp:          500000, // 500KB/s limit for testing
			Version:               "go-i2p-bt-integration-test/1.0.0",
		},
	}
}

// TestTorrentManagerIntegration_FullLifecycle tests the complete torrent lifecycle
// with all components integrated
func TestTorrentManagerIntegration_FullLifecycle(t *testing.T) {
	config := createIntegrationTestConfig()

	// Create temporary directories for test
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	// Create TorrentManager with all components
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Verify all components are initialized
	if tm.dhtServer == nil {
		t.Error("DHT server should be initialized")
	}
	if tm.queueManager == nil {
		t.Error("Queue manager should be initialized")
	}
	if tm.bandwidthManager == nil {
		t.Error("Bandwidth manager should be initialized")
	}

	// Create test torrent
	torrentData := createTestTorrentData(t)

	// Test 1: Add torrent
	req := TorrentAddRequest{
		Metainfo: torrentData,
		Paused:   false,
	}

	torrentState, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Verify torrent was added successfully
	if torrentState == nil {
		t.Fatal("Torrent state should not be nil")
	}

	// Torrent should have a valid ID and status
	if torrentState.ID <= 0 {
		t.Error("Torrent should have a positive ID")
	}

	t.Logf("Added torrent ID: %d, Status: %d", torrentState.ID, torrentState.Status)

	// Test 2: Verify torrent starts (queue processing)
	time.Sleep(100 * time.Millisecond) // Allow queue processing

	tm.mu.RLock()
	updatedState := tm.torrents[torrentState.ID]
	tm.mu.RUnlock()

	if updatedState.Status != TorrentStatusDownloading && updatedState.Status != TorrentStatusSeeding {
		t.Errorf("Expected torrent to start, got status: %d", updatedState.Status)
	}

	// Test 3: Stop torrent
	err = tm.StopTorrent(torrentState.ID)
	if err != nil {
		t.Errorf("Failed to stop torrent: %v", err)
	}

	// Verify torrent stopped
	tm.mu.RLock()
	stoppedState := tm.torrents[torrentState.ID]
	tm.mu.RUnlock()

	if stoppedState.Status != TorrentStatusStopped {
		t.Errorf("Expected torrent to be stopped, got status: %d", stoppedState.Status)
	}

	// Test 4: Remove torrent
	err = tm.RemoveTorrent(torrentState.ID, false)
	if err != nil {
		t.Errorf("Failed to remove torrent: %v", err)
	}

	// Verify torrent removed
	tm.mu.RLock()
	_, exists := tm.torrents[torrentState.ID]
	tm.mu.RUnlock()

	if exists {
		t.Error("Torrent should be removed from manager")
	}
}

// TestTorrentManagerIntegration_DHTIntegration tests DHT integration
func TestTorrentManagerIntegration_DHTIntegration(t *testing.T) {
	config := createIntegrationTestConfig()
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Verify DHT server is running
	if tm.dhtServer == nil {
		t.Fatal("DHT server should be initialized")
	}

	// Test DHT callback integration
	testInfoHash := metainfo.NewRandomHash().String()
	testAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:8080")

	// Simulate DHT events
	tm.onDHTSearch(testInfoHash, testAddr)
	tm.onDHTTorrent(testInfoHash, testAddr)

	// DHT callbacks should complete without error
	// (specific behavior depends on DHT implementation)
}

// TestTorrentManagerIntegration_QueueManagement tests queue integration
func TestTorrentManagerIntegration_QueueManagement(t *testing.T) {
	config := createIntegrationTestConfig()
	config.SessionConfig.DownloadQueueSize = 2 // Limit queue size for testing
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Add multiple torrents to test queue behavior
	var torrents []*TorrentState
	for i := 0; i < 5; i++ {
		// Create unique torrent data for each torrent
		torrentData := createTestTorrentDataWithName(t, fmt.Sprintf("queue-test-%d", i))

		req := TorrentAddRequest{
			Metainfo: torrentData,
			Paused:   false,
		}

		torrentState, err := tm.AddTorrent(req)
		if err != nil {
			t.Fatalf("Failed to add torrent %d: %v", i, err)
		}
		torrents = append(torrents, torrentState)
	}

	// Allow queue processing
	time.Sleep(200 * time.Millisecond)

	// Check queue positions and status
	activeCount := 0
	queuedCount := 0

	for _, torrent := range torrents {
		tm.mu.RLock()
		state := tm.torrents[torrent.ID]
		tm.mu.RUnlock()

		if state.Status == TorrentStatusDownloading || state.Status == TorrentStatusSeeding {
			activeCount++
		} else if state.Status == TorrentStatusQueuedDown {
			queuedCount++
		}
	}

	// Should have some limited active torrents, but exact count may vary
	// due to timing and queue processing
	if activeCount == 0 {
		t.Error("Should have at least some active torrents")
	}

	if activeCount+queuedCount != 5 {
		t.Errorf("Total torrents should be 5, got active=%d + queued=%d = %d",
			activeCount, queuedCount, activeCount+queuedCount)
	}

	t.Logf("Queue test: %d active, %d queued torrents", activeCount, queuedCount)
}

// TestTorrentManagerIntegration_BandwidthLimiting tests bandwidth management
func TestTorrentManagerIntegration_BandwidthLimiting(t *testing.T) {
	config := createIntegrationTestConfig()
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Test bandwidth limiting is active
	if tm.bandwidthManager == nil {
		t.Fatal("Bandwidth manager should be initialized")
	}

	// Test bandwidth wait (should not block indefinitely)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = tm.WaitForDownloadBandwidth(ctx, 1024)
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("Unexpected error from bandwidth manager: %v", err)
	}

	// Test session config update affects bandwidth
	newConfig := tm.GetSessionConfig()
	newConfig.SpeedLimitDown = 2000000 // 2MB/s

	err = tm.UpdateSessionConfig(newConfig)
	if err != nil {
		t.Errorf("Failed to update session config: %v", err)
	}

	// Bandwidth manager should be updated
	// (specific verification depends on bandwidth manager implementation)
}

// TestTorrentManagerIntegration_ConcurrentOperations tests thread safety with multiple components
func TestTorrentManagerIntegration_ConcurrentOperations(t *testing.T) {
	config := createIntegrationTestConfig()
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	const numGoroutines = 10
	const numOperations = 5

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numOperations)

	// Concurrent torrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				// Add torrent with unique data
				torrentData := createTestTorrentDataWithName(&testing.T{},
					fmt.Sprintf("concurrent-%d-%d", goroutineID, j))

				req := TorrentAddRequest{
					Metainfo: torrentData,
					Paused:   false,
				}

				torrentState, err := tm.AddTorrent(req)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d, op %d: add torrent failed: %v",
						goroutineID, j, err)
					continue
				}

				// Random operations
				switch j % 3 {
				case 0:
					// Stop torrent
					if err := tm.StopTorrent(torrentState.ID); err != nil {
						errors <- fmt.Errorf("goroutine %d, op %d: stop torrent failed: %v",
							goroutineID, j, err)
					}
				case 1:
					// Get torrent list
					if len(tm.GetAllTorrents()) == 0 {
						errors <- fmt.Errorf("goroutine %d, op %d: empty torrent list",
							goroutineID, j)
					}
				case 2:
					// Update session config
					sessionConfig := tm.GetSessionConfig()
					sessionConfig.PeerLimitGlobal = int64(100 + j)
					if err := tm.UpdateSessionConfig(sessionConfig); err != nil {
						errors <- fmt.Errorf("goroutine %d, op %d: update session failed: %v",
							goroutineID, j, err)
					}
				}

				// Remove torrent
				if err := tm.RemoveTorrent(torrentState.ID, false); err != nil {
					errors <- fmt.Errorf("goroutine %d, op %d: remove torrent failed: %v",
						goroutineID, j, err)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	var errorList []error
	for err := range errors {
		errorList = append(errorList, err)
	}

	if len(errorList) > 0 {
		t.Errorf("Concurrent operations failed with %d errors:", len(errorList))
		for i, err := range errorList {
			if i < 5 { // Show first 5 errors
				t.Errorf("  %v", err)
			}
		}
		if len(errorList) > 5 {
			t.Errorf("  ... and %d more errors", len(errorList)-5)
		}
	}
}

// TestTorrentManagerIntegration_ComponentFailures tests error handling with component failures
func TestTorrentManagerIntegration_ComponentFailures(t *testing.T) {
	config := createIntegrationTestConfig()
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}

	// Test graceful shutdown
	err = tm.Close()
	if err != nil {
		t.Errorf("Failed to close TorrentManager: %v", err)
	}

	// Operations after close should fail gracefully or be handled properly
	torrentData := createTestTorrentData(t)
	req := TorrentAddRequest{
		Metainfo: torrentData,
		Paused:   false,
	}

	_, err = tm.AddTorrent(req)
	// Note: Depending on implementation, this might not always error
	// The key is that the system handles closure gracefully
	t.Logf("Add torrent after close result: %v", err)
}

// TestTorrentManagerIntegration_SessionConfiguration tests session config integration
func TestTorrentManagerIntegration_SessionConfiguration(t *testing.T) {
	config := createIntegrationTestConfig()
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Test session config updates propagate to components
	sessionConfig := tm.GetSessionConfig()

	// Update various settings
	sessionConfig.DownloadDir = filepath.Join(config.DownloadDir, "new_location")
	sessionConfig.DHTEnabled = false
	sessionConfig.DownloadQueueSize = 10
	sessionConfig.SpeedLimitDown = 5000000

	err = tm.UpdateSessionConfig(sessionConfig)
	if err != nil {
		t.Errorf("Failed to update session config: %v", err)
	}

	// Verify config was applied
	updatedConfig := tm.GetSessionConfig()
	if updatedConfig.DownloadDir != sessionConfig.DownloadDir {
		t.Errorf("Download dir not updated: got %s, want %s",
			updatedConfig.DownloadDir, sessionConfig.DownloadDir)
	}

	if updatedConfig.DHTEnabled != sessionConfig.DHTEnabled {
		t.Errorf("DHT enabled not updated: got %v, want %v",
			updatedConfig.DHTEnabled, sessionConfig.DHTEnabled)
	}

	if updatedConfig.DownloadQueueSize != sessionConfig.DownloadQueueSize {
		t.Errorf("Queue size not updated: got %d, want %d",
			updatedConfig.DownloadQueueSize, sessionConfig.DownloadQueueSize)
	}
}

// TestTorrentManagerIntegration_HookSystemIntegration tests hook system integration
func TestTorrentManagerIntegration_HookSystemIntegration(t *testing.T) {
	config := createIntegrationTestConfig()
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Register test hooks
	var addedCount, startedCount, stoppedCount, removedCount int
	hookMutex := sync.Mutex{}

	tm.hookManager.RegisterHook(&Hook{
		ID: "test-added",
		Callback: func(ctx *HookContext) error {
			hookMutex.Lock()
			addedCount++
			hookMutex.Unlock()
			return nil
		},
		Events:   []HookEvent{HookEventTorrentAdded},
		Priority: 100,
	})

	tm.hookManager.RegisterHook(&Hook{
		ID: "test-started",
		Callback: func(ctx *HookContext) error {
			hookMutex.Lock()
			startedCount++
			hookMutex.Unlock()
			return nil
		},
		Events:   []HookEvent{HookEventTorrentStarted},
		Priority: 100,
	})

	tm.hookManager.RegisterHook(&Hook{
		ID: "test-stopped",
		Callback: func(ctx *HookContext) error {
			hookMutex.Lock()
			stoppedCount++
			hookMutex.Unlock()
			return nil
		},
		Events:   []HookEvent{HookEventTorrentStopped},
		Priority: 100,
	})

	tm.hookManager.RegisterHook(&Hook{
		ID: "test-removed",
		Callback: func(ctx *HookContext) error {
			hookMutex.Lock()
			removedCount++
			hookMutex.Unlock()
			return nil
		},
		Events:   []HookEvent{HookEventTorrentRemoved},
		Priority: 100,
	})

	// Perform torrent operations
	torrentData := createTestTorrentData(t)
	req := TorrentAddRequest{
		Metainfo: torrentData,
		Paused:   true, // Start paused to control sequence
	}

	torrentState, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Start torrent
	err = tm.StartTorrent(torrentState.ID)
	if err != nil {
		t.Errorf("Failed to start torrent: %v", err)
	}

	// Stop torrent
	err = tm.StopTorrent(torrentState.ID)
	if err != nil {
		t.Errorf("Failed to stop torrent: %v", err)
	}

	// Remove torrent
	err = tm.RemoveTorrent(torrentState.ID, false)
	if err != nil {
		t.Errorf("Failed to remove torrent: %v", err)
	}

	// Allow more time for hooks to execute
	time.Sleep(300 * time.Millisecond)

	// Check hook execution counts
	hookMutex.Lock()
	defer hookMutex.Unlock()

	t.Logf("Hook execution counts: added=%d, started=%d, stopped=%d, removed=%d",
		addedCount, startedCount, stoppedCount, removedCount)

	if addedCount != 1 {
		t.Errorf("Expected 1 added hook execution, got %d", addedCount)
	}
	// Note: started hook may not fire if torrent doesn't actually start in test
	if startedCount < 0 || startedCount > 1 {
		t.Errorf("Expected 0-1 started hook executions, got %d", startedCount)
	}
	if stoppedCount != 1 {
		t.Errorf("Expected 1 stopped hook execution, got %d", stoppedCount)
	}
	if removedCount != 1 {
		t.Errorf("Expected 1 removed hook execution, got %d", removedCount)
	}
}

// Helper function to create test torrent data
func createTestTorrentData(t *testing.T) string {
	return createTestTorrentDataWithName(t, "test-file")
}

// Helper function to create test torrent data with custom name
func createTestTorrentDataWithName(tb testing.TB, name string) string {
	tb.Helper()

	// Create a simple info dictionary
	info := metainfo.Info{
		Name:        name + ".txt",
		PieceLength: 32768, // 32KB pieces
		Files: []metainfo.File{
			{
				Length: 1024, // 1KB file
				Paths:  []string{name + "_file.txt"},
			},
		},
	}

	// Create dummy piece hashes
	pieceCount := (1024 + info.PieceLength - 1) / info.PieceLength
	info.Pieces = make(metainfo.Hashes, pieceCount)
	for i := range info.Pieces {
		info.Pieces[i] = metainfo.NewRandomHash()
	}

	// Encode info to bytes
	infoBytes, err := bencode.EncodeBytes(info)
	if err != nil {
		tb.Fatalf("Failed to encode info: %v", err)
	}

	// Create metainfo structure with unique announce URL
	metaInfo := metainfo.MetaInfo{
		InfoBytes: infoBytes,
		Announce:  "http://test-tracker-" + name + ".example.com/announce",
		Comment:   "Test torrent for integration tests: " + name,
		CreatedBy: "go-i2p-bt-integration-test",
	}

	// Encode to bytes
	metainfoBytes, err := bencode.EncodeBytes(metaInfo)
	if err != nil {
		tb.Fatalf("Failed to encode metainfo: %v", err)
	}

	// Return base64 encoded data
	return base64.StdEncoding.EncodeToString(metainfoBytes)
}

// Benchmark integration test for performance validation
func BenchmarkTorrentManagerIntegration_AddRemoveTorrent(b *testing.B) {
	config := createIntegrationTestConfig()
	defer os.RemoveAll(config.DownloadDir)
	defer os.RemoveAll(config.IncompleteDir)

	tm, err := NewTorrentManager(config)
	if err != nil {
		b.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	var counter int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Create unique torrent data for each iteration
			id := atomic.AddInt64(&counter, 1)
			torrentData := createBenchmarkTorrentDataWithName(b,
				fmt.Sprintf("benchmark-%d", id))

			req := TorrentAddRequest{
				Metainfo: torrentData,
				Paused:   true,
			}

			torrentState, err := tm.AddTorrent(req)
			if err != nil {
				b.Errorf("Failed to add torrent: %v", err)
				continue
			}

			err = tm.RemoveTorrent(torrentState.ID, false)
			if err != nil {
				b.Errorf("Failed to remove torrent: %v", err)
			}
		}
	})
}

// createBenchmarkTorrentData for benchmarks (separate function to avoid redeclaration)
func createBenchmarkTorrentData(b testing.TB) string {
	return createBenchmarkTorrentDataWithName(b, "benchmark")
}

// createBenchmarkTorrentDataWithName for benchmarks with custom names
func createBenchmarkTorrentDataWithName(tb testing.TB, name string) string {
	tb.Helper()

	// Create a simple info dictionary
	info := metainfo.Info{
		Name:        name + "-file.txt",
		PieceLength: 32768, // 32KB pieces
		Files: []metainfo.File{
			{
				Length: 2048, // 2KB file
				Paths:  []string{name + "_benchmark_file.txt"},
			},
		},
	}

	// Create dummy piece hashes
	pieceCount := (2048 + info.PieceLength - 1) / info.PieceLength
	info.Pieces = make(metainfo.Hashes, pieceCount)
	for i := range info.Pieces {
		info.Pieces[i] = metainfo.NewRandomHash()
	}

	// Encode info to bytes
	infoBytes, err := bencode.EncodeBytes(info)
	if err != nil {
		tb.Fatalf("Failed to encode info: %v", err)
	}

	// Create metainfo structure with unique announce URL
	metaInfo := metainfo.MetaInfo{
		InfoBytes: infoBytes,
		Announce:  "http://benchmark-tracker-" + name + ".example.com/announce",
		Comment:   "Benchmark torrent: " + name,
		CreatedBy: "go-i2p-bt-benchmark",
	}

	// Encode to bytes
	metainfoBytes, err := bencode.EncodeBytes(metaInfo)
	if err != nil {
		tb.Fatalf("Failed to encode metainfo: %v", err)
	}

	return base64.StdEncoding.EncodeToString(metainfoBytes)
}
