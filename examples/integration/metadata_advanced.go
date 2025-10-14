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

// Package main demonstrates advanced metadata management integration
package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-i2p/go-i2p-bt/rpc"
)

// MetadataExtensions provide high-level functionality on top of base metadata manager
type MetadataExtensions struct {
	manager *rpc.MetadataManager
}

// NewMetadataExtensions creates a new metadata extensions instance
func NewMetadataExtensions(manager *rpc.MetadataManager) *MetadataExtensions {
	return &MetadataExtensions{
		manager: manager,
	}
}

// Utility functions for working with the metadata manager API

func (me *MetadataExtensions) setMetadata(torrentID int64, key string, value interface{}, source string, tags []string) error {
	request := &rpc.MetadataRequest{
		TorrentID: torrentID,
		Set: map[string]interface{}{
			key: value,
		},
		Source: source,
		Tags:   tags,
	}

	response := me.manager.SetMetadata(request)
	if !response.Success {
		return fmt.Errorf("failed to set metadata: %v", response.Errors)
	}

	return nil
}

func (me *MetadataExtensions) getMetadata(torrentID int64, key string) (interface{}, bool) {
	query := &rpc.MetadataQuery{
		TorrentIDs: []int64{torrentID},
		Keys:       []string{key},
	}

	results := me.manager.GetMetadata(query)
	if torrentMeta, exists := results[torrentID]; exists {
		if metadata, exists := torrentMeta.Metadata[key]; exists {
			return metadata.Value, true
		}
	}

	return nil, false
}

func (me *MetadataExtensions) getAllTorrentIDs() []int64 {
	// Get all metadata by querying without filters
	results := me.manager.GetMetadata(&rpc.MetadataQuery{})

	var torrentIDs []int64
	for torrentID := range results {
		torrentIDs = append(torrentIDs, torrentID)
	}

	return torrentIDs
}

// Category Management

func (me *MetadataExtensions) SetTorrentCategory(torrentID int64, category, subcategory string, tags []string, contentType string) error {
	categoryData := map[string]interface{}{
		"category":     category,
		"subcategory":  subcategory,
		"tags":         tags,
		"content_type": contentType,
		"updated_at":   time.Now().Unix(),
	}

	return me.setMetadata(torrentID, "category", categoryData, "category_manager", []string{"category"})
}

func (me *MetadataExtensions) GetTorrentsByCategory(category string) []int64 {
	var result []int64

	for _, torrentID := range me.getAllTorrentIDs() {
		if categoryData, exists := me.getMetadata(torrentID, "category"); exists {
			if catMap, ok := categoryData.(map[string]interface{}); ok {
				if cat, ok := catMap["category"].(string); ok && cat == category {
					result = append(result, torrentID)
				}
			}
		}
	}

	return result
}

func (me *MetadataExtensions) GetTorrentsByTag(tag string) []int64 {
	var result []int64

	for _, torrentID := range me.getAllTorrentIDs() {
		if categoryData, exists := me.getMetadata(torrentID, "category"); exists {
			if catMap, ok := categoryData.(map[string]interface{}); ok {
				if tagsSlice, ok := catMap["tags"].([]interface{}); ok {
					for _, t := range tagsSlice {
						if tagStr, ok := t.(string); ok && tagStr == tag {
							result = append(result, torrentID)
							break
						}
					}
				}
			}
		}
	}

	return result
}

// Quality Management

func (me *MetadataExtensions) SetTorrentQuality(torrentID int64, videoQuality, audioQuality, encoding, resolution string, bitrate int, userRating float64) error {
	qualityData := map[string]interface{}{
		"video_quality":    videoQuality,
		"audio_quality":    audioQuality,
		"encoding":         encoding,
		"resolution":       resolution,
		"bitrate":          bitrate,
		"user_rating":      userRating,
		"community_rating": 0.0,
		"updated_at":       time.Now().Unix(),
	}

	// Calculate size category based on bitrate
	if bitrate > 0 {
		sizeCategory := "unknown"
		if bitrate < 1000000 {
			sizeCategory = "small"
		} else if bitrate < 5000000 {
			sizeCategory = "medium"
		} else {
			sizeCategory = "large"
		}
		qualityData["size_category"] = sizeCategory
	}

	return me.setMetadata(torrentID, "quality", qualityData, "quality_manager", []string{"quality"})
}

func (me *MetadataExtensions) UpdateCommunityRating(torrentID int64, rating float64) error {
	qualityData := map[string]interface{}{
		"community_rating": rating,
		"updated_at":       time.Now().Unix(),
	}

	// Get existing quality metadata to preserve other fields
	if existingData, exists := me.getMetadata(torrentID, "quality"); exists {
		if existingMap, ok := existingData.(map[string]interface{}); ok {
			// Copy existing fields
			for key, value := range existingMap {
				if key != "community_rating" && key != "updated_at" {
					qualityData[key] = value
				}
			}
		}
	}

	return me.setMetadata(torrentID, "quality", qualityData, "community_rating", []string{"quality", "rating"})
}

// GetHighestRatedTorrents returns torrent IDs with community ratings above minRating,
// sorted by rating in descending order and limited to the specified count.
func (me *MetadataExtensions) GetHighestRatedTorrents(minRating float64, limit int) []int64 {
	rated := me.collectRatedTorrents(minRating)
	sortTorrentsByRating(rated)
	return extractTopTorrentIDs(rated, limit)
}

// ratedTorrent represents a torrent with its associated rating.
type ratedTorrent struct {
	ID     int64
	Rating float64
}

// collectRatedTorrents gathers all torrents with ratings at or above the minimum threshold.
// It iterates through all torrents and extracts their community ratings from quality metadata.
func (me *MetadataExtensions) collectRatedTorrents(minRating float64) []ratedTorrent {
	var rated []ratedTorrent

	for _, torrentID := range me.getAllTorrentIDs() {
		if rating, ok := me.extractTorrentRating(torrentID); ok && rating >= minRating {
			rated = append(rated, ratedTorrent{ID: torrentID, Rating: rating})
		}
	}

	return rated
}

// extractTorrentRating retrieves the community rating for a specific torrent.
// Returns the rating and true if found, or 0.0 and false if not available.
func (me *MetadataExtensions) extractTorrentRating(torrentID int64) (float64, bool) {
	qualityData, exists := me.getMetadata(torrentID, "quality")
	if !exists {
		return 0.0, false
	}

	qualityMap, ok := qualityData.(map[string]interface{})
	if !ok {
		return 0.0, false
	}

	rating, ok := qualityMap["community_rating"].(float64)
	if !ok {
		return 0.0, false
	}

	return rating, true
}

// sortTorrentsByRating sorts the rated torrents by rating in descending order using bubble sort.
func sortTorrentsByRating(rated []ratedTorrent) {
	for i := 0; i < len(rated)-1; i++ {
		for j := i + 1; j < len(rated); j++ {
			if rated[i].Rating < rated[j].Rating {
				rated[i], rated[j] = rated[j], rated[i]
			}
		}
	}
}

// extractTopTorrentIDs extracts torrent IDs from the rated list up to the specified limit.
func extractTopTorrentIDs(rated []ratedTorrent, limit int) []int64 {
	var result []int64
	for i, rt := range rated {
		if i >= limit {
			break
		}
		result = append(result, rt.ID)
	}
	return result
}

// Tracking Management

func (me *MetadataExtensions) StartTorrentTracking(torrentID int64, sourceTracker string) error {
	trackingData := map[string]interface{}{
		"download_start": time.Now().Unix(),
		"first_seen":     time.Now().Unix(),
		"last_activity":  time.Now().Unix(),
		"retry_count":    0,
		"error_history":  []string{},
		"source_tracker": sourceTracker,
	}

	return me.setMetadata(torrentID, "tracking", trackingData, "tracking_manager", []string{"tracking"})
}

func (me *MetadataExtensions) UpdateTorrentActivity(torrentID int64) error {
	trackingData := map[string]interface{}{
		"last_activity": time.Now().Unix(),
	}

	// Get existing tracking data to preserve other fields
	if existingData, exists := me.getMetadata(torrentID, "tracking"); exists {
		if existingMap, ok := existingData.(map[string]interface{}); ok {
			// Copy existing fields
			for key, value := range existingMap {
				if key != "last_activity" {
					trackingData[key] = value
				}
			}
		}
	}

	return me.setMetadata(torrentID, "tracking", trackingData, "activity_tracker", []string{"tracking", "activity"})
}

func (me *MetadataExtensions) CompleteTorrentDownload(torrentID int64) error {
	updateData := map[string]interface{}{
		"download_complete": time.Now().Unix(),
		"last_activity":     time.Now().Unix(),
	}

	// Get existing tracking data to preserve other fields
	if existingData, exists := me.getMetadata(torrentID, "tracking"); exists {
		if existingMap, ok := existingData.(map[string]interface{}); ok {
			// Copy existing fields
			for key, value := range existingMap {
				if key != "download_complete" && key != "last_activity" {
					updateData[key] = value
				}
			}
		}
	}

	return me.setMetadata(torrentID, "tracking", updateData, "completion_tracker", []string{"tracking", "completed"})
}

// RecordTorrentError records an error occurrence for a torrent and updates tracking metadata.
// It increments the retry count, adds the error to history, and maintains a rolling history of the last 10 errors.
func (me *MetadataExtensions) RecordTorrentError(torrentID int64, errorMsg string) error {
	trackingData, err := me.loadTrackingData(torrentID)
	if err != nil {
		return err
	}

	incrementRetryCount(trackingData)
	updateErrorHistory(trackingData, errorMsg)
	updateLastActivity(trackingData)

	return me.setMetadata(torrentID, "tracking", trackingData, "error_tracker", []string{"tracking", "error"})
}

// loadTrackingData retrieves and validates the tracking metadata for a torrent.
// Returns an error if the tracking data is not found or invalid.
func (me *MetadataExtensions) loadTrackingData(torrentID int64) (map[string]interface{}, error) {
	existingData, exists := me.getMetadata(torrentID, "tracking")
	if !exists {
		return nil, fmt.Errorf("tracking data not found for torrent %d", torrentID)
	}

	trackingMap, ok := existingData.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("tracking data not found for torrent %d", torrentID)
	}

	return trackingMap, nil
}

// incrementRetryCount increases the retry count in the tracking data by one.
// Handles both float64 and int representations from metadata storage.
func incrementRetryCount(trackingData map[string]interface{}) {
	retryCount := 0
	if count, ok := trackingData["retry_count"].(float64); ok {
		retryCount = int(count) + 1
	} else if count, ok := trackingData["retry_count"].(int); ok {
		retryCount = count + 1
	}
	trackingData["retry_count"] = retryCount
}

// updateErrorHistory adds a new error to the error history with a timestamp.
// Maintains a rolling history of the last 10 errors.
func updateErrorHistory(trackingData map[string]interface{}, errorMsg string) {
	errorHistory := extractErrorHistory(trackingData)
	errorEntry := formatErrorEntry(errorMsg)
	errorHistory = append(errorHistory, errorEntry)
	errorHistory = limitErrorHistory(errorHistory, 10)
	trackingData["error_history"] = errorHistory
}

// extractErrorHistory retrieves the existing error history from tracking data.
// Returns an empty slice if no history exists or if the data format is invalid.
func extractErrorHistory(trackingData map[string]interface{}) []string {
	errorHistory := []string{}
	if history, ok := trackingData["error_history"].([]interface{}); ok {
		for _, err := range history {
			if errStr, ok := err.(string); ok {
				errorHistory = append(errorHistory, errStr)
			}
		}
	}
	return errorHistory
}

// formatErrorEntry creates a timestamped error entry string.
func formatErrorEntry(errorMsg string) string {
	return fmt.Sprintf("%d: %s", time.Now().Unix(), errorMsg)
}

// limitErrorHistory ensures the error history does not exceed the specified maximum size.
// Keeps only the most recent entries.
func limitErrorHistory(history []string, maxSize int) []string {
	if len(history) > maxSize {
		return history[len(history)-maxSize:]
	}
	return history
}

// updateLastActivity sets the last activity timestamp to the current time.
func updateLastActivity(trackingData map[string]interface{}) {
	trackingData["last_activity"] = time.Now().Unix()
}

func (me *MetadataExtensions) GetDownloadDuration(torrentID int64) (time.Duration, error) {
	trackingData, exists := me.getMetadata(torrentID, "tracking")
	if !exists {
		return 0, fmt.Errorf("tracking data not found for torrent %d", torrentID)
	}

	trackingMap, ok := trackingData.(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid tracking data format")
	}

	var startTime, endTime time.Time

	if start, ok := trackingMap["download_start"].(float64); ok {
		startTime = time.Unix(int64(start), 0)
	} else {
		return 0, fmt.Errorf("download start time not found")
	}

	if end, ok := trackingMap["download_complete"].(float64); ok {
		endTime = time.Unix(int64(end), 0)
	} else {
		endTime = time.Now() // If not completed, use current time
	}

	return endTime.Sub(startTime), nil
}

// Preferences Management

func (me *MetadataExtensions) SetTorrentPreferences(torrentID int64, autoStart bool, priority string, bandwidthLimit int, seedRatioLimit float64, downloadLocation string) error {
	preferences := map[string]interface{}{
		"auto_start":        autoStart,
		"priority":          priority,
		"bandwidth_limit":   bandwidthLimit,
		"seed_ratio_limit":  seedRatioLimit,
		"seed_time_limit":   int64(24 * time.Hour / time.Second), // Store as seconds
		"download_location": downloadLocation,
		"custom_flags":      map[string]interface{}{},
		"updated_at":        time.Now().Unix(),
	}

	return me.setMetadata(torrentID, "preferences", preferences, "preferences_manager", []string{"preferences"})
}

func (me *MetadataExtensions) SetCustomFlag(torrentID int64, flagName string, flagValue interface{}) error {
	// Get existing preferences data
	preferencesData := map[string]interface{}{
		"custom_flags": map[string]interface{}{},
		"updated_at":   time.Now().Unix(),
	}

	if existingData, exists := me.getMetadata(torrentID, "preferences"); exists {
		if existingMap, ok := existingData.(map[string]interface{}); ok {
			// Copy existing fields
			for key, value := range existingMap {
				if key != "updated_at" {
					preferencesData[key] = value
				}
			}
		}
	}

	// Update custom flags
	customFlags := map[string]interface{}{}
	if flags, ok := preferencesData["custom_flags"].(map[string]interface{}); ok {
		customFlags = flags
	}

	customFlags[flagName] = flagValue
	preferencesData["custom_flags"] = customFlags
	preferencesData["updated_at"] = time.Now().Unix()

	return me.setMetadata(torrentID, "preferences", preferencesData, "custom_flags", []string{"preferences", "custom"})
}

func (me *MetadataExtensions) GetTorrentsWithCustomFlag(flagName string) []int64 {
	var result []int64

	for _, torrentID := range me.getAllTorrentIDs() {
		if preferencesData, exists := me.getMetadata(torrentID, "preferences"); exists {
			if prefsMap, ok := preferencesData.(map[string]interface{}); ok {
				if customFlags, ok := prefsMap["custom_flags"].(map[string]interface{}); ok {
					if _, hasFlag := customFlags[flagName]; hasFlag {
						result = append(result, torrentID)
					}
				}
			}
		}
	}

	return result
}

// Report Generation

// GenerateMetadataReport creates a comprehensive metadata report for all torrents.
// This method coordinates the collection and formatting of various statistics
// into a human-readable report format.
func (me *MetadataExtensions) GenerateMetadataReport() string {
	torrentIDs := me.getAllTorrentIDs()

	report := "=== Advanced Metadata Report ===\n\n"
	report += fmt.Sprintf("Total Torrents: %d\n\n", len(torrentIDs))

	// Collect category and tag statistics
	categoryStats, tagStats := me.collectCategoryStatistics(torrentIDs)
	report += me.formatCategoryReport(categoryStats, tagStats)

	// Collect quality statistics
	qualityStats := me.collectQualityStatistics(torrentIDs)
	report += me.formatQualityReport(qualityStats)

	// Collect download status statistics
	statusStats := me.collectDownloadStatusStatistics(torrentIDs)
	report += me.formatDownloadStatusReport(statusStats)

	return report
}

// collectCategoryStatistics gathers category and tag distribution data from all torrents.
// Returns separate maps for category counts and tag counts.
func (me *MetadataExtensions) collectCategoryStatistics(torrentIDs []int64) (map[string]int, map[string]int) {
	categoryStats := make(map[string]int)
	tagStats := make(map[string]int)

	for _, torrentID := range torrentIDs {
		me.processTorrentCategories(torrentID, categoryStats, tagStats)
	}

	return categoryStats, tagStats
}

// processTorrentCategories extracts and counts category and tag data for a single torrent.
// Updates the provided category and tag statistics maps in place.
func (me *MetadataExtensions) processTorrentCategories(torrentID int64, categoryStats, tagStats map[string]int) {
	categoryData, exists := me.getMetadata(torrentID, "category")
	if !exists {
		return
	}

	catMap := extractCategoryMap(categoryData)
	if catMap == nil {
		return
	}

	incrementCategoryCount(catMap, categoryStats)
	incrementTagCounts(catMap, tagStats)
}

// extractCategoryMap converts metadata to a category map with type validation.
// Returns nil if the data cannot be converted to the expected map type.
func extractCategoryMap(categoryData interface{}) map[string]interface{} {
	catMap, ok := categoryData.(map[string]interface{})
	if !ok {
		return nil
	}
	return catMap
}

// incrementCategoryCount extracts the category name from the map and increments its count.
func incrementCategoryCount(catMap map[string]interface{}, categoryStats map[string]int) {
	category, ok := catMap["category"].(string)
	if ok {
		categoryStats[category]++
	}
}

// incrementTagCounts extracts tags from the category map and increments their counts.
// Handles the tags slice type assertion and iterates through individual tag strings.
func incrementTagCounts(catMap map[string]interface{}, tagStats map[string]int) {
	tags, ok := catMap["tags"].([]interface{})
	if !ok {
		return
	}

	for _, tag := range tags {
		if tagStr, ok := tag.(string); ok {
			tagStats[tagStr]++
		}
	}
}

// formatCategoryReport formats category and tag statistics into a readable report section.
func (me *MetadataExtensions) formatCategoryReport(categoryStats, tagStats map[string]int) string {
	report := "Category Distribution:\n"
	for category, count := range categoryStats {
		report += fmt.Sprintf("  %s: %d\n", category, count)
	}
	report += "\n"

	report += "Tag Distribution:\n"
	for tag, count := range tagStats {
		report += fmt.Sprintf("  %s: %d\n", tag, count)
	}
	report += "\n"

	return report
}

// collectQualityStatistics analyzes quality ratings across all torrents.
// Categorizes torrents into quality tiers based on community ratings.
func (me *MetadataExtensions) collectQualityStatistics(torrentIDs []int64) map[string]int {
	qualityStats := map[string]int{
		"high_quality":   0,
		"medium_quality": 0,
		"low_quality":    0,
		"unrated":        0,
	}

	for _, torrentID := range torrentIDs {
		processTorrentQuality(me, torrentID, qualityStats)
	}

	return qualityStats
}

// processTorrentQuality extracts and categorizes quality rating for a single torrent.
// Updates the quality statistics map in place based on the torrent's community rating.
func processTorrentQuality(me *MetadataExtensions, torrentID int64, qualityStats map[string]int) {
	rating, found := extractQualityRating(me, torrentID)
	if !found {
		qualityStats["unrated"]++
		return
	}

	tier := categorizeByRating(rating)
	qualityStats[tier]++
}

// extractQualityRating retrieves the community rating for a torrent.
// Returns the rating value and true if found, or 0.0 and false if not available.
func extractQualityRating(me *MetadataExtensions, torrentID int64) (float64, bool) {
	qualityData, exists := me.getMetadata(torrentID, "quality")
	if !exists {
		return 0.0, false
	}

	qualityMap, ok := qualityData.(map[string]interface{})
	if !ok {
		return 0.0, false
	}

	rating, ok := qualityMap["community_rating"].(float64)
	if !ok {
		return 0.0, false
	}

	return rating, true
}

// categorizeByRating determines the quality tier based on rating value.
// Returns one of: "high_quality" (>=4.0), "medium_quality" (>=2.5), or "low_quality" (<2.5).
func categorizeByRating(rating float64) string {
	if rating >= 4.0 {
		return "high_quality"
	}
	if rating >= 2.5 {
		return "medium_quality"
	}
	return "low_quality"
}

// formatQualityReport formats quality statistics into a readable report section.
func (me *MetadataExtensions) formatQualityReport(qualityStats map[string]int) string {
	report := "Quality Distribution:\n"
	for quality, count := range qualityStats {
		report += fmt.Sprintf("  %s: %d\n", quality, count)
	}
	report += "\n"

	return report
}

// collectDownloadStatusStatistics analyzes download completion status across all torrents.
// Categorizes torrents by their current download state and error status.
func (me *MetadataExtensions) collectDownloadStatusStatistics(torrentIDs []int64) map[string]int {
	statusStats := map[string]int{
		"completed":   0,
		"in_progress": 0,
		"errored":     0,
	}

	for _, torrentID := range torrentIDs {
		if trackingData, exists := me.getMetadata(torrentID, "tracking"); exists {
			if trackingMap, ok := trackingData.(map[string]interface{}); ok {
				if _, hasComplete := trackingMap["download_complete"]; hasComplete {
					statusStats["completed"]++
				} else {
					if retries, ok := trackingMap["retry_count"].(float64); ok && retries > 0 {
						statusStats["errored"]++
					} else if retries, ok := trackingMap["retry_count"].(int); ok && retries > 0 {
						statusStats["errored"]++
					} else {
						statusStats["in_progress"]++
					}
				}
			}
		}
	}

	return statusStats
}

// formatDownloadStatusReport formats download status statistics into a readable report section.
func (me *MetadataExtensions) formatDownloadStatusReport(statusStats map[string]int) string {
	report := "Download Status:\n"
	report += fmt.Sprintf("  Completed: %d\n", statusStats["completed"])
	report += fmt.Sprintf("  In Progress: %d\n", statusStats["in_progress"])
	report += fmt.Sprintf("  With Errors: %d\n", statusStats["errored"])

	return report
}

// Example usage and demonstration

func demonstrateAdvancedMetadata() {
	fmt.Println("=== Advanced Metadata Management Integration Example ===")

	// Create metadata manager
	manager := rpc.NewMetadataManager()
	extensions := NewMetadataExtensions(manager)

	// Set up constraints for validation
	constraints := rpc.MetadataConstraints{
		MaxKeyLength:      100,
		MaxValueSize:      10240, // 10KB
		MaxKeysPerTorrent: 50,
		AllowedTypes:      []string{"string", "int", "int64", "float64", "bool", "map", "slice"},
		RequiredKeys:      []string{},
		ReadOnlyKeys:      []string{},
	}
	manager.SetConstraints(constraints)

	// Simulate various torrents with comprehensive metadata
	fmt.Println("\n--- Setting Up Sample Torrents ---")

	// Movie torrent
	torrentID1 := int64(1001)
	extensions.SetTorrentCategory(torrentID1, "movies", "action", []string{"sci-fi", "blockbuster", "2024"}, "video")
	extensions.SetTorrentQuality(torrentID1, "4K", "DTS-HD", "H.265", "3840x2160", 15000000, 4.5)
	extensions.StartTorrentTracking(torrentID1, "tracker.movies.com")
	extensions.SetTorrentPreferences(torrentID1, true, "high", 0, 2.0, "/movies")
	extensions.SetCustomFlag(torrentID1, "watchlist", true)
	extensions.SetCustomFlag(torrentID1, "imdb_id", "tt1234567")

	// TV Series torrent
	torrentID2 := int64(1002)
	extensions.SetTorrentCategory(torrentID2, "tv", "drama", []string{"netflix", "season-1", "complete"}, "video")
	extensions.SetTorrentQuality(torrentID2, "1080p", "AC3", "H.264", "1920x1080", 5000000, 4.2)
	extensions.StartTorrentTracking(torrentID2, "tracker.series.com")
	extensions.SetTorrentPreferences(torrentID2, true, "normal", 5000000, 1.5, "/tv-shows")
	extensions.SetCustomFlag(torrentID2, "season", 1)
	extensions.SetCustomFlag(torrentID2, "episodes", 10)

	// Music torrent
	torrentID3 := int64(1003)
	extensions.SetTorrentCategory(torrentID3, "music", "electronic", []string{"ambient", "2023", "flac"}, "audio")
	extensions.SetTorrentQuality(torrentID3, "", "FLAC", "Lossless", "", 1411000, 4.0)
	extensions.StartTorrentTracking(torrentID3, "tracker.music.com")
	extensions.SetTorrentPreferences(torrentID3, false, "low", 1000000, 3.0, "/music")
	extensions.SetCustomFlag(torrentID3, "artist", "Electronic Artist")
	extensions.SetCustomFlag(torrentID3, "album", "Ambient Dreams")

	// Game torrent
	torrentID4 := int64(1004)
	extensions.SetTorrentCategory(torrentID4, "games", "pc", []string{"rpg", "indie", "steam"}, "software")
	extensions.StartTorrentTracking(torrentID4, "tracker.games.com")
	extensions.SetTorrentPreferences(torrentID4, false, "normal", 0, 1.0, "/games")
	extensions.SetCustomFlag(torrentID4, "platform", "PC")
	extensions.SetCustomFlag(torrentID4, "steam_id", "12345")

	// Simulate some activity
	fmt.Println("\n--- Simulating Torrent Activity ---")

	// Complete first torrent
	time.Sleep(100 * time.Millisecond)
	extensions.UpdateTorrentActivity(torrentID1)
	extensions.CompleteTorrentDownload(torrentID1)
	extensions.UpdateCommunityRating(torrentID1, 4.7)

	// Add some errors to second torrent
	extensions.RecordTorrentError(torrentID2, "connection timeout")
	extensions.RecordTorrentError(torrentID2, "tracker unreachable")
	extensions.UpdateTorrentActivity(torrentID2)

	// Complete third torrent
	time.Sleep(50 * time.Millisecond)
	extensions.UpdateTorrentActivity(torrentID3)
	extensions.CompleteTorrentDownload(torrentID3)

	// Fourth torrent still in progress
	extensions.UpdateTorrentActivity(torrentID4)

	fmt.Println("\n--- Demonstrating Search Capabilities ---")

	// Search by category
	movieTorrents := extensions.GetTorrentsByCategory("movies")
	fmt.Printf("Movie torrents: %v\n", movieTorrents)

	// Search by tag
	flacTorrents := extensions.GetTorrentsByTag("flac")
	fmt.Printf("FLAC torrents: %v\n", flacTorrents)

	// Get highest rated torrents
	highRated := extensions.GetHighestRatedTorrents(4.0, 5)
	fmt.Printf("Highest rated torrents: %v\n", highRated)

	// Get torrents with custom flags
	watchlistTorrents := extensions.GetTorrentsWithCustomFlag("watchlist")
	fmt.Printf("Watchlist torrents: %v\n", watchlistTorrents)

	fmt.Println("\n--- Metadata Analysis ---")

	// Show download durations
	for _, torrentID := range []int64{torrentID1, torrentID2, torrentID3, torrentID4} {
		if duration, err := extensions.GetDownloadDuration(torrentID); err == nil {
			fmt.Printf("Torrent %d download duration: %v\n", torrentID, duration.Truncate(time.Millisecond))
		}
	}

	// Show detailed metadata for one torrent
	fmt.Printf("\n--- Detailed Metadata for Torrent %d ---\n", torrentID1)

	query := &rpc.MetadataQuery{
		TorrentIDs: []int64{torrentID1},
	}
	results := manager.GetMetadata(query)

	if torrentMeta, exists := results[torrentID1]; exists {
		for key, metadata := range torrentMeta.Metadata {
			valueJSON, _ := json.MarshalIndent(metadata.Value, "", "  ")
			fmt.Printf("%s: %s (Type: %s, Source: %s, Tags: %v)\n",
				key, valueJSON, metadata.Type, metadata.Source, metadata.Tags)
		}
	}

	// Generate comprehensive report
	fmt.Println("\n--- Comprehensive Metadata Report ---")
	fmt.Println(extensions.GenerateMetadataReport())

	// Show metrics
	fmt.Println("\n--- Metadata Manager Metrics ---")
	metrics := manager.GetMetrics()
	fmt.Printf("Total Operations: %d\n", metrics.TotalOperations)
	fmt.Printf("Total Torrents: %d\n", metrics.TotalTorrents)
	fmt.Printf("Total Keys: %d\n", metrics.TotalKeys)
	fmt.Printf("Validation Errors: %d\n", metrics.ValidationErrors)
	fmt.Printf("Average Latency: %v\n", metrics.AverageLatency)
	fmt.Printf("Last Operation: %v\n", metrics.LastOperation.Format(time.RFC3339))

	fmt.Println("Advanced metadata management demonstration completed!")
}

// demonstrateAdvancedMetadata can be called from a main function to run the example
// func main() {
//     demonstrateAdvancedMetadata()
// }
