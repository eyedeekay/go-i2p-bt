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
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewHookManager(t *testing.T) {
	hm := NewHookManager()

	if hm == nil {
		t.Fatal("NewHookManager returned nil")
	}

	if hm.hooks == nil {
		t.Error("hooks map not initialized")
	}

	if hm.globalHooks == nil {
		t.Error("globalHooks slice not initialized")
	}

	if hm.defaultTimeout != 10*time.Second {
		t.Errorf("Expected default timeout 10s, got %v", hm.defaultTimeout)
	}
}

func TestHookManager_SetLogger(t *testing.T) {
	hm := NewHookManager()

	called := false
	logger := func(format string, args ...interface{}) {
		called = true
	}

	hm.SetLogger(logger)
	hm.logger("test")

	if !called {
		t.Error("Logger was not set properly")
	}
}

func TestHookManager_SetDefaultTimeout(t *testing.T) {
	hm := NewHookManager()
	timeout := 5 * time.Second

	hm.SetDefaultTimeout(timeout)

	if hm.defaultTimeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, hm.defaultTimeout)
	}
}

func TestHookManager_RegisterHook(t *testing.T) {
	hm := NewHookManager()

	tests := []struct {
		name    string
		hook    *Hook
		wantErr bool
	}{
		{
			name:    "nil hook",
			hook:    nil,
			wantErr: true,
		},
		{
			name: "empty ID",
			hook: &Hook{
				ID:       "",
				Callback: func(ctx *HookContext) error { return nil },
			},
			wantErr: true,
		},
		{
			name: "nil callback",
			hook: &Hook{
				ID:       "test",
				Callback: nil,
			},
			wantErr: true,
		},
		{
			name: "valid hook with events",
			hook: &Hook{
				ID:       "test-hook",
				Callback: func(ctx *HookContext) error { return nil },
				Events:   []HookEvent{HookEventTorrentAdded},
				Priority: 1,
			},
			wantErr: false,
		},
		{
			name: "valid global hook",
			hook: &Hook{
				ID:       "global-hook",
				Callback: func(ctx *HookContext) error { return nil },
				Events:   []HookEvent{}, // Empty events makes it global
				Priority: 2,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := hm.RegisterHook(tt.hook)
			if (err != nil) != tt.wantErr {
				t.Errorf("RegisterHook() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHookManager_RegisterDuplicateHook(t *testing.T) {
	hm := NewHookManager()

	hook := &Hook{
		ID:       "duplicate-test",
		Callback: func(ctx *HookContext) error { return nil },
		Events:   []HookEvent{HookEventTorrentAdded},
	}

	// Register once - should succeed
	err := hm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("First registration failed: %v", err)
	}

	// Register again - should fail
	err = hm.RegisterHook(hook)
	if err == nil {
		t.Error("Expected error for duplicate hook registration")
	}
}

func TestHookManager_UnregisterHook(t *testing.T) {
	hm := NewHookManager()

	// Test unregistering non-existent hook
	err := hm.UnregisterHook("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent hook")
	}

	// Test empty ID
	err = hm.UnregisterHook("")
	if err == nil {
		t.Error("Expected error for empty hook ID")
	}

	// Register and unregister event-specific hook
	hook1 := &Hook{
		ID:       "test-hook-1",
		Callback: func(ctx *HookContext) error { return nil },
		Events:   []HookEvent{HookEventTorrentAdded},
	}

	err = hm.RegisterHook(hook1)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	err = hm.UnregisterHook("test-hook-1")
	if err != nil {
		t.Errorf("Failed to unregister hook: %v", err)
	}

	// Register and unregister global hook
	hook2 := &Hook{
		ID:       "global-hook-1",
		Callback: func(ctx *HookContext) error { return nil },
		Events:   []HookEvent{}, // Global hook
	}

	err = hm.RegisterHook(hook2)
	if err != nil {
		t.Fatalf("Failed to register global hook: %v", err)
	}

	err = hm.UnregisterHook("global-hook-1")
	if err != nil {
		t.Errorf("Failed to unregister global hook: %v", err)
	}
}

func TestHookManager_ExecuteHooks(t *testing.T) {
	hm := NewHookManager()

	// Track hook execution
	var executedHooks []string
	var mu sync.Mutex

	// Register event-specific hook
	hook1 := &Hook{
		ID: "event-hook",
		Callback: func(ctx *HookContext) error {
			mu.Lock()
			executedHooks = append(executedHooks, "event-hook")
			mu.Unlock()
			return nil
		},
		Events:   []HookEvent{HookEventTorrentAdded},
		Priority: 1,
	}

	// Register global hook
	hook2 := &Hook{
		ID: "global-hook",
		Callback: func(ctx *HookContext) error {
			mu.Lock()
			executedHooks = append(executedHooks, "global-hook")
			mu.Unlock()
			return nil
		},
		Events:   []HookEvent{}, // Global
		Priority: 2,
	}

	err := hm.RegisterHook(hook1)
	if err != nil {
		t.Fatalf("Failed to register hook1: %v", err)
	}

	err = hm.RegisterHook(hook2)
	if err != nil {
		t.Fatalf("Failed to register hook2: %v", err)
	}

	// Create test torrent
	torrent := &TorrentState{
		ID: 1,
	}

	// Execute hooks
	hm.ExecuteHooks(HookEventTorrentAdded, torrent, nil)

	// Wait for async execution
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(executedHooks) != 2 {
		t.Errorf("Expected 2 hooks executed, got %d", len(executedHooks))
	}

	// Check priority order (global hook has priority 2, should execute first)
	if executedHooks[0] != "global-hook" {
		t.Errorf("Expected global-hook to execute first, got %s", executedHooks[0])
	}
}

func TestHookManager_ExecuteHooksWithError(t *testing.T) {
	hm := NewHookManager()

	// Set up logger to capture errors
	var logMessages []string
	var logMu sync.Mutex
	hm.SetLogger(func(format string, args ...interface{}) {
		logMu.Lock()
		logMessages = append(logMessages, format)
		logMu.Unlock()
	})

	// Register hook that returns error
	hook := &Hook{
		ID: "error-hook",
		Callback: func(ctx *HookContext) error {
			return errors.New("test error")
		},
		Events:          []HookEvent{HookEventTorrentAdded},
		ContinueOnError: true,
	}

	err := hm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	torrent := &TorrentState{ID: 1}
	hm.ExecuteHooks(HookEventTorrentAdded, torrent, nil)

	// Wait for async execution
	time.Sleep(100 * time.Millisecond)

	logMu.Lock()
	defer logMu.Unlock()

	if len(logMessages) == 0 {
		t.Error("Expected error to be logged")
	}
}

func TestHookManager_ExecuteHooksWithTimeout(t *testing.T) {
	hm := NewHookManager()

	// Set up logger to capture timeouts
	var logMessages []string
	var logMu sync.Mutex
	hm.SetLogger(func(format string, args ...interface{}) {
		logMu.Lock()
		logMessages = append(logMessages, format)
		logMu.Unlock()
	})

	// Register hook that takes longer than timeout
	hook := &Hook{
		ID: "timeout-hook",
		Callback: func(ctx *HookContext) error {
			time.Sleep(200 * time.Millisecond) // Longer than timeout
			return nil
		},
		Events:  []HookEvent{HookEventTorrentAdded},
		Timeout: 50 * time.Millisecond, // Short timeout
	}

	err := hm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	torrent := &TorrentState{ID: 1}
	hm.ExecuteHooks(HookEventTorrentAdded, torrent, nil)

	// Wait for timeout and cleanup
	time.Sleep(300 * time.Millisecond)

	logMu.Lock()
	defer logMu.Unlock()

	if len(logMessages) == 0 {
		t.Error("Expected timeout to be logged")
	}
}

func TestHookManager_ExecuteHooksWithPanic(t *testing.T) {
	hm := NewHookManager()

	// Set up logger to capture panics
	var logMessages []string
	var logMu sync.Mutex
	hm.SetLogger(func(format string, args ...interface{}) {
		logMu.Lock()
		logMessages = append(logMessages, format)
		logMu.Unlock()
	})

	// Register hook that panics
	hook := &Hook{
		ID: "panic-hook",
		Callback: func(ctx *HookContext) error {
			panic("test panic")
		},
		Events: []HookEvent{HookEventTorrentAdded},
	}

	err := hm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	torrent := &TorrentState{ID: 1}
	hm.ExecuteHooks(HookEventTorrentAdded, torrent, nil)

	// Wait for async execution
	time.Sleep(100 * time.Millisecond)

	logMu.Lock()
	defer logMu.Unlock()

	if len(logMessages) == 0 {
		t.Error("Expected panic to be logged")
	}
}

func TestHookManager_ExecuteHooksWithContext(t *testing.T) {
	hm := NewHookManager()

	// Use channel for proper synchronization between goroutines
	contextReceived := make(chan context.Context, 1)
	hook := &Hook{
		ID: "context-hook",
		Callback: func(ctx *HookContext) error {
			// Send the received context through channel instead of shared variable
			contextReceived <- ctx.Context
			return nil
		},
		Events: []HookEvent{HookEventTorrentAdded},
	}

	err := hm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	torrent := &TorrentState{ID: 1}
	hm.ExecuteHooksWithContext(ctx, HookEventTorrentAdded, torrent, nil)

	// Wait for hook execution with proper timeout and synchronization
	select {
	case receivedContext := <-contextReceived:
		if receivedContext == nil {
			t.Error("Hook received nil context")
		}
	case <-time.After(1 * time.Second):
		t.Error("Hook execution timed out - did not receive context")
	}
}

func TestHookManager_GetMetrics(t *testing.T) {
	hm := NewHookManager()

	metrics := hm.GetMetrics()
	if metrics.TotalExecutions != 0 {
		t.Errorf("Expected 0 total executions, got %d", metrics.TotalExecutions)
	}

	// Register and execute a hook to update metrics
	hook := &Hook{
		ID: "metrics-hook",
		Callback: func(ctx *HookContext) error {
			return nil
		},
		Events: []HookEvent{HookEventTorrentAdded},
	}

	err := hm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	torrent := &TorrentState{ID: 1}
	hm.ExecuteHooks(HookEventTorrentAdded, torrent, nil)

	// Wait for async execution
	time.Sleep(100 * time.Millisecond)

	metrics = hm.GetMetrics()
	if metrics.TotalExecutions == 0 {
		t.Error("Expected metrics to be updated after hook execution")
	}
}

func TestHookManager_ListHooks(t *testing.T) {
	hm := NewHookManager()

	// Initially should be empty
	hooks := hm.ListHooks()
	if len(hooks) != 0 {
		t.Errorf("Expected empty hooks list, got %v", hooks)
	}

	// Register event-specific hook
	hook1 := &Hook{
		ID:       "event-hook",
		Callback: func(ctx *HookContext) error { return nil },
		Events:   []HookEvent{HookEventTorrentAdded},
	}

	// Register global hook
	hook2 := &Hook{
		ID:       "global-hook",
		Callback: func(ctx *HookContext) error { return nil },
		Events:   []HookEvent{}, // Global
	}

	err := hm.RegisterHook(hook1)
	if err != nil {
		t.Fatalf("Failed to register hook1: %v", err)
	}

	err = hm.RegisterHook(hook2)
	if err != nil {
		t.Fatalf("Failed to register hook2: %v", err)
	}

	hooks = hm.ListHooks()

	if len(hooks) != 2 {
		t.Errorf("Expected 2 hook groups, got %d", len(hooks))
	}

	if _, exists := hooks["global"]; !exists {
		t.Error("Expected global hooks group")
	}

	if _, exists := hooks[string(HookEventTorrentAdded)]; !exists {
		t.Error("Expected torrent-added hooks group")
	}
}

func TestHookPriorityOrdering(t *testing.T) {
	hm := NewHookManager()

	var executionOrder []string
	var mu sync.Mutex

	// Register hooks with different priorities
	hooks := []*Hook{
		{
			ID: "low-priority",
			Callback: func(ctx *HookContext) error {
				mu.Lock()
				executionOrder = append(executionOrder, "low-priority")
				mu.Unlock()
				return nil
			},
			Events:   []HookEvent{HookEventTorrentAdded},
			Priority: 1,
		},
		{
			ID: "high-priority",
			Callback: func(ctx *HookContext) error {
				mu.Lock()
				executionOrder = append(executionOrder, "high-priority")
				mu.Unlock()
				return nil
			},
			Events:   []HookEvent{HookEventTorrentAdded},
			Priority: 10,
		},
		{
			ID: "medium-priority",
			Callback: func(ctx *HookContext) error {
				mu.Lock()
				executionOrder = append(executionOrder, "medium-priority")
				mu.Unlock()
				return nil
			},
			Events:   []HookEvent{HookEventTorrentAdded},
			Priority: 5,
		},
	}

	for _, hook := range hooks {
		err := hm.RegisterHook(hook)
		if err != nil {
			t.Fatalf("Failed to register hook %s: %v", hook.ID, err)
		}
	}

	torrent := &TorrentState{ID: 1}
	hm.ExecuteHooks(HookEventTorrentAdded, torrent, nil)

	// Wait for async execution
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(executionOrder) != 3 {
		t.Errorf("Expected 3 hooks executed, got %d", len(executionOrder))
	}

	// Should execute in priority order: high (10), medium (5), low (1)
	expected := []string{"high-priority", "medium-priority", "low-priority"}
	for i, expected := range expected {
		if i >= len(executionOrder) || executionOrder[i] != expected {
			t.Errorf("Expected execution order %v, got %v", expected, executionOrder)
			break
		}
	}
}

func TestHookContext(t *testing.T) {
	hm := NewHookManager()

	// Use channel for proper synchronization between goroutines
	contextReceived := make(chan *HookContext, 1)
	hook := &Hook{
		ID: "context-test",
		Callback: func(ctx *HookContext) error {
			// Send the received context through channel instead of shared variable
			contextReceived <- ctx
			return nil
		},
		Events: []HookEvent{HookEventTorrentAdded},
	}

	err := hm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	torrent := &TorrentState{
		ID: 123,
	}

	data := map[string]interface{}{
		"test-key": "test-value",
	}

	hm.ExecuteHooks(HookEventTorrentAdded, torrent, data)

	// Wait for hook execution with proper timeout and synchronization
	select {
	case receivedContext := <-contextReceived:
		if receivedContext == nil {
			t.Fatal("Hook received nil context")
		}

		if receivedContext.Event != HookEventTorrentAdded {
			t.Errorf("Expected event %s, got %s", HookEventTorrentAdded, receivedContext.Event)
		}

		if receivedContext.Torrent.ID != 123 {
			t.Errorf("Expected torrent ID 123, got %d", receivedContext.Torrent.ID)
		}

		if receivedContext.Data["test-key"] != "test-value" {
			t.Errorf("Expected test-value, got %v", receivedContext.Data["test-key"])
		}

		if receivedContext.Timestamp.IsZero() {
			t.Error("Expected non-zero timestamp")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Hook execution timed out - did not receive context")
	}
}

// Benchmark hook execution performance
func BenchmarkHookExecution(b *testing.B) {
	hm := NewHookManager()

	hook := &Hook{
		ID: "benchmark-hook",
		Callback: func(ctx *HookContext) error {
			// Simulate lightweight processing
			_ = ctx.Torrent.ID + 1
			return nil
		},
		Events: []HookEvent{HookEventTorrentAdded},
	}

	err := hm.RegisterHook(hook)
	if err != nil {
		b.Fatalf("Failed to register hook: %v", err)
	}

	torrent := &TorrentState{ID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hm.ExecuteHooks(HookEventTorrentAdded, torrent, nil)
	}
}
