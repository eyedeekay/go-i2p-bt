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
	"sync"
	"time"
)

// BandwidthScheduleRule represents a single bandwidth scheduling rule
type BandwidthScheduleRule struct {
	// Name is a human-readable identifier for this rule
	Name string `json:"name"`

	// DaysOfWeek specifies which days this rule applies (0=Sunday, 1=Monday, etc.)
	// Empty slice means all days
	DaysOfWeek []int `json:"days_of_week"`

	// StartTime is when this rule becomes active (format: "HH:MM")
	StartTime string `json:"start_time"`

	// EndTime is when this rule becomes inactive (format: "HH:MM")
	EndTime string `json:"end_time"`

	// DownloadLimit is the download speed limit in bytes per second (0 = unlimited)
	DownloadLimit int64 `json:"download_limit"`

	// UploadLimit is the upload speed limit in bytes per second (0 = unlimited)
	UploadLimit int64 `json:"upload_limit"`

	// Priority determines rule precedence when multiple rules match (higher = more important)
	Priority int `json:"priority"`

	// Enabled indicates whether this rule is active
	Enabled bool `json:"enabled"`
}

// BandwidthScheduler manages time-based bandwidth scheduling
type BandwidthScheduler struct {
	mu             sync.RWMutex
	enabled        bool
	rules          []*BandwidthScheduleRule
	currentRule    *BandwidthScheduleRule
	bandwidthMgr   *BandwidthManager
	ticker         *time.Ticker
	stopChan       chan struct{}
	updateCallback func(downloadLimit, uploadLimit int64)
}

// NewBandwidthScheduler creates a new bandwidth scheduler
func NewBandwidthScheduler(bandwidthMgr *BandwidthManager) *BandwidthScheduler {
	return &BandwidthScheduler{
		enabled:      false,
		rules:        make([]*BandwidthScheduleRule, 0),
		bandwidthMgr: bandwidthMgr,
		stopChan:     make(chan struct{}),
	}
}

// IsEnabled returns whether bandwidth scheduling is enabled
func (bs *BandwidthScheduler) IsEnabled() bool {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.enabled
}

// SetEnabled enables or disables bandwidth scheduling
func (bs *BandwidthScheduler) SetEnabled(enabled bool) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.enabled == enabled {
		return
	}

	bs.enabled = enabled

	if enabled {
		bs.startScheduler()
	} else {
		bs.stopScheduler()
	}
}

// SetUpdateCallback sets the callback function for bandwidth limit updates
func (bs *BandwidthScheduler) SetUpdateCallback(callback func(downloadLimit, uploadLimit int64)) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.updateCallback = callback
}

// AddRule adds a new scheduling rule
func (bs *BandwidthScheduler) AddRule(rule *BandwidthScheduleRule) error {
	if err := bs.validateRule(rule); err != nil {
		return fmt.Errorf("invalid rule: %w", err)
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Check for duplicate names
	for _, existingRule := range bs.rules {
		if existingRule.Name == rule.Name {
			return fmt.Errorf("rule with name '%s' already exists", rule.Name)
		}
	}

	bs.rules = append(bs.rules, rule)

	// If scheduler is running, trigger immediate evaluation
	if bs.enabled {
		go bs.evaluateRules()
	}

	return nil
}

// RemoveRule removes a scheduling rule by name
func (bs *BandwidthScheduler) RemoveRule(name string) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	for i, rule := range bs.rules {
		if rule.Name == name {
			// Remove rule by swapping with last element and truncating
			bs.rules[i] = bs.rules[len(bs.rules)-1]
			bs.rules = bs.rules[:len(bs.rules)-1]

			// If scheduler is running, trigger immediate evaluation
			if bs.enabled {
				go bs.evaluateRules()
			}

			return nil
		}
	}

	return fmt.Errorf("rule with name '%s' not found", name)
}

// UpdateRule updates an existing scheduling rule
func (bs *BandwidthScheduler) UpdateRule(rule *BandwidthScheduleRule) error {
	if err := bs.validateRule(rule); err != nil {
		return fmt.Errorf("invalid rule: %w", err)
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()

	for i, existingRule := range bs.rules {
		if existingRule.Name == rule.Name {
			bs.rules[i] = rule

			// If scheduler is running, trigger immediate evaluation
			if bs.enabled {
				go bs.evaluateRules()
			}

			return nil
		}
	}

	return fmt.Errorf("rule with name '%s' not found", rule.Name)
}

// GetRules returns a copy of all scheduling rules
func (bs *BandwidthScheduler) GetRules() []*BandwidthScheduleRule {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	rules := make([]*BandwidthScheduleRule, len(bs.rules))
	for i, rule := range bs.rules {
		// Create a copy to prevent external modification
		ruleCopy := *rule
		rules[i] = &ruleCopy
	}

	return rules
}

// GetCurrentRule returns the currently active rule (if any)
func (bs *BandwidthScheduler) GetCurrentRule() *BandwidthScheduleRule {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	if bs.currentRule == nil {
		return nil
	}

	// Return a copy to prevent external modification
	ruleCopy := *bs.currentRule
	return &ruleCopy
}

// GetStats returns scheduler statistics
func (bs *BandwidthScheduler) GetStats() map[string]interface{} {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	stats := map[string]interface{}{
		"enabled":      bs.enabled,
		"rules_count":  len(bs.rules),
		"current_rule": nil,
	}

	if bs.currentRule != nil {
		stats["current_rule"] = map[string]interface{}{
			"name":           bs.currentRule.Name,
			"download_limit": bs.currentRule.DownloadLimit,
			"upload_limit":   bs.currentRule.UploadLimit,
			"priority":       bs.currentRule.Priority,
		}
	}

	return stats
}

// validateRule validates a scheduling rule
func (bs *BandwidthScheduler) validateRule(rule *BandwidthScheduleRule) error {
	if rule.Name == "" {
		return fmt.Errorf("rule name cannot be empty")
	}

	if err := bs.validateTimeFormat(rule.StartTime); err != nil {
		return fmt.Errorf("invalid start time: %w", err)
	}

	if err := bs.validateTimeFormat(rule.EndTime); err != nil {
		return fmt.Errorf("invalid end time: %w", err)
	}

	for _, day := range rule.DaysOfWeek {
		if day < 0 || day > 6 {
			return fmt.Errorf("invalid day of week: %d (must be 0-6)", day)
		}
	}

	if rule.DownloadLimit < 0 {
		return fmt.Errorf("download limit cannot be negative")
	}

	if rule.UploadLimit < 0 {
		return fmt.Errorf("upload limit cannot be negative")
	}

	return nil
}

// validateTimeFormat validates time format (HH:MM)
func (bs *BandwidthScheduler) validateTimeFormat(timeStr string) error {
	_, err := time.Parse("15:04", timeStr)
	return err
}

// startScheduler starts the background scheduler
func (bs *BandwidthScheduler) startScheduler() {
	if bs.ticker != nil {
		bs.stopScheduler()
	}

	// Check every minute for rule changes
	bs.ticker = time.NewTicker(1 * time.Minute)
	ticker := bs.ticker // Create local reference to avoid race condition

	go func() {
		defer func() {
			if ticker != nil {
				ticker.Stop()
			}
		}()

		// Immediate evaluation
		bs.evaluateRules()

		for {
			select {
			case <-ticker.C:
				bs.evaluateRules()
			case <-bs.stopChan:
				return
			}
		}
	}()
} // stopScheduler stops the background scheduler
func (bs *BandwidthScheduler) stopScheduler() {
	if bs.ticker != nil {
		bs.ticker.Stop()
		bs.ticker = nil
	}

	// Send stop signal - non-blocking
	select {
	case bs.stopChan <- struct{}{}:
	default:
	}
}

// evaluateRules evaluates all rules and applies the highest priority matching rule
func (bs *BandwidthScheduler) evaluateRules() {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if !bs.enabled {
		return
	}

	now := time.Now()
	var bestRule *BandwidthScheduleRule

	for _, rule := range bs.rules {
		if !rule.Enabled {
			continue
		}

		if bs.ruleMatches(rule, now) {
			if bestRule == nil || rule.Priority > bestRule.Priority {
				bestRule = rule
			}
		}
	}

	// Apply the best matching rule
	if bestRule != bs.currentRule {
		bs.currentRule = bestRule
		bs.applyRule(bestRule)
	}
}

// ruleMatches checks if a rule matches the current time
func (bs *BandwidthScheduler) ruleMatches(rule *BandwidthScheduleRule, now time.Time) bool {
	// Check day of week
	if len(rule.DaysOfWeek) > 0 {
		currentDay := int(now.Weekday())
		dayMatches := false

		for _, day := range rule.DaysOfWeek {
			if day == currentDay {
				dayMatches = true
				break
			}
		}

		if !dayMatches {
			return false
		}
	}

	// Check time range
	currentTime := now.Format("15:04")

	// Handle overnight rules (e.g., 23:00 - 06:00)
	if rule.StartTime > rule.EndTime {
		return currentTime >= rule.StartTime || currentTime < rule.EndTime
	}

	// Normal time range
	return currentTime >= rule.StartTime && currentTime < rule.EndTime
}

// applyRule applies bandwidth limits from a rule
func (bs *BandwidthScheduler) applyRule(rule *BandwidthScheduleRule) {
	if rule == nil {
		// No rule active - use default limits
		if bs.updateCallback != nil {
			bs.updateCallback(0, 0) // 0 means use session defaults
		}
		return
	}

	// Apply rule limits
	if bs.updateCallback != nil {
		bs.updateCallback(rule.DownloadLimit, rule.UploadLimit)
	}

	// Also update bandwidth manager if available
	if bs.bandwidthMgr != nil {
		config := SessionConfiguration{
			SpeedLimitDown:        rule.DownloadLimit,
			SpeedLimitDownEnabled: rule.DownloadLimit > 0,
			SpeedLimitUp:          rule.UploadLimit,
			SpeedLimitUpEnabled:   rule.UploadLimit > 0,
		}
		bs.bandwidthMgr.UpdateConfiguration(config)
	}
}

// Stop stops the bandwidth scheduler
func (bs *BandwidthScheduler) Stop() {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.enabled = false
	bs.stopScheduler()
}
