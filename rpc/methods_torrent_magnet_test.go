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
	"testing"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

func TestTorrentMagnet(t *testing.T) {
	// Create a test torrent manager
	manager := createTestTorrentManager(t)
	methods := NewRPCMethods(manager)

	// Create test torrent data with a byte array hash
	testHash := metainfo.Hash{0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12}

	// Create test metainfo with name
	testMetaInfo := &metainfo.MetaInfo{
		Announce: "http://tracker.example.com/announce",
	}
	// Create Info structure to set the name
	testMetaInfo.InfoBytes = []byte(`d4:name12:Test Torrente`) // Simple bencode with name

	testTorrent := &TorrentState{
		ID:       1,
		InfoHash: testHash,
		Status:   TorrentStatusDownloading,
		MetaInfo: testMetaInfo,
	}

	// Add the torrent to the manager
	manager.mu.Lock()
	manager.torrents[1] = testTorrent
	manager.mu.Unlock()

	t.Run("GetAllMagnetLinks", func(t *testing.T) {
		// Test getting all magnet links (no IDs specified)
		req := TorrentMagnetRequest{}
		response, err := methods.TorrentMagnet(req)
		if err != nil {
			t.Fatalf("TorrentMagnet failed: %v", err)
		}

		if len(response.Magnets) != 1 {
			t.Fatalf("Expected 1 magnet link, got %d", len(response.Magnets))
		}

		magnet := response.Magnets[0]
		if magnet.ID != 1 {
			t.Errorf("Expected ID 1, got %d", magnet.ID)
		}
		if magnet.Name == "" {
			t.Error("Expected non-empty name")
		}
		if magnet.HashString != testHash.HexString() {
			t.Errorf("Expected hash '%s', got '%s'", testHash.HexString(), magnet.HashString)
		}
		if magnet.MagnetLink == "" {
			t.Error("Expected non-empty magnet link")
		}

		// Verify magnet link format
		expectedPrefix := "magnet:?xt=urn:btih:" + testHash.HexString()
		if !contains(magnet.MagnetLink, expectedPrefix) {
			t.Errorf("Magnet link should contain '%s', got '%s'", expectedPrefix, magnet.MagnetLink)
		}
	})

	t.Run("GetSpecificMagnetLinks", func(t *testing.T) {
		// Test getting magnet links for specific IDs
		req := TorrentMagnetRequest{IDs: []int64{1}}
		response, err := methods.TorrentMagnet(req)
		if err != nil {
			t.Fatalf("TorrentMagnet failed: %v", err)
		}

		if len(response.Magnets) != 1 {
			t.Fatalf("Expected 1 magnet link, got %d", len(response.Magnets))
		}

		magnet := response.Magnets[0]
		if magnet.ID != 1 {
			t.Errorf("Expected ID 1, got %d", magnet.ID)
		}
	})

	t.Run("NonexistentTorrentID", func(t *testing.T) {
		// Test getting magnet links for non-existent torrent ID
		req := TorrentMagnetRequest{IDs: []int64{999}}
		response, err := methods.TorrentMagnet(req)
		if err != nil {
			t.Fatalf("TorrentMagnet failed: %v", err)
		}

		// Should return empty list for non-existent torrents
		if len(response.Magnets) != 0 {
			t.Fatalf("Expected 0 magnet links for non-existent torrent, got %d", len(response.Magnets))
		}
	})

	t.Run("MixedValidInvalidIDs", func(t *testing.T) {
		// Test getting magnet links for mix of valid and invalid IDs
		req := TorrentMagnetRequest{IDs: []int64{1, 999, 2}}
		response, err := methods.TorrentMagnet(req)
		if err != nil {
			t.Fatalf("TorrentMagnet failed: %v", err)
		}

		// Should return only valid torrents
		if len(response.Magnets) != 1 {
			t.Fatalf("Expected 1 magnet link for valid torrent, got %d", len(response.Magnets))
		}

		magnet := response.Magnets[0]
		if magnet.ID != 1 {
			t.Errorf("Expected ID 1, got %d", magnet.ID)
		}
	})
}

func TestGenerateMagnetLink(t *testing.T) {
	manager := createTestTorrentManager(t)
	methods := NewRPCMethods(manager)

	testHash := metainfo.Hash{0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12}

	t.Run("WithMetaInfo", func(t *testing.T) {
		// Create test metainfo
		metaInfo := &metainfo.MetaInfo{
			Announce: "http://tracker.example.com/announce",
		}

		torrent := &TorrentState{
			ID:       1,
			InfoHash: testHash,
			MetaInfo: metaInfo,
		}

		magnetLink := methods.generateMagnetLink(torrent)

		// Should contain the info hash
		expectedPrefix := "magnet:?xt=urn:btih:" + testHash.HexString()
		if !contains(magnetLink, expectedPrefix) {
			t.Errorf("Magnet link should contain '%s', got '%s'", expectedPrefix, magnetLink)
		}

		// Should contain tracker information if available
		if !contains(magnetLink, "tr=") {
			t.Error("Magnet link should contain tracker information")
		}
	})

	t.Run("WithoutMetaInfo", func(t *testing.T) {
		torrent := &TorrentState{
			ID:       2,
			InfoHash: testHash,
			MetaInfo: nil,
		}

		magnetLink := methods.generateMagnetLink(torrent)

		// Should still contain the info hash
		expectedPrefix := "magnet:?xt=urn:btih:" + testHash.HexString()
		if !contains(magnetLink, expectedPrefix) {
			t.Errorf("Magnet link should contain '%s', got '%s'", expectedPrefix, magnetLink)
		}

		// Should contain hash as display name when no metainfo
		expectedHashName := "dn=" + testHash.HexString()
		if !contains(magnetLink, expectedHashName) {
			t.Error("Magnet link should contain hash as display name when no metainfo")
		}
	})

	t.Run("EmptyName", func(t *testing.T) {
		torrent := &TorrentState{
			ID:       3,
			InfoHash: testHash,
			MetaInfo: nil,
		}

		magnetLink := methods.generateMagnetLink(torrent)

		// Should use hash as name when name is empty
		expectedPrefix := "magnet:?xt=urn:btih:" + testHash.HexString()
		if !contains(magnetLink, expectedPrefix) {
			t.Errorf("Magnet link should contain '%s', got '%s'", expectedPrefix, magnetLink)
		}

		// Should contain hash as display name
		expectedHashName := "dn=" + testHash.HexString()
		if !contains(magnetLink, expectedHashName) {
			t.Error("Magnet link should contain hash as display name when name is empty")
		}
	})
}

func TestTorrentMagnet_WithWebSeeds(t *testing.T) {
	// Create a test torrent manager
	manager := createTestTorrentManager(t)
	methods := NewRPCMethods(manager)

	// Create test torrent data
	testHash := metainfo.Hash{0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12}

	// Create test metainfo with URLList (WebSeeds)
	testMetaInfo := &metainfo.MetaInfo{
		Announce: "http://tracker.example.com/announce",
		URLList:  metainfo.URLList{"http://webseed1.example.com/", "http://webseed2.example.com/"},
	}
	testMetaInfo.InfoBytes = []byte(`d4:name12:Test Torrente`)

	testTorrent := &TorrentState{
		ID:       1,
		InfoHash: testHash,
		Status:   TorrentStatusDownloading,
		MetaInfo: testMetaInfo,
	}

	// Add the torrent to the manager
	manager.mu.Lock()
	manager.torrents[1] = testTorrent
	manager.mu.Unlock()

	// Add WebSeeds to the torrent
	err := manager.AddWebSeed(1, "http://webseed3.example.com/", WebSeedTypeHTTPFTP)
	if err != nil {
		t.Fatalf("Failed to add WebSeed: %v", err)
	}
	err = manager.AddWebSeed(1, "http://webseed4.example.com/", WebSeedTypeHTTPFTP)
	if err != nil {
		t.Fatalf("Failed to add WebSeed: %v", err)
	}

	t.Run("MagnetWithWebSeeds", func(t *testing.T) {
		req := TorrentMagnetRequest{IDs: []int64{1}}
		resp, err := methods.TorrentMagnet(req)
		if err != nil {
			t.Fatalf("TorrentMagnet failed: %v", err)
		}

		if len(resp.Magnets) != 1 {
			t.Fatalf("Expected 1 magnet, got %d", len(resp.Magnets))
		}

		magnet := resp.Magnets[0]
		magnetLink := magnet.MagnetLink

		// Should contain WebSeeds from MetaInfo URLList
		if !contains(magnetLink, "ws=http%3A%2F%2Fwebseed1.example.com%2F") {
			t.Errorf("Magnet link should contain WebSeed from URLList: %s", magnetLink)
		}
		if !contains(magnetLink, "ws=http%3A%2F%2Fwebseed2.example.com%2F") {
			t.Errorf("Magnet link should contain WebSeed from URLList: %s", magnetLink)
		}

		// Should contain WebSeeds from TorrentManager
		if !contains(magnetLink, "ws=http%3A%2F%2Fwebseed3.example.com%2F") {
			t.Errorf("Magnet link should contain WebSeed from manager: %s", magnetLink)
		}
		if !contains(magnetLink, "ws=http%3A%2F%2Fwebseed4.example.com%2F") {
			t.Errorf("Magnet link should contain WebSeed from manager: %s", magnetLink)
		}

		// Should still contain basic magnet components
		if !contains(magnetLink, "xt=urn:btih:") {
			t.Errorf("Magnet link should contain info hash: %s", magnetLink)
		}
		if !contains(magnetLink, "dn=Test+Torrent") {
			t.Errorf("Magnet link should contain display name: %s", magnetLink)
		}
	})

	t.Run("MagnetWithFailedWebSeeds", func(t *testing.T) {
		// Add a failed WebSeed
		err := manager.AddWebSeed(1, "http://failed-webseed.example.com/", WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add WebSeed: %v", err)
		}

		// Mark the WebSeed as failed by accessing the manager directly
		manager.webseedManager.mu.Lock()
		if seeds, exists := manager.webseedManager.webSeeds["1"]; exists {
			for _, ws := range seeds {
				if ws.URL == "http://failed-webseed.example.com/" {
					ws.Failed = true
					ws.Active = false
					break
				}
			}
		}
		manager.webseedManager.mu.Unlock()

		req := TorrentMagnetRequest{IDs: []int64{1}}
		resp, err := methods.TorrentMagnet(req)
		if err != nil {
			t.Fatalf("TorrentMagnet failed: %v", err)
		}

		magnet := resp.Magnets[0]
		magnetLink := magnet.MagnetLink

		// Should not contain failed WebSeed
		if contains(magnetLink, "ws=http%3A%2F%2Ffailed-webseed.example.com%2F") {
			t.Errorf("Magnet link should not contain failed WebSeed: %s", magnetLink)
		}

		// Should still contain active WebSeeds
		if !contains(magnetLink, "ws=http%3A%2F%2Fwebseed3.example.com%2F") {
			t.Errorf("Magnet link should still contain active WebSeeds: %s", magnetLink)
		}
	})

	t.Run("MagnetFallbackWithWebSeeds", func(t *testing.T) {
		// Create torrent without MetaInfo to test fallback
		testTorrentFallback := &TorrentState{
			ID:       2,
			InfoHash: testHash,
			Status:   TorrentStatusDownloading,
			MetaInfo: nil, // No MetaInfo to force fallback
		}

		manager.mu.Lock()
		manager.torrents[2] = testTorrentFallback
		manager.mu.Unlock()

		// Add WebSeeds to the fallback torrent
		err := manager.AddWebSeed(2, "http://fallback-webseed.example.com/", WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add WebSeed: %v", err)
		}

		req := TorrentMagnetRequest{IDs: []int64{2}}
		resp, err := methods.TorrentMagnet(req)
		if err != nil {
			t.Fatalf("TorrentMagnet failed: %v", err)
		}

		magnet := resp.Magnets[0]
		magnetLink := magnet.MagnetLink

		// Should contain WebSeed even in fallback mode
		if !contains(magnetLink, "ws=http%3A%2F%2Ffallback-webseed.example.com%2F") {
			t.Errorf("Fallback magnet link should contain WebSeed: %s", magnetLink)
		}

		// Should contain basic magnet components
		if !contains(magnetLink, "xt=urn:btih:") {
			t.Errorf("Fallback magnet link should contain info hash: %s", magnetLink)
		}
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			indexOfSubstring(s, substr) >= 0))
}

func indexOfSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
