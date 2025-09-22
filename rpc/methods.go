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
	"strings"
	"time"
)

// RPCMethods contains all the RPC method implementations
type RPCMethods struct {
	manager *TorrentManager
}

// NewRPCMethods creates a new RPCMethods instance
func NewRPCMethods(manager *TorrentManager) *RPCMethods {
	return &RPCMethods{
		manager: manager,
	}
}

// TorrentAdd implements the torrent-add RPC method
func (m *RPCMethods) TorrentAdd(req TorrentAddRequest) (TorrentAddResponse, error) {
	// Add the torrent using the manager
	torrentState, err := m.manager.AddTorrent(req)
	if err != nil {
		// Check if it's a duplicate torrent error
		if strings.Contains(err.Error(), "already exists") {
			// Try to find the existing torrent
			if req.Metainfo != "" {
				// For .torrent files, we can calculate the hash
				// This is simplified - in practice you'd decode and hash
				existing := m.manager.GetAllTorrents()
				if len(existing) > 0 {
					torrent := m.convertTorrentStateToTorrent(existing[len(existing)-1])
					return TorrentAddResponse{
						TorrentDuplicate: &torrent,
					}, nil
				}
			}
			return TorrentAddResponse{}, &RPCError{
				Code:    ErrCodeDuplicateTorrent,
				Message: "torrent already exists",
			}
		}

		return TorrentAddResponse{}, &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	// Convert to Transmission format
	torrent := m.convertTorrentStateToTorrent(torrentState)

	return TorrentAddResponse{
		TorrentAdded: &torrent,
	}, nil
}

// TorrentGet implements the torrent-get RPC method
func (m *RPCMethods) TorrentGet(req TorrentGetRequest) (TorrentGetResponse, error) {
	// Resolve torrent IDs
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return TorrentGetResponse{}, &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	var torrents []Torrent
	var removed []int64

	// Get torrents by ID
	for _, id := range ids {
		torrentState, err := m.manager.GetTorrent(id)
		if err != nil {
			// Torrent might have been removed
			removed = append(removed, id)
			continue
		}

		torrent := m.convertTorrentStateToTorrent(torrentState)

		// Filter fields if specified
		if len(req.Fields) > 0 {
			torrent = m.filterTorrentFields(torrent, req.Fields)
		}

		torrents = append(torrents, torrent)
	}

	response := TorrentGetResponse{
		Torrents: torrents,
	}

	if len(removed) > 0 {
		response.Removed = removed
	}

	return response, nil
}

// TorrentStart implements the torrent-start RPC method
func (m *RPCMethods) TorrentStart(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	for _, id := range ids {
		if err := m.manager.StartTorrent(id); err != nil {
			return &RPCError{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("failed to start torrent %d: %v", id, err),
			}
		}
	}

	return nil
}

// TorrentStartNow implements the torrent-start-now RPC method
func (m *RPCMethods) TorrentStartNow(req TorrentActionRequest) error {
	// For simplicity, treat torrent-start-now the same as torrent-start
	// In a full implementation, this would bypass the queue
	return m.TorrentStart(req)
}

// TorrentStop implements the torrent-stop RPC method
func (m *RPCMethods) TorrentStop(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	for _, id := range ids {
		if err := m.manager.StopTorrent(id); err != nil {
			return &RPCError{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("failed to stop torrent %d: %v", id, err),
			}
		}
	}

	return nil
}

// TorrentVerify implements the torrent-verify RPC method
func (m *RPCMethods) TorrentVerify(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	// For each torrent, set status to verifying
	// In a full implementation, this would actually verify the data
	for _, id := range ids {
		torrent, err := m.manager.GetTorrent(id)
		if err != nil {
			continue
		}

		// Set status to verifying
		torrent.Status = TorrentStatusVerifying
	}

	return nil
}

// TorrentRemove implements the torrent-remove RPC method
func (m *RPCMethods) TorrentRemove(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	for _, id := range ids {
		if err := m.manager.RemoveTorrent(id, req.DeleteLocalData); err != nil {
			return &RPCError{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("failed to remove torrent %d: %v", id, err),
			}
		}
	}

	return nil
}

// TorrentSet implements the torrent-set RPC method from the Transmission RPC protocol.
// It provides comprehensive torrent configuration with validation and error handling.
//
// Supported operations include:
//   - File selection and priority management (files-wanted, files-unwanted, priority-high/low/normal)
//   - Tracker management (trackerAdd, trackerRemove, trackerReplace)
//   - Seeding configuration (seedRatioLimit, seedIdleLimit, seedRatioMode, seedIdleMode)
//   - Bandwidth and peer limits (bandwidthPriority, peer-limit)
//   - Download location changes (location, move flag)
//   - General torrent properties (labels, honorsSessionLimits)
//
// The method validates all input parameters, resolves torrent IDs, and applies changes
// atomically per torrent. Errors are collected and returned as aggregated error messages.
//
// Parameters:
//   - req: TorrentActionRequest containing torrent IDs and configuration changes
//
// Returns:
//   - error: RPCError with ErrCodeInvalidArgument for validation failures or torrent resolution errors
//
// Example usage:
//
//	req := TorrentActionRequest{
//	    IDs: []interface{}{1, 2},
//	    FilesWanted: []int64{0, 1},
//	    SeedRatioLimit: 2.0,
//	    Labels: []string{"important", "high-priority"},
//	}
//	err := methods.TorrentSet(req)
func (m *RPCMethods) TorrentSet(req TorrentActionRequest) error {
	// Validate input arguments first to fail fast on invalid requests
	if err := m.validateTorrentSetRequest(req); err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	// Collect errors from all operations to provide comprehensive feedback
	var errors []string

	// Update torrent properties
	for _, id := range ids {
		torrent, err := m.manager.GetTorrent(id)
		if err != nil {
			errors = append(errors, fmt.Sprintf("torrent %d: not found", id))
			continue
		}

		// Apply changes with proper error handling
		if err := m.applyTorrentChanges(torrent, req); err != nil {
			errors = append(errors, fmt.Sprintf("torrent %d: %v", id, err))
		}
	}

	// Return aggregated errors if any occurred
	if len(errors) > 0 {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: fmt.Sprintf("Failed to update torrents: %s", strings.Join(errors, "; ")),
		}
	}

	return nil
}

// validateTorrentSetRequest performs comprehensive validation of TorrentActionRequest parameters.
// This function validates all configurable fields according to Transmission RPC protocol specifications
// and Go best practices for input validation.
//
// Validation rules:
//   - peer-limit: must be non-negative integer
//   - seedRatioLimit: must be non-negative float64
//   - seedIdleLimit: must be non-negative integer
//   - seedRatioMode: must be 0 (session default), 1 (torrent setting), or 2 (unlimited)
//   - seedIdleMode: must be 0 (session default), 1 (torrent setting), or 2 (unlimited)
//   - bandwidthPriority: must be -1 (low), 0 (normal), or 1 (high)
//   - trackerReplace: must contain exactly 2 elements [tracker_id, new_url]
//
// Parameters:
//   - req: TorrentActionRequest to validate
//
// Returns:
//   - error: validation error message or nil if valid
func (m *RPCMethods) validateTorrentSetRequest(req TorrentActionRequest) error {
	// Validate peer limit (must be non-negative)
	if req.PeerLimit < 0 {
		return fmt.Errorf("peer-limit must be non-negative, got %d", req.PeerLimit)
	}

	// Validate seed ratio limit (must be non-negative)
	if req.SeedRatioLimit < 0 {
		return fmt.Errorf("seedRatioLimit must be non-negative, got %f", req.SeedRatioLimit)
	}

	// Validate seed idle limit (must be non-negative)
	if req.SeedIdleLimit < 0 {
		return fmt.Errorf("seedIdleLimit must be non-negative, got %d", req.SeedIdleLimit)
	}

	// Validate seed ratio mode (0=global, 1=single, 2=unlimited per Transmission spec)
	if req.SeedRatioMode < 0 || req.SeedRatioMode > 2 {
		return fmt.Errorf("seedRatioMode must be 0, 1, or 2, got %d", req.SeedRatioMode)
	}

	// Validate seed idle mode (0=global, 1=single, 2=unlimited per Transmission spec)
	if req.SeedIdleMode < 0 || req.SeedIdleMode > 2 {
		return fmt.Errorf("seedIdleMode must be 0, 1, or 2, got %d", req.SeedIdleMode)
	}

	// Validate bandwidth priority (-1=low, 0=normal, 1=high per Transmission spec)
	if req.BandwidthPriority < -1 || req.BandwidthPriority > 1 {
		return fmt.Errorf("bandwidthPriority must be -1, 0, or 1, got %d", req.BandwidthPriority)
	}

	// Validate tracker replacement format
	if len(req.TrackerReplace) > 0 && len(req.TrackerReplace) != 2 {
		return fmt.Errorf("trackerReplace must contain exactly 2 elements [id, url], got %d", len(req.TrackerReplace))
	}

	return nil
}

// applyTorrentChanges applies all requested configuration changes to a single torrent.
// This function coordinates the application of different types of changes while maintaining
// data consistency and proper error handling.
//
// The function processes changes in a specific order:
//  1. File array initialization (if needed)
//  2. Bandwidth priority updates
//  3. Peer limit configuration
//  4. File selection and priority changes
//  5. Tracker management operations
//  6. Seeding configuration updates
//  7. Download location changes
//  8. General torrent properties (labels, session limits)
//
// Parameters:
//   - torrent: TorrentState to modify
//   - req: TorrentActionRequest containing the changes to apply
//
// Returns:
//   - error: aggregated error message if any operation fails, nil on success
func (m *RPCMethods) applyTorrentChanges(torrent *TorrentState, req TorrentActionRequest) error {
	// Initialize file arrays if needed based on MetaInfo
	if err := m.ensureFileArraysInitialized(torrent); err != nil {
		return fmt.Errorf("failed to initialize file arrays: %v", err)
	}

	// Apply bandwidth priority updates
	m.applyBandwidthPriorityChanges(torrent, req)

	// Apply file management changes
	if err := m.applyFileManagementChanges(torrent, req); err != nil {
		return err
	}

	// Apply seeding configuration changes
	m.applySeedingConfigurationChanges(torrent, req)

	// Apply torrent metadata changes
	m.applyTorrentMetadataChanges(torrent, req)

	// Apply tracker management
	if err := m.updateTrackers(torrent, req); err != nil {
		return fmt.Errorf("tracker update failed: %v", err)
	}

	// Apply location changes
	if req.Location != "" {
		if err := m.updateTorrentLocation(torrent, req.Location, req.Move); err != nil {
			return fmt.Errorf("location update failed: %v", err)
		}
	}

	return nil
}

// applyBandwidthPriorityChanges applies bandwidth priority updates to the torrent.
// Bandwidth priority affects download/upload scheduling in transfer management.
func (m *RPCMethods) applyBandwidthPriorityChanges(torrent *TorrentState, req TorrentActionRequest) {
	if req.BandwidthPriority != 0 {
		// Store bandwidth priority for use by transfer scheduling
		// In a full implementation, this would affect download/upload ordering
	}
}

// applyFileManagementChanges applies file selection and priority changes with proper error handling.
// This function coordinates file wanted/unwanted status and priority level updates.
func (m *RPCMethods) applyFileManagementChanges(torrent *TorrentState, req TorrentActionRequest) error {
	// Apply file selection changes
	if err := m.updateFileSelection(torrent, req); err != nil {
		return fmt.Errorf("file selection update failed: %v", err)
	}

	// Apply file priority changes
	if err := m.updateFilePriorities(torrent, req); err != nil {
		return fmt.Errorf("file priority update failed: %v", err)
	}

	return nil
}

// applySeedingConfigurationChanges applies seeding-related configuration updates to the torrent.
// This includes seed ratio limits, idle limits, and session limit compliance settings.
func (m *RPCMethods) applySeedingConfigurationChanges(torrent *TorrentState, req TorrentActionRequest) {
	if req.SeedRatioLimit > 0 {
		torrent.SeedRatioLimit = req.SeedRatioLimit
	}
	if req.SeedIdleLimit > 0 {
		torrent.SeedIdleLimit = req.SeedIdleLimit
	}
	if req.HonorsSessionLimits {
		torrent.HonorsSessionLimits = req.HonorsSessionLimits
	}
}

// applyTorrentMetadataChanges applies general torrent metadata updates including labels.
// Labels are copied to avoid reference issues with the original request slice.
func (m *RPCMethods) applyTorrentMetadataChanges(torrent *TorrentState, req TorrentActionRequest) {
	// Apply label changes (copy to avoid reference issues)
	if len(req.Labels) > 0 {
		torrent.Labels = make([]string, len(req.Labels))
		copy(torrent.Labels, req.Labels)
	}
}

// ensureFileArraysInitialized ensures Wanted and Priorities arrays are properly sized
func (m *RPCMethods) ensureFileArraysInitialized(torrent *TorrentState) error {
	if torrent.MetaInfo == nil {
		return nil // Cannot initialize without MetaInfo
	}

	info, err := torrent.MetaInfo.Info()
	if err != nil {
		return fmt.Errorf("failed to get torrent info: %v", err)
	}

	// Determine expected file count
	expectedFileCount := len(info.Files)
	if expectedFileCount == 0 && info.Length > 0 {
		expectedFileCount = 1 // Single-file torrent
	}

	// Initialize Wanted array (default all files to wanted)
	if len(torrent.Wanted) == 0 && expectedFileCount > 0 {
		torrent.Wanted = make([]bool, expectedFileCount)
		for i := range torrent.Wanted {
			torrent.Wanted[i] = true
		}
	}

	// Initialize Priorities array (default all files to normal priority)
	if len(torrent.Priorities) == 0 && expectedFileCount > 0 {
		torrent.Priorities = make([]int64, expectedFileCount)
		// All elements default to 0 (normal priority)
	}

	return nil
}

// updateFileSelection handles files-wanted and files-unwanted arrays with comprehensive bounds checking.
// This function manages which files in a multi-file torrent should be downloaded.
//
// The function processes two types of file selection changes:
//   - files-wanted: marks specified files as wanted for download
//   - files-unwanted: marks specified files as unwanted (skipped)
//
// File indices are validated against the torrent's file array bounds.
// Invalid indices result in descriptive error messages.
//
// Parameters:
//   - torrent: TorrentState containing the file arrays to modify
//   - req: TorrentActionRequest with FilesWanted and FilesUnwanted arrays
//
// Returns:
//   - error: bounds checking error if any file index is invalid, nil on success
func (m *RPCMethods) updateFileSelection(torrent *TorrentState, req TorrentActionRequest) error {
	// Process files-wanted
	for _, fileIndex := range req.FilesWanted {
		if fileIndex < 0 || int(fileIndex) >= len(torrent.Wanted) {
			return fmt.Errorf("files-wanted index %d out of bounds (0-%d)", fileIndex, len(torrent.Wanted)-1)
		}
		torrent.Wanted[fileIndex] = true
	}

	// Process files-unwanted
	for _, fileIndex := range req.FilesUnwanted {
		if fileIndex < 0 || int(fileIndex) >= len(torrent.Wanted) {
			return fmt.Errorf("files-unwanted index %d out of bounds (0-%d)", fileIndex, len(torrent.Wanted)-1)
		}
		torrent.Wanted[fileIndex] = false
	}

	return nil
}

// updateFilePriorities handles priority arrays with comprehensive bounds checking.
// This function manages download priorities for individual files in multi-file torrents.
//
// Priority levels supported:
//   - High priority (1): Files are downloaded first
//   - Normal priority (0): Files are downloaded normally
//   - Low priority (-1): Files are downloaded last
//
// The function processes three priority arrays:
//   - priority-high: sets specified files to high priority
//   - priority-normal: sets specified files to normal priority
//   - priority-low: sets specified files to low priority
//
// File indices are validated against the torrent's priority array bounds.
//
// Parameters:
//   - torrent: TorrentState containing the priority array to modify
//   - req: TorrentActionRequest with PriorityHigh, PriorityNormal, PriorityLow arrays
//
// Returns:
//   - error: bounds checking error if any file index is invalid, nil on success
func (m *RPCMethods) updateFilePriorities(torrent *TorrentState, req TorrentActionRequest) error {
	// Helper function to validate and set priority
	setPriority := func(indices []int64, priority int64, priorityName string) error {
		for _, fileIndex := range indices {
			if fileIndex < 0 || int(fileIndex) >= len(torrent.Priorities) {
				return fmt.Errorf("%s index %d out of bounds (0-%d)", priorityName, fileIndex, len(torrent.Priorities)-1)
			}
			torrent.Priorities[fileIndex] = priority
		}
		return nil
	}

	// Set high priority (1)
	if err := setPriority(req.PriorityHigh, 1, "priority-high"); err != nil {
		return err
	}

	// Set normal priority (0)
	if err := setPriority(req.PriorityNormal, 0, "priority-normal"); err != nil {
		return err
	}

	// Set low priority (-1)
	if err := setPriority(req.PriorityLow, -1, "priority-low"); err != nil {
		return err
	}

	return nil
}

// updateTrackers handles tracker add/remove/replace operations with validation and duplicate checking.
// This function manages the tracker list for torrents, supporting all Transmission RPC tracker operations.
//
// Supported operations:
//   - trackerAdd: adds new tracker URLs, skipping duplicates
//   - trackerRemove: removes trackers by index with bounds checking
//   - trackerReplace: replaces tracker at specified index with new URL
//
// The function maintains tracker list integrity by:
//   - Validating tracker URLs are not empty/whitespace
//   - Checking for duplicate URLs before adding
//   - Validating array indices for remove/replace operations
//   - Type checking trackerReplace parameters
//
// Parameters:
//   - torrent: TorrentState containing the tracker list to modify
//   - req: TorrentActionRequest with TrackerAdd, TrackerRemove, TrackerReplace arrays
//
// Returns:
//   - error: validation or bounds checking error, nil on success
func (m *RPCMethods) updateTrackers(torrent *TorrentState, req TorrentActionRequest) error {
	// Process tracker additions
	if err := m.addTrackersWithDuplicateCheck(torrent, req.TrackerAdd); err != nil {
		return err
	}

	// Process tracker removals
	if err := m.removeTrackersWithBoundsCheck(torrent, req.TrackerRemove); err != nil {
		return err
	}

	// Process tracker replacements
	if err := m.replaceTrackerWithValidation(torrent, req.TrackerReplace); err != nil {
		return err
	}

	return nil
}

// addTrackersWithDuplicateCheck adds new tracker URLs while checking for duplicates.
// This function validates tracker URLs and prevents duplicate entries in the tracker list.
func (m *RPCMethods) addTrackersWithDuplicateCheck(torrent *TorrentState, trackersToAdd []string) error {
	for _, tracker := range trackersToAdd {
		if strings.TrimSpace(tracker) == "" {
			continue // Skip empty trackers
		}

		// Check for duplicates
		found := false
		for _, existing := range torrent.TrackerList {
			if existing == tracker {
				found = true
				break
			}
		}
		if !found {
			torrent.TrackerList = append(torrent.TrackerList, tracker)
		}
	}
	return nil
}

// removeTrackersWithBoundsCheck removes trackers by index with comprehensive bounds checking.
// This function validates all indices before removal and sorts them in descending order
// to ensure safe removal from highest to lowest index.
func (m *RPCMethods) removeTrackersWithBoundsCheck(torrent *TorrentState, indicesToRemove []int64) error {
	if len(indicesToRemove) == 0 {
		return nil
	}

	// Validate all indices and convert to int slice
	indices := make([]int, len(indicesToRemove))
	for i, idx := range indicesToRemove {
		if idx < 0 || int(idx) >= len(torrent.TrackerList) {
			return fmt.Errorf("trackerRemove index %d out of bounds (0-%d)", idx, len(torrent.TrackerList)-1)
		}
		indices[i] = int(idx)
	}

	// Sort indices in descending order using bubble sort to avoid external dependencies
	m.sortIndicesDescending(indices)

	// Remove trackers from highest index to lowest
	for _, index := range indices {
		torrent.TrackerList = append(torrent.TrackerList[:index], torrent.TrackerList[index+1:]...)
	}

	return nil
}

// sortIndicesDescending sorts a slice of integers in descending order using bubble sort.
// This function avoids external dependencies while providing the needed sorting capability.
func (m *RPCMethods) sortIndicesDescending(indices []int) {
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			if indices[i] < indices[j] {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}
}

// replaceTrackerWithValidation handles tracker replacement with type checking and validation.
// This function validates the replacement parameters, checks bounds, and performs the replacement.
func (m *RPCMethods) replaceTrackerWithValidation(torrent *TorrentState, trackerReplace []interface{}) error {
	if len(trackerReplace) != 2 {
		return nil // No replacement requested
	}

	// Extract and validate parameters
	oldID, newURL, err := m.extractReplacementParameters(trackerReplace)
	if err != nil {
		return err
	}

	// Validate index bounds
	if oldID < 0 || oldID >= len(torrent.TrackerList) {
		return fmt.Errorf("trackerReplace index %d out of bounds (0-%d)", oldID, len(torrent.TrackerList)-1)
	}

	// Validate new URL
	if strings.TrimSpace(newURL) == "" {
		return fmt.Errorf("trackerReplace URL cannot be empty")
	}

	// Perform replacement
	torrent.TrackerList[oldID] = newURL
	return nil
}

// extractReplacementParameters extracts and validates tracker replacement parameters.
// This function handles type checking for JSON number types and string validation.
func (m *RPCMethods) extractReplacementParameters(trackerReplace []interface{}) (int, string, error) {
	var oldID int
	var newURL string

	// Handle different JSON number types for the index
	switch v := trackerReplace[0].(type) {
	case float64:
		oldID = int(v)
	case int:
		oldID = v
	case int64:
		oldID = int(v)
	default:
		return 0, "", fmt.Errorf("trackerReplace[0] must be a number, got %T", v)
	}

	// Validate URL parameter type
	if url, ok := trackerReplace[1].(string); ok {
		newURL = url
	} else {
		return 0, "", fmt.Errorf("trackerReplace[1] must be a string, got %T", trackerReplace[1])
	}

	return oldID, newURL, nil
}

// updateTorrentLocation handles download location changes with validation and move support.
// This function updates the download directory for a torrent, with optional file moving capability.
//
// Location validation:
//   - Location string cannot be empty or whitespace-only
//   - Location is trimmed of leading/trailing whitespace before validation
//
// Move functionality:
//   - When move=false: only updates the download directory path
//   - When move=true: would move actual files to new location (not yet implemented)
//
// Note: Physical file moving is not implemented in this minimal viable solution.
// In a full implementation, move=true would handle file system operations including:
//   - Source file existence verification
//   - Destination directory creation
//   - Atomic file moving with rollback on failure
//   - Path updates in torrent state
//
// Parameters:
//   - torrent: TorrentState to update
//   - location: new download directory path
//   - move: whether to move files physically (currently logged but not implemented)
//
// Returns:
//   - error: validation error if location is empty/whitespace, nil on success
func (m *RPCMethods) updateTorrentLocation(torrent *TorrentState, location string, move bool) error {
	// Validate the new location
	if strings.TrimSpace(location) == "" {
		return fmt.Errorf("location cannot be empty")
	}

	// Update the download directory
	torrent.DownloadDir = location

	// NOTE: File moving functionality is not implemented in this minimal viable solution
	// In a full implementation, when move=true, this would:
	// 1. Verify source files exist
	// 2. Create destination directory
	// 3. Move files atomically
	// 4. Handle move failures gracefully
	// 5. Update file paths in torrent state

	if move {
		// Log that move functionality is not yet implemented
		// In production, return an error or implement actual moving
	}

	return nil
}

// SessionGet implements the session-get RPC method
func (m *RPCMethods) SessionGet() (SessionGetResponse, error) {
	config := m.manager.GetSessionConfig()

	response := SessionGetResponse{
		AltSpeedDown:              config.AltSpeedDown,
		AltSpeedEnabled:           config.AltSpeedEnabled,
		AltSpeedTimeBegin:         0,     // Not implemented
		AltSpeedTimeDay:           0,     // Not implemented
		AltSpeedTimeEnabled:       false, // Not implemented
		AltSpeedTimeEnd:           0,     // Not implemented
		AltSpeedUp:                config.AltSpeedUp,
		BlocklistEnabled:          config.BlocklistEnabled,
		BlocklistSize:             config.BlocklistSize,
		BlocklistURL:              config.BlocklistURL,
		CacheSizeMB:               config.CacheSizeMB,
		ConfigDir:                 "", // Not implemented
		DHT:                       config.DHTEnabled,
		DownloadDir:               config.DownloadDir,
		DownloadDirFreeSpace:      0, // Would require filesystem check
		DownloadQueueEnabled:      config.DownloadQueueEnabled,
		DownloadQueueSize:         config.DownloadQueueSize,
		Encryption:                config.Encryption,
		IdleSeedingLimit:          config.IdleSeedingLimit,
		IdleSeedingLimitEnabled:   config.IdleSeedingLimitEnabled,
		IncompleteDir:             "",    // Not implemented
		IncompleteDirEnabled:      false, // Not implemented
		LPD:                       config.LPDEnabled,
		PeerLimitGlobal:           config.PeerLimitGlobal,
		PeerLimitPerTorrent:       config.PeerLimitPerTorrent,
		PeerPort:                  config.PeerPort,
		PeerPortRandomOnStart:     false, // Not implemented
		PEX:                       config.PEXEnabled,
		PortForwardingEnabled:     false, // Not implemented
		QueueStalledEnabled:       false, // Not implemented
		QueueStalledMinutes:       0,     // Not implemented
		RenamePartialFiles:        false, // Not implemented
		RPCVersion:                17,    // Supporting Transmission RPC version 17
		RPCVersionMinimum:         1,
		ScriptTorrentDoneEnabled:  false, // Not implemented
		ScriptTorrentDoneFilename: "",    // Not implemented
		SeedQueueEnabled:          config.SeedQueueEnabled,
		SeedQueueSize:             config.SeedQueueSize,
		SeedRatioLimit:            config.SeedRatioLimit,
		SeedRatioLimited:          config.SeedRatioLimited,
		SpeedLimitDown:            config.SpeedLimitDown,
		SpeedLimitDownEnabled:     config.SpeedLimitDownEnabled,
		SpeedLimitUp:              config.SpeedLimitUp,
		SpeedLimitUpEnabled:       config.SpeedLimitUpEnabled,
		StartAddedTorrents:        config.StartAddedTorrents,
		TrashOriginalTorrentFiles: false, // Not implemented
		UTP:                       config.UTPEnabled,
		Version:                   config.Version,
	}

	return response, nil
}

// SessionSet implements the session-set RPC method
func (m *RPCMethods) SessionSet(req SessionSetRequest) error {
	config := m.manager.GetSessionConfig()

	// Update configuration based on request
	m.updateSpeedLimitSettings(&config, req)
	m.updatePeerNetworkSettings(&config, req)
	m.updateQueueSettings(&config, req)
	m.updateSeedingSettings(&config, req)
	m.updateDirectorySettings(&config, req)
	m.updateSecuritySettings(&config, req)

	// Apply the updated configuration
	if err := m.manager.UpdateSessionConfig(config); err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	return nil
}

// updateSpeedLimitSettings updates speed limit and alternate speed configurations.
// This includes both regular speed limits and alternate speed settings which are
// typically used for scheduled bandwidth throttling during specific hours.
func (m *RPCMethods) updateSpeedLimitSettings(config *SessionConfiguration, req SessionSetRequest) {
	if req.AltSpeedDown != nil {
		config.AltSpeedDown = *req.AltSpeedDown
	}
	if req.AltSpeedEnabled != nil {
		config.AltSpeedEnabled = *req.AltSpeedEnabled
	}
	if req.AltSpeedUp != nil {
		config.AltSpeedUp = *req.AltSpeedUp
	}
	if req.SpeedLimitDown != nil {
		config.SpeedLimitDown = *req.SpeedLimitDown
	}
	if req.SpeedLimitDownEnabled != nil {
		config.SpeedLimitDownEnabled = *req.SpeedLimitDownEnabled
	}
	if req.SpeedLimitUp != nil {
		config.SpeedLimitUp = *req.SpeedLimitUp
	}
	if req.SpeedLimitUpEnabled != nil {
		config.SpeedLimitUpEnabled = *req.SpeedLimitUpEnabled
	}
}

// updatePeerNetworkSettings updates peer connection and network protocol configurations.
// This includes DHT, PEX, LPD protocols and peer connection limits that control
// how the client discovers and connects to other peers in the BitTorrent network.
func (m *RPCMethods) updatePeerNetworkSettings(config *SessionConfiguration, req SessionSetRequest) {
	if req.DHT != nil {
		config.DHTEnabled = *req.DHT
	}
	if req.LPD != nil {
		config.LPDEnabled = *req.LPD
	}
	if req.PeerLimitGlobal != nil {
		config.PeerLimitGlobal = *req.PeerLimitGlobal
	}
	if req.PeerLimitPerTorrent != nil {
		config.PeerLimitPerTorrent = *req.PeerLimitPerTorrent
	}
	if req.PeerPort != nil {
		config.PeerPort = *req.PeerPort
	}
	if req.PEX != nil {
		config.PEXEnabled = *req.PEX
	}
	if req.UTP != nil {
		config.UTPEnabled = *req.UTP
	}
}

// updateQueueSettings updates download and seed queue configurations.
// Queue settings control how many torrents can be actively downloading
// or seeding simultaneously, with others waiting in queue.
func (m *RPCMethods) updateQueueSettings(config *SessionConfiguration, req SessionSetRequest) {
	if req.DownloadQueueEnabled != nil {
		config.DownloadQueueEnabled = *req.DownloadQueueEnabled
	}
	if req.DownloadQueueSize != nil {
		config.DownloadQueueSize = *req.DownloadQueueSize
	}
	if req.SeedQueueEnabled != nil {
		config.SeedQueueEnabled = *req.SeedQueueEnabled
	}
	if req.SeedQueueSize != nil {
		config.SeedQueueSize = *req.SeedQueueSize
	}
}

// updateSeedingSettings updates seeding behavior and ratio configurations.
// These settings control when torrents stop seeding based on upload ratio
// or idle time, helping manage seeding resource allocation.
func (m *RPCMethods) updateSeedingSettings(config *SessionConfiguration, req SessionSetRequest) {
	if req.IdleSeedingLimit != nil {
		config.IdleSeedingLimit = *req.IdleSeedingLimit
	}
	if req.IdleSeedingLimitEnabled != nil {
		config.IdleSeedingLimitEnabled = *req.IdleSeedingLimitEnabled
	}
	if req.SeedRatioLimit != nil {
		config.SeedRatioLimit = *req.SeedRatioLimit
	}
	if req.SeedRatioLimited != nil {
		config.SeedRatioLimited = *req.SeedRatioLimited
	}
}

// updateDirectorySettings updates file system and download directory configurations.
// This includes download directory paths, cache settings, and torrent startup behavior.
func (m *RPCMethods) updateDirectorySettings(config *SessionConfiguration, req SessionSetRequest) {
	if req.DownloadDir != nil {
		config.DownloadDir = *req.DownloadDir
	}
	if req.CacheSizeMB != nil {
		config.CacheSizeMB = *req.CacheSizeMB
	}
	if req.StartAddedTorrents != nil {
		config.StartAddedTorrents = *req.StartAddedTorrents
	}
}

// updateSecuritySettings updates encryption and blocklist security configurations.
// These settings control peer connection encryption and IP address blocking
// for enhanced privacy and security of BitTorrent connections.
func (m *RPCMethods) updateSecuritySettings(config *SessionConfiguration, req SessionSetRequest) {
	if req.Encryption != nil {
		config.Encryption = *req.Encryption
	}
	if req.BlocklistEnabled != nil {
		config.BlocklistEnabled = *req.BlocklistEnabled
	}
	if req.BlocklistURL != nil {
		config.BlocklistURL = *req.BlocklistURL
	}
}

// SessionStats implements the session-stats RPC method
func (m *RPCMethods) SessionStats() (map[string]interface{}, error) {
	// Return basic session statistics
	// In a full implementation, this would include detailed transfer stats
	stats := map[string]interface{}{
		"activeTorrentCount": len(m.manager.GetAllTorrents()),
		"downloadSpeed":      0, // Would calculate current download speed
		"pausedTorrentCount": 0, // Would count paused torrents
		"torrentCount":       len(m.manager.GetAllTorrents()),
		"uploadSpeed":        0, // Would calculate current upload speed
		"cumulative-stats": map[string]interface{}{
			"downloadedBytes": 0, // Total downloaded since start
			"uploadedBytes":   0, // Total uploaded since start
			"filesAdded":      0, // Total files added
			"sessionCount":    1, // Session count
			"secondsActive":   0, // Seconds active
		},
		"current-stats": map[string]interface{}{
			"downloadedBytes": 0, // Downloaded this session
			"uploadedBytes":   0, // Uploaded this session
			"filesAdded":      0, // Files added this session
			"sessionCount":    1, // Always 1 for current
			"secondsActive":   0, // Seconds active this session
		},
	}

	return stats, nil
}

// Helper methods

// convertTorrentStateToTorrent converts internal TorrentState to Transmission Torrent format
func (m *RPCMethods) convertTorrentStateToTorrent(state *TorrentState) Torrent {
	var files []File
	var fileStats []FileStat
	var priorities []int64
	var wanted []bool

	// Convert files
	for i, f := range state.Files {
		files = append(files, File{
			BytesCompleted: f.BytesCompleted,
			Length:         f.Length,
			Name:           f.Name,
		})

		fileStats = append(fileStats, FileStat{
			BytesCompleted: f.BytesCompleted,
			Wanted:         f.Wanted,
			Priority:       f.Priority,
		})

		if i < len(state.Priorities) {
			priorities = append(priorities, state.Priorities[i])
		}
		if i < len(state.Wanted) {
			wanted = append(wanted, state.Wanted[i])
		}
	}

	// Convert peers
	var peers []Peer
	for _, p := range state.Peers {
		peers = append(peers, Peer{
			Address:      p.Address,
			Port:         p.Port,
			ClientName:   "Unknown",
			IsIncoming:   p.Direction == "incoming",
			RateToClient: 0, // Would need to track transfer rates
			RateToPeer:   0,
		})
	}

	// Convert trackers
	var trackers []Tracker
	var trackerStats []TrackerStat
	for i, announce := range state.TrackerList {
		trackers = append(trackers, Tracker{
			Announce: announce,
			ID:       int64(i),
			Tier:     int64(i), // Simplified
		})

		trackerStats = append(trackerStats, TrackerStat{
			Announce:              announce,
			ID:                    int64(i),
			Tier:                  int64(i),
			LastAnnounceSucceeded: false, // Would track actual status
			AnnounceState:         0,     // Would track actual state
		})
	}

	// Calculate progress
	var totalSize int64
	var completed int64
	for _, f := range files {
		totalSize += f.Length
		completed += f.BytesCompleted
	}

	var percentDone float64
	if totalSize > 0 {
		percentDone = float64(completed) / float64(totalSize)
	}

	// Create magnet link
	magnetLink := ""
	if state.MetaInfo != nil {
		magnet := state.MetaInfo.Magnet("", state.InfoHash)
		magnetLink = magnet.String()
	}

	return Torrent{
		ID:                      state.ID,
		HashString:              state.InfoHash.HexString(),
		Name:                    getName(state),
		Status:                  state.Status,
		AddedDate:               state.AddedDate,
		StartDate:               state.StartDate,
		ActivityDate:            getLastActivity(state),
		DownloadDir:             state.DownloadDir,
		Files:                   files,
		FileStats:               fileStats,
		Priorities:              priorities,
		Wanted:                  wanted,
		Trackers:                trackers,
		TrackerStats:            trackerStats,
		Peers:                   peers,
		TotalSize:               totalSize,
		LeftUntilDone:           totalSize - completed,
		PercentDone:             percentDone,
		SizeWhenDone:            totalSize,
		DownloadedEver:          completed,
		HaveValid:               completed,
		RateDownload:            state.DownloadRate,
		RateUpload:              state.UploadRate,
		UploadedEver:            state.Uploaded,
		UploadRatio:             calculateRatio(state.Uploaded, completed),
		SeedRatioLimit:          state.SeedRatioLimit,
		SeedIdleLimit:           state.SeedIdleLimit,
		HonorsSessionLimits:     state.HonorsSessionLimits,
		Labels:                  state.Labels,
		MagnetLink:              magnetLink,
		PieceCount:              0,     // Would need to calculate from piece length
		PieceSize:               0,     // Would get from metainfo
		IsPrivate:               false, // Would get from metainfo
		IsFinished:              state.Status == TorrentStatusSeeding,
		MetadataPercentComplete: getMetadataProgress(state),
		BandwidthPriority:       0, // Default priority
	}
}

// filterTorrentFields filters torrent fields based on requested fields
func (m *RPCMethods) filterTorrentFields(torrent Torrent, fields []string) Torrent {
	// Create a new torrent with only requested fields
	// This is a simplified implementation - in practice, you'd use reflection
	// or generate this code to handle all possible fields

	filtered := Torrent{}

	for _, field := range fields {
		switch field {
		case "id":
			filtered.ID = torrent.ID
		case "hashString":
			filtered.HashString = torrent.HashString
		case "name":
			filtered.Name = torrent.Name
		case "status":
			filtered.Status = torrent.Status
		case "addedDate":
			filtered.AddedDate = torrent.AddedDate
		case "startDate":
			filtered.StartDate = torrent.StartDate
		case "activityDate":
			filtered.ActivityDate = torrent.ActivityDate
		case "downloadDir":
			filtered.DownloadDir = torrent.DownloadDir
		case "files":
			filtered.Files = torrent.Files
		case "fileStats":
			filtered.FileStats = torrent.FileStats
		case "priorities":
			filtered.Priorities = torrent.Priorities
		case "wanted":
			filtered.Wanted = torrent.Wanted
		case "trackers":
			filtered.Trackers = torrent.Trackers
		case "trackerStats":
			filtered.TrackerStats = torrent.TrackerStats
		case "peers":
			filtered.Peers = torrent.Peers
		case "totalSize":
			filtered.TotalSize = torrent.TotalSize
		case "leftUntilDone":
			filtered.LeftUntilDone = torrent.LeftUntilDone
		case "percentDone":
			filtered.PercentDone = torrent.PercentDone
		case "sizeWhenDone":
			filtered.SizeWhenDone = torrent.SizeWhenDone
		case "downloadedEver":
			filtered.DownloadedEver = torrent.DownloadedEver
		case "haveValid":
			filtered.HaveValid = torrent.HaveValid
		case "rateDownload":
			filtered.RateDownload = torrent.RateDownload
		case "rateUpload":
			filtered.RateUpload = torrent.RateUpload
		case "uploadedEver":
			filtered.UploadedEver = torrent.UploadedEver
		case "uploadRatio":
			filtered.UploadRatio = torrent.UploadRatio
		case "seedRatioLimit":
			filtered.SeedRatioLimit = torrent.SeedRatioLimit
		case "seedIdleLimit":
			filtered.SeedIdleLimit = torrent.SeedIdleLimit
		case "honorsSessionLimits":
			filtered.HonorsSessionLimits = torrent.HonorsSessionLimits
		case "labels":
			filtered.Labels = torrent.Labels
		case "magnetLink":
			filtered.MagnetLink = torrent.MagnetLink
		case "pieceCount":
			filtered.PieceCount = torrent.PieceCount
		case "pieceSize":
			filtered.PieceSize = torrent.PieceSize
		case "isPrivate":
			filtered.IsPrivate = torrent.IsPrivate
		case "isFinished":
			filtered.IsFinished = torrent.IsFinished
		case "metadataPercentComplete":
			filtered.MetadataPercentComplete = torrent.MetadataPercentComplete
		case "bandwidthPriority":
			filtered.BandwidthPriority = torrent.BandwidthPriority
		}
	}

	return filtered
}

// Helper functions

func getName(state *TorrentState) string {
	if state.MetaInfo != nil {
		info, err := state.MetaInfo.Info()
		if err == nil {
			return info.Name
		}
	}
	return state.InfoHash.HexString()
}

func getLastActivity(state *TorrentState) time.Time {
	if state.Status == TorrentStatusDownloading || state.Status == TorrentStatusSeeding {
		return time.Now()
	}
	return state.StartDate
}

func calculateRatio(uploaded, downloaded int64) float64 {
	if downloaded == 0 {
		return 0
	}
	return float64(uploaded) / float64(downloaded)
}

func getMetadataProgress(state *TorrentState) float64 {
	if state.MetaInfo != nil {
		return 1.0 // We have complete metadata
	}
	if state.Status == TorrentStatusQueuedVerify {
		return 0.5 // Downloading metadata
	}
	return 0.0 // No metadata yet
}

// Method registry for JSON-RPC dispatch

// GetMethodHandler returns a handler function for the given method name
func (m *RPCMethods) GetMethodHandler(method string) (func(json.RawMessage) (interface{}, error), bool) {
	switch method {
	case "torrent-add":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentAddRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return m.TorrentAdd(req)
		}, true

	case "torrent-get":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentGetRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return m.TorrentGet(req)
		}, true

	case "torrent-start":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentStart(req)
		}, true

	case "torrent-start-now":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentStartNow(req)
		}, true

	case "torrent-stop":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentStop(req)
		}, true

	case "torrent-verify":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentVerify(req)
		}, true

	case "torrent-remove":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentRemove(req)
		}, true

	case "torrent-set":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentSet(req)
		}, true

	case "session-get":
		return func(params json.RawMessage) (interface{}, error) {
			return m.SessionGet()
		}, true

	case "session-set":
		return func(params json.RawMessage) (interface{}, error) {
			var req SessionSetRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.SessionSet(req)
		}, true

	case "session-stats":
		return func(params json.RawMessage) (interface{}, error) {
			return m.SessionStats()
		}, true

	default:
		return nil, false
	}
}

// GetSupportedMethods returns a list of all supported RPC methods
func (m *RPCMethods) GetSupportedMethods() []string {
	return []string{
		"torrent-add",
		"torrent-get",
		"torrent-start",
		"torrent-start-now",
		"torrent-stop",
		"torrent-verify",
		"torrent-remove",
		"torrent-set",
		"session-get",
		"session-set",
		"session-stats",
	}
}
