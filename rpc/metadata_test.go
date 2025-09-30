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
	"strings"
	"testing"
	"time"
)

// Test helper functions

func createTestMetadataManager() *MetadataManager {
	return NewMetadataManager()
}

func createTestMetadataRequest(torrentID int64, set map[string]interface{}, remove []string) *MetadataRequest {
	return &MetadataRequest{
		TorrentID: torrentID,
		Set:       set,
		Remove:    remove,
		Source:    "test",
		Tags:      []string{"test-tag"},
	}
}

func createSimpleSetRequest(torrentID int64, key string, value interface{}) *MetadataRequest {
	return createTestMetadataRequest(torrentID, map[string]interface{}{key: value}, nil)
}

func createSimpleRemoveRequest(torrentID int64, keys ...string) *MetadataRequest {
	return createTestMetadataRequest(torrentID, nil, keys)
}

// Test cases

func TestNewMetadataManager(t *testing.T) {
	mm := NewMetadataManager()

	if mm == nil {
		t.Fatal("NewMetadataManager returned nil")
	}

	if mm.metadata == nil {
		t.Error("metadata map not initialized")
	}

	if mm.constraints.MaxKeyLength != 100 {
		t.Error("default MaxKeyLength not set correctly")
	}

	if mm.constraints.MaxValueSize != 10240 {
		t.Error("default MaxValueSize not set correctly")
	}

	if mm.constraints.MaxKeysPerTorrent != 50 {
		t.Error("default MaxKeysPerTorrent not set correctly")
	}

	if len(mm.constraints.AllowedTypes) == 0 {
		t.Error("default AllowedTypes not set")
	}
}

func TestSetMetadata(t *testing.T) {
	mm := createTestMetadataManager()

	// Test setting basic metadata
	request := createSimpleSetRequest(1, "test_key", "test_value")
	response := mm.SetMetadata(request)

	if !response.Success {
		t.Fatalf("SetMetadata failed: %v", response.Errors)
	}

	if response.Metadata == nil {
		t.Error("Response metadata is nil")
	}

	if response.NewVersion != 1 {
		t.Errorf("Expected version 1, got %d", response.NewVersion)
	}

	// Check metadata was stored
	if len(response.Metadata.Metadata) != 1 {
		t.Errorf("Expected 1 metadata entry, got %d", len(response.Metadata.Metadata))
	}

	if value, exists := response.Metadata.Metadata["test_key"]; !exists {
		t.Error("test_key not found in metadata")
	} else {
		if value.Value != "test_value" {
			t.Errorf("Expected 'test_value', got %v", value.Value)
		}
		if value.Type != "string" {
			t.Errorf("Expected type 'string', got %s", value.Type)
		}
		if value.Source != "test" {
			t.Errorf("Expected source 'test', got %s", value.Source)
		}
	}
}

func TestSetMetadataMultipleTypes(t *testing.T) {
	mm := createTestMetadataManager()

	testCases := []struct {
		key      string
		value    interface{}
		expected string
	}{
		{"string_key", "string_value", "string"},
		{"int_key", 42, "int"},
		{"int64_key", int64(42), "int64"},
		{"float_key", 3.14, "float64"},
		{"bool_key", true, "bool"},
		{"map_key", map[string]interface{}{"nested": "value"}, "map"},
		{"slice_key", []string{"item1", "item2"}, "slice"},
	}

	setMap := make(map[string]interface{})
	for _, tc := range testCases {
		setMap[tc.key] = tc.value
	}

	request := createTestMetadataRequest(1, setMap, nil)
	response := mm.SetMetadata(request)

	if !response.Success {
		t.Fatalf("SetMetadata failed: %v", response.Errors)
	}

	// Verify all types were stored correctly
	for _, tc := range testCases {
		if value, exists := response.Metadata.Metadata[tc.key]; !exists {
			t.Errorf("Key %s not found", tc.key)
		} else if value.Type != tc.expected {
			t.Errorf("Key %s: expected type %s, got %s", tc.key, tc.expected, value.Type)
		}
	}
}

func TestSetMetadataUpdate(t *testing.T) {
	mm := createTestMetadataManager()

	// Set initial value
	request1 := createSimpleSetRequest(1, "update_key", "initial_value")
	response1 := mm.SetMetadata(request1)

	if !response1.Success {
		t.Fatalf("Initial SetMetadata failed: %v", response1.Errors)
	}

	initialTime := response1.Metadata.Metadata["update_key"].CreatedAt

	// Wait a bit to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Update the value
	request2 := createSimpleSetRequest(1, "update_key", "updated_value")
	response2 := mm.SetMetadata(request2)

	if !response2.Success {
		t.Fatalf("Update SetMetadata failed: %v", response2.Errors)
	}

	if response2.NewVersion != 2 {
		t.Errorf("Expected version 2, got %d", response2.NewVersion)
	}

	updatedValue := response2.Metadata.Metadata["update_key"]
	if updatedValue.Value != "updated_value" {
		t.Errorf("Expected 'updated_value', got %v", updatedValue.Value)
	}

	// Check that CreatedAt was preserved but UpdatedAt was changed
	if !updatedValue.CreatedAt.Equal(initialTime) {
		t.Error("CreatedAt should be preserved on update")
	}

	if updatedValue.UpdatedAt.Equal(initialTime) {
		t.Error("UpdatedAt should be different on update")
	}
}

func TestRemoveMetadata(t *testing.T) {
	mm := createTestMetadataManager()

	// Set some metadata
	request1 := createTestMetadataRequest(1, map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}, nil)
	response1 := mm.SetMetadata(request1)

	if !response1.Success {
		t.Fatalf("SetMetadata failed: %v", response1.Errors)
	}

	// Remove some keys
	request2 := createSimpleRemoveRequest(1, "key1", "key3")
	response2 := mm.SetMetadata(request2)

	if !response2.Success {
		t.Fatalf("RemoveMetadata failed: %v", response2.Errors)
	}

	if response2.NewVersion != 2 {
		t.Errorf("Expected version 2, got %d", response2.NewVersion)
	}

	// Check that key2 remains and key1, key3 are removed
	if len(response2.Metadata.Metadata) != 1 {
		t.Errorf("Expected 1 remaining key, got %d", len(response2.Metadata.Metadata))
	}

	if _, exists := response2.Metadata.Metadata["key2"]; !exists {
		t.Error("key2 should still exist")
	}

	if _, exists := response2.Metadata.Metadata["key1"]; exists {
		t.Error("key1 should be removed")
	}

	if _, exists := response2.Metadata.Metadata["key3"]; exists {
		t.Error("key3 should be removed")
	}
}

func TestMetadataValidation(t *testing.T) {
	mm := createTestMetadataManager()

	// Test nil request
	response := mm.SetMetadata(nil)
	if response.Success {
		t.Error("SetMetadata should fail with nil request")
	}

	// Test empty key
	request := createSimpleSetRequest(1, "", "value")
	response = mm.SetMetadata(request)
	if response.Success {
		t.Error("SetMetadata should fail with empty key")
	}

	// Test key too long
	longKey := strings.Repeat("a", 101)
	request = createSimpleSetRequest(1, longKey, "value")
	response = mm.SetMetadata(request)
	if response.Success {
		t.Error("SetMetadata should fail with too long key")
	}

	// Test value too large
	largeValue := strings.Repeat("x", 20000) // Much larger than 10KB limit
	request = createSimpleSetRequest(1, "large_key", largeValue)
	response = mm.SetMetadata(request)
	if response.Success {
		t.Error("SetMetadata should fail with too large value")
	}
}

func TestMetadataConstraints(t *testing.T) {
	mm := createTestMetadataManager()

	// Set restrictive constraints
	constraints := MetadataConstraints{
		MaxKeyLength:      10,
		MaxValueSize:      100,
		MaxKeysPerTorrent: 2,
		AllowedTypes:      []string{"string"},
		RequiredKeys:      []string{"required_key"},
		ReadOnlyKeys:      []string{"readonly_key"},
	}
	mm.SetConstraints(constraints)

	// Test disallowed type
	request := createSimpleSetRequest(1, "int_key", 42)
	response := mm.SetMetadata(request)
	if response.Success {
		t.Error("SetMetadata should fail with disallowed type")
	}

	// Test max keys limit
	request = createTestMetadataRequest(1, map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3", // This should fail due to limit of 2
	}, nil)
	response = mm.SetMetadata(request)
	if response.Success {
		t.Error("SetMetadata should fail when exceeding max keys limit")
	}

	// Test read-only key modification
	// First set the read-only key
	request = createSimpleSetRequest(1, "readonly_key", "initial")
	response = mm.SetMetadata(request)
	if !response.Success {
		t.Fatalf("Initial SetMetadata failed: %v", response.Errors)
	}

	// Try to modify it
	request = createSimpleSetRequest(1, "readonly_key", "modified")
	response = mm.SetMetadata(request)
	if response.Success {
		t.Error("SetMetadata should fail when modifying read-only key")
	}

	// Test removing required key
	request = createSimpleSetRequest(1, "required_key", "value")
	mm.SetMetadata(request) // Set it first

	request = createSimpleRemoveRequest(1, "required_key")
	response = mm.SetMetadata(request)
	if response.Success {
		t.Error("SetMetadata should fail when removing required key")
	}
}

func TestGetMetadata(t *testing.T) {
	mm := createTestMetadataManager()

	// Set metadata for multiple torrents
	request1 := createTestMetadataRequest(1, map[string]interface{}{
		"torrent1_key1": "value1",
		"torrent1_key2": "value2",
	}, nil)
	mm.SetMetadata(request1)

	request2 := createTestMetadataRequest(2, map[string]interface{}{
		"torrent2_key1": "value1",
		"torrent2_key2": "value2",
	}, nil)
	mm.SetMetadata(request2)

	// Test getting all metadata
	query := &MetadataQuery{}
	result := mm.GetMetadata(query)

	if len(result) != 2 {
		t.Errorf("Expected 2 torrents, got %d", len(result))
	}

	// Test getting specific torrent
	query = &MetadataQuery{TorrentIDs: []int64{1}}
	result = mm.GetMetadata(query)

	if len(result) != 1 {
		t.Errorf("Expected 1 torrent, got %d", len(result))
	}

	if _, exists := result[1]; !exists {
		t.Error("Torrent 1 not found in result")
	}

	// Test getting specific keys
	query = &MetadataQuery{
		TorrentIDs: []int64{1},
		Keys:       []string{"torrent1_key1"},
	}
	result = mm.GetMetadata(query)

	if len(result[1].Metadata) != 1 {
		t.Errorf("Expected 1 key, got %d", len(result[1].Metadata))
	}

	if _, exists := result[1].Metadata["torrent1_key1"]; !exists {
		t.Error("torrent1_key1 not found in filtered result")
	}
}

func TestGetMetadataFiltering(t *testing.T) {
	mm := createTestMetadataManager()

	// Set metadata with different sources and tags
	now := time.Now()

	request1 := &MetadataRequest{
		TorrentID: 1,
		Set:       map[string]interface{}{"key1": "value1"},
		Source:    "source1",
		Tags:      []string{"tag1", "tag2"},
	}
	mm.SetMetadata(request1)

	time.Sleep(10 * time.Millisecond)

	request2 := &MetadataRequest{
		TorrentID: 1,
		Set:       map[string]interface{}{"key2": "value2"},
		Source:    "source2",
		Tags:      []string{"tag2", "tag3"},
	}
	mm.SetMetadata(request2)

	// Test filtering by source
	query := &MetadataQuery{
		TorrentIDs: []int64{1},
		Source:     "source1",
	}
	result := mm.GetMetadata(query)

	if len(result[1].Metadata) != 1 {
		t.Errorf("Expected 1 key for source1, got %d", len(result[1].Metadata))
	}

	// Test filtering by tags
	query = &MetadataQuery{
		TorrentIDs: []int64{1},
		Tags:       []string{"tag2"},
	}
	result = mm.GetMetadata(query)

	if len(result[1].Metadata) != 2 {
		t.Errorf("Expected 2 keys with tag2, got %d", len(result[1].Metadata))
	}

	// Test filtering by creation time
	query = &MetadataQuery{
		TorrentIDs:   []int64{1},
		CreatedAfter: now.Add(5 * time.Millisecond),
	}
	result = mm.GetMetadata(query)

	if len(result[1].Metadata) != 1 {
		t.Errorf("Expected 1 key created after time, got %d", len(result[1].Metadata))
	}
}

func TestRemoveAllMetadata(t *testing.T) {
	mm := createTestMetadataManager()

	// Set metadata
	request := createTestMetadataRequest(1, map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}, nil)
	mm.SetMetadata(request)

	// Remove all metadata
	removed := mm.RemoveAllMetadata(1)
	if !removed {
		t.Error("RemoveAllMetadata should return true for existing torrent")
	}

	// Check metadata is gone
	query := &MetadataQuery{TorrentIDs: []int64{1}}
	result := mm.GetMetadata(query)

	if len(result) != 0 {
		t.Error("Metadata should be completely removed")
	}

	// Test removing non-existent torrent
	removed = mm.RemoveAllMetadata(999)
	if removed {
		t.Error("RemoveAllMetadata should return false for non-existent torrent")
	}
}

func TestOptimisticLocking(t *testing.T) {
	mm := createTestMetadataManager()

	// Set initial metadata
	request := createSimpleSetRequest(1, "test_key", "initial_value")
	response := mm.SetMetadata(request)

	if !response.Success {
		t.Fatalf("Initial SetMetadata failed: %v", response.Errors)
	}

	currentVersion := response.NewVersion

	// Test successful update with correct version
	request = &MetadataRequest{
		TorrentID:       1,
		Set:             map[string]interface{}{"test_key": "updated_value"},
		ExpectedVersion: currentVersion,
	}
	response = mm.SetMetadata(request)

	if !response.Success {
		t.Fatalf("Version-checked update failed: %v", response.Errors)
	}

	// Test failed update with wrong version
	request = &MetadataRequest{
		TorrentID:       1,
		Set:             map[string]interface{}{"test_key": "another_value"},
		ExpectedVersion: currentVersion, // This is now outdated
	}
	response = mm.SetMetadata(request)

	if response.Success {
		t.Error("Update should fail with outdated version")
	}

	if len(response.Errors) == 0 || !strings.Contains(response.Errors[0], "version mismatch") {
		t.Error("Should get version mismatch error")
	}
}

func TestMetadataCallbacks(t *testing.T) {
	mm := createTestMetadataManager()

	var changedCalls []string
	var removedCalls []string

	// Set callbacks
	mm.SetMetadataChangedCallback(func(torrentID int64, key string, oldValue, newValue *MetadataValue) {
		changedCalls = append(changedCalls, fmt.Sprintf("changed:%d:%s", torrentID, key))
	})

	mm.SetMetadataRemovedCallback(func(torrentID int64, key string, removedValue *MetadataValue) {
		removedCalls = append(removedCalls, fmt.Sprintf("removed:%d:%s", torrentID, key))
	})

	// Set metadata
	request := createSimpleSetRequest(1, "test_key", "test_value")
	mm.SetMetadata(request)

	if len(changedCalls) != 1 || changedCalls[0] != "changed:1:test_key" {
		t.Errorf("Expected 1 changed callback, got %v", changedCalls)
	}

	// Update metadata
	request = createSimpleSetRequest(1, "test_key", "updated_value")
	mm.SetMetadata(request)

	if len(changedCalls) != 2 || changedCalls[1] != "changed:1:test_key" {
		t.Errorf("Expected 2 changed callbacks, got %v", changedCalls)
	}

	// Remove metadata
	request = createSimpleRemoveRequest(1, "test_key")
	mm.SetMetadata(request)

	if len(removedCalls) != 1 || removedCalls[0] != "removed:1:test_key" {
		t.Errorf("Expected 1 removed callback, got %v", removedCalls)
	}
}

func TestMetadataMetrics(t *testing.T) {
	mm := createTestMetadataManager()

	// Perform some operations
	request1 := createTestMetadataRequest(1, map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}, nil)
	mm.SetMetadata(request1)

	request2 := createTestMetadataRequest(2, map[string]interface{}{
		"key3": "value3",
	}, nil)
	mm.SetMetadata(request2)

	// Check metrics
	metrics := mm.GetMetrics()

	if metrics.TotalTorrents != 2 {
		t.Errorf("Expected 2 total torrents, got %d", metrics.TotalTorrents)
	}

	if metrics.TotalKeys != 3 {
		t.Errorf("Expected 3 total keys, got %d", metrics.TotalKeys)
	}

	if metrics.TotalOperations != 2 {
		t.Errorf("Expected 2 total operations, got %d", metrics.TotalOperations)
	}
}

func TestConstraintHelpers(t *testing.T) {
	// Test CreateDefaultConstraints
	defaults := CreateDefaultConstraints()
	if defaults.MaxKeyLength != 100 {
		t.Error("Default constraints not created correctly")
	}

	// Test CreateRestrictiveConstraints
	restrictive := CreateRestrictiveConstraints()
	if restrictive.MaxKeyLength != 50 {
		t.Error("Restrictive constraints not created correctly")
	}

	if restrictive.MaxValueSize >= defaults.MaxValueSize {
		t.Error("Restrictive constraints should be more restrictive")
	}
}

// Benchmark tests

func BenchmarkSetMetadata(b *testing.B) {
	mm := createTestMetadataManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request := createSimpleSetRequest(1, fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
		mm.SetMetadata(request)
	}
}

func BenchmarkGetMetadata(b *testing.B) {
	mm := createTestMetadataManager()

	// Set up some test data
	for i := 0; i < 100; i++ {
		request := createSimpleSetRequest(int64(i), fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
		mm.SetMetadata(request)
	}

	query := &MetadataQuery{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mm.GetMetadata(query)
	}
}

func BenchmarkMetadataValidation(b *testing.B) {
	mm := createTestMetadataManager()

	torrentMeta := &TorrentMetadata{
		TorrentID: 1,
		Metadata:  make(map[string]*MetadataValue),
		Version:   1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mm.validateMetadata("test_key", "test_value", torrentMeta)
	}
}
