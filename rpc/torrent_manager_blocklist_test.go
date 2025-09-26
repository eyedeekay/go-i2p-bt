package rpc

import (
	"net"
	"testing"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

func TestTorrentManager_BlocklistIntegration(t *testing.T) {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			BlocklistEnabled: true,
			BlocklistURL:     "", // Will be set manually for testing
		},
	}

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Manually set up blocklist for testing
	tm.blocklistManager.SetEnabled(true)
	tm.blocklistManager.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.1"), end: net.ParseIP("192.168.1.255")},
		{start: net.ParseIP("10.0.0.100"), end: net.ParseIP("10.0.0.100")}, // Single IP
	}

	// Create a test torrent
	torrent := &TorrentState{
		ID: 1,
	}

	// Test updating peers with mixed blocked/allowed IPs
	testPeers := []metainfo.Address{
		{IP: &net.IPAddr{IP: net.ParseIP("192.168.1.100")}, Port: 6881}, // Should be blocked
		{IP: &net.IPAddr{IP: net.ParseIP("8.8.8.8")}, Port: 6881},       // Should be allowed
		{IP: &net.IPAddr{IP: net.ParseIP("10.0.0.100")}, Port: 6881},    // Should be blocked (single IP)
		{IP: &net.IPAddr{IP: net.ParseIP("1.1.1.1")}, Port: 6881},       // Should be allowed
	}

	tm.updateTorrentPeers(torrent, testPeers)

	// Check that only non-blocked peers were added
	if len(torrent.Peers) != 2 {
		t.Errorf("Expected 2 peers (non-blocked), got %d", len(torrent.Peers))
	}

	// Verify the correct peers were kept
	allowedIPs := map[string]bool{
		"8.8.8.8": false,
		"1.1.1.1": false,
	}

	for _, peer := range torrent.Peers {
		if found, exists := allowedIPs[peer.Address]; exists {
			if found {
				t.Errorf("Duplicate peer %s found", peer.Address)
			}
			allowedIPs[peer.Address] = true
		} else {
			t.Errorf("Unexpected peer %s found (should have been blocked)", peer.Address)
		}
	}

	// Ensure all expected peers were found
	for ip, found := range allowedIPs {
		if !found {
			t.Errorf("Expected peer %s not found", ip)
		}
	}
}

func TestTorrentManager_BlocklistDisabled(t *testing.T) {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			BlocklistEnabled: false, // Disabled
		},
	}

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Set up blocklist ranges but keep it disabled
	tm.blocklistManager.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.1"), end: net.ParseIP("192.168.1.255")},
	}

	torrent := &TorrentState{
		ID: 1,
	}

	// Test with IPs that would be blocked if enabled
	testPeers := []metainfo.Address{
		{IP: &net.IPAddr{IP: net.ParseIP("192.168.1.100")}, Port: 6881}, // Would be blocked if enabled
		{IP: &net.IPAddr{IP: net.ParseIP("8.8.8.8")}, Port: 6881},       // Always allowed
	}

	tm.updateTorrentPeers(torrent, testPeers)

	// All peers should be added since blocklist is disabled
	if len(torrent.Peers) != 2 {
		t.Errorf("Expected 2 peers (blocklist disabled), got %d", len(torrent.Peers))
	}
}

func TestTorrentManager_SessionConfigBlocklist(t *testing.T) {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			BlocklistEnabled: false,
			BlocklistURL:     "",
		},
	}

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Test initial state
	sessionConfig := tm.GetSessionConfig()
	if sessionConfig.BlocklistEnabled {
		t.Error("Blocklist should be disabled initially")
	}
	if sessionConfig.BlocklistURL != "" {
		t.Error("Blocklist URL should be empty initially")
	}
	if sessionConfig.BlocklistSize != 0 {
		t.Error("Blocklist size should be 0 initially")
	}

	// Update session configuration to enable blocklist
	newConfig := sessionConfig
	newConfig.BlocklistEnabled = true
	newConfig.BlocklistURL = "http://example.com/blocklist.dat"

	err = tm.UpdateSessionConfig(newConfig)
	if err != nil {
		t.Fatalf("Failed to update session config: %v", err)
	}

	// Verify configuration was applied
	updatedConfig := tm.GetSessionConfig()
	if !updatedConfig.BlocklistEnabled {
		t.Error("Blocklist should be enabled after update")
	}
	if updatedConfig.BlocklistURL != "http://example.com/blocklist.dat" {
		t.Errorf("Expected URL 'http://example.com/blocklist.dat', got '%s'",
			updatedConfig.BlocklistURL)
	}

	// Verify blocklist manager was updated
	if !tm.blocklistManager.IsEnabled() {
		t.Error("BlocklistManager should be enabled")
	}
	if tm.blocklistManager.GetURL() != "http://example.com/blocklist.dat" {
		t.Error("BlocklistManager URL should be updated")
	}
}

func TestTorrentManager_SessionConfigBlocklistURLChange(t *testing.T) {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			BlocklistEnabled: true,
			BlocklistURL:     "http://example.com/blocklist1.dat",
		},
	}

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Verify initial URL
	if tm.blocklistManager.GetURL() != "http://example.com/blocklist1.dat" {
		t.Errorf("Initial blocklist URL not set correctly: expected 'http://example.com/blocklist1.dat', got '%s'", tm.blocklistManager.GetURL())
	}

	// Update to new URL
	sessionConfig := tm.GetSessionConfig()
	sessionConfig.BlocklistURL = "http://example.com/blocklist2.dat"

	err = tm.UpdateSessionConfig(sessionConfig)
	if err != nil {
		t.Fatalf("Failed to update session config: %v", err)
	}

	// Verify URL was changed
	if tm.blocklistManager.GetURL() != "http://example.com/blocklist2.dat" {
		t.Error("Blocklist URL was not updated")
	}
}

func TestTorrentManager_BlocklistSizeInSessionConfig(t *testing.T) {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			BlocklistEnabled: true,
		},
	}

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Manually set blocklist ranges to simulate loaded blocklist
	tm.blocklistManager.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.1"), end: net.ParseIP("192.168.1.255")},
		{start: net.ParseIP("10.0.0.1"), end: net.ParseIP("10.0.0.100")},
		{start: net.ParseIP("172.16.0.1"), end: net.ParseIP("172.16.0.1")},
	}
	tm.blocklistManager.size = 3

	// Get session config and verify size is included
	sessionConfig := tm.GetSessionConfig()
	if sessionConfig.BlocklistSize != 3 {
		t.Errorf("Expected blocklist size 3, got %d", sessionConfig.BlocklistSize)
	}
}

func TestTorrentManager_BlocklistConfigurationValidation(t *testing.T) {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			BlocklistEnabled: false,
			BlocklistURL:     "",
		},
	}

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Test valid configuration updates
	validConfigs := []SessionConfiguration{
		{
			BlocklistEnabled: true,
			BlocklistURL:     "http://example.com/blocklist.dat",
		},
		{
			BlocklistEnabled: false,
			BlocklistURL:     "",
		},
		{
			BlocklistEnabled: true,
			BlocklistURL:     "https://secure.example.com/blocklist.txt",
		},
	}

	for i, validConfig := range validConfigs {
		// Apply other required fields
		validConfig.PeerPort = 6881
		validConfig.DownloadDir = config.DownloadDir

		err = tm.UpdateSessionConfig(validConfig)
		if err != nil {
			t.Errorf("Valid config %d should not fail: %v", i, err)
		}
	}
}

func TestTorrentManager_BlocklistPeerFiltering_EdgeCases(t *testing.T) {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			BlocklistEnabled: true,
		},
	}

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Set up blocklist
	tm.blocklistManager.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.1"), end: net.ParseIP("192.168.1.255")},
	}

	torrent := &TorrentState{
		ID: 1,
		Peers: []PeerInfo{
			{Address: "existing.peer.com", Port: 6881, Direction: "incoming"},
		},
	}

	// Test with empty peer list
	tm.updateTorrentPeers(torrent, []metainfo.Address{})

	// Should still have the existing peer
	if len(torrent.Peers) != 1 {
		t.Errorf("Expected 1 existing peer, got %d", len(torrent.Peers))
	}

	// Test with all blocked peers
	blockedPeers := []metainfo.Address{
		{IP: &net.IPAddr{IP: net.ParseIP("192.168.1.100")}, Port: 6881},
		{IP: &net.IPAddr{IP: net.ParseIP("192.168.1.200")}, Port: 6881},
	}

	tm.updateTorrentPeers(torrent, blockedPeers)

	// Should still only have the existing peer (no new peers added)
	if len(torrent.Peers) != 1 {
		t.Errorf("Expected 1 peer (blocked peers not added), got %d", len(torrent.Peers))
	}

	// Test with duplicate allowed peer
	allowedPeers := []metainfo.Address{
		{IP: &net.IPAddr{IP: net.ParseIP("8.8.8.8")}, Port: 6881},
		{IP: &net.IPAddr{IP: net.ParseIP("8.8.8.8")}, Port: 6881}, // Duplicate
	}

	tm.updateTorrentPeers(torrent, allowedPeers)

	// Should have original peer + one new peer (duplicate filtered)
	if len(torrent.Peers) != 2 {
		t.Errorf("Expected 2 peers (duplicate filtered), got %d", len(torrent.Peers))
	}
}
