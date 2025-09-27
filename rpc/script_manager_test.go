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
	"path/filepath"
	"testing"
	"time"
)

// createTestScript creates a temporary executable script for testing
func createTestScript(t *testing.T, content string) string {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test_script.sh")

	// Write script content
	err := ioutil.WriteFile(scriptPath, []byte(content), 0755)
	if err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	return scriptPath
}

// createTestTorrent creates a test TorrentState for script execution
func createTestTorrent(t *testing.T) *TorrentState {
	// Create a minimal test torrent state for script testing
	// MetaInfo is nil to test graceful handling of missing metadata
	return &TorrentState{
		ID:          123,
		InfoHash:    [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14},
		MetaInfo:    nil, // Test nil metainfo handling
		Status:      1,   // TorrentStatusDownloading
		DownloadDir: "/tmp/downloads",
		Downloaded:  512000, // 50% complete
		PercentDone: 0.5,
	}
}

func TestNewScriptManager(t *testing.T) {
	sm := NewScriptManager()

	if sm == nil {
		t.Fatal("NewScriptManager returned nil")
	}

	if sm.hooks == nil {
		t.Error("hooks map not initialized")
	}

	if sm.defaultTimeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", sm.defaultTimeout)
	}

	if sm.logger == nil {
		t.Error("logger not set")
	}
}

func TestScriptManager_UpdateHookConfig(t *testing.T) {
	sm := NewScriptManager()

	// Test adding a hook configuration
	config := &ScriptHookConfig{
		Enabled:  true,
		Filename: "/path/to/script.sh",
		Timeout:  10 * time.Second,
		Environment: map[string]string{
			"CUSTOM_VAR": "test_value",
		},
	}

	sm.UpdateHookConfig(ScriptHookTorrentDone, config)

	retrievedConfig, exists := sm.GetHookConfig(ScriptHookTorrentDone)
	if !exists {
		t.Fatal("Hook configuration not found")
	}

	if retrievedConfig.Enabled != config.Enabled {
		t.Errorf("Expected Enabled %v, got %v", config.Enabled, retrievedConfig.Enabled)
	}

	if retrievedConfig.Filename != config.Filename {
		t.Errorf("Expected Filename %s, got %s", config.Filename, retrievedConfig.Filename)
	}

	if retrievedConfig.Timeout != config.Timeout {
		t.Errorf("Expected Timeout %v, got %v", config.Timeout, retrievedConfig.Timeout)
	}

	if retrievedConfig.Environment["CUSTOM_VAR"] != "test_value" {
		t.Error("Custom environment variable not preserved")
	}
}

func TestScriptManager_UpdateHookConfig_DefaultTimeout(t *testing.T) {
	sm := NewScriptManager()

	// Test default timeout assignment
	config := &ScriptHookConfig{
		Enabled:  true,
		Filename: "/path/to/script.sh",
		// Timeout not set (0)
	}

	sm.UpdateHookConfig(ScriptHookTorrentDone, config)

	retrievedConfig, _ := sm.GetHookConfig(ScriptHookTorrentDone)
	if retrievedConfig.Timeout != sm.defaultTimeout {
		t.Errorf("Expected timeout %v, got %v", sm.defaultTimeout, retrievedConfig.Timeout)
	}

	if retrievedConfig.Environment == nil {
		t.Error("Environment map not initialized")
	}
}

func TestScriptManager_UpdateHookConfig_RemoveHook(t *testing.T) {
	sm := NewScriptManager()

	// Add a hook first
	config := &ScriptHookConfig{
		Enabled:  true,
		Filename: "/path/to/script.sh",
	}
	sm.UpdateHookConfig(ScriptHookTorrentDone, config)

	// Verify it exists
	_, exists := sm.GetHookConfig(ScriptHookTorrentDone)
	if !exists {
		t.Fatal("Hook should exist")
	}

	// Remove it by passing nil
	sm.UpdateHookConfig(ScriptHookTorrentDone, nil)

	// Verify it's removed
	_, exists = sm.GetHookConfig(ScriptHookTorrentDone)
	if exists {
		t.Error("Hook should be removed")
	}
}

func TestScriptManager_IsHookEnabled(t *testing.T) {
	sm := NewScriptManager()

	// Test disabled hook
	if sm.IsHookEnabled(ScriptHookTorrentDone) {
		t.Error("Hook should be disabled by default")
	}

	// Test enabled hook with filename
	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: "/path/to/script.sh",
	})

	if !sm.IsHookEnabled(ScriptHookTorrentDone) {
		t.Error("Hook should be enabled")
	}

	// Test enabled hook without filename
	sm.UpdateHookConfig(ScriptHookTorrentAdded, &ScriptHookConfig{
		Enabled:  true,
		Filename: "", // Empty filename
	})

	if sm.IsHookEnabled(ScriptHookTorrentAdded) {
		t.Error("Hook should be disabled when filename is empty")
	}
}

func TestScriptManager_GetEnabledHooks(t *testing.T) {
	sm := NewScriptManager()

	// No hooks enabled initially
	enabled := sm.GetEnabledHooks()
	if len(enabled) != 0 {
		t.Errorf("Expected 0 enabled hooks, got %d", len(enabled))
	}

	// Enable one hook
	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: "/path/to/script.sh",
	})

	enabled = sm.GetEnabledHooks()
	if len(enabled) != 1 {
		t.Errorf("Expected 1 enabled hook, got %d", len(enabled))
	}

	if enabled[0] != ScriptHookTorrentDone {
		t.Errorf("Expected %s, got %s", ScriptHookTorrentDone, enabled[0])
	}

	// Enable second hook
	sm.UpdateHookConfig(ScriptHookTorrentAdded, &ScriptHookConfig{
		Enabled:  true,
		Filename: "/path/to/another_script.sh",
	})

	enabled = sm.GetEnabledHooks()
	if len(enabled) != 2 {
		t.Errorf("Expected 2 enabled hooks, got %d", len(enabled))
	}
}

func TestScriptManager_ExecuteHook_Success(t *testing.T) {
	sm := NewScriptManager()

	// Create a simple script that outputs environment variables
	scriptContent := `#!/bin/bash
echo "Torrent ID: $TR_TORRENT_ID"
echo "Torrent Name: $TR_TORRENT_NAME"
echo "Custom Var: $TEST_VAR"
exit 0
`
	scriptPath := createTestScript(t, scriptContent)

	// Configure the hook
	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  5 * time.Second,
		Environment: map[string]string{
			"TEST_VAR": "custom_value",
		},
	})

	// Create test torrent
	torrent := createTestTorrent(t)

	// Execute the hook
	err := sm.ExecuteHook(ScriptHookTorrentDone, torrent)
	if err != nil {
		t.Errorf("Script execution failed: %v", err)
	}
}

func TestScriptManager_ExecuteHook_Timeout(t *testing.T) {
	sm := NewScriptManager()

	// Create a script that sleeps longer than the timeout
	scriptContent := `#!/bin/bash
sleep 3
echo "Should not reach this"
exit 0
`
	scriptPath := createTestScript(t, scriptContent)

	// Configure the hook with short timeout
	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  1 * time.Second, // Short timeout
	})

	// Create test torrent
	torrent := createTestTorrent(t)

	// Execute the hook - should timeout
	err := sm.ExecuteHook(ScriptHookTorrentDone, torrent)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

func TestScriptManager_ExecuteHook_ScriptError(t *testing.T) {
	sm := NewScriptManager()

	// Create a script that exits with error
	scriptContent := `#!/bin/bash
echo "Error occurred" >&2
exit 1
`
	scriptPath := createTestScript(t, scriptContent)

	// Configure the hook
	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  5 * time.Second,
	})

	// Create test torrent
	torrent := createTestTorrent(t)

	// Execute the hook - should fail
	err := sm.ExecuteHook(ScriptHookTorrentDone, torrent)
	if err == nil {
		t.Error("Expected script error, got nil")
	}
}

func TestScriptManager_ExecuteHook_DisabledHook(t *testing.T) {
	sm := NewScriptManager()

	// Configure disabled hook
	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  false,
		Filename: "/path/to/script.sh",
	})

	// Create test torrent
	torrent := createTestTorrent(t)

	// Execute the hook - should succeed (no-op)
	err := sm.ExecuteHook(ScriptHookTorrentDone, torrent)
	if err != nil {
		t.Errorf("Disabled hook execution should succeed: %v", err)
	}
}

func TestScriptManager_ExecuteHook_NoConfiguration(t *testing.T) {
	sm := NewScriptManager()

	// Create test torrent
	torrent := createTestTorrent(t)

	// Execute unconfigured hook - should succeed (no-op)
	err := sm.ExecuteHook(ScriptHookTorrentDone, torrent)
	if err != nil {
		t.Errorf("Unconfigured hook execution should succeed: %v", err)
	}
}

func TestScriptManager_validateScriptFile(t *testing.T) {
	sm := NewScriptManager()

	// Test empty filename
	err := sm.validateScriptFile("")
	if err == nil {
		t.Error("Expected error for empty filename")
	}

	// Test non-existent file
	err = sm.validateScriptFile("/path/to/non/existent/script.sh")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	// Test directory instead of file
	tmpDir := t.TempDir()
	err = sm.validateScriptFile(tmpDir)
	if err == nil {
		t.Error("Expected error for directory path")
	}

	// Test non-executable file
	nonExecFile := filepath.Join(tmpDir, "non_exec.txt")
	ioutil.WriteFile(nonExecFile, []byte("test"), 0644) // No execute permission
	err = sm.validateScriptFile(nonExecFile)
	if err == nil {
		t.Error("Expected error for non-executable file")
	}

	// Test valid executable file
	execFile := filepath.Join(tmpDir, "exec.sh")
	ioutil.WriteFile(execFile, []byte("#!/bin/bash\necho test"), 0755) // With execute permission
	err = sm.validateScriptFile(execFile)
	if err != nil {
		t.Errorf("Expected no error for valid executable file: %v", err)
	}
}

func TestScriptManager_buildTorrentEnvironment(t *testing.T) {
	sm := NewScriptManager()

	// Create test torrent
	torrent := createTestTorrent(t)

	// Custom environment
	customEnv := map[string]string{
		"CUSTOM_VAR":    "custom_value",
		"TR_TORRENT_ID": "999", // Should override torrent ID
	}

	env := sm.buildTorrentEnvironment(torrent, customEnv)

	// Parse environment into map for easy testing
	envMap := make(map[string]string)
	for _, pair := range env {
		if idx := len("TR_TORRENT_ID="); len(pair) > idx && pair[:idx] == "TR_TORRENT_ID=" {
			envMap["TR_TORRENT_ID"] = pair[idx:]
		}
		if idx := len("TR_TORRENT_NAME="); len(pair) > idx && pair[:idx] == "TR_TORRENT_NAME=" {
			envMap["TR_TORRENT_NAME"] = pair[idx:]
		}
		if idx := len("TR_TORRENT_HASH="); len(pair) > idx && pair[:idx] == "TR_TORRENT_HASH=" {
			envMap["TR_TORRENT_HASH"] = pair[idx:]
		}
		if idx := len("TR_TORRENT_DIR="); len(pair) > idx && pair[:idx] == "TR_TORRENT_DIR=" {
			envMap["TR_TORRENT_DIR"] = pair[idx:]
		}
		if idx := len("CUSTOM_VAR="); len(pair) > idx && pair[:idx] == "CUSTOM_VAR=" {
			envMap["CUSTOM_VAR"] = pair[idx:]
		}
	}

	// Test custom environment override
	if envMap["TR_TORRENT_ID"] != "999" {
		t.Errorf("Expected TR_TORRENT_ID=999, got %s", envMap["TR_TORRENT_ID"])
	}

	// Test custom variable
	if envMap["CUSTOM_VAR"] != "custom_value" {
		t.Errorf("Expected CUSTOM_VAR=custom_value, got %s", envMap["CUSTOM_VAR"])
	}

	// Test torrent fields (name should be empty since MetaInfo is nil in test)
	if _, exists := envMap["TR_TORRENT_NAME"]; exists {
		t.Errorf("TR_TORRENT_NAME should not be set when MetaInfo is nil, got %s", envMap["TR_TORRENT_NAME"])
	}

	if envMap["TR_TORRENT_DIR"] != "/tmp/downloads" {
		t.Errorf("Expected TR_TORRENT_DIR=/tmp/downloads, got %s", envMap["TR_TORRENT_DIR"])
	}

	expectedHash := "0102030405060708090a0b0c0d0e0f1011121314"
	if envMap["TR_TORRENT_HASH"] != expectedHash {
		t.Errorf("Expected TR_TORRENT_HASH=%s, got %s", expectedHash, envMap["TR_TORRENT_HASH"])
	}
}

func TestScriptManager_SetLogger(t *testing.T) {
	sm := NewScriptManager()

	// Test custom logger
	var logMessages []string
	customLogger := func(format string, args ...interface{}) {
		logMessages = append(logMessages, fmt.Sprintf(format, args...))
	}

	sm.SetLogger(customLogger)

	// Create a simple script
	scriptContent := `#!/bin/bash
echo "test output"
exit 0
`
	scriptPath := createTestScript(t, scriptContent)

	// Configure and execute hook
	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
	})

	torrent := createTestTorrent(t)
	err := sm.ExecuteHook(ScriptHookTorrentDone, torrent)
	if err != nil {
		t.Errorf("Script execution failed: %v", err)
	}

	// Verify custom logger was called
	if len(logMessages) == 0 {
		t.Error("Expected log messages from custom logger")
	}
}

func TestScriptManager_SetDefaultTimeout(t *testing.T) {
	sm := NewScriptManager()

	newTimeout := 60 * time.Second
	sm.SetDefaultTimeout(newTimeout)

	if sm.GetDefaultTimeout() != newTimeout {
		t.Errorf("Expected timeout %v, got %v", newTimeout, sm.GetDefaultTimeout())
	}

	// Test that new hooks get the new default timeout
	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: "/path/to/script.sh",
		// Timeout not set
	})

	config, _ := sm.GetHookConfig(ScriptHookTorrentDone)
	if config.Timeout != newTimeout {
		t.Errorf("Expected new hook to have timeout %v, got %v", newTimeout, config.Timeout)
	}
}

// Benchmark script execution performance
func BenchmarkScriptManager_ExecuteHook(b *testing.B) {
	sm := NewScriptManager()

	// Create a fast script
	scriptContent := `#!/bin/bash
exit 0
`
	scriptPath := createTestScript(&testing.T{}, scriptContent)

	sm.UpdateHookConfig(ScriptHookTorrentDone, &ScriptHookConfig{
		Enabled:  true,
		Filename: scriptPath,
		Timeout:  5 * time.Second,
	})

	torrent := createTestTorrent(&testing.T{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := sm.ExecuteHook(ScriptHookTorrentDone, torrent)
		if err != nil {
			b.Fatalf("Script execution failed: %v", err)
		}
	}
}
