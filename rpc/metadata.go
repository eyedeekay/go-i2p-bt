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
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// MetadataValue represents a typed metadata value with validation
type MetadataValue struct {
	// Value is the actual metadata value
	Value interface{} `json:"value"`
	// Type indicates the value type for validation
	Type string `json:"type"`
	// CreatedAt timestamp when metadata was created
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt timestamp when metadata was last updated
	UpdatedAt time.Time `json:"updated_at"`
	// Source indicates which component created this metadata
	Source string `json:"source,omitempty"`
	// Tags for organizing and filtering metadata
	Tags []string `json:"tags,omitempty"`
}

// TorrentMetadata stores custom metadata for a torrent
type TorrentMetadata struct {
	// TorrentID identifies which torrent this metadata belongs to
	TorrentID int64 `json:"torrent_id"`
	// Metadata key-value pairs
	Metadata map[string]*MetadataValue `json:"metadata"`
	// Version for optimistic locking and change detection
	Version int64 `json:"version"`
	// LastModified timestamp
	LastModified time.Time `json:"last_modified"`
}

// MetadataConstraints defines validation rules for metadata
type MetadataConstraints struct {
	// MaxKeyLength maximum allowed key length
	MaxKeyLength int
	// MaxValueSize maximum serialized value size in bytes
	MaxValueSize int
	// MaxKeysPerTorrent maximum number of metadata keys per torrent
	MaxKeysPerTorrent int
	// AllowedTypes list of allowed value types
	AllowedTypes []string
	// RequiredKeys list of keys that must be present
	RequiredKeys []string
	// ReadOnlyKeys list of keys that cannot be modified after creation
	ReadOnlyKeys []string
}

// MetadataRequest represents a request to modify torrent metadata
type MetadataRequest struct {
	// TorrentID identifies the target torrent
	TorrentID int64 `json:"torrent_id"`
	// Set metadata entries to add or update
	Set map[string]interface{} `json:"set,omitempty"`
	// Remove metadata keys to delete
	Remove []string `json:"remove,omitempty"`
	// Source component making the request
	Source string `json:"source,omitempty"`
	// Tags to apply to new metadata entries
	Tags []string `json:"tags,omitempty"`
	// ExpectedVersion for optimistic locking
	ExpectedVersion int64 `json:"expected_version,omitempty"`
}

// MetadataResponse represents the response to a metadata operation
type MetadataResponse struct {
	// Success indicates if the operation succeeded
	Success bool `json:"success"`
	// Metadata the current metadata state after operation
	Metadata *TorrentMetadata `json:"metadata,omitempty"`
	// Errors any validation or processing errors
	Errors []string `json:"errors,omitempty"`
	// NewVersion the version after successful update
	NewVersion int64 `json:"new_version,omitempty"`
}

// MetadataQuery represents a query for torrent metadata
type MetadataQuery struct {
	// TorrentIDs to query (empty means all torrents)
	TorrentIDs []int64 `json:"torrent_ids,omitempty"`
	// Keys to include (empty means all keys)
	Keys []string `json:"keys,omitempty"`
	// Tags to filter by (all must match)
	Tags []string `json:"tags,omitempty"`
	// Source to filter by
	Source string `json:"source,omitempty"`
	// CreatedAfter filter by creation time
	CreatedAfter time.Time `json:"created_after,omitempty"`
	// CreatedBefore filter by creation time
	CreatedBefore time.Time `json:"created_before,omitempty"`
}

// MetadataManager manages custom metadata for torrents
type MetadataManager struct {
	mu sync.RWMutex

	// Storage for torrent metadata
	metadata map[int64]*TorrentMetadata

	// Validation constraints
	constraints MetadataConstraints

	// Event callbacks
	onMetadataChanged func(torrentID int64, key string, oldValue, newValue *MetadataValue)
	onMetadataRemoved func(torrentID int64, key string, removedValue *MetadataValue)

	// Performance metrics
	metrics MetadataMetrics
}

// MetadataMetrics tracks metadata manager performance
type MetadataMetrics struct {
	TotalTorrents    int64         `json:"total_torrents"`
	TotalKeys        int64         `json:"total_keys"`
	TotalOperations  int64         `json:"total_operations"`
	ValidationErrors int64         `json:"validation_errors"`
	AverageLatency   time.Duration `json:"average_latency"`
	LastOperation    time.Time     `json:"last_operation"`
}

// NewMetadataManager creates a new metadata manager with default constraints
func NewMetadataManager() *MetadataManager {
	return &MetadataManager{
		metadata: make(map[int64]*TorrentMetadata),
		constraints: MetadataConstraints{
			MaxKeyLength:      100,
			MaxValueSize:      10240, // 10KB
			MaxKeysPerTorrent: 50,
			AllowedTypes:      []string{"string", "int", "int64", "float64", "bool", "map", "slice"},
			RequiredKeys:      []string{},
			ReadOnlyKeys:      []string{},
		},
	}
}

// SetConstraints updates the validation constraints
func (mm *MetadataManager) SetConstraints(constraints MetadataConstraints) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.constraints = constraints
}

// SetMetadataChangedCallback sets callback for metadata changes
func (mm *MetadataManager) SetMetadataChangedCallback(callback func(torrentID int64, key string, oldValue, newValue *MetadataValue)) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.onMetadataChanged = callback
}

// SetMetadataRemovedCallback sets callback for metadata removal
func (mm *MetadataManager) SetMetadataRemovedCallback(callback func(torrentID int64, key string, removedValue *MetadataValue)) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.onMetadataRemoved = callback
}

// SetMetadata sets metadata for a torrent
func (mm *MetadataManager) SetMetadata(request *MetadataRequest) *MetadataResponse {
	if request == nil {
		return &MetadataResponse{
			Success: false,
			Errors:  []string{"request cannot be nil"},
		}
	}

	start := time.Now()
	mm.mu.Lock()
	defer mm.updateMetrics(start)

	torrentMeta := mm.getOrCreateTorrentMetadata(request.TorrentID)

	if response := mm.validateVersion(request, torrentMeta); response != nil {
		return response
	}

	errors := make([]string, 0)
	errors = append(errors, mm.processMetadataRemovals(request, torrentMeta)...)
	errors = append(errors, mm.processMetadataUpdates(request, torrentMeta)...)

	return mm.buildMetadataResponse(torrentMeta, errors)
}

// updateMetrics updates metadata manager metrics after an operation
func (mm *MetadataManager) updateMetrics(start time.Time) {
	mm.metrics.TotalOperations++
	mm.metrics.AverageLatency = (mm.metrics.AverageLatency + time.Since(start)) / 2
	mm.metrics.LastOperation = time.Now()
	mm.mu.Unlock()
}

// getOrCreateTorrentMetadata retrieves existing or creates new torrent metadata
func (mm *MetadataManager) getOrCreateTorrentMetadata(torrentID int64) *TorrentMetadata {
	torrentMeta, exists := mm.metadata[torrentID]
	if !exists {
		torrentMeta = &TorrentMetadata{
			TorrentID:    torrentID,
			Metadata:     make(map[string]*MetadataValue),
			Version:      0,
			LastModified: time.Now(),
		}
		mm.metadata[torrentID] = torrentMeta
		mm.metrics.TotalTorrents++
	}
	return torrentMeta
}

// validateVersion checks version for optimistic locking
func (mm *MetadataManager) validateVersion(request *MetadataRequest, torrentMeta *TorrentMetadata) *MetadataResponse {
	if request.ExpectedVersion > 0 && torrentMeta.Version != request.ExpectedVersion {
		return &MetadataResponse{
			Success: false,
			Errors:  []string{fmt.Sprintf("version mismatch: expected %d, got %d", request.ExpectedVersion, torrentMeta.Version)},
		}
	}
	return nil
}

// processMetadataRemovals processes metadata removal requests
func (mm *MetadataManager) processMetadataRemovals(request *MetadataRequest, torrentMeta *TorrentMetadata) []string {
	var errors []string

	for _, key := range request.Remove {
		if err := mm.validateKeyForRemoval(key); err != nil {
			errors = append(errors, fmt.Sprintf("remove key '%s': %v", key, err))
			continue
		}

		if oldValue, exists := torrentMeta.Metadata[key]; exists {
			delete(torrentMeta.Metadata, key)
			mm.metrics.TotalKeys--

			if mm.onMetadataRemoved != nil {
				mm.onMetadataRemoved(request.TorrentID, key, oldValue)
			}
		}
	}

	return errors
}

// processMetadataUpdates processes metadata set operations
func (mm *MetadataManager) processMetadataUpdates(request *MetadataRequest, torrentMeta *TorrentMetadata) []string {
	var errors []string

	for key, value := range request.Set {
		if err := mm.validateMetadata(key, value, torrentMeta); err != nil {
			errors = append(errors, fmt.Sprintf("set key '%s': %v", key, err))
			mm.metrics.ValidationErrors++
			continue
		}

		oldValue := torrentMeta.Metadata[key]
		newValue := mm.createMetadataValue(value, request.Source, request.Tags, oldValue)
		torrentMeta.Metadata[key] = newValue

		if oldValue == nil {
			mm.metrics.TotalKeys++
		}

		if mm.onMetadataChanged != nil {
			mm.onMetadataChanged(request.TorrentID, key, oldValue, newValue)
		}
	}

	return errors
}

// createMetadataValue creates a new MetadataValue preserving creation time if updating
func (mm *MetadataManager) createMetadataValue(value interface{}, source string, tags []string, oldValue *MetadataValue) *MetadataValue {
	newValue := &MetadataValue{
		Value:     value,
		Type:      mm.getValueType(value),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Source:    source,
		Tags:      tags,
	}

	if oldValue != nil {
		newValue.CreatedAt = oldValue.CreatedAt
	}

	return newValue
}

// buildMetadataResponse creates the final response with version updates
func (mm *MetadataManager) buildMetadataResponse(torrentMeta *TorrentMetadata, errors []string) *MetadataResponse {
	torrentMeta.Version++
	torrentMeta.LastModified = time.Now()

	response := &MetadataResponse{
		Success:    len(errors) == 0,
		Metadata:   mm.copyTorrentMetadata(torrentMeta),
		NewVersion: torrentMeta.Version,
	}

	if len(errors) > 0 {
		response.Errors = errors
	}

	return response
}

// GetMetadata retrieves metadata for torrents
func (mm *MetadataManager) GetMetadata(query *MetadataQuery) map[int64]*TorrentMetadata {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	result := make(map[int64]*TorrentMetadata)

	// Determine torrent IDs to query
	var torrentIDs []int64
	if len(query.TorrentIDs) > 0 {
		torrentIDs = query.TorrentIDs
	} else {
		// Get all torrent IDs
		for torrentID := range mm.metadata {
			torrentIDs = append(torrentIDs, torrentID)
		}
	}

	// Query each torrent
	for _, torrentID := range torrentIDs {
		torrentMeta, exists := mm.metadata[torrentID]
		if !exists {
			continue
		}

		// Filter metadata based on query
		filteredMeta := mm.filterMetadata(torrentMeta, query)
		if filteredMeta != nil && len(filteredMeta.Metadata) > 0 {
			result[torrentID] = filteredMeta
		}
	}

	return result
}

// RemoveAllMetadata removes all metadata for a torrent
func (mm *MetadataManager) RemoveAllMetadata(torrentID int64) bool {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	torrentMeta, exists := mm.metadata[torrentID]
	if !exists {
		return false
	}

	// Trigger removal callbacks for all keys
	if mm.onMetadataRemoved != nil {
		for key, value := range torrentMeta.Metadata {
			mm.onMetadataRemoved(torrentID, key, value)
		}
	}

	// Update metrics
	mm.metrics.TotalKeys -= int64(len(torrentMeta.Metadata))
	mm.metrics.TotalTorrents--

	// Remove from storage
	delete(mm.metadata, torrentID)
	return true
}

// GetMetrics returns current metadata manager metrics
func (mm *MetadataManager) GetMetrics() MetadataMetrics {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.metrics
}

// validateMetadata validates a metadata key-value pair
func (mm *MetadataManager) validateMetadata(key string, value interface{}, torrentMeta *TorrentMetadata) error {
	if err := mm.validateKey(key, torrentMeta); err != nil {
		return err
	}

	if err := mm.validateValueType(value); err != nil {
		return err
	}

	if err := mm.validateValueSerialization(value); err != nil {
		return err
	}

	return mm.validateTorrentCapacity(key, torrentMeta)
}

// validateKey validates the metadata key constraints
func (mm *MetadataManager) validateKey(key string, torrentMeta *TorrentMetadata) error {
	// Validate key length
	if len(key) > mm.constraints.MaxKeyLength {
		return fmt.Errorf("key length %d exceeds maximum %d", len(key), mm.constraints.MaxKeyLength)
	}

	// Validate key is not empty
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	// Check if key is read-only
	for _, readOnlyKey := range mm.constraints.ReadOnlyKeys {
		if key == readOnlyKey {
			if _, exists := torrentMeta.Metadata[key]; exists {
				return fmt.Errorf("key '%s' is read-only", key)
			}
		}
	}

	return nil
}

// validateValueType validates the value type is allowed
func (mm *MetadataManager) validateValueType(value interface{}) error {
	valueType := mm.getValueType(value)
	if !mm.isAllowedType(valueType) {
		return fmt.Errorf("type '%s' is not allowed", valueType)
	}
	return nil
}

// validateValueSerialization validates the value can be serialized and meets size constraints
func (mm *MetadataManager) validateValueSerialization(value interface{}) error {
	serialized, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("value is not serializable: %w", err)
	}
	if len(serialized) > mm.constraints.MaxValueSize {
		return fmt.Errorf("serialized value size %d exceeds maximum %d", len(serialized), mm.constraints.MaxValueSize)
	}
	return nil
}

// validateTorrentCapacity validates the torrent doesn't exceed maximum key count
func (mm *MetadataManager) validateTorrentCapacity(key string, torrentMeta *TorrentMetadata) error {
	if _, exists := torrentMeta.Metadata[key]; !exists {
		if len(torrentMeta.Metadata) >= mm.constraints.MaxKeysPerTorrent {
			return fmt.Errorf("torrent already has maximum number of metadata keys (%d)", mm.constraints.MaxKeysPerTorrent)
		}
	}
	return nil
}

// validateKeyForRemoval validates a key can be removed
func (mm *MetadataManager) validateKeyForRemoval(key string) error {
	// Check if key is required
	for _, requiredKey := range mm.constraints.RequiredKeys {
		if key == requiredKey {
			return fmt.Errorf("key '%s' is required and cannot be removed", key)
		}
	}

	// Check if key is read-only
	for _, readOnlyKey := range mm.constraints.ReadOnlyKeys {
		if key == readOnlyKey {
			return fmt.Errorf("key '%s' is read-only and cannot be removed", key)
		}
	}

	return nil
}

// getValueType determines the type string for a value
func (mm *MetadataManager) getValueType(value interface{}) string {
	if value == nil {
		return "nil"
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return "int"
	case reflect.Int64:
		return "int64"
	case reflect.Float32, reflect.Float64:
		return "float64"
	case reflect.Bool:
		return "bool"
	case reflect.Map:
		return "map"
	case reflect.Slice, reflect.Array:
		return "slice"
	default:
		return rv.Kind().String()
	}
}

// isAllowedType checks if a type is in the allowed types list
func (mm *MetadataManager) isAllowedType(valueType string) bool {
	for _, allowedType := range mm.constraints.AllowedTypes {
		if valueType == allowedType {
			return true
		}
	}
	return false
}

// copyTorrentMetadata creates a deep copy of torrent metadata
func (mm *MetadataManager) copyTorrentMetadata(original *TorrentMetadata) *TorrentMetadata {
	result := &TorrentMetadata{
		TorrentID:    original.TorrentID,
		Version:      original.Version,
		LastModified: original.LastModified,
		Metadata:     make(map[string]*MetadataValue),
	}

	for key, value := range original.Metadata {
		result.Metadata[key] = &MetadataValue{
			Value:     value.Value,
			Type:      value.Type,
			CreatedAt: value.CreatedAt,
			UpdatedAt: value.UpdatedAt,
			Source:    value.Source,
			Tags:      make([]string, len(value.Tags)),
		}
		copy(result.Metadata[key].Tags, value.Tags)
	}

	return result
}

// filterMetadata filters metadata based on query criteria
func (mm *MetadataManager) filterMetadata(torrentMeta *TorrentMetadata, query *MetadataQuery) *TorrentMetadata {
	filtered := &TorrentMetadata{
		TorrentID:    torrentMeta.TorrentID,
		Version:      torrentMeta.Version,
		LastModified: torrentMeta.LastModified,
		Metadata:     make(map[string]*MetadataValue),
	}

	for key, value := range torrentMeta.Metadata {
		// Filter by keys
		if len(query.Keys) > 0 && !mm.containsString(query.Keys, key) {
			continue
		}

		// Filter by source
		if query.Source != "" && value.Source != query.Source {
			continue
		}

		// Filter by tags (all query tags must be present)
		if len(query.Tags) > 0 && !mm.containsAllTags(value.Tags, query.Tags) {
			continue
		}

		// Filter by creation time
		if !query.CreatedAfter.IsZero() && value.CreatedAt.Before(query.CreatedAfter) {
			continue
		}
		if !query.CreatedBefore.IsZero() && value.CreatedAt.After(query.CreatedBefore) {
			continue
		}

		// Include this metadata entry
		filtered.Metadata[key] = &MetadataValue{
			Value:     value.Value,
			Type:      value.Type,
			CreatedAt: value.CreatedAt,
			UpdatedAt: value.UpdatedAt,
			Source:    value.Source,
			Tags:      make([]string, len(value.Tags)),
		}
		copy(filtered.Metadata[key].Tags, value.Tags)
	}

	return filtered
}

// containsString checks if a slice contains a string
func (mm *MetadataManager) containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// containsAllTags checks if value tags contain all required tags
func (mm *MetadataManager) containsAllTags(valueTags, requiredTags []string) bool {
	for _, required := range requiredTags {
		found := false
		for _, valueTag := range valueTags {
			if valueTag == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// CreateDefaultConstraints creates default metadata constraints
func CreateDefaultConstraints() MetadataConstraints {
	return MetadataConstraints{
		MaxKeyLength:      100,
		MaxValueSize:      10240, // 10KB
		MaxKeysPerTorrent: 50,
		AllowedTypes:      []string{"string", "int", "int64", "float64", "bool", "map", "slice"},
		RequiredKeys:      []string{},
		ReadOnlyKeys:      []string{},
	}
}

// CreateRestrictiveConstraints creates more restrictive metadata constraints
func CreateRestrictiveConstraints() MetadataConstraints {
	return MetadataConstraints{
		MaxKeyLength:      50,
		MaxValueSize:      1024, // 1KB
		MaxKeysPerTorrent: 10,
		AllowedTypes:      []string{"string", "int64", "bool"},
		RequiredKeys:      []string{},
		ReadOnlyKeys:      []string{},
	}
}
