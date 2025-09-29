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
)

func TestBandwidthScheduleRPCMethods(t *testing.T) {
	// Create test torrent manager with bandwidth scheduler
	config := TorrentManagerConfig{
		SessionConfig: SessionConfiguration{
			DownloadDir:     "/tmp/test-downloads",
			PeerPort:        51413,
			PeerLimitGlobal: 200,
		},
		ErrorLog: func(format string, args ...interface{}) {
			t.Logf(format, args...)
		},
	}

	manager, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create torrent manager: %v", err)
	}
	defer manager.Close()

	methods := NewRPCMethods(manager)

	// Test BandwidthScheduleGet with empty scheduler
	getReq := BandwidthScheduleGetRequest{}
	getResp, err := methods.BandwidthScheduleGet(getReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleGet failed: %v", err)
	}

	if getResp.Enabled {
		t.Error("Scheduler should be disabled by default")
	}

	if len(getResp.Rules) != 0 {
		t.Error("Should have no rules initially")
	}

	if getResp.Current != nil {
		t.Error("Should have no current rule initially")
	}

	// Test enabling the scheduler
	enabled := true
	setReq := BandwidthScheduleSetRequest{
		Enabled: &enabled,
	}

	err = methods.BandwidthScheduleSet(setReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleSet failed: %v", err)
	}

	// Verify scheduler is now enabled
	getResp, err = methods.BandwidthScheduleGet(getReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleGet failed: %v", err)
	}

	if !getResp.Enabled {
		t.Error("Scheduler should be enabled after setting")
	}
}

func TestBandwidthScheduleRuleAddRemoveUpdate(t *testing.T) {
	// Create test torrent manager with bandwidth scheduler
	config := TorrentManagerConfig{
		SessionConfig: SessionConfiguration{
			DownloadDir:     "/tmp/test-downloads",
			PeerPort:        51413,
			PeerLimitGlobal: 200,
		},
		ErrorLog: func(format string, args ...interface{}) {
			t.Logf(format, args...)
		},
	}

	manager, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create torrent manager: %v", err)
	}
	defer manager.Close()

	methods := NewRPCMethods(manager)

	// Test adding a rule
	rule := &BandwidthScheduleRule{
		Name:          "test_rule",
		DaysOfWeek:    []int{1, 2, 3, 4, 5}, // Weekdays
		StartTime:     "09:00",
		EndTime:       "17:00",
		DownloadLimit: 1024000, // 1MB/s
		UploadLimit:   512000,  // 512KB/s
		Priority:      1,
		Enabled:       true,
	}

	addReq := BandwidthScheduleRuleAddRequest{Rule: rule}
	err = methods.BandwidthScheduleRuleAdd(addReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleRuleAdd failed: %v", err)
	}

	// Verify rule was added
	getReq := BandwidthScheduleGetRequest{}
	getResp, err := methods.BandwidthScheduleGet(getReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleGet failed: %v", err)
	}

	if len(getResp.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(getResp.Rules))
	}

	if getResp.Rules[0].Name != "test_rule" {
		t.Errorf("Expected rule name 'test_rule', got '%s'", getResp.Rules[0].Name)
	}

	// Test updating the rule
	updatedRule := &BandwidthScheduleRule{
		Name:          "test_rule",
		DaysOfWeek:    []int{1, 2, 3, 4, 5},
		StartTime:     "08:00", // Changed start time
		EndTime:       "18:00", // Changed end time
		DownloadLimit: 2048000, // Changed download limit
		UploadLimit:   1024000, // Changed upload limit
		Priority:      2,       // Changed priority
		Enabled:       true,
	}

	updateReq := BandwidthScheduleRuleUpdateRequest{Rule: updatedRule}
	err = methods.BandwidthScheduleRuleUpdate(updateReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleRuleUpdate failed: %v", err)
	}

	// Verify rule was updated
	getResp, err = methods.BandwidthScheduleGet(getReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleGet failed: %v", err)
	}

	if len(getResp.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(getResp.Rules))
	}

	updatedRuleFromGet := getResp.Rules[0]
	if updatedRuleFromGet.StartTime != "08:00" {
		t.Errorf("Expected start time '08:00', got '%s'", updatedRuleFromGet.StartTime)
	}

	if updatedRuleFromGet.DownloadLimit != 2048000 {
		t.Errorf("Expected download limit 2048000, got %d", updatedRuleFromGet.DownloadLimit)
	}

	if updatedRuleFromGet.Priority != 2 {
		t.Errorf("Expected priority 2, got %d", updatedRuleFromGet.Priority)
	}

	// Test removing the rule
	removeReq := BandwidthScheduleRuleRemoveRequest{Name: "test_rule"}
	err = methods.BandwidthScheduleRuleRemove(removeReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleRuleRemove failed: %v", err)
	}

	// Verify rule was removed
	getResp, err = methods.BandwidthScheduleGet(getReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleGet failed: %v", err)
	}

	if len(getResp.Rules) != 0 {
		t.Errorf("Expected 0 rules after removal, got %d", len(getResp.Rules))
	}
}

func TestBandwidthScheduleRPCErrorHandling(t *testing.T) {
	// Create test torrent manager with bandwidth scheduler
	config := TorrentManagerConfig{
		SessionConfig: SessionConfiguration{
			DownloadDir:     "/tmp/test-downloads",
			PeerPort:        51413,
			PeerLimitGlobal: 200,
		},
		ErrorLog: func(format string, args ...interface{}) {
			t.Logf(format, args...)
		},
	}

	manager, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create torrent manager: %v", err)
	}
	defer manager.Close()

	methods := NewRPCMethods(manager)

	// Test adding invalid rule (empty name)
	invalidRule := &BandwidthScheduleRule{
		Name:          "", // Invalid: empty name
		StartTime:     "09:00",
		EndTime:       "17:00",
		DownloadLimit: 1024000,
		UploadLimit:   512000,
		Priority:      1,
		Enabled:       true,
	}

	addReq := BandwidthScheduleRuleAddRequest{Rule: invalidRule}
	err = methods.BandwidthScheduleRuleAdd(addReq)
	if err == nil {
		t.Error("Expected error for invalid rule with empty name")
	}

	// Test adding nil rule
	nilRuleReq := BandwidthScheduleRuleAddRequest{Rule: nil}
	err = methods.BandwidthScheduleRuleAdd(nilRuleReq)
	if err == nil {
		t.Error("Expected error for nil rule")
	}

	// Test removing non-existent rule
	removeReq := BandwidthScheduleRuleRemoveRequest{Name: "non_existent"}
	err = methods.BandwidthScheduleRuleRemove(removeReq)
	if err == nil {
		t.Error("Expected error for removing non-existent rule")
	}

	// Test removing rule with empty name
	emptyNameReq := BandwidthScheduleRuleRemoveRequest{Name: ""}
	err = methods.BandwidthScheduleRuleRemove(emptyNameReq)
	if err == nil {
		t.Error("Expected error for removing rule with empty name")
	}

	// Test updating non-existent rule
	updateRule := &BandwidthScheduleRule{
		Name:          "non_existent",
		StartTime:     "09:00",
		EndTime:       "17:00",
		DownloadLimit: 1024000,
		UploadLimit:   512000,
		Priority:      1,
		Enabled:       true,
	}

	updateReq := BandwidthScheduleRuleUpdateRequest{Rule: updateRule}
	err = methods.BandwidthScheduleRuleUpdate(updateReq)
	if err == nil {
		t.Error("Expected error for updating non-existent rule")
	}

	// Test updating with nil rule
	nilUpdateReq := BandwidthScheduleRuleUpdateRequest{Rule: nil}
	err = methods.BandwidthScheduleRuleUpdate(nilUpdateReq)
	if err == nil {
		t.Error("Expected error for updating with nil rule")
	}
}

func TestBandwidthScheduleSetWithRules(t *testing.T) {
	// Create test torrent manager with bandwidth scheduler
	config := TorrentManagerConfig{
		SessionConfig: SessionConfiguration{
			DownloadDir:     "/tmp/test-downloads",
			PeerPort:        51413,
			PeerLimitGlobal: 200,
		},
		ErrorLog: func(format string, args ...interface{}) {
			t.Logf(format, args...)
		},
	}

	manager, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create torrent manager: %v", err)
	}
	defer manager.Close()

	methods := NewRPCMethods(manager)

	// Add some initial rules
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

	addReq1 := BandwidthScheduleRuleAddRequest{Rule: rule1}
	err = methods.BandwidthScheduleRuleAdd(addReq1)
	if err != nil {
		t.Fatalf("Failed to add rule1: %v", err)
	}

	addReq2 := BandwidthScheduleRuleAddRequest{Rule: rule2}
	err = methods.BandwidthScheduleRuleAdd(addReq2)
	if err != nil {
		t.Fatalf("Failed to add rule2: %v", err)
	}

	// Verify we have 2 rules
	getReq := BandwidthScheduleGetRequest{}
	getResp, err := methods.BandwidthScheduleGet(getReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleGet failed: %v", err)
	}

	if len(getResp.Rules) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(getResp.Rules))
	}

	// Replace all rules with a new set
	newRule1 := &BandwidthScheduleRule{
		Name:          "new_rule1",
		StartTime:     "10:00",
		EndTime:       "16:00",
		DownloadLimit: 512000,
		UploadLimit:   256000,
		Priority:      1,
		Enabled:       true,
	}

	newRule2 := &BandwidthScheduleRule{
		Name:          "new_rule2",
		StartTime:     "20:00",
		EndTime:       "23:00",
		DownloadLimit: 4096000,
		UploadLimit:   2048000,
		Priority:      2,
		Enabled:       true,
	}

	setReq := BandwidthScheduleSetRequest{
		Rules: []*BandwidthScheduleRule{newRule1, newRule2},
	}

	err = methods.BandwidthScheduleSet(setReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleSet failed: %v", err)
	}

	// Verify old rules were replaced
	getResp, err = methods.BandwidthScheduleGet(getReq)
	if err != nil {
		t.Fatalf("BandwidthScheduleGet failed: %v", err)
	}

	if len(getResp.Rules) != 2 {
		t.Errorf("Expected 2 rules after set, got %d", len(getResp.Rules))
	}

	// Check that we have the new rules and not the old ones
	ruleNames := make(map[string]bool)
	for _, rule := range getResp.Rules {
		ruleNames[rule.Name] = true
	}

	if !ruleNames["new_rule1"] {
		t.Error("Expected to find new_rule1 after set")
	}

	if !ruleNames["new_rule2"] {
		t.Error("Expected to find new_rule2 after set")
	}

	if ruleNames["rule1"] {
		t.Error("Should not find old rule1 after set")
	}

	if ruleNames["rule2"] {
		t.Error("Should not find old rule2 after set")
	}
}
