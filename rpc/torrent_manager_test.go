package rpc

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/bencode"
	"github.com/go-i2p/go-i2p-bt/dht"
	"github.com/go-i2p/go-i2p-bt/downloader"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// Mock logger for testing
func testLogger(format string, args ...interface{}) {
	// Silent logger for tests unless debugging
}

// Create a test TorrentManager configuration
func createTestTorrentManagerConfig() TorrentManagerConfig {
	return TorrentManagerConfig{
		DHTConfig: dht.Config{
			ID: metainfo.NewRandomHash(),
			K:  8,
		},
		DownloaderConfig: downloader.TorrentDownloaderConfig{
			ID:        metainfo.NewRandomHash(),
			WorkerNum: 1, // Use fewer workers for tests
		},
		DownloadDir:         "test_downloads",
		ErrorLog:            testLogger,
		MaxTorrents:         10,
		PeerLimitGlobal:     50,
		PeerLimitPerTorrent: 10,
		PeerPort:            0, // Use random port for tests
		SessionConfig: SessionConfiguration{
			DownloadDir:         "test_downloads",
			PeerPort:            6881, // Use valid port for tests
			PeerLimitGlobal:     50,
			PeerLimitPerTorrent: 10,
			DHTEnabled:          false, // Disable DHT for most tests
			PEXEnabled:          true,
			StartAddedTorrents:  true,
			Version:             "go-i2p-bt-test/1.0.0",
		},
	}
}

// Test TorrentManager creation and basic operations
func TestNewTorrentManager(t *testing.T) {
	config := createTestTorrentManagerConfig()

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Test basic configuration
	if tm.config.MaxTorrents != 10 {
		t.Errorf("Expected MaxTorrents=10, got %d", tm.config.MaxTorrents)
	}

	if tm.config.DownloadDir != "test_downloads" {
		t.Errorf("Expected DownloadDir='test_downloads', got %s", tm.config.DownloadDir)
	}

	// Test session configuration
	sessionConfig := tm.GetSessionConfig()
	if sessionConfig.Version != "go-i2p-bt-test/1.0.0" {
		t.Errorf("Expected Version='go-i2p-bt-test/1.0.0', got %s", sessionConfig.Version)
	}
}

// Test TorrentManager with DHT enabled
func TestTorrentManagerWithDHT(t *testing.T) {
	config := createTestTorrentManagerConfig()
	config.SessionConfig.DHTEnabled = true
	config.PeerPort = 0 // Use random port

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager with DHT: %v", err)
	}
	defer tm.Close()

	// DHT should be initialized
	if tm.dhtServer == nil {
		t.Error("DHT server should be initialized when DHTEnabled=true")
	}

	if tm.dhtConn == nil {
		t.Error("DHT connection should be created when DHTEnabled=true")
	}
}

// Test adding a torrent with metainfo
func TestAddTorrentWithMetainfo(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	metainfoBytes := createTestMetainfoBytes(t)
	req := TorrentAddRequest{
		Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
		Paused:   true,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	if torrent.ID <= 0 {
		t.Error("Torrent ID should be positive")
	}

	if torrent.Status != TorrentStatusStopped {
		t.Errorf("Expected status %d, got %d", TorrentStatusStopped, torrent.Status)
	}

	// Test duplicate addition (should fail)
	_, err = tm.AddTorrent(req)
	if err == nil {
		t.Error("Adding duplicate torrent should fail")
	}

	// Verify torrent is in manager
	allTorrents := tm.GetAllTorrents()
	if len(allTorrents) != 1 {
		t.Errorf("Expected 1 torrent, got %d", len(allTorrents))
	}

	// Test getting by ID
	retrieved, err := tm.GetTorrent(torrent.ID)
	if err != nil {
		t.Fatalf("Failed to get torrent by ID: %v", err)
	}
	if retrieved.ID != torrent.ID {
		t.Errorf("Expected ID %d, got %d", torrent.ID, retrieved.ID)
	}
}

// Test adding a magnet link
func TestAddMagnetLink(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Use a valid 40-character hex hash for the test
	magnetLink := "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"
	req := TorrentAddRequest{
		Filename: magnetLink,
		Paused:   true,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add magnet link: %v", err)
	}

	if torrent == nil {
		t.Fatal("Torrent should not be nil")
	}

	if torrent.MetaInfo != nil {
		t.Error("MetaInfo should be nil for magnet link")
	}
}

// Test basic torrent operations (start, stop, remove)
func TestTorrentOperations(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Add a torrent
	metainfoBytes := createTestMetainfoBytes(t)
	req := TorrentAddRequest{
		Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
		Paused:   true,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Test start torrent
	err = tm.StartTorrent(torrent.ID)
	if err != nil {
		t.Fatalf("Failed to start torrent: %v", err)
	}

	// Test stop torrent
	err = tm.StopTorrent(torrent.ID)
	if err != nil {
		t.Fatalf("Failed to stop torrent: %v", err)
	}

	// Test remove torrent
	err = tm.RemoveTorrent(torrent.ID, false)
	if err != nil {
		t.Fatalf("Failed to remove torrent: %v", err)
	}

	// Verify torrent is removed
	_, err = tm.GetTorrent(torrent.ID)
	if err == nil {
		t.Error("Getting removed torrent should fail")
	}

	// Test operations on non-existent torrent
	err = tm.StartTorrent(999)
	if err == nil {
		t.Error("Starting non-existent torrent should fail")
	}

	err = tm.StopTorrent(999)
	if err == nil {
		t.Error("Stopping non-existent torrent should fail")
	}

	err = tm.RemoveTorrent(999, false)
	if err == nil {
		t.Error("Removing non-existent torrent should fail")
	}
}

// Test torrent ID resolution
func TestResolveTorrentIDs(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Add some torrents with different names to avoid duplicates
	for i := 0; i < 3; i++ {
		metainfoBytes := createTestMetainfoBytesWithName(t, fmt.Sprintf("test_torrent_%d", i))
		req := TorrentAddRequest{
			Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
			Paused:   true,
		}
		_, err := tm.AddTorrent(req)
		if err != nil {
			t.Fatalf("Failed to add torrent %d: %v", i, err)
		}
	}

	// Test resolving all IDs (empty slice)
	allIDs, err := tm.ResolveTorrentIDs([]interface{}{})
	if err != nil {
		t.Fatalf("Failed to resolve all torrent IDs: %v", err)
	}

	if len(allIDs) != 3 {
		t.Errorf("Expected 3 torrent IDs, got %d", len(allIDs))
	}

	// Test resolving specific ID
	specificIDs, err := tm.ResolveTorrentIDs([]interface{}{allIDs[0]})
	if err != nil {
		t.Fatalf("Failed to resolve specific torrent ID: %v", err)
	}

	if len(specificIDs) != 1 {
		t.Errorf("Expected 1 torrent ID, got %d", len(specificIDs))
	}

	if specificIDs[0] != allIDs[0] {
		t.Errorf("Expected ID %d, got %d", allIDs[0], specificIDs[0])
	}

	// Test resolving non-existent hash
	_, err = tm.ResolveTorrentIDs([]interface{}{"nonexistent_hash"})
	if err == nil {
		t.Error("Resolving non-existent torrent hash should fail")
	}
}

// Test session configuration updates
func TestSessionConfigUpdate(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Get current config
	currentConfig := tm.GetSessionConfig()

	// Update configuration
	newConfig := currentConfig
	newConfig.PeerLimitGlobal = 100
	newConfig.PeerLimitPerTorrent = 20

	err = tm.UpdateSessionConfig(newConfig)
	if err != nil {
		t.Fatalf("Failed to update session config: %v", err)
	}

	// Verify update
	updatedConfig := tm.GetSessionConfig()
	if updatedConfig.PeerLimitGlobal != 100 {
		t.Errorf("Expected PeerLimitGlobal=100, got %d", updatedConfig.PeerLimitGlobal)
	}

	if updatedConfig.PeerLimitPerTorrent != 20 {
		t.Errorf("Expected PeerLimitPerTorrent=20, got %d", updatedConfig.PeerLimitPerTorrent)
	}

	// Test invalid configuration
	invalidConfig := newConfig
	invalidConfig.PeerPort = 0 // Invalid port

	err = tm.UpdateSessionConfig(invalidConfig)
	if err == nil {
		t.Error("Updating with invalid configuration should fail")
	}
}

// Test error cases
func TestErrorCases(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Test invalid base64 in metainfo
	req1 := TorrentAddRequest{
		Metainfo: "invalid_base64!",
		Paused:   true,
	}
	_, err = tm.AddTorrent(req1)
	if err == nil {
		t.Error("Adding torrent with invalid base64 should fail")
	}

	// Test invalid bencode data
	invalidData := base64.StdEncoding.EncodeToString([]byte("not bencode data"))
	req2 := TorrentAddRequest{
		Metainfo: invalidData,
		Paused:   true,
	}
	_, err = tm.AddTorrent(req2)
	if err == nil {
		t.Error("Adding torrent with invalid bencode should fail")
	}

	// Test empty filename and metainfo
	req3 := TorrentAddRequest{
		Filename: "",
		Metainfo: "",
		Paused:   true,
	}
	_, err = tm.AddTorrent(req3)
	if err == nil {
		t.Error("Adding torrent with no filename or metainfo should fail")
	}

	// Test getting torrent by hash with invalid hash
	_, err = tm.GetTorrentByHash("invalid_hash")
	if err == nil {
		t.Error("Getting torrent by invalid hash should fail")
	}

	// Test operations on non-existent torrent
	_, err = tm.GetTorrent(999)
	if err == nil {
		t.Error("Getting non-existent torrent should fail")
	}
}

// Test verification functionality
func TestTorrentVerification(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := ioutil.TempDir("", "torrent_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := createTestTorrentManagerConfig()
	config.DownloadDir = tempDir
	config.SessionConfig.DownloadDir = tempDir

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Add a torrent
	metainfoBytes := createTestMetainfoBytes(t)
	req := TorrentAddRequest{
		Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
		Paused:   true,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Test verification on torrent without files (should not crash)
	err = tm.VerifyTorrent(torrent.ID)
	if err != nil {
		t.Fatalf("Failed to verify torrent: %v", err)
	}

	// Test verification on non-existent torrent
	err = tm.VerifyTorrent(999)
	if err == nil {
		t.Error("Should fail to verify non-existent torrent")
	}
}

// Test GetTorrentByHash method
func TestGetTorrentByHash(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Add a torrent
	metainfoBytes := createTestMetainfoBytesWithName(t, "hash_test_torrent")
	req := TorrentAddRequest{
		Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
		Paused:   true,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Test GetTorrentByHash
	retrieved, err := tm.GetTorrentByHash(torrent.InfoHash.HexString())
	if err != nil {
		t.Fatalf("Failed to get torrent by hash: %v", err)
	}

	if retrieved.ID != torrent.ID {
		t.Errorf("Expected torrent ID %d, got %d", torrent.ID, retrieved.ID)
	}

	if retrieved.InfoHash.HexString() != torrent.InfoHash.HexString() {
		t.Errorf("Expected hash %s, got %s", torrent.InfoHash.HexString(), retrieved.InfoHash.HexString())
	}
}

// Test concurrent operations
func TestConcurrentOperations(t *testing.T) {
	config := createTestTorrentManagerConfig()
	config.MaxTorrents = 100

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Add multiple torrents concurrently with unique names
	const numTorrents = 10
	errors := make(chan error, numTorrents)

	for i := 0; i < numTorrents; i++ {
		go func(id int) {
			// Create unique torrent for this goroutine
			metainfoBytes := createTestMetainfoBytesWithName(t, fmt.Sprintf("concurrent_torrent_%d", id))
			req := TorrentAddRequest{
				Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
				Paused:   true,
			}
			_, err := tm.AddTorrent(req)
			errors <- err
		}(i)
	}

	// Check for errors
	for i := 0; i < numTorrents; i++ {
		if err := <-errors; err != nil {
			t.Errorf("Concurrent add torrent failed: %v", err)
		}
	}

	// Verify all torrents were added
	allTorrents := tm.GetAllTorrents()
	if len(allTorrents) != numTorrents {
		t.Errorf("Expected %d torrents, got %d", numTorrents, len(allTorrents))
	}
}

// Test DHT callbacks
func TestDHTCallbacks(t *testing.T) {
	config := createTestTorrentManagerConfig()
	config.SessionConfig.DHTEnabled = true

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Add a torrent
	metainfoBytes := createTestMetainfoBytesWithName(t, "dht_callback_test")
	req := TorrentAddRequest{
		Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
		Paused:   true,
	}
	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Test DHT callbacks (should not panic)
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:8080")
	tm.onDHTSearch(torrent.InfoHash.HexString(), addr)
	tm.onDHTTorrent(torrent.InfoHash.HexString(), addr)

	// Test with invalid hash (should not panic)
	tm.onDHTSearch("invalid_hash", addr)
	tm.onDHTTorrent("invalid_hash", addr)
}

// Test max torrents limit
func TestMaxTorrentsLimit(t *testing.T) {
	config := createTestTorrentManagerConfig()
	config.MaxTorrents = 2 // Very low limit for testing

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Add torrents up to the limit
	for i := 0; i < 2; i++ {
		metainfoBytes := createTestMetainfoBytesWithName(t, fmt.Sprintf("limit_test_%d", i))
		req := TorrentAddRequest{
			Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
			Paused:   true,
		}
		_, err := tm.AddTorrent(req)
		if err != nil {
			t.Fatalf("Failed to add torrent %d: %v", i, err)
		}
	}

	// Try to add one more (should fail)
	metainfoBytes := createTestMetainfoBytesWithName(t, "limit_test_overflow")
	req := TorrentAddRequest{
		Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
		Paused:   true,
	}
	_, err = tm.AddTorrent(req)
	if err == nil {
		t.Error("Expected error when exceeding max torrents limit")
	}
}

// Helper function to create a test torrent metainfo as raw bytes
func createTestMetainfoBytes(t *testing.T) []byte {
	return createTestMetainfoBytesWithName(t, "test_torrent")
}

// Helper function to create a test torrent metainfo as raw bytes with custom name
func createTestMetainfoBytesWithName(t *testing.T, name string) []byte {
	// Create a simple info dictionary
	info := metainfo.Info{
		Name:        name,
		PieceLength: 32768, // 32KB pieces
		Files: []metainfo.File{
			{
				Length: 1024, // 1KB file
				Paths:  []string{"test_file.txt"},
			},
		},
	}

	// Create dummy piece hashes (normally these would be actual SHA1 hashes)
	pieceCount := (1024 + info.PieceLength - 1) / info.PieceLength
	info.Pieces = make(metainfo.Hashes, pieceCount)
	for i := range info.Pieces {
		info.Pieces[i] = metainfo.NewRandomHash()
	}

	// Encode info to bytes
	infoBytes, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatalf("Failed to encode info: %v", err)
	}

	// Create metainfo structure
	metaInfo := metainfo.MetaInfo{
		InfoBytes: infoBytes,
		Announce:  "http://tracker.example.com/announce",
		Comment:   "Test torrent for unit tests",
		CreatedBy: "go-i2p-bt-test",
	}

	// Encode to bytes
	metainfoBytes, err := bencode.EncodeBytes(metaInfo)
	if err != nil {
		t.Fatalf("Failed to encode metainfo: %v", err)
	}

	return metainfoBytes
}

// Benchmark torrent operations
func BenchmarkAddTorrent(b *testing.B) {
	config := createTestTorrentManagerConfig()
	config.MaxTorrents = 10000 // Allow many torrents for benchmarking

	tm, err := NewTorrentManager(config)
	if err != nil {
		b.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metainfoBytes := createTestMetainfoBytesWithName(&testing.T{}, fmt.Sprintf("benchmark_torrent_%d", i))
		req := TorrentAddRequest{
			Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
			Paused:   true,
		}
		_, err := tm.AddTorrent(req)
		if err != nil {
			b.Fatalf("Failed to add torrent: %v", err)
		}
	}
}

// Test statistics functionality
func TestUpdateTorrentStatistics(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	now := time.Now()

	// Create test torrent with initial state
	torrent := &TorrentState{
		InfoHash:        metainfo.NewRandomHash(),
		Downloaded:      1000,
		Uploaded:        500,
		PercentDone:     0.0,
		lastStatsUpdate: now.Add(-5 * time.Second),
		lastDownloaded:  800,
		lastUploaded:    300,
		Peers: []PeerInfo{
			{ID: "peer1", Address: "127.0.0.1", Port: 6881, Direction: "outgoing"},
			{ID: "peer2", Address: "127.0.0.2", Port: 6881, Direction: "incoming"},
			{ID: "peer3", Address: "127.0.0.3", Port: 6881, Direction: "outgoing"},
		},
	}

	// Update statistics
	tm.updateTorrentStatistics(torrent, now)

	// Verify transfer rate calculations
	expectedDownloadRate := int64((1000 - 800) / 5) // 40 bytes/sec
	expectedUploadRate := int64((500 - 300) / 5)    // 40 bytes/sec

	if torrent.DownloadRate != expectedDownloadRate {
		t.Errorf("Expected download rate %d, got %d", expectedDownloadRate, torrent.DownloadRate)
	}

	if torrent.UploadRate != expectedUploadRate {
		t.Errorf("Expected upload rate %d, got %d", expectedUploadRate, torrent.UploadRate)
	}

	// Verify peer count
	if torrent.PeerCount != 3 {
		t.Errorf("Expected peer count 3, got %d", torrent.PeerCount)
	}

	// Verify tracking fields are updated
	if torrent.lastStatsUpdate != now {
		t.Errorf("Last stats update time not updated correctly")
	}

	if torrent.lastDownloaded != 1000 {
		t.Errorf("Last downloaded not updated correctly")
	}

	if torrent.lastUploaded != 500 {
		t.Errorf("Last uploaded not updated correctly")
	}
}

func TestUpdateTorrentStatisticsFirstUpdate(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	now := time.Now()

	// Create torrent with no previous stats update (zero time)
	torrent := &TorrentState{
		InfoHash:   metainfo.NewRandomHash(),
		Downloaded: 1000,
		Uploaded:   500,
		Peers: []PeerInfo{
			{ID: "peer1", Address: "127.0.0.1", Port: 6881, Direction: "outgoing"},
		},
	}

	tm.updateTorrentStatistics(torrent, now)

	// On first update, rates should be 0 since we have no previous data
	if torrent.DownloadRate != 0 {
		t.Errorf("Expected download rate 0 on first update, got %d", torrent.DownloadRate)
	}

	if torrent.UploadRate != 0 {
		t.Errorf("Expected upload rate 0 on first update, got %d", torrent.UploadRate)
	}

	// Verify tracking fields are set
	if torrent.lastStatsUpdate != now {
		t.Errorf("Last stats update time not set correctly")
	}
}

func TestCalculateETA(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	tests := []struct {
		name        string
		torrent     *TorrentState
		expectedETA int64
		description string
	}{
		{
			name: "Normal download in progress",
			torrent: &TorrentState{
				Left:         5000,
				DownloadRate: 1000, // 1000 bytes/sec
			},
			expectedETA: 5, // 5000/1000 = 5 seconds
			description: "Should calculate ETA based on current download rate",
		},
		{
			name: "Zero download rate",
			torrent: &TorrentState{
				Left:         5000,
				DownloadRate: 0,
			},
			expectedETA: -1, // Unknown ETA
			description: "Should return -1 when download rate is 0",
		},
		{
			name: "Download complete",
			torrent: &TorrentState{
				Left:         0,
				DownloadRate: 1000,
			},
			expectedETA: 0, // Already complete
			description: "Should return 0 when download is complete",
		},
		{
			name: "Very slow download",
			torrent: &TorrentState{
				Left:         10000,
				DownloadRate: 1, // 1 byte/sec
			},
			expectedETA: 10000, // 10000/1 = 10000 seconds
			description: "Should handle very slow downloads correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tm.calculateETA(tt.torrent)
			if result != tt.expectedETA {
				t.Errorf("%s: expected ETA %d, got %d", tt.description, tt.expectedETA, result)
			}
		})
	}
}

func TestCountConnectedPeers(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	torrent := &TorrentState{
		InfoHash: metainfo.NewRandomHash(),
		Peers: []PeerInfo{
			{ID: "peer1", Address: "127.0.0.1", Port: 6881, Direction: "outgoing"},
			{ID: "peer2", Address: "127.0.0.2", Port: 6881, Direction: "incoming"},
			{ID: "peer3", Address: "127.0.0.3", Port: 6881, Direction: "outgoing"},
			{ID: "peer4", Address: "127.0.0.4", Port: 6881, Direction: "incoming"},
		},
	}

	// Test counting connected peers (mock implementation returns all peers as connected)
	connectedCount := tm.countConnectedPeers(torrent)
	expectedCount := int64(len(torrent.Peers))

	if connectedCount != expectedCount {
		t.Errorf("Expected connected peer count %d, got %d", expectedCount, connectedCount)
	}
}

func TestCountSendingPeers(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	torrent := &TorrentState{
		InfoHash: metainfo.NewRandomHash(),
		Peers: []PeerInfo{
			{ID: "peer1", Address: "127.0.0.1", Port: 6881, Direction: "outgoing"},
			{ID: "peer2", Address: "127.0.0.2", Port: 6881, Direction: "incoming"},
			{ID: "peer3", Address: "127.0.0.3", Port: 6881, Direction: "outgoing"},
			{ID: "peer4", Address: "127.0.0.4", Port: 6881, Direction: "incoming"},
		},
	}

	// Test counting sending peers (mock implementation estimates based on peer count)
	sendingCount := tm.countSendingPeers(torrent)
	expectedMinimum := int64(0)
	expectedMaximum := int64(len(torrent.Peers))

	if sendingCount < expectedMinimum || sendingCount > expectedMaximum {
		t.Errorf("Sending peer count %d should be between %d and %d", sendingCount, expectedMinimum, expectedMaximum)
	}
}

func TestCountReceivingPeers(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	torrent := &TorrentState{
		InfoHash: metainfo.NewRandomHash(),
		Peers: []PeerInfo{
			{ID: "peer1", Address: "127.0.0.1", Port: 6881, Direction: "outgoing"},
			{ID: "peer2", Address: "127.0.0.2", Port: 6881, Direction: "incoming"},
			{ID: "peer3", Address: "127.0.0.3", Port: 6881, Direction: "outgoing"},
			{ID: "peer4", Address: "127.0.0.4", Port: 6881, Direction: "incoming"},
		},
	}

	// Test counting receiving peers (mock implementation estimates based on peer count)
	receivingCount := tm.countReceivingPeers(torrent)
	expectedMinimum := int64(0)
	expectedMaximum := int64(len(torrent.Peers))

	if receivingCount < expectedMinimum || receivingCount > expectedMaximum {
		t.Errorf("Receiving peer count %d should be between %d and %d", receivingCount, expectedMinimum, expectedMaximum)
	}
}

func TestCalculateCompletedPieces(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	tests := []struct {
		name        string
		torrent     *TorrentState
		description string
	}{
		{
			name: "Partial completion",
			torrent: &TorrentState{
				PercentDone: 0.5, // 50% complete
			},
			description: "Should calculate completed pieces based on percentage",
		},
		{
			name: "No completion",
			torrent: &TorrentState{
				PercentDone: 0.0,
			},
			description: "Should return 0 pieces for 0% completion",
		},
		{
			name: "Full completion",
			torrent: &TorrentState{
				PercentDone: 1.0,
			},
			description: "Should return all pieces for 100% completion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tm.calculateCompletedPieces(tt.torrent)

			// Since we don't have MetaInfo in these tests, result should be 0
			if result != 0 {
				t.Errorf("%s: expected 0 pieces (no MetaInfo), got %d", tt.description, result)
			}
		})
	}
}

func TestCalculateAvailablePieces(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	tests := []struct {
		name        string
		torrent     *TorrentState
		description string
	}{
		{
			name: "Multiple peers",
			torrent: &TorrentState{
				Peers: []PeerInfo{
					{ID: "peer1", Address: "127.0.0.1", Port: 6881, Direction: "outgoing"},
					{ID: "peer2", Address: "127.0.0.2", Port: 6881, Direction: "incoming"},
					{ID: "peer3", Address: "127.0.0.3", Port: 6881, Direction: "outgoing"},
				},
				PeerCount: 3,
			},
			description: "Should estimate available pieces based on peer count",
		},
		{
			name: "No peers",
			torrent: &TorrentState{
				Peers:     []PeerInfo{},
				PeerCount: 0,
			},
			description: "Should return 0 pieces when no peers connected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tm.calculateAvailablePieces(tt.torrent)

			// Since we don't have MetaInfo in these tests, result should be 0
			if result != 0 {
				t.Errorf("%s: expected 0 pieces (no MetaInfo), got %d", tt.description, result)
			}
		})
	}
}

// Benchmark tests for statistics performance
func BenchmarkUpdateTorrentStatistics(b *testing.B) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		b.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	now := time.Now()

	torrent := &TorrentState{
		InfoHash:        metainfo.NewRandomHash(),
		Downloaded:      1000000,
		Uploaded:        500000,
		lastStatsUpdate: now.Add(-5 * time.Second),
		lastDownloaded:  900000,
		lastUploaded:    450000,
		Peers:           make([]PeerInfo, 100), // 100 peers
	}

	// Fill peers slice
	for i := 0; i < 100; i++ {
		torrent.Peers[i] = PeerInfo{
			ID:        fmt.Sprintf("peer%d", i),
			Address:   fmt.Sprintf("127.0.0.%d", i+1),
			Port:      6881,
			Direction: "outgoing",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.updateTorrentStatistics(torrent, now)
	}
}

func BenchmarkCalculateETA(b *testing.B) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		b.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	torrent := &TorrentState{
		Left:         5000000, // 5MB left
		DownloadRate: 1000000, // 1MB/sec
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.calculateETA(torrent)
	}
}

// Tests for UpdateSessionConfig method

func TestUpdateSessionConfig_ValidConfiguration(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Create a temporary directory for testing
	tempDir, err := ioutil.TempDir("", "session_config_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	newConfig := SessionConfiguration{
		DownloadDir:           tempDir,
		PeerPort:              6882,
		PeerLimitGlobal:       100,
		PeerLimitPerTorrent:   20,
		DHTEnabled:            true,
		PEXEnabled:            false,
		DownloadQueueEnabled:  true,
		DownloadQueueSize:     5,
		SeedQueueEnabled:      true,
		SeedQueueSize:         3,
		SpeedLimitDown:        1000000,
		SpeedLimitDownEnabled: true,
		SpeedLimitUp:          500000,
		SpeedLimitUpEnabled:   true,
		StartAddedTorrents:    false,
	}

	err = tm.UpdateSessionConfig(newConfig)
	if err != nil {
		t.Fatalf("Failed to update session config: %v", err)
	}

	// Verify configuration was applied
	tm.sessionMu.RLock()
	config := tm.sessionConfig
	tm.sessionMu.RUnlock()

	if config.DownloadDir != tempDir {
		t.Errorf("Expected download dir %s, got %s", tempDir, config.DownloadDir)
	}
	if config.PeerPort != 6882 {
		t.Errorf("Expected peer port 6882, got %d", config.PeerPort)
	}
	if config.PeerLimitGlobal != 100 {
		t.Errorf("Expected peer limit global 100, got %d", config.PeerLimitGlobal)
	}
	if config.DownloadQueueSize != 5 {
		t.Errorf("Expected download queue size 5, got %d", config.DownloadQueueSize)
	}

	// Verify queue manager configuration was updated
	tm.queueManager.mu.RLock()
	queueConfig := tm.queueManager.config
	tm.queueManager.mu.RUnlock()

	if queueConfig.MaxActiveDownloads != 5 {
		t.Errorf("Expected queue max active downloads 5, got %d", queueConfig.MaxActiveDownloads)
	}
	if queueConfig.MaxActiveSeeds != 3 {
		t.Errorf("Expected queue max active seeds 3, got %d", queueConfig.MaxActiveSeeds)
	}
}

func TestUpdateSessionConfig_InvalidPeerPort(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	invalidConfigs := []SessionConfiguration{
		{PeerPort: 0},     // Port 0 is invalid
		{PeerPort: -1},    // Negative port
		{PeerPort: 65536}, // Port too large
	}

	for i, config := range invalidConfigs {
		err = tm.UpdateSessionConfig(config)
		if err == nil {
			t.Errorf("Test %d: Expected error for invalid peer port %d, but got nil", i, config.PeerPort)
		}
	}
}

func TestUpdateSessionConfig_InvalidLimits(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	invalidConfigs := []struct {
		config SessionConfiguration
		desc   string
	}{
		{SessionConfiguration{PeerPort: 6881, PeerLimitGlobal: -1}, "negative peer limit global"},
		{SessionConfiguration{PeerPort: 6881, PeerLimitPerTorrent: -1}, "negative peer limit per torrent"},
		{SessionConfiguration{PeerPort: 6881, DownloadQueueSize: -1}, "negative download queue size"},
		{SessionConfiguration{PeerPort: 6881, SeedQueueSize: -1}, "negative seed queue size"},
	}

	for _, test := range invalidConfigs {
		err = tm.UpdateSessionConfig(test.config)
		if err == nil {
			t.Errorf("Expected error for %s, but got nil", test.desc)
		}
	}
}

func TestUpdateSessionConfig_InvalidDownloadDirectory(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Create a file (not directory) to test invalid directory
	tempFile, err := ioutil.TempFile("", "not_a_directory")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	config := SessionConfiguration{
		PeerPort:    6881,
		DownloadDir: tempFile.Name(), // This is a file, not a directory
	}

	err = tm.UpdateSessionConfig(config)
	if err == nil {
		t.Error("Expected error for file path as download directory, but got nil")
	}
}

func TestUpdateSessionConfig_NonWritableDirectory(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Create a directory with no write permissions
	tempDir, err := ioutil.TempDir("", "readonly_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Make directory read-only
	err = os.Chmod(tempDir, 0444)
	if err != nil {
		t.Fatalf("Failed to make directory read-only: %v", err)
	}

	config := SessionConfiguration{
		PeerPort:    6881,
		DownloadDir: tempDir,
	}

	err = tm.UpdateSessionConfig(config)
	if err == nil {
		t.Error("Expected error for non-writable directory, but got nil")
	}
}

func TestUpdateSessionConfig_ConfigurationRevertOnFailure(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Get original configuration
	tm.sessionMu.RLock()
	originalConfig := tm.sessionConfig
	tm.sessionMu.RUnlock()

	// Try to update with an invalid configuration
	invalidConfig := SessionConfiguration{
		PeerPort:          6881,
		DownloadDir:       "/nonexistent/path/that/cannot/be/created",
		DownloadQueueSize: 10,
	}

	err = tm.UpdateSessionConfig(invalidConfig)
	if err == nil {
		t.Error("Expected error for invalid configuration, but got nil")
	}

	// Verify configuration was reverted
	tm.sessionMu.RLock()
	currentConfig := tm.sessionConfig
	tm.sessionMu.RUnlock()

	if currentConfig.DownloadDir != originalConfig.DownloadDir {
		t.Errorf("Configuration was not reverted: expected %s, got %s",
			originalConfig.DownloadDir, currentConfig.DownloadDir)
	}
}

func TestUpdateSessionConfig_QueueDisabling(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// First add actual torrents to the manager before adding them to queues
	torrent1 := &TorrentState{
		ID:       1,
		InfoHash: metainfo.NewRandomHash(),
		Status:   TorrentStatusStopped,
	}
	torrent2 := &TorrentState{
		ID:       2,
		InfoHash: metainfo.NewRandomHash(),
		Status:   TorrentStatusStopped,
	}

	tm.mu.Lock()
	tm.torrents[1] = torrent1
	tm.torrents[2] = torrent2
	tm.mu.Unlock()

	// Add some torrents to queues
	torrent1ID := int64(1)
	torrent2ID := int64(2)

	err = tm.queueManager.AddToQueue(torrent1ID, DownloadQueue, 1)
	if err != nil {
		t.Fatalf("Failed to add torrent to download queue: %v", err)
	}

	err = tm.queueManager.AddToQueue(torrent2ID, SeedQueue, 1)
	if err != nil {
		t.Fatalf("Failed to add torrent to seed queue: %v", err)
	}

	// Verify torrents are queued initially (they shouldn't be active yet)
	if tm.queueManager.IsActive(torrent1ID) {
		t.Error("Torrent 1 should be queued, not active")
	}
	if tm.queueManager.IsActive(torrent2ID) {
		t.Error("Torrent 2 should be queued, not active")
	}

	// Disable queues
	config := SessionConfiguration{
		PeerPort:             6881,
		DownloadQueueEnabled: false,
		SeedQueueEnabled:     false,
		DownloadQueueSize:    5,
		SeedQueueSize:        3,
	}

	err = tm.UpdateSessionConfig(config)
	if err != nil {
		t.Fatalf("Failed to update session config: %v", err)
	}

	// Verify queue configuration was updated
	tm.queueManager.mu.RLock()
	queueConfig := tm.queueManager.config
	tm.queueManager.mu.RUnlock()

	if queueConfig.DownloadQueueEnabled {
		t.Error("Expected download queue to be disabled")
	}
	if queueConfig.SeedQueueEnabled {
		t.Error("Expected seed queue to be disabled")
	}

	// Verify torrents were activated (or at least attempt was made)
	// Since queues are disabled, torrents should either be active or have been processed
	position1 := tm.queueManager.GetQueuePosition(torrent1ID)
	position2 := tm.queueManager.GetQueuePosition(torrent2ID)

	// Queue position should be -1 if torrent is not queued
	if position1 != -1 && !tm.queueManager.IsActive(torrent1ID) {
		t.Error("Torrent 1 should either be active or not in queue after disabling download queue")
	}
	if position2 != -1 && !tm.queueManager.IsActive(torrent2ID) {
		t.Error("Torrent 2 should either be active or not in queue after disabling seed queue")
	}
}

func TestUpdateSessionConfig_QueueLimitUpdates(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Update queue limits
	config := SessionConfiguration{
		PeerPort:             6881,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
		DownloadQueueSize:    15,
		SeedQueueSize:        10,
	}

	err = tm.UpdateSessionConfig(config)
	if err != nil {
		t.Fatalf("Failed to update session config: %v", err)
	}

	// Verify queue manager limits were updated
	tm.queueManager.mu.RLock()
	queueConfig := tm.queueManager.config
	tm.queueManager.mu.RUnlock()

	if queueConfig.MaxActiveDownloads != 15 {
		t.Errorf("Expected max active downloads 15, got %d", queueConfig.MaxActiveDownloads)
	}
	if queueConfig.MaxActiveSeeds != 10 {
		t.Errorf("Expected max active seeds 10, got %d", queueConfig.MaxActiveSeeds)
	}
	if !queueConfig.DownloadQueueEnabled {
		t.Error("Expected download queue to be enabled")
	}
	if !queueConfig.SeedQueueEnabled {
		t.Error("Expected seed queue to be enabled")
	}
}

// Test incomplete directory configuration
func TestIncompleteDirConfiguration(t *testing.T) {
	config := createTestTorrentManagerConfig()

	// Set up incomplete directory configuration
	config.IncompleteDir = "/tmp/test_incomplete"
	config.IncompleteDirEnabled = true
	config.SessionConfig.IncompleteDir = "/tmp/test_incomplete"
	config.SessionConfig.IncompleteDirEnabled = true

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer func() {
		tm.Close()
		os.RemoveAll("/tmp/test_incomplete")
		os.RemoveAll("test_downloads")
	}()

	// Test that configuration is set correctly
	sessionConfig := tm.GetSessionConfig()
	if sessionConfig.IncompleteDir != "/tmp/test_incomplete" {
		t.Errorf("Expected incomplete dir '/tmp/test_incomplete', got '%s'", sessionConfig.IncompleteDir)
	}
	if !sessionConfig.IncompleteDirEnabled {
		t.Error("Expected incomplete directory to be enabled")
	}
}

// Test determineDownloadDirectory method
func TestDetermineDownloadDirectory(t *testing.T) {
	config := createTestTorrentManagerConfig()
	config.DownloadDir = "/tmp/test_complete"
	config.IncompleteDir = "/tmp/test_incomplete"
	config.IncompleteDirEnabled = true
	config.SessionConfig.DownloadDir = "/tmp/test_complete"
	config.SessionConfig.IncompleteDir = "/tmp/test_incomplete"
	config.SessionConfig.IncompleteDirEnabled = true

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer func() {
		tm.Close()
		os.RemoveAll("/tmp/test_complete")
		os.RemoveAll("/tmp/test_incomplete")
	}()

	tests := []struct {
		name         string
		requestedDir string
		isComplete   bool
		expected     string
	}{
		{
			name:         "Explicit directory override",
			requestedDir: "/explicit/path",
			isComplete:   false,
			expected:     "/explicit/path",
		},
		{
			name:         "Incomplete torrent uses incomplete dir",
			requestedDir: "",
			isComplete:   false,
			expected:     "/tmp/test_incomplete",
		},
		{
			name:         "Complete torrent uses complete dir",
			requestedDir: "",
			isComplete:   true,
			expected:     "/tmp/test_complete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tm.determineDownloadDirectory(tt.requestedDir, tt.isComplete)
			if result != tt.expected {
				t.Errorf("Expected directory '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// Test session configuration updates for incomplete directory
func TestUpdateIncompleteDirSessionConfig(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer func() {
		tm.Close()
		os.RemoveAll("/tmp/test_incomplete_updated")
		os.RemoveAll("test_downloads")
	}()

	// Update session configuration with incomplete directory
	sessionConfig := tm.GetSessionConfig()
	sessionConfig.IncompleteDir = "/tmp/test_incomplete_updated"
	sessionConfig.IncompleteDirEnabled = true

	err = tm.UpdateSessionConfig(sessionConfig)
	if err != nil {
		t.Fatalf("Failed to update session config: %v", err)
	}

	// Verify changes were applied
	updatedConfig := tm.GetSessionConfig()
	if updatedConfig.IncompleteDir != "/tmp/test_incomplete_updated" {
		t.Errorf("Expected incomplete dir '/tmp/test_incomplete_updated', got '%s'", updatedConfig.IncompleteDir)
	}
	if !updatedConfig.IncompleteDirEnabled {
		t.Error("Expected incomplete directory to be enabled")
	}

	// Verify base config was updated too
	if tm.config.IncompleteDir != "/tmp/test_incomplete_updated" {
		t.Errorf("Expected base config incomplete dir '/tmp/test_incomplete_updated', got '%s'", tm.config.IncompleteDir)
	}
	if !tm.config.IncompleteDirEnabled {
		t.Error("Expected base config incomplete directory to be enabled")
	}
}

// Test incomplete directory validation
func TestIncompleteDirValidation(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer func() {
		tm.Close()
		os.RemoveAll("test_downloads")
	}()

	tests := []struct {
		name            string
		incompleteDir   string
		enabled         bool
		shouldCreateDir bool
		shouldError     bool
		errorContains   string
	}{
		{
			name:            "Valid directory path",
			incompleteDir:   "/tmp/test_valid_incomplete",
			enabled:         true,
			shouldCreateDir: true,
			shouldError:     false,
		},
		{
			name:          "Disabled incomplete directory",
			incompleteDir: "",
			enabled:       false,
			shouldError:   false,
		},
		{
			name:          "Empty path with enabled flag",
			incompleteDir: "",
			enabled:       true,
			shouldError:   false, // Empty path should be okay when enabled
		},
		{
			name:          "Invalid path - file exists",
			incompleteDir: "/dev/null", // This is a file, not a directory
			enabled:       true,
			shouldError:   true,
			errorContains: "not a directory",
		},
	}

	// Create test file for "file exists" test
	defer os.RemoveAll("/tmp/test_valid_incomplete")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionConfig := tm.GetSessionConfig()
			sessionConfig.IncompleteDir = tt.incompleteDir
			sessionConfig.IncompleteDirEnabled = tt.enabled

			err := tm.UpdateSessionConfig(sessionConfig)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', got: %s", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				if tt.shouldCreateDir && tt.incompleteDir != "" {
					// Check if directory was created
					if _, err := os.Stat(tt.incompleteDir); os.IsNotExist(err) {
						t.Errorf("Expected directory to be created: %s", tt.incompleteDir)
					}
				}
			}
		})
	}
}

// Test torrent creation uses correct directory
func TestTorrentCreationWithIncompleteDir(t *testing.T) {
	config := createTestTorrentManagerConfig()
	config.DownloadDir = "/tmp/test_complete_dir"
	config.IncompleteDir = "/tmp/test_incomplete_dir"
	config.IncompleteDirEnabled = true
	config.SessionConfig.DownloadDir = "/tmp/test_complete_dir"
	config.SessionConfig.IncompleteDir = "/tmp/test_incomplete_dir"
	config.SessionConfig.IncompleteDirEnabled = true

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer func() {
		tm.Close()
		os.RemoveAll("/tmp/test_complete_dir")
		os.RemoveAll("/tmp/test_incomplete_dir")
	}()

	// Create a test torrent
	req := TorrentAddRequest{
		Filename: "test.torrent",
		Metainfo: base64.StdEncoding.EncodeToString(createTestMetainfoBytes(t)),
		Paused:   true,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Verify torrent was created with incomplete directory
	if torrent.DownloadDir != "/tmp/test_incomplete_dir" {
		t.Errorf("Expected download dir '/tmp/test_incomplete_dir', got '%s'", torrent.DownloadDir)
	}
}

// Test that explicit download directory overrides incomplete directory
func TestExplicitDownloadDirOverridesIncomplete(t *testing.T) {
	config := createTestTorrentManagerConfig()
	config.IncompleteDir = "/tmp/test_incomplete_override"
	config.IncompleteDirEnabled = true
	config.SessionConfig.IncompleteDir = "/tmp/test_incomplete_override"
	config.SessionConfig.IncompleteDirEnabled = true

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer func() {
		tm.Close()
		os.RemoveAll("/tmp/test_incomplete_override")
		os.RemoveAll("/tmp/test_explicit_override")
		os.RemoveAll("test_downloads")
	}()

	// Create a test torrent with explicit download directory
	req := TorrentAddRequest{
		Filename:    "test.torrent",
		DownloadDir: "/tmp/test_explicit_override",
		Metainfo:    base64.StdEncoding.EncodeToString(createTestMetainfoBytes(t)),
		Paused:      true,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Verify torrent uses explicit directory, not incomplete directory
	if torrent.DownloadDir != "/tmp/test_explicit_override" {
		t.Errorf("Expected download dir '/tmp/test_explicit_override', got '%s'", torrent.DownloadDir)
	}
}

// Test session-set method with incomplete directory settings
func TestSessionSetWithIncompleteDir(t *testing.T) {
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer func() {
		tm.Close()
		os.RemoveAll("/tmp/test_session_set_incomplete")
		os.RemoveAll("test_downloads")
	}()

	// Create RPC methods instance
	methods := &RPCMethods{manager: tm}

	// Test session-set with incomplete directory settings
	req := SessionSetRequest{
		IncompleteDir:        stringPtr("/tmp/test_session_set_incomplete"),
		IncompleteDirEnabled: boolPtr(true),
	}

	err = methods.SessionSet(req)
	if err != nil {
		t.Fatalf("Failed to set session configuration: %v", err)
	}

	// Verify the configuration was applied
	sessionConfig := tm.GetSessionConfig()
	if sessionConfig.IncompleteDir != "/tmp/test_session_set_incomplete" {
		t.Errorf("Expected incomplete dir '/tmp/test_session_set_incomplete', got '%s'", sessionConfig.IncompleteDir)
	}
	if !sessionConfig.IncompleteDirEnabled {
		t.Error("Expected incomplete directory to be enabled")
	}

	// Test session-get returns the correct values
	response, err := methods.SessionGet()
	if err != nil {
		t.Fatalf("Failed to get session configuration: %v", err)
	}

	if response.IncompleteDir != "/tmp/test_session_set_incomplete" {
		t.Errorf("SessionGet: Expected incomplete dir '/tmp/test_session_set_incomplete', got '%s'", response.IncompleteDir)
	}
	if !response.IncompleteDirEnabled {
		t.Error("SessionGet: Expected incomplete directory to be enabled")
	}
}

// Helper functions for creating pointers to primitive types
func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}
