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
	"sync"
	"time"
)

// HookEvent represents the type of torrent lifecycle event
type HookEvent string

const (
	// HookEventTorrentAdded is triggered when a new torrent is added to the manager
	HookEventTorrentAdded HookEvent = "torrent-added"
	// HookEventTorrentStarted is triggered when a torrent starts downloading or seeding
	HookEventTorrentStarted HookEvent = "torrent-started"
	// HookEventTorrentStopped is triggered when a torrent is paused or stopped
	HookEventTorrentStopped HookEvent = "torrent-stopped"
	// HookEventTorrentCompleted is triggered when a torrent finishes downloading
	HookEventTorrentCompleted HookEvent = "torrent-completed"
	// HookEventTorrentRemoved is triggered when a torrent is removed from the manager
	HookEventTorrentRemoved HookEvent = "torrent-removed"
	// HookEventTorrentMetadata is triggered when torrent metadata is received
	HookEventTorrentMetadata HookEvent = "torrent-metadata"
	// HookEventTorrentError is triggered when a torrent encounters an error
	HookEventTorrentError HookEvent = "torrent-error"
)

// HookContext provides context and data for hook execution
type HookContext struct {
	// Event type that triggered the hook
	Event HookEvent
	// Torrent state snapshot at the time of the event
	Torrent *TorrentState
	// Additional event-specific data
	Data map[string]interface{}
	// Context for cancellation and timeouts
	Context context.Context
	// Timestamp when the event occurred
	Timestamp time.Time
}

// HookCallback is a function that handles torrent lifecycle events
// It receives a HookContext with event details and should return an error if processing fails
type HookCallback func(ctx *HookContext) error

// Hook represents a registered callback with metadata
type Hook struct {
	// Unique identifier for the hook
	ID string
	// Callback function to execute
	Callback HookCallback
	// Events this hook should respond to
	Events []HookEvent
	// Priority for execution order (higher priority executes first)
	Priority int
	// Timeout for hook execution (0 means no timeout)
	Timeout time.Duration
	// Whether to continue execution if this hook fails
	ContinueOnError bool
}

// HookManager manages lifecycle event hooks for torrent operations
// It provides a thread-safe way to register, unregister, and execute hooks
type HookManager struct {
	mu sync.RWMutex
	// Registered hooks by event type
	hooks map[HookEvent][]*Hook
	// Global hooks that respond to all events
	globalHooks []*Hook
	// Default timeout for hook execution
	defaultTimeout time.Duration
	// Logger for hook execution events
	logger func(format string, args ...interface{})
	// Metrics for hook performance
	metrics HookMetrics
}

// HookMetrics tracks hook execution statistics
type HookMetrics struct {
	TotalExecutions int64
	TotalErrors     int64
	TotalTimeouts   int64
	AverageLatency  time.Duration
}

// NewHookManager creates a new hook manager with default configuration
func NewHookManager() *HookManager {
	return &HookManager{
		hooks:          make(map[HookEvent][]*Hook),
		globalHooks:    make([]*Hook, 0),
		defaultTimeout: 10 * time.Second,                            // Default 10-second timeout
		logger:         func(format string, args ...interface{}) {}, // No-op logger by default
	}
}

// SetLogger sets a custom logger for hook execution events
func (hm *HookManager) SetLogger(logger func(format string, args ...interface{})) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.logger = logger
}

// SetDefaultTimeout sets the default timeout for hook execution
func (hm *HookManager) SetDefaultTimeout(timeout time.Duration) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.defaultTimeout = timeout
}

// RegisterHook registers a new hook for torrent lifecycle events
// Returns the hook ID for later reference
func (hm *HookManager) RegisterHook(hook *Hook) error {
	if hook == nil {
		return fmt.Errorf("hook cannot be nil")
	}
	if hook.ID == "" {
		return fmt.Errorf("hook ID cannot be empty")
	}
	if hook.Callback == nil {
		return fmt.Errorf("hook callback cannot be nil")
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Set default timeout if not specified
	if hook.Timeout == 0 {
		hook.Timeout = hm.defaultTimeout
	}

	// Register for specific events or as global hook
	if len(hook.Events) == 0 {
		// Global hook - responds to all events
		hm.globalHooks = append(hm.globalHooks, hook)
		hm.sortHooksByPriority(hm.globalHooks)
	} else {
		// Event-specific hooks
		for _, event := range hook.Events {
			if hm.hooks[event] == nil {
				hm.hooks[event] = make([]*Hook, 0)
			}

			// Check for duplicate hook IDs
			for _, existingHook := range hm.hooks[event] {
				if existingHook.ID == hook.ID {
					return fmt.Errorf("hook with ID '%s' already registered for event '%s'", hook.ID, event)
				}
			}

			hm.hooks[event] = append(hm.hooks[event], hook)
			hm.sortHooksByPriority(hm.hooks[event])
		}
	}

	hm.logger("Registered hook '%s' for events: %v", hook.ID, hook.Events)
	return nil
}

// UnregisterHook removes a hook by ID
func (hm *HookManager) UnregisterHook(hookID string) error {
	if hookID == "" {
		return fmt.Errorf("hook ID cannot be empty")
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	removed := false

	// Remove from global hooks
	for i, hook := range hm.globalHooks {
		if hook.ID == hookID {
			hm.globalHooks = append(hm.globalHooks[:i], hm.globalHooks[i+1:]...)
			removed = true
			break
		}
	}

	// Remove from event-specific hooks
	for event, hooks := range hm.hooks {
		for i, hook := range hooks {
			if hook.ID == hookID {
				hm.hooks[event] = append(hooks[:i], hooks[i+1:]...)
				removed = true
				break
			}
		}
	}

	if !removed {
		return fmt.Errorf("hook with ID '%s' not found", hookID)
	}

	hm.logger("Unregistered hook '%s'", hookID)
	return nil
}

// ExecuteHooks executes all registered hooks for a specific event
func (hm *HookManager) ExecuteHooks(event HookEvent, torrent *TorrentState, data map[string]interface{}) {
	ctx := &HookContext{
		Event:     event,
		Torrent:   torrent,
		Data:      data,
		Context:   context.Background(),
		Timestamp: time.Now(),
	}

	hm.executeHooksWithContext(ctx)
}

// ExecuteHooksWithContext executes hooks with a custom context for cancellation
func (hm *HookManager) ExecuteHooksWithContext(ctx context.Context, event HookEvent, torrent *TorrentState, data map[string]interface{}) {
	hookCtx := &HookContext{
		Event:     event,
		Torrent:   torrent,
		Data:      data,
		Context:   ctx,
		Timestamp: time.Now(),
	}

	hm.executeHooksWithContext(hookCtx)
}

// executeHooksWithContext is the internal implementation for hook execution
func (hm *HookManager) executeHooksWithContext(hookCtx *HookContext) {
	hm.mu.RLock()

	// Collect all hooks to execute
	var hooksToExecute []*Hook

	// Add global hooks
	hooksToExecute = append(hooksToExecute, hm.globalHooks...)

	// Add event-specific hooks
	if eventHooks, exists := hm.hooks[hookCtx.Event]; exists {
		hooksToExecute = append(hooksToExecute, eventHooks...)
	}

	// Sort all hooks by priority to ensure correct execution order
	hm.sortHooksByPriority(hooksToExecute)

	hm.mu.RUnlock()

	if len(hooksToExecute) == 0 {
		return
	}

	// Execute hooks in separate goroutines to avoid blocking
	go hm.executeHooksAsync(hookCtx, hooksToExecute)
}

// executeHooksAsync executes hooks asynchronously with proper error handling and metrics
func (hm *HookManager) executeHooksAsync(hookCtx *HookContext, hooks []*Hook) {
	start := time.Now()
	defer func() {
		hm.mu.Lock()
		hm.metrics.TotalExecutions++
		hm.metrics.AverageLatency = (hm.metrics.AverageLatency + time.Since(start)) / 2
		hm.mu.Unlock()
	}()

	for _, hook := range hooks {
		func(h *Hook) {
			defer func() {
				if r := recover(); r != nil {
					hm.mu.Lock()
					hm.metrics.TotalErrors++
					hm.mu.Unlock()
					hm.logger("Hook '%s' panicked: %v", h.ID, r)
				}
			}()

			// Create context with timeout
			ctx, cancel := context.WithTimeout(hookCtx.Context, h.Timeout)
			defer cancel()

			// Update context in hook context
			hookCtxCopy := *hookCtx
			hookCtxCopy.Context = ctx

			// Execute hook with timeout
			done := make(chan error, 1)
			go func() {
				done <- h.Callback(&hookCtxCopy)
			}()

			select {
			case err := <-done:
				if err != nil {
					hm.mu.Lock()
					hm.metrics.TotalErrors++
					hm.mu.Unlock()
					hm.logger("Hook '%s' failed for event '%s': %v", h.ID, hookCtx.Event, err)
					if !h.ContinueOnError {
						return
					}
				}
			case <-ctx.Done():
				hm.mu.Lock()
				hm.metrics.TotalTimeouts++
				hm.mu.Unlock()
				hm.logger("Hook '%s' timed out for event '%s'", h.ID, hookCtx.Event)
			}
		}(hook)
	}
}

// GetMetrics returns current hook execution metrics
func (hm *HookManager) GetMetrics() HookMetrics {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.metrics
}

// ListHooks returns information about all registered hooks
func (hm *HookManager) ListHooks() map[string][]string {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	result := make(map[string][]string)

	// Add global hooks
	if len(hm.globalHooks) > 0 {
		var globalHookIDs []string
		for _, hook := range hm.globalHooks {
			globalHookIDs = append(globalHookIDs, hook.ID)
		}
		result["global"] = globalHookIDs
	}

	// Add event-specific hooks
	for event, hooks := range hm.hooks {
		if len(hooks) > 0 {
			var hookIDs []string
			for _, hook := range hooks {
				hookIDs = append(hookIDs, hook.ID)
			}
			result[string(event)] = hookIDs
		}
	}

	return result
}

// sortHooksByPriority sorts hooks by priority (highest first)
func (hm *HookManager) sortHooksByPriority(hooks []*Hook) {
	for i := 0; i < len(hooks)-1; i++ {
		for j := i + 1; j < len(hooks); j++ {
			if hooks[i].Priority < hooks[j].Priority {
				hooks[i], hooks[j] = hooks[j], hooks[i]
			}
		}
	}
}
