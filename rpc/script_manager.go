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
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// ScriptHookType represents the type of lifecycle event
type ScriptHookType string

const (
	// ScriptHookTorrentDone is called when a torrent finishes downloading
	ScriptHookTorrentDone ScriptHookType = "torrent-done"
	// ScriptHookTorrentAdded is called when a new torrent is added
	ScriptHookTorrentAdded ScriptHookType = "torrent-added"
)

// ScriptHookConfig holds configuration for script execution
type ScriptHookConfig struct {
	// Enabled determines if the script should be executed
	Enabled bool
	// Filename is the path to the script to execute
	Filename string
	// Timeout is the maximum duration to wait for script execution
	Timeout time.Duration
	// Environment variables to pass to the script
	Environment map[string]string
}

// ScriptManager handles execution of lifecycle scripts following Transmission RPC patterns
// It provides secure script execution with timeout, environment variables, and proper error handling
type ScriptManager struct {
	mu sync.RWMutex
	// Script configurations by hook type
	hooks map[ScriptHookType]*ScriptHookConfig
	// Default timeout for script execution
	defaultTimeout time.Duration
	// Logger for script execution events
	logger func(format string, args ...interface{})
}

// NewScriptManager creates a new script manager with default configuration
func NewScriptManager() *ScriptManager {
	return &ScriptManager{
		hooks:          make(map[ScriptHookType]*ScriptHookConfig),
		defaultTimeout: 30 * time.Second, // Default 30-second timeout
		logger:         log.Printf,
	}
}

// SetLogger sets a custom logger for script execution events
func (sm *ScriptManager) SetLogger(logger func(format string, args ...interface{})) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.logger = logger
}

// UpdateHookConfig updates the configuration for a specific hook type
// This method is called when session configuration changes are applied
func (sm *ScriptManager) UpdateHookConfig(hookType ScriptHookType, config *ScriptHookConfig) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if config == nil {
		delete(sm.hooks, hookType)
		return
	}

	// Set default timeout if not specified
	if config.Timeout == 0 {
		config.Timeout = sm.defaultTimeout
	}

	// Initialize environment map if nil
	if config.Environment == nil {
		config.Environment = make(map[string]string)
	}

	sm.hooks[hookType] = config
}

// GetHookConfig returns the current configuration for a hook type
func (sm *ScriptManager) GetHookConfig(hookType ScriptHookType) (*ScriptHookConfig, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	config, exists := sm.hooks[hookType]
	return config, exists
}

// validateHookConfiguration checks if the hook is properly configured and enabled.
func (sm *ScriptManager) validateHookConfiguration(hookType ScriptHookType) (*ScriptHookConfig, error) {
	sm.mu.RLock()
	config, exists := sm.hooks[hookType]
	sm.mu.RUnlock()

	if !exists || !config.Enabled || config.Filename == "" {
		return nil, nil // Hook not configured or disabled - not an error
	}

	// Validate script file exists and is executable
	if err := sm.validateScriptFile(config.Filename); err != nil {
		return nil, fmt.Errorf("script validation failed: %w", err)
	}

	return config, nil
}

// getTorrentName safely extracts the torrent name for logging purposes.
func (sm *ScriptManager) getTorrentName(torrent *TorrentState) string {
	if torrent.MetaInfo != nil {
		if info, err := torrent.MetaInfo.Info(); err == nil {
			return info.Name
		}
	}
	return "unknown"
}

// logScriptExecution logs the start of script execution if logger is configured.
func (sm *ScriptManager) logScriptExecution(hookType ScriptHookType, config *ScriptHookConfig, torrent *TorrentState) {
	if sm.logger != nil {
		name := sm.getTorrentName(torrent)
		sm.logger("Executing %s script: %s for torrent %d (%s)",
			hookType, config.Filename, torrent.ID, name)
	}
}

// executeScriptWithTimeout runs the script with the configured timeout and environment.
func (sm *ScriptManager) executeScriptWithTimeout(config *ScriptHookConfig, env []string) ([]byte, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, config.Filename)
	cmd.Env = env

	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	return output, duration, err
}

// logScriptResult logs the execution result if logger is configured.
func (sm *ScriptManager) logScriptResult(config *ScriptHookConfig, output []byte, duration time.Duration, err error) {
	if sm.logger != nil {
		if err != nil {
			sm.logger("Script %s failed after %v: %v\nOutput: %s",
				config.Filename, duration, err, string(output))
		} else {
			sm.logger("Script %s completed successfully in %v",
				config.Filename, duration)
		}
	}
}

// ExecuteHook executes a script hook for the given torrent lifecycle event
// It passes torrent information as environment variables following Transmission patterns
func (sm *ScriptManager) ExecuteHook(hookType ScriptHookType, torrent *TorrentState) error {
	// Validate hook configuration
	config, err := sm.validateHookConfiguration(hookType)
	if err != nil {
		return err
	}
	if config == nil {
		return nil // Hook not configured or disabled
	}

	// Build environment variables with torrent information
	env := sm.buildTorrentEnvironment(torrent, config.Environment)

	// Log script execution start
	sm.logScriptExecution(hookType, config, torrent)

	// Execute script with timeout
	output, duration, err := sm.executeScriptWithTimeout(config, env)

	// Log execution result
	sm.logScriptResult(config, output, duration, err)

	if err != nil {
		return fmt.Errorf("script execution failed: %w", err)
	}

	return nil
}

// validateScriptFile checks if the script file exists and is executable
func (sm *ScriptManager) validateScriptFile(filename string) error {
	if filename == "" {
		return fmt.Errorf("script filename is empty")
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Check if file exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("script file does not exist: %s", absPath)
		}
		return fmt.Errorf("failed to stat script file: %w", err)
	}

	// Check if file is regular (not directory)
	if !info.Mode().IsRegular() {
		return fmt.Errorf("script path is not a regular file: %s", absPath)
	}

	// Check if file is executable (owner execute bit)
	if info.Mode()&0o100 == 0 {
		return fmt.Errorf("script file is not executable: %s", absPath)
	}

	return nil
}

// buildTorrentEnvironment creates environment variables for script execution
// Following Transmission's environment variable patterns
func (sm *ScriptManager) buildTorrentEnvironment(torrent *TorrentState, customEnv map[string]string) []string {
	env := os.Environ() // Start with system environment

	// Add Transmission-compatible torrent information
	torrentVars := map[string]string{
		"TR_TORRENT_ID":     strconv.FormatInt(torrent.ID, 10),
		"TR_TORRENT_DIR":    torrent.DownloadDir,
		"TR_TIME_LOCALTIME": strconv.FormatInt(time.Now().Unix(), 10),
	}

	// Add optional fields if available
	if torrent.MetaInfo != nil {
		if info, err := torrent.MetaInfo.Info(); err == nil {
			torrentVars["TR_TORRENT_NAME"] = info.Name
			torrentVars["TR_TORRENT_SIZE"] = strconv.FormatInt(info.TotalLength(), 10)
		}
	}

	// Add info hash as hex string
	torrentVars["TR_TORRENT_HASH"] = fmt.Sprintf("%x", torrent.InfoHash[:])

	// Add status-specific information
	if torrent.Status > 0 {
		torrentVars["TR_TORRENT_STATUS"] = strconv.FormatInt(torrent.Status, 10)
	}

	if torrent.PercentDone > 0 {
		torrentVars["TR_TORRENT_PERCENT_DONE"] = fmt.Sprintf("%.2f", torrent.PercentDone)
	}

	// Calculate completed bytes from download progress
	if torrent.Downloaded > 0 {
		torrentVars["TR_TORRENT_BYTES_COMPLETED"] = strconv.FormatInt(torrent.Downloaded, 10)
	}

	// Add custom environment variables (override system/torrent vars if specified)
	for key, value := range customEnv {
		torrentVars[key] = value
	}

	// Convert to environment slice format
	for key, value := range torrentVars {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	return env
}

// IsHookEnabled returns true if the specified hook is enabled
func (sm *ScriptManager) IsHookEnabled(hookType ScriptHookType) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	config, exists := sm.hooks[hookType]
	return exists && config.Enabled && config.Filename != ""
}

// GetEnabledHooks returns a list of all enabled hook types
func (sm *ScriptManager) GetEnabledHooks() []ScriptHookType {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var enabled []ScriptHookType
	for hookType, config := range sm.hooks {
		if config.Enabled && config.Filename != "" {
			enabled = append(enabled, hookType)
		}
	}
	return enabled
}

// SetDefaultTimeout sets the default timeout for script execution
func (sm *ScriptManager) SetDefaultTimeout(timeout time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.defaultTimeout = timeout
}

// GetDefaultTimeout returns the current default timeout
func (sm *ScriptManager) GetDefaultTimeout() time.Duration {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.defaultTimeout
}
