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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/dht"
	"github.com/go-i2p/go-i2p-bt/downloader"
)

// createIntegrationTestScript creates a script that writes execution details to a file
func createIntegrationTestScript(t *testing.T, outputFile string) string {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test_script.sh")

	// Script that captures environment variables and writes them to output file
	scriptContent := fmt.Sprintf(`#!/bin/bash
echo "Script executed at $(date)" >> %s
echo "TR_TORRENT_ID=$TR_TORRENT_ID" >> %s
echo "TR_TORRENT_HASH=$TR_TORRENT_HASH" >> %s
echo "TR_TORRENT_DIR=$TR_TORRENT_DIR" >> %s
echo "TR_TORRENT_STATUS=$TR_TORRENT_STATUS" >> %s
echo "TR_TIME_LOCALTIME=$TR_TIME_LOCALTIME" >> %s
exit 0
`, outputFile, outputFile, outputFile, outputFile, outputFile, outputFile)

	err := ioutil.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	if err != nil {
		t.Fatalf("Failed to create integration test script: %v", err)
	}

	return scriptPath
}

// createTestTorrentManager creates a TorrentManager for integration testing
func createTestTorrentManager(t *testing.T) *TorrentManager {
	tmpDir := t.TempDir()

	config := TorrentManagerConfig{
		DownloadDir: tmpDir,
		DHTConfig: dht.Config{
			K: 8, // Default bucket size
		},
		DownloaderConfig:    downloader.TorrentDownloaderConfig{},
		MaxTorrents:         10,
		PeerLimitGlobal:     50,
		PeerLimitPerTorrent: 10,
		PeerPort:            6881,
		SessionConfig: SessionConfiguration{
			DownloadDir:           tmpDir,
			DHTEnabled:            false, // Disable DHT for testing
			PeerLimitGlobal:       50,
			PeerLimitPerTorrent:   10,
			SpeedLimitDown:        0, // No limits for testing
			SpeedLimitDownEnabled: false,
			SpeedLimitUp:          0,
			SpeedLimitUpEnabled:   false,
			DownloadQueueEnabled:  true,
			DownloadQueueSize:     5,
			SeedQueueEnabled:      true,
			SeedQueueSize:         5,
			StartAddedTorrents:    true,
			// Script settings will be configured in individual tests
			ScriptTorrentDoneEnabled:  false,
			ScriptTorrentDoneFilename: "",
		},
	}

	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}

	return tm
}

func TestTorrentManager_ScriptHooks_TorrentAdded(t *testing.T) {
	tm := createTestTorrentManager(t)
	defer tm.Close()

	// Create output file for script execution verification
	outputFile := filepath.Join(t.TempDir(), "script_output.txt")
	scriptPath := createIntegrationTestScript(t, outputFile)

	// Configure torrent-added script (simulated - we'll configure the script manager directly)
	tm.scriptManager.UpdateHookConfig(ScriptHookTorrentAdded, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  5 * time.Second,
	})

	// Add a torrent to trigger the script (using magnet URI format)
	req := TorrentAddRequest{
		Filename: "magnet:?xt=urn:btih:0102030405060708090a0b0c0d0e0f1011121314&dn=test-torrent.txt",
		Paused:   false,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Wait for script execution (executed in goroutine)
	time.Sleep(2 * time.Second)

	// Verify script was executed
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Error("Script output file not created - script may not have executed")
		return
	}

	content, err := ioutil.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read script output: %v", err)
	}

	output := string(content)

	// Verify script received correct environment variables
	if !strings.Contains(output, fmt.Sprintf("TR_TORRENT_ID=%d", torrent.ID)) {
		t.Errorf("Script output missing correct torrent ID, got: %s", output)
	}

	if !strings.Contains(output, "TR_TORRENT_HASH=") {
		t.Error("Script output missing torrent hash")
	}

	if !strings.Contains(output, "TR_TORRENT_DIR=") {
		t.Error("Script output missing torrent directory")
	}
}

func TestTorrentManager_ScriptHooks_TorrentCompleted(t *testing.T) {
	tm := createTestTorrentManager(t)
	defer tm.Close()

	// Create output file for script execution verification
	outputFile := filepath.Join(t.TempDir(), "completion_script_output.txt")
	scriptPath := createIntegrationTestScript(t, outputFile)

	// Configure torrent-done script
	tm.scriptManager.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  5 * time.Second,
	})

	// Add a torrent
	req := TorrentAddRequest{
		Filename: "magnet:?xt=urn:btih:0102030405060708090a0b0c0d0e0f1011121314&dn=test-torrent.txt",
		Paused:   true, // Start paused so we can manually trigger completion
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Wait for torrent-added script to complete
	time.Sleep(1 * time.Second)

	// Remove the output file to ensure only completion script writes to it
	os.Remove(outputFile)

	// Simulate torrent completion by updating statistics
	tm.mu.Lock()
	torrent.Downloaded = 1024000 // Set to full size
	torrent.PercentDone = 1.0    // Mark as complete
	torrent.wasComplete = false  // Ensure transition from incomplete to complete
	tm.mu.Unlock()

	// Trigger statistics update which should detect completion
	tm.updateTorrentStatistics(torrent, time.Now())

	// Wait for script execution
	time.Sleep(2 * time.Second)

	// Verify completion script was executed
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Error("Completion script output file not created - script may not have executed")
		return
	}

	content, err := ioutil.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read completion script output: %v", err)
	}

	output := string(content)

	// Verify script received correct environment variables
	if !strings.Contains(output, fmt.Sprintf("TR_TORRENT_ID=%d", torrent.ID)) {
		t.Errorf("Completion script output missing correct torrent ID, got: %s", output)
	}
}

func TestTorrentManager_ScriptHooks_Configuration(t *testing.T) {
	tm := createTestTorrentManager(t)
	defer tm.Close()

	// Test initial configuration (should be disabled)
	if tm.scriptManager.IsHookEnabled(ScriptHookTorrentDone) {
		t.Error("Script hook should be disabled initially")
	}

	// Create test script
	outputFile := filepath.Join(t.TempDir(), "config_test_output.txt")
	scriptPath := createIntegrationTestScript(t, outputFile)

	// Update session config to enable script
	newConfig := tm.sessionConfig
	newConfig.ScriptTorrentDoneEnabled = true
	newConfig.ScriptTorrentDoneFilename = scriptPath

	err := tm.UpdateSessionConfig(newConfig)
	if err != nil {
		t.Fatalf("Failed to update session config: %v", err)
	}

	// Verify script is now enabled
	if !tm.scriptManager.IsHookEnabled(ScriptHookTorrentDone) {
		t.Error("Script hook should be enabled after configuration update")
	}

	// Test disabling script
	newConfig.ScriptTorrentDoneEnabled = false
	err = tm.UpdateSessionConfig(newConfig)
	if err != nil {
		t.Fatalf("Failed to update session config to disable script: %v", err)
	}

	// Verify script is now disabled
	if tm.scriptManager.IsHookEnabled(ScriptHookTorrentDone) {
		t.Error("Script hook should be disabled after configuration update")
	}
}

func TestTorrentManager_ScriptHooks_ErrorHandling(t *testing.T) {
	tm := createTestTorrentManager(t)
	defer tm.Close()

	// Create a script that will fail
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "failing_script.sh")
	scriptContent := `#!/bin/bash
echo "This script will fail" >&2
exit 1
`
	err := ioutil.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	if err != nil {
		t.Fatalf("Failed to create failing script: %v", err)
	}

	// Configure failing script
	tm.scriptManager.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  5 * time.Second,
	})

	// Create a mock torrent to test script execution
	mockTorrent := &TorrentState{
		ID:          999,
		InfoHash:    [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		DownloadDir: tmpDir,
		PercentDone: 1.0,
	}

	// Execute script hook directly and verify error handling
	err = tm.scriptManager.ExecuteHook(ScriptHookTorrentDone, mockTorrent)
	if err == nil {
		t.Error("Expected error from failing script, got nil")
	}

	// Test non-existent script
	tm.scriptManager.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: "/path/to/nonexistent/script.sh",
		Timeout:  5 * time.Second,
	})

	err = tm.scriptManager.ExecuteHook(ScriptHookTorrentDone, mockTorrent)
	if err == nil {
		t.Error("Expected error for non-existent script, got nil")
	}
}

func TestTorrentManager_ScriptHooks_Timeout(t *testing.T) {
	tm := createTestTorrentManager(t)
	defer tm.Close()

	// Create a script that runs longer than timeout
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "slow_script.sh")
	scriptContent := `#!/bin/bash
sleep 3
echo "Should not reach this"
exit 0
`
	err := ioutil.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	if err != nil {
		t.Fatalf("Failed to create slow script: %v", err)
	}

	// Configure script with short timeout
	tm.scriptManager.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  1 * time.Second, // Shorter than script execution time
	})

	// Create a mock torrent to test script execution
	mockTorrent := &TorrentState{
		ID:          999,
		InfoHash:    [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		DownloadDir: tmpDir,
		PercentDone: 1.0,
	}

	// Execute script hook and verify timeout error
	start := time.Now()
	err = tm.scriptManager.ExecuteHook(ScriptHookTorrentDone, mockTorrent)
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	// Verify script was killed around timeout duration (allow some tolerance for cleanup)
	if duration > 4*time.Second {
		t.Errorf("Script execution took too long: %v (expected around 1-3s including cleanup)", duration)
	}
}

// Benchmark script hook performance
func BenchmarkTorrentManager_ScriptHooks_ExecuteHook(b *testing.B) {
	tm := createTestTorrentManager(&testing.T{})
	defer tm.Close()

	// Create a fast script
	tmpDir := b.TempDir()
	scriptPath := filepath.Join(tmpDir, "fast_script.sh")
	scriptContent := `#!/bin/bash
exit 0
`
	err := ioutil.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	if err != nil {
		b.Fatalf("Failed to create fast script: %v", err)
	}

	tm.scriptManager.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  5 * time.Second,
	})

	mockTorrent := &TorrentState{
		ID:          999,
		InfoHash:    [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		DownloadDir: tmpDir,
		PercentDone: 1.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := tm.scriptManager.ExecuteHook(ScriptHookTorrentDone, mockTorrent)
		if err != nil {
			b.Fatalf("Script execution failed: %v", err)
		}
	}
}
