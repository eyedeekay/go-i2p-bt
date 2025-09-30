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
	"sync"
	"testing"
	"time"
)

func TestTorrentManagerHooksIntegration(t *testing.T) {
	// Create a test torrent manager
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Track executed hooks
	var executedHooks []string
	var hookData []map[string]interface{}
	var mu sync.Mutex

	// Register a hook for torrent added events
	hook := &Hook{
		ID: "test-added-hook",
		Callback: func(ctx *HookContext) error {
			mu.Lock()
			defer mu.Unlock()
			executedHooks = append(executedHooks, string(ctx.Event))
			hookData = append(hookData, ctx.Data)
			return nil
		},
		Events:   []HookEvent{HookEventTorrentAdded},
		Priority: 1,
	}

	err = tm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	// Add a torrent to trigger the hook
	addRequest := TorrentAddRequest{
		Filename: "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678&dn=test",
		Paused:   true,
	}

	torrent, err := tm.AddTorrent(addRequest)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Wait for async hook execution
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(executedHooks) != 1 {
		t.Errorf("Expected 1 hook execution, got %d", len(executedHooks))
	}

	if executedHooks[0] != string(HookEventTorrentAdded) {
		t.Errorf("Expected hook event %s, got %s", HookEventTorrentAdded, executedHooks[0])
	}

	// Verify the torrent was created
	if torrent.ID == 0 {
		t.Error("Expected non-zero torrent ID")
	}

	// Test hook metrics
	metrics := tm.GetHookMetrics()
	if metrics.TotalExecutions == 0 {
		t.Error("Expected hook metrics to show executions")
	}

	// Test listing hooks
	hooks := tm.ListHooks()
	if len(hooks) == 0 {
		t.Error("Expected to find registered hooks")
	}

	// Test unregistering hook
	err = tm.UnregisterHook("test-added-hook")
	if err != nil {
		t.Errorf("Failed to unregister hook: %v", err)
	}

	// Verify hook was removed
	hooks = tm.ListHooks()
	found := false
	for _, hookList := range hooks {
		for _, hookID := range hookList {
			if hookID == "test-added-hook" {
				found = true
				break
			}
		}
	}
	if found {
		t.Error("Hook should have been removed")
	}
}

func TestTorrentManagerMultipleHooks(t *testing.T) {
	// Create a test torrent manager
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Track executed hooks with priority order
	var executionOrder []string
	var mu sync.Mutex

	// Register hooks with different priorities
	hooks := []*Hook{
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
			ID: "global-hook",
			Callback: func(ctx *HookContext) error {
				mu.Lock()
				executionOrder = append(executionOrder, "global-hook")
				mu.Unlock()
				return nil
			},
			Events:   []HookEvent{}, // Global hook
			Priority: 5,
		},
	}

	for _, hook := range hooks {
		err = tm.RegisterHook(hook)
		if err != nil {
			t.Fatalf("Failed to register hook %s: %v", hook.ID, err)
		}
	}

	// Add a torrent to trigger hooks
	addRequest := TorrentAddRequest{
		Filename: "magnet:?xt=urn:btih:abcdef1234567890abcdef1234567890abcdef12&dn=test2",
		Paused:   true,
	}

	_, err = tm.AddTorrent(addRequest)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Wait for async hook execution
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(executionOrder) != 3 {
		t.Errorf("Expected 3 hook executions, got %d", len(executionOrder))
	}

	// Verify priority order: high (10) > global (5) > low (1)
	expected := []string{"high-priority", "global-hook", "low-priority"}
	for i, expected := range expected {
		if i >= len(executionOrder) || executionOrder[i] != expected {
			t.Errorf("Expected execution order %v, got %v", expected, executionOrder)
			break
		}
	}
}

func TestTorrentManagerHookTimeout(t *testing.T) {
	// Create a test torrent manager
	config := createTestTorrentManagerConfig()
	tm, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	// Set a very short timeout
	tm.SetHookTimeout(50 * time.Millisecond)

	// Register a hook that takes longer than the timeout
	hook := &Hook{
		ID: "slow-hook",
		Callback: func(ctx *HookContext) error {
			time.Sleep(200 * time.Millisecond) // Longer than timeout
			return nil
		},
		Events: []HookEvent{HookEventTorrentAdded},
	}

	err = tm.RegisterHook(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	// Add a torrent to trigger the hook
	addRequest := TorrentAddRequest{
		Filename: "magnet:?xt=urn:btih:fedcba0987654321fedcba0987654321fedcba09&dn=test3",
		Paused:   true,
	}

	_, err = tm.AddTorrent(addRequest)
	if err != nil {
		t.Fatalf("Failed to add torrent: %v", err)
	}

	// Wait for timeout and cleanup
	time.Sleep(300 * time.Millisecond)

	// Check that timeout was recorded in metrics
	metrics := tm.GetHookMetrics()
	if metrics.TotalTimeouts == 0 {
		t.Error("Expected hook timeout to be recorded in metrics")
	}
}
