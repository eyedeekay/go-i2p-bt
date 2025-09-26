package rpc

import (
	"testing"
)

func TestTorrentManager_BlocklistInitialization_Debug(t *testing.T) {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			BlocklistEnabled: true,
			BlocklistURL:     "http://example.com/test.dat",
		},
	}
	
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()
	
	// Check if blocklist manager was initialized
	if tm.blocklistManager == nil {
		t.Fatal("BlocklistManager is nil")
	}
	
	// Check enabled status
	if !tm.blocklistManager.IsEnabled() {
		t.Error("BlocklistManager should be enabled")
	}
	
	// Check URL
	url := tm.blocklistManager.GetURL()
	expected := "http://example.com/test.dat"
	if url != expected {
		t.Errorf("Expected URL '%s', got '%s'", expected, url)
	}
	
	// Check session config
	sessionConfig := tm.GetSessionConfig()
	if !sessionConfig.BlocklistEnabled {
		t.Error("Session config should show blocklist enabled")
	}
	if sessionConfig.BlocklistURL != expected {
		t.Errorf("Session config URL: expected '%s', got '%s'", expected, sessionConfig.BlocklistURL)
	}
}