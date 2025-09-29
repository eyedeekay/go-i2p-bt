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
	"testing"
	"time"
)

func TestNewBandwidthScheduler(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	if scheduler == nil {
		t.Fatal("NewBandwidthScheduler returned nil")
	}

	if scheduler.IsEnabled() {
		t.Error("New scheduler should be disabled by default")
	}

	if len(scheduler.GetRules()) != 0 {
		t.Error("New scheduler should have no rules")
	}

	if scheduler.GetCurrentRule() != nil {
		t.Error("New scheduler should have no current rule")
	}
}

func TestBandwidthScheduleRuleValidation(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	tests := []struct {
		name        string
		rule        *BandwidthScheduleRule
		expectError bool
	}{
		{
			name: "valid rule",
			rule: &BandwidthScheduleRule{
				Name:          "test",
				DaysOfWeek:    []int{1, 2, 3, 4, 5},
				StartTime:     "09:00",
				EndTime:       "17:00",
				DownloadLimit: 1024000,
				UploadLimit:   512000,
				Priority:      1,
				Enabled:       true,
			},
			expectError: false,
		},
		{
			name: "empty name",
			rule: &BandwidthScheduleRule{
				Name:          "",
				StartTime:     "09:00",
				EndTime:       "17:00",
				DownloadLimit: 1024000,
				UploadLimit:   512000,
				Priority:      1,
				Enabled:       true,
			},
			expectError: true,
		},
		{
			name: "invalid start time",
			rule: &BandwidthScheduleRule{
				Name:          "test",
				StartTime:     "25:00",
				EndTime:       "17:00",
				DownloadLimit: 1024000,
				UploadLimit:   512000,
				Priority:      1,
				Enabled:       true,
			},
			expectError: true,
		},
		{
			name: "invalid end time",
			rule: &BandwidthScheduleRule{
				Name:          "test",
				StartTime:     "09:00",
				EndTime:       "invalid",
				DownloadLimit: 1024000,
				UploadLimit:   512000,
				Priority:      1,
				Enabled:       true,
			},
			expectError: true,
		},
		{
			name: "invalid day of week",
			rule: &BandwidthScheduleRule{
				Name:          "test",
				DaysOfWeek:    []int{-1},
				StartTime:     "09:00",
				EndTime:       "17:00",
				DownloadLimit: 1024000,
				UploadLimit:   512000,
				Priority:      1,
				Enabled:       true,
			},
			expectError: true,
		},
		{
			name: "negative download limit",
			rule: &BandwidthScheduleRule{
				Name:          "test",
				StartTime:     "09:00",
				EndTime:       "17:00",
				DownloadLimit: -1,
				UploadLimit:   512000,
				Priority:      1,
				Enabled:       true,
			},
			expectError: true,
		},
		{
			name: "negative upload limit",
			rule: &BandwidthScheduleRule{
				Name:          "test",
				StartTime:     "09:00",
				EndTime:       "17:00",
				DownloadLimit: 1024000,
				UploadLimit:   -1,
				Priority:      1,
				Enabled:       true,
			},
			expectError: true,
		},
		{
			name: "overnight rule",
			rule: &BandwidthScheduleRule{
				Name:          "overnight",
				StartTime:     "23:00",
				EndTime:       "06:00",
				DownloadLimit: 0, // unlimited
				UploadLimit:   0, // unlimited
				Priority:      1,
				Enabled:       true,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := scheduler.AddRule(tt.rule)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestAddRemoveUpdateRules(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	rule1 := &BandwidthScheduleRule{
		Name:          "rule1",
		StartTime:     "09:00",
		EndTime:       "17:00",
		DownloadLimit: 1024000,
		UploadLimit:   512000,
		Priority:      1,
		Enabled:       true,
	}

	rule2 := &BandwidthScheduleRule{
		Name:          "rule2",
		StartTime:     "18:00",
		EndTime:       "22:00",
		DownloadLimit: 2048000,
		UploadLimit:   1024000,
		Priority:      2,
		Enabled:       true,
	}

	// Test adding rules
	if err := scheduler.AddRule(rule1); err != nil {
		t.Fatalf("Failed to add rule1: %v", err)
	}

	if err := scheduler.AddRule(rule2); err != nil {
		t.Fatalf("Failed to add rule2: %v", err)
	}

	rules := scheduler.GetRules()
	if len(rules) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(rules))
	}

	// Test duplicate name
	duplicate := &BandwidthScheduleRule{
		Name:          "rule1",
		StartTime:     "10:00",
		EndTime:       "16:00",
		DownloadLimit: 512000,
		UploadLimit:   256000,
		Priority:      3,
		Enabled:       true,
	}

	if err := scheduler.AddRule(duplicate); err == nil {
		t.Error("Expected error for duplicate rule name")
	}

	// Test updating rule
	updatedRule1 := &BandwidthScheduleRule{
		Name:          "rule1",
		StartTime:     "08:00",
		EndTime:       "18:00",
		DownloadLimit: 512000,
		UploadLimit:   256000,
		Priority:      5,
		Enabled:       false,
	}

	if err := scheduler.UpdateRule(updatedRule1); err != nil {
		t.Fatalf("Failed to update rule1: %v", err)
	}

	rules = scheduler.GetRules()
	var foundRule *BandwidthScheduleRule
	for _, rule := range rules {
		if rule.Name == "rule1" {
			foundRule = rule
			break
		}
	}

	if foundRule == nil {
		t.Fatal("Rule1 not found after update")
	}

	if foundRule.StartTime != "08:00" || foundRule.Priority != 5 || foundRule.Enabled {
		t.Error("Rule not updated correctly")
	}

	// Test removing rule
	if err := scheduler.RemoveRule("rule1"); err != nil {
		t.Fatalf("Failed to remove rule1: %v", err)
	}

	rules = scheduler.GetRules()
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule after removal, got %d", len(rules))
	}

	// Test removing non-existent rule
	if err := scheduler.RemoveRule("nonexistent"); err == nil {
		t.Error("Expected error for removing non-existent rule")
	}

	// Test updating non-existent rule
	if err := scheduler.UpdateRule(rule1); err == nil {
		t.Error("Expected error for updating non-existent rule")
	}
}

func TestRuleMatching(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	tests := []struct {
		name     string
		rule     *BandwidthScheduleRule
		testTime time.Time
		matches  bool
	}{
		{
			name: "weekday rule during weekday",
			rule: &BandwidthScheduleRule{
				Name:       "weekday",
				DaysOfWeek: []int{1, 2, 3, 4, 5}, // Monday-Friday
				StartTime:  "09:00",
				EndTime:    "17:00",
				Enabled:    true,
			},
			testTime: time.Date(2025, 1, 13, 10, 30, 0, 0, time.UTC), // Monday 10:30
			matches:  true,
		},
		{
			name: "weekday rule during weekend",
			rule: &BandwidthScheduleRule{
				Name:       "weekday",
				DaysOfWeek: []int{1, 2, 3, 4, 5}, // Monday-Friday
				StartTime:  "09:00",
				EndTime:    "17:00",
				Enabled:    true,
			},
			testTime: time.Date(2025, 1, 11, 10, 30, 0, 0, time.UTC), // Saturday 10:30
			matches:  false,
		},
		{
			name: "time rule before start",
			rule: &BandwidthScheduleRule{
				Name:      "daytime",
				StartTime: "09:00",
				EndTime:   "17:00",
				Enabled:   true,
			},
			testTime: time.Date(2025, 1, 13, 8, 30, 0, 0, time.UTC), // 08:30
			matches:  false,
		},
		{
			name: "time rule after end",
			rule: &BandwidthScheduleRule{
				Name:      "daytime",
				StartTime: "09:00",
				EndTime:   "17:00",
				Enabled:   true,
			},
			testTime: time.Date(2025, 1, 13, 18, 30, 0, 0, time.UTC), // 18:30
			matches:  false,
		},
		{
			name: "overnight rule during night",
			rule: &BandwidthScheduleRule{
				Name:      "overnight",
				StartTime: "23:00",
				EndTime:   "06:00",
				Enabled:   true,
			},
			testTime: time.Date(2025, 1, 13, 2, 30, 0, 0, time.UTC), // 02:30
			matches:  true,
		},
		{
			name: "overnight rule during day",
			rule: &BandwidthScheduleRule{
				Name:      "overnight",
				StartTime: "23:00",
				EndTime:   "06:00",
				Enabled:   true,
			},
			testTime: time.Date(2025, 1, 13, 12, 30, 0, 0, time.UTC), // 12:30
			matches:  false,
		},
		{
			name: "disabled rule",
			rule: &BandwidthScheduleRule{
				Name:      "disabled",
				StartTime: "09:00",
				EndTime:   "17:00",
				Enabled:   false,
			},
			testTime: time.Date(2025, 1, 13, 12, 30, 0, 0, time.UTC), // 12:30
			matches:  true,                                           // ruleMatches only checks time, not enabled flag
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := scheduler.ruleMatches(tt.rule, tt.testTime)
			if matches != tt.matches {
				t.Errorf("Expected %v, got %v", tt.matches, matches)
			}
		})
	}
}

func TestPrioritySelection(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	// Add rules with different priorities
	rule1 := &BandwidthScheduleRule{
		Name:          "low_priority",
		StartTime:     "09:00",
		EndTime:       "17:00",
		DownloadLimit: 1024000,
		UploadLimit:   512000,
		Priority:      1,
		Enabled:       true,
	}

	rule2 := &BandwidthScheduleRule{
		Name:          "high_priority",
		StartTime:     "10:00",
		EndTime:       "16:00",
		DownloadLimit: 2048000,
		UploadLimit:   1024000,
		Priority:      5,
		Enabled:       true,
	}

	rule3 := &BandwidthScheduleRule{
		Name:          "medium_priority",
		StartTime:     "08:00",
		EndTime:       "18:00",
		DownloadLimit: 512000,
		UploadLimit:   256000,
		Priority:      3,
		Enabled:       true,
	}

	scheduler.AddRule(rule1)
	scheduler.AddRule(rule2)
	scheduler.AddRule(rule3)

	// Mock time to be within overlapping rules
	testTime := time.Date(2025, 1, 13, 12, 0, 0, 0, time.UTC) // Monday 12:00

	// Find the best matching rule manually
	var bestRule *BandwidthScheduleRule
	for _, rule := range scheduler.GetRules() {
		if scheduler.ruleMatches(rule, testTime) {
			if bestRule == nil || rule.Priority > bestRule.Priority {
				bestRule = rule
			}
		}
	}

	if bestRule == nil {
		t.Fatal("No matching rule found")
	}

	if bestRule.Name != "high_priority" {
		t.Errorf("Expected high_priority rule to be selected, got %s", bestRule.Name)
	}

	if bestRule.Priority != 5 {
		t.Errorf("Expected priority 5, got %d", bestRule.Priority)
	}
}

func TestSchedulerEnabledDisabled(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	// Add a test rule
	rule := &BandwidthScheduleRule{
		Name:          "test",
		StartTime:     "00:00",
		EndTime:       "23:59",
		DownloadLimit: 1024000,
		UploadLimit:   512000,
		Priority:      1,
		Enabled:       true,
	}

	scheduler.AddRule(rule)

	// Test enabling scheduler
	scheduler.SetEnabled(true)
	if !scheduler.IsEnabled() {
		t.Error("Scheduler should be enabled")
	}

	// Test disabling scheduler
	scheduler.SetEnabled(false)
	if scheduler.IsEnabled() {
		t.Error("Scheduler should be disabled")
	}

	// Test double enable/disable
	scheduler.SetEnabled(true)
	scheduler.SetEnabled(true)
	if !scheduler.IsEnabled() {
		t.Error("Scheduler should remain enabled")
	}

	scheduler.SetEnabled(false)
	scheduler.SetEnabled(false)
	if scheduler.IsEnabled() {
		t.Error("Scheduler should remain disabled")
	}
}

func TestUpdateCallback(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	var callbackCalled bool
	var lastDownLimit, lastUpLimit int64

	scheduler.SetUpdateCallback(func(downloadLimit, uploadLimit int64) {
		callbackCalled = true
		lastDownLimit = downloadLimit
		lastUpLimit = uploadLimit
	})

	// Test applying a rule
	rule := &BandwidthScheduleRule{
		Name:          "test",
		DownloadLimit: 1024000,
		UploadLimit:   512000,
		Priority:      1,
	}

	scheduler.applyRule(rule)

	if !callbackCalled {
		t.Error("Callback should have been called")
	}

	if lastDownLimit != 1024000 {
		t.Errorf("Expected download limit 1024000, got %d", lastDownLimit)
	}

	if lastUpLimit != 512000 {
		t.Errorf("Expected upload limit 512000, got %d", lastUpLimit)
	}

	// Test applying nil rule
	callbackCalled = false
	scheduler.applyRule(nil)

	if !callbackCalled {
		t.Error("Callback should have been called for nil rule")
	}

	if lastDownLimit != 0 || lastUpLimit != 0 {
		t.Error("Expected zero limits for nil rule")
	}
}

func TestGetStats(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	// Test stats with no rules
	stats := scheduler.GetStats()
	if stats["enabled"].(bool) {
		t.Error("Scheduler should be disabled")
	}

	if stats["rules_count"].(int) != 0 {
		t.Error("Rules count should be 0")
	}

	if stats["current_rule"] != nil {
		t.Error("Current rule should be nil")
	}

	// Add a rule and enable scheduler
	rule := &BandwidthScheduleRule{
		Name:          "test",
		StartTime:     "00:00",
		EndTime:       "23:59",
		DownloadLimit: 1024000,
		UploadLimit:   512000,
		Priority:      1,
		Enabled:       true,
	}

	scheduler.AddRule(rule)
	scheduler.SetEnabled(true)

	// Force rule evaluation
	scheduler.evaluateRules()

	stats = scheduler.GetStats()
	if !stats["enabled"].(bool) {
		t.Error("Scheduler should be enabled")
	}

	if stats["rules_count"].(int) != 1 {
		t.Error("Rules count should be 1")
	}

	if stats["current_rule"] == nil {
		t.Error("Current rule should not be nil")
	}
}

func TestConcurrentAccess(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Test concurrent rule additions
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rule := &BandwidthScheduleRule{
				Name:          fmt.Sprintf("rule_%d", id),
				StartTime:     "09:00",
				EndTime:       "17:00",
				DownloadLimit: int64(1024000 + id*1000),
				UploadLimit:   int64(512000 + id*500),
				Priority:      id,
				Enabled:       true,
			}
			scheduler.AddRule(rule)
		}(i)
	}

	wg.Wait()

	rules := scheduler.GetRules()
	if len(rules) != numGoroutines {
		t.Errorf("Expected %d rules, got %d", numGoroutines, len(rules))
	}

	// Test concurrent enable/disable
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			scheduler.SetEnabled(id%2 == 0)
		}(i)
	}

	wg.Wait()

	// Test concurrent stats access
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats := scheduler.GetStats()
			if stats == nil {
				t.Error("Stats should not be nil")
			}
		}()
	}

	wg.Wait()
}

func TestSchedulerStop(t *testing.T) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	rule := &BandwidthScheduleRule{
		Name:          "test",
		StartTime:     "00:00",
		EndTime:       "23:59",
		DownloadLimit: 1024000,
		UploadLimit:   512000,
		Priority:      1,
		Enabled:       true,
	}

	scheduler.AddRule(rule)
	scheduler.SetEnabled(true)

	if !scheduler.IsEnabled() {
		t.Error("Scheduler should be enabled")
	}

	// Stop the scheduler
	scheduler.Stop()

	if scheduler.IsEnabled() {
		t.Error("Scheduler should be disabled after stop")
	}

	// Multiple stops should be safe
	scheduler.Stop()
	scheduler.Stop()
}

func BenchmarkRuleEvaluation(b *testing.B) {
	bandwidthMgr := NewBandwidthManager(SessionConfiguration{})
	scheduler := NewBandwidthScheduler(bandwidthMgr)

	// Add multiple rules
	for i := 0; i < 100; i++ {
		rule := &BandwidthScheduleRule{
			Name:          fmt.Sprintf("rule_%d", i),
			DaysOfWeek:    []int{i % 7},
			StartTime:     fmt.Sprintf("%02d:00", i%24),
			EndTime:       fmt.Sprintf("%02d:30", i%24),
			DownloadLimit: int64(1024000 + i*1000),
			UploadLimit:   int64(512000 + i*500),
			Priority:      i,
			Enabled:       true,
		}
		scheduler.AddRule(rule)
	}

	scheduler.SetEnabled(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scheduler.evaluateRules()
	}
}
