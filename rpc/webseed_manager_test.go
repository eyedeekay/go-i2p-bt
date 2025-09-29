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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebSeedManager(t *testing.T) {
	t.Run("CreateWebSeedManager", func(t *testing.T) {
		ws := NewWebSeedManager(true)
		if !ws.IsEnabled() {
			t.Error("WebSeed should be enabled")
		}

		stats := ws.GetStats()
		if stats["enabled"] != true {
			t.Error("WebSeed should be enabled in stats")
		}
	})

	t.Run("EnableDisableWebSeed", func(t *testing.T) {
		ws := NewWebSeedManager(false)
		if ws.IsEnabled() {
			t.Error("WebSeed should be disabled initially")
		}

		ws.SetEnabled(true)
		if !ws.IsEnabled() {
			t.Error("WebSeed should be enabled after SetEnabled(true)")
		}

		ws.SetEnabled(false)
		if ws.IsEnabled() {
			t.Error("WebSeed should be disabled after SetEnabled(false)")
		}
	})

	t.Run("AddWebSeed", func(t *testing.T) {
		ws := NewWebSeedManager(true)
		torrentID := "test-torrent"
		url := "http://example.com/file.bin"

		err := ws.AddWebSeed(torrentID, url, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed: %v", err)
		}

		webseeds := ws.GetWebSeeds(torrentID)
		if len(webseeds) != 1 {
			t.Errorf("Expected 1 webseed, got %d", len(webseeds))
		}

		if webseeds[0].URL != url {
			t.Errorf("Expected URL %s, got %s", url, webseeds[0].URL)
		}

		if webseeds[0].Type != WebSeedTypeHTTPFTP {
			t.Errorf("Expected type %s, got %s", WebSeedTypeHTTPFTP, webseeds[0].Type)
		}
	})

	t.Run("AddInvalidWebSeed", func(t *testing.T) {
		ws := NewWebSeedManager(true)
		torrentID := "test-torrent"
		invalidURL := "not-a-url"

		err := ws.AddWebSeed(torrentID, invalidURL, WebSeedTypeHTTPFTP)
		if err == nil {
			t.Error("Expected error for invalid URL")
		}
	})

	t.Run("RemoveWebSeed", func(t *testing.T) {
		ws := NewWebSeedManager(true)
		torrentID := "test-torrent"
		url := "http://example.com/file.bin"

		err := ws.AddWebSeed(torrentID, url, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed: %v", err)
		}

		webseeds := ws.GetWebSeeds(torrentID)
		if len(webseeds) != 1 {
			t.Error("Webseed should be added")
		}

		ws.RemoveWebSeed(torrentID, url)
		webseeds = ws.GetWebSeeds(torrentID)
		if len(webseeds) != 0 {
			t.Error("Webseed should be removed")
		}
	})

	t.Run("GetActiveWebSeeds", func(t *testing.T) {
		ws := NewWebSeedManager(true)
		torrentID := "test-torrent"
		url1 := "http://example.com/file1.bin"
		url2 := "http://example.com/file2.bin"

		// Add two webseeds
		err := ws.AddWebSeed(torrentID, url1, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed 1: %v", err)
		}

		err = ws.AddWebSeed(torrentID, url2, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed 2: %v", err)
		}

		// Both should be active
		active := ws.GetActiveWebSeeds(torrentID)
		if len(active) != 2 {
			t.Errorf("Expected 2 active webseeds, got %d", len(active))
		}

		// Mark one as failed
		ws.markSeedFailed(torrentID, url1, &fakeError{})
		ws.markSeedFailed(torrentID, url1, &fakeError{})
		ws.markSeedFailed(torrentID, url1, &fakeError{}) // 3 failures to mark as failed

		active = ws.GetActiveWebSeeds(torrentID)
		if len(active) != 1 {
			t.Errorf("Expected 1 active webseed after failure, got %d", len(active))
		}
	})

	t.Run("CleanupTorrent", func(t *testing.T) {
		ws := NewWebSeedManager(true)
		torrentID := "test-torrent"
		url := "http://example.com/file.bin"

		err := ws.AddWebSeed(torrentID, url, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed: %v", err)
		}

		webseeds := ws.GetWebSeeds(torrentID)
		if len(webseeds) != 1 {
			t.Error("Webseed should be added")
		}

		ws.CleanupTorrent(torrentID)
		webseeds = ws.GetWebSeeds(torrentID)
		if webseeds != nil {
			t.Error("All webseeds should be removed after cleanup")
		}
	})

	t.Run("DisabledWebSeed", func(t *testing.T) {
		ws := NewWebSeedManager(false)
		torrentID := "test-torrent"
		url := "http://example.com/file.bin"

		// Should not add webseed when disabled
		err := ws.AddWebSeed(torrentID, url, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Errorf("Should not error when disabled: %v", err)
		}

		webseeds := ws.GetWebSeeds(torrentID)
		if webseeds != nil {
			t.Error("Should not add webseeds when disabled")
		}
	})
}

func TestWebSeedDownload(t *testing.T) {
	t.Run("DownloadFromHTTPSeed", func(t *testing.T) {
		// Create test HTTP server
		testData := []byte("test file content for piece download")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for Range header
			rangeHeader := r.Header.Get("Range")
			if rangeHeader == "" {
				t.Error("Expected Range header")
			}

			// For simplicity, just return the test data
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(testData)
		}))
		defer server.Close()

		ws := NewWebSeedManager(true)
		torrentID := "test-torrent"

		err := ws.AddWebSeed(torrentID, server.URL, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed: %v", err)
		}

		ctx := context.Background()
		data, err := ws.DownloadPieceData(ctx, torrentID, 0, int64(len(testData)), "testfile.bin")
		if err != nil {
			t.Fatalf("Failed to download from webseed: %v", err)
		}

		if string(data) != string(testData) {
			t.Errorf("Downloaded data mismatch. Expected: %s, Got: %s", string(testData), string(data))
		}
	})

	t.Run("DownloadFromFailingSeed", func(t *testing.T) {
		// Create test HTTP server that always fails
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		ws := NewWebSeedManager(true)
		torrentID := "test-torrent"

		err := ws.AddWebSeed(torrentID, server.URL, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed: %v", err)
		}

		ctx := context.Background()
		_, err = ws.DownloadPieceData(ctx, torrentID, 0, 1024, "testfile.bin")
		if err == nil {
			t.Error("Expected download to fail")
		}

		// Check that webseed was marked as failed after multiple attempts
		webseeds := ws.GetWebSeeds(torrentID)
		if len(webseeds) == 0 {
			t.Fatal("Webseed should still exist")
		}

		// Try a few more times to mark it as failed
		ws.DownloadPieceData(ctx, torrentID, 0, 1024, "testfile.bin")
		ws.DownloadPieceData(ctx, torrentID, 0, 1024, "testfile.bin")

		active := ws.GetActiveWebSeeds(torrentID)
		if len(active) != 0 {
			t.Error("Webseed should be marked as failed after multiple failures")
		}
	})

	t.Run("RetryFailedSeeds", func(t *testing.T) {
		ws := NewWebSeedManager(true)
		torrentID := "test-torrent"
		url := "http://example.com/file.bin"

		err := ws.AddWebSeed(torrentID, url, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed: %v", err)
		}

		// Mark as failed
		ws.markSeedFailed(torrentID, url, &fakeError{})
		ws.markSeedFailed(torrentID, url, &fakeError{})
		ws.markSeedFailed(torrentID, url, &fakeError{})

		active := ws.GetActiveWebSeeds(torrentID)
		if len(active) != 0 {
			t.Error("Webseed should be failed")
		}

		// Retry failed seeds (normally this would wait for cooldown, but we force it)
		ws.mu.Lock()
		seeds := ws.webSeeds[torrentID]
		for _, seed := range seeds {
			seed.LastTried = time.Now().Add(-10 * time.Minute) // Simulate old failure
		}
		ws.mu.Unlock()

		retried := ws.RetryFailedSeeds(torrentID)
		if retried != 1 {
			t.Errorf("Expected 1 retry, got %d", retried)
		}

		active = ws.GetActiveWebSeeds(torrentID)
		if len(active) != 1 {
			t.Error("Webseed should be active after retry")
		}
	})
}

func TestTorrentManagerWebSeed(t *testing.T) {
	t.Run("WebSeedIntegration", func(t *testing.T) {
		manager := createTestTorrentManager(t)

		// Check initial WebSeed stats
		stats := manager.GetWebSeedStats()
		if stats["enabled"] != true {
			t.Error("WebSeed should be enabled by default")
		}

		// Add webseed
		url := "http://example.com/file.bin"
		err := manager.AddWebSeed(1, url, WebSeedTypeHTTPFTP)
		if err != nil {
			t.Fatalf("Failed to add webseed: %v", err)
		}

		// Get webseeds
		webseeds := manager.GetWebSeeds(1)
		if len(webseeds) != 1 {
			t.Errorf("Expected 1 webseed, got %d", len(webseeds))
		}

		if webseeds[0].URL != url {
			t.Errorf("Expected URL %s, got %s", url, webseeds[0].URL)
		}

		// Remove webseed
		manager.RemoveWebSeed(1, url)
		webseeds = manager.GetWebSeeds(1)
		if len(webseeds) != 0 {
			t.Error("Webseed should be removed")
		}
	})

	t.Run("WebSeedDisabledByConfig", func(t *testing.T) {
		manager := createTestTorrentManager(t)

		// Disable WebSeed
		config := manager.GetSessionConfig()
		config.WebSeedsEnabled = false
		err := manager.UpdateSessionConfig(config)
		if err != nil {
			t.Fatalf("Failed to update session config: %v", err)
		}

		// Check WebSeed is disabled
		stats := manager.GetWebSeedStats()
		if stats["enabled"] != false {
			t.Error("WebSeed should be disabled after config update")
		}
	})
}

// fakeError is a simple error implementation for testing
type fakeError struct{}

func (e *fakeError) Error() string {
	return "fake error for testing"
}
