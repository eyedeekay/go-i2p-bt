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

// Package main demonstrates event-driven analytics integration
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-i2p/go-i2p-bt/rpc"
)

// AnalyticsSubscriber implements event-driven analytics for torrent statistics
type AnalyticsSubscriber struct {
	id      string
	filters []rpc.EventType

	// Analytics data
	mu    sync.RWMutex
	stats AnalyticsStats

	// Event tracking
	eventCounts map[rpc.EventType]int64
	lastEvents  map[rpc.EventType]time.Time

	// Performance tracking
	processingTimes   []time.Duration
	maxProcessingTime time.Duration
}

// AnalyticsStats holds comprehensive analytics data
type AnalyticsStats struct {
	// Torrent lifecycle stats
	TorrentsAdded     int64 `json:"torrents_added"`
	TorrentsStarted   int64 `json:"torrents_started"`
	TorrentsCompleted int64 `json:"torrents_completed"`
	TorrentsRemoved   int64 `json:"torrents_removed"`
	TorrentsErrored   int64 `json:"torrents_errored"`

	// Session stats
	SessionStarts int64 `json:"session_starts"`
	SessionStops  int64 `json:"session_stops"`
	ConfigChanges int64 `json:"config_changes"`

	// Performance stats
	TotalEvents        int64         `json:"total_events"`
	AvgProcessingTime  time.Duration `json:"avg_processing_time"`
	PeakProcessingTime time.Duration `json:"peak_processing_time"`

	// Time tracking
	FirstEvent time.Time     `json:"first_event"`
	LastEvent  time.Time     `json:"last_event"`
	Uptime     time.Duration `json:"uptime"`

	// Error tracking
	ProcessingErrors int64   `json:"processing_errors"`
	ErrorRate        float64 `json:"error_rate"`
}

// NewAnalyticsSubscriber creates a new analytics subscriber
func NewAnalyticsSubscriber() *AnalyticsSubscriber {
	return &AnalyticsSubscriber{
		id: "analytics-subscriber",
		filters: []rpc.EventType{
			rpc.EventTorrentAdded,
			rpc.EventTorrentStarted,
			rpc.EventTorrentCompleted,
			rpc.EventTorrentRemoved,
			rpc.EventTorrentError,
			rpc.EventSessionStarted,
			rpc.EventSessionStopped,
			rpc.EventSessionConfigChanged,
		},
		eventCounts:     make(map[rpc.EventType]int64),
		lastEvents:      make(map[rpc.EventType]time.Time),
		processingTimes: make([]time.Duration, 0, 1000),
		stats: AnalyticsStats{
			FirstEvent: time.Now(),
		},
	}
}

// EventSubscriber interface implementation

func (as *AnalyticsSubscriber) ID() string {
	return as.id
}

func (as *AnalyticsSubscriber) GetSubscriptionFilters() []rpc.EventType {
	return as.filters
}

func (as *AnalyticsSubscriber) HandleEvent(ctx context.Context, event *rpc.Event) error {
	start := time.Now()
	defer func() {
		processingTime := time.Since(start)
		as.recordProcessingTime(processingTime)
	}()

	as.mu.Lock()
	defer as.mu.Unlock()

	// Update general stats
	as.stats.TotalEvents++
	as.stats.LastEvent = time.Now()
	as.eventCounts[event.Type]++
	as.lastEvents[event.Type] = time.Now()

	// Set first event time if this is the first event
	if as.stats.FirstEvent.IsZero() {
		as.stats.FirstEvent = time.Now()
	}

	// Update uptime
	as.stats.Uptime = time.Since(as.stats.FirstEvent)

	// Process specific event types
	switch event.Type {
	case rpc.EventTorrentAdded:
		as.handleTorrentAdded(event)
	case rpc.EventTorrentStarted:
		as.handleTorrentStarted(event)
	case rpc.EventTorrentCompleted:
		as.handleTorrentCompleted(event)
	case rpc.EventTorrentRemoved:
		as.handleTorrentRemoved(event)
	case rpc.EventTorrentError:
		as.handleTorrentError(event)
	case rpc.EventSessionStarted:
		as.handleSessionStarted(event)
	case rpc.EventSessionStopped:
		as.handleSessionStopped(event)
	case rpc.EventSessionConfigChanged:
		as.handleConfigChanged(event)
	}

	// Simulate potential processing error for demonstration
	if event.Type == rpc.EventTorrentError {
		if errorData, ok := event.Data["simulate_processing_error"]; ok {
			if simulate, ok := errorData.(bool); ok && simulate {
				as.stats.ProcessingErrors++
				as.updateErrorRate()
				return fmt.Errorf("simulated processing error")
			}
		}
	}

	return nil
}

// Event handlers

func (as *AnalyticsSubscriber) handleTorrentAdded(event *rpc.Event) {
	as.stats.TorrentsAdded++

	log.Printf("[Analytics] Torrent added: %s", as.getTorrentInfo(event))

	// Track additional metadata if available
	if event.Data != nil {
		if source, ok := event.Data["source"]; ok {
			log.Printf("[Analytics] Torrent add source: %v", source)
		}
	}
}

func (as *AnalyticsSubscriber) handleTorrentStarted(event *rpc.Event) {
	as.stats.TorrentsStarted++

	log.Printf("[Analytics] Torrent started: %s", as.getTorrentInfo(event))
}

func (as *AnalyticsSubscriber) handleTorrentCompleted(event *rpc.Event) {
	as.stats.TorrentsCompleted++

	log.Printf("[Analytics] Torrent completed: %s", as.getTorrentInfo(event))

	// Calculate completion rate
	if as.stats.TorrentsStarted > 0 {
		completionRate := float64(as.stats.TorrentsCompleted) / float64(as.stats.TorrentsStarted) * 100
		log.Printf("[Analytics] Current completion rate: %.2f%%", completionRate)
	}
}

func (as *AnalyticsSubscriber) handleTorrentRemoved(event *rpc.Event) {
	as.stats.TorrentsRemoved++

	log.Printf("[Analytics] Torrent removed: %s", as.getTorrentInfo(event))
}

func (as *AnalyticsSubscriber) handleTorrentError(event *rpc.Event) {
	as.stats.TorrentsErrored++

	log.Printf("[Analytics] Torrent error: %s", as.getTorrentInfo(event))

	if event.Error != nil {
		log.Printf("[Analytics] Error details: %v", event.Error)
	}
}

func (as *AnalyticsSubscriber) handleSessionStarted(event *rpc.Event) {
	as.stats.SessionStarts++

	log.Printf("[Analytics] Session started from source: %s", event.Source)
}

func (as *AnalyticsSubscriber) handleSessionStopped(event *rpc.Event) {
	as.stats.SessionStops++

	log.Printf("[Analytics] Session stopped from source: %s", event.Source)
}

func (as *AnalyticsSubscriber) handleConfigChanged(event *rpc.Event) {
	as.stats.ConfigChanges++

	log.Printf("[Analytics] Configuration changed from source: %s", event.Source)

	if event.Data != nil {
		if changes, ok := event.Data["changes"]; ok {
			log.Printf("[Analytics] Config changes: %v", changes)
		}
	}
}

// Utility methods

func (as *AnalyticsSubscriber) getTorrentInfo(event *rpc.Event) string {
	if event.Torrent != nil {
		return fmt.Sprintf("ID=%d, Status=%d", event.Torrent.ID, event.Torrent.Status)
	}
	return "unknown torrent"
}

func (as *AnalyticsSubscriber) recordProcessingTime(duration time.Duration) {
	as.mu.Lock()
	defer as.mu.Unlock()

	// Record processing time
	as.processingTimes = append(as.processingTimes, duration)

	// Keep only recent measurements (last 1000)
	if len(as.processingTimes) > 1000 {
		as.processingTimes = as.processingTimes[len(as.processingTimes)-1000:]
	}

	// Update peak processing time
	if duration > as.maxProcessingTime {
		as.maxProcessingTime = duration
		as.stats.PeakProcessingTime = duration
	}

	// Calculate average processing time
	var total time.Duration
	for _, pt := range as.processingTimes {
		total += pt
	}
	as.stats.AvgProcessingTime = total / time.Duration(len(as.processingTimes))
}

func (as *AnalyticsSubscriber) updateErrorRate() {
	if as.stats.TotalEvents > 0 {
		as.stats.ErrorRate = float64(as.stats.ProcessingErrors) / float64(as.stats.TotalEvents) * 100
	}
}

// Public methods for accessing analytics data

func (as *AnalyticsSubscriber) GetStats() AnalyticsStats {
	as.mu.RLock()
	defer as.mu.RUnlock()

	// Update uptime before returning
	stats := as.stats
	stats.Uptime = time.Since(as.stats.FirstEvent)

	return stats
}

func (as *AnalyticsSubscriber) GetEventCounts() map[rpc.EventType]int64 {
	as.mu.RLock()
	defer as.mu.RUnlock()

	result := make(map[rpc.EventType]int64)
	for eventType, count := range as.eventCounts {
		result[eventType] = count
	}

	return result
}

func (as *AnalyticsSubscriber) GetLastEventTimes() map[rpc.EventType]time.Time {
	as.mu.RLock()
	defer as.mu.RUnlock()

	result := make(map[rpc.EventType]time.Time)
	for eventType, lastTime := range as.lastEvents {
		result[eventType] = lastTime
	}

	return result
}

func (as *AnalyticsSubscriber) GenerateReport() string {
	stats := as.GetStats()
	eventCounts := as.GetEventCounts()

	report := "=== Torrent Analytics Report ===\n\n"

	// Summary stats
	report += "Summary Statistics:\n"
	report += fmt.Sprintf("  Total Events Processed: %d\n", stats.TotalEvents)
	report += fmt.Sprintf("  Uptime: %v\n", stats.Uptime)
	report += fmt.Sprintf("  First Event: %v\n", stats.FirstEvent.Format(time.RFC3339))
	report += fmt.Sprintf("  Last Event: %v\n", stats.LastEvent.Format(time.RFC3339))
	report += "\n"

	// Torrent lifecycle stats
	report += "Torrent Lifecycle:\n"
	report += fmt.Sprintf("  Added: %d\n", stats.TorrentsAdded)
	report += fmt.Sprintf("  Started: %d\n", stats.TorrentsStarted)
	report += fmt.Sprintf("  Completed: %d\n", stats.TorrentsCompleted)
	report += fmt.Sprintf("  Removed: %d\n", stats.TorrentsRemoved)
	report += fmt.Sprintf("  Errored: %d\n", stats.TorrentsErrored)

	// Completion rate
	if stats.TorrentsStarted > 0 {
		completionRate := float64(stats.TorrentsCompleted) / float64(stats.TorrentsStarted) * 100
		report += fmt.Sprintf("  Completion Rate: %.2f%%\n", completionRate)
	}
	report += "\n"

	// Session stats
	report += "Session Activity:\n"
	report += fmt.Sprintf("  Session Starts: %d\n", stats.SessionStarts)
	report += fmt.Sprintf("  Session Stops: %d\n", stats.SessionStops)
	report += fmt.Sprintf("  Config Changes: %d\n", stats.ConfigChanges)
	report += "\n"

	// Performance stats
	report += "Performance Metrics:\n"
	report += fmt.Sprintf("  Average Processing Time: %v\n", stats.AvgProcessingTime)
	report += fmt.Sprintf("  Peak Processing Time: %v\n", stats.PeakProcessingTime)
	report += fmt.Sprintf("  Processing Errors: %d\n", stats.ProcessingErrors)
	report += fmt.Sprintf("  Error Rate: %.4f%%\n", stats.ErrorRate)
	report += "\n"

	// Event type breakdown
	report += "Event Type Breakdown:\n"
	for eventType, count := range eventCounts {
		report += fmt.Sprintf("  %s: %d\n", eventType, count)
	}

	return report
}

// Real-time analytics dashboard simulation

func (as *AnalyticsSubscriber) StartDashboard(ctx context.Context, updateInterval time.Duration) {
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				as.printDashboard()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (as *AnalyticsSubscriber) printDashboard() {
	stats := as.GetStats()

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("             REAL-TIME ANALYTICS DASHBOARD")
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("Uptime: %-20v Total Events: %-10d Error Rate: %.2f%%\n",
		stats.Uptime.Truncate(time.Second), stats.TotalEvents, stats.ErrorRate)

	fmt.Printf("Added: %-5d Started: %-5d Completed: %-5d Removed: %-5d\n",
		stats.TorrentsAdded, stats.TorrentsStarted, stats.TorrentsCompleted, stats.TorrentsRemoved)

	if stats.TorrentsStarted > 0 {
		completionRate := float64(stats.TorrentsCompleted) / float64(stats.TorrentsStarted) * 100
		fmt.Printf("Completion Rate: %.1f%%\n", completionRate)
	}

	fmt.Printf("Avg Processing: %-10v Peak Processing: %-10v\n",
		stats.AvgProcessingTime.Truncate(time.Microsecond),
		stats.PeakProcessingTime.Truncate(time.Microsecond))

	fmt.Println(strings.Repeat("=", 60))
}

// Example usage and demonstration

func demonstrateEventAnalytics() {
	fmt.Println("=== Event-Driven Analytics Integration Example ===")

	// Create event notification system
	ens := rpc.NewEventNotificationSystem()
	ens.SetLogger(func(format string, args ...interface{}) {
		log.Printf("[EventSystem] "+format, args...)
	})

	// Create analytics subscriber
	analytics := NewAnalyticsSubscriber()

	// Subscribe analytics to events
	if err := ens.Subscribe(analytics); err != nil {
		log.Fatalf("Failed to subscribe analytics: %v", err)
	}

	// Start real-time dashboard
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	analytics.StartDashboard(ctx, 2*time.Second)

	// Simulate various events
	fmt.Println("\n--- Simulating Torrent Events ---")

	// Session start
	ens.PublishSessionEvent(rpc.EventSessionStarted, map[string]interface{}{
		"startup_time": time.Now(),
	})

	// Add some torrents
	for i := 1; i <= 5; i++ {
		torrent := &rpc.TorrentState{
			ID:          int64(i),
			Status:      rpc.TorrentStatusDownloading,
			DownloadDir: "/downloads",
		}

		ens.PublishTorrentEvent(rpc.EventTorrentAdded, torrent, map[string]interface{}{
			"source":   "user",
			"priority": "normal",
		})

		time.Sleep(200 * time.Millisecond) // Small delay for realistic timing
	}

	// Start some torrents
	for i := 1; i <= 5; i++ {
		torrent := &rpc.TorrentState{
			ID:     int64(i),
			Status: rpc.TorrentStatusDownloading,
		}

		ens.PublishTorrentEvent(rpc.EventTorrentStarted, torrent, map[string]interface{}{
			"auto_start": true,
		})

		time.Sleep(100 * time.Millisecond)
	}

	// Complete some torrents
	for i := 1; i <= 3; i++ {
		torrent := &rpc.TorrentState{
			ID:     int64(i),
			Status: rpc.TorrentStatusSeeding,
		}

		ens.PublishTorrentEvent(rpc.EventTorrentCompleted, torrent, map[string]interface{}{
			"completion_time": time.Now(),
			"size":            int64(i * 1024 * 1024), // i MB
		})

		time.Sleep(150 * time.Millisecond)
	}

	// Simulate some errors
	for i := 4; i <= 5; i++ {
		ens.PublishErrorEvent(rpc.EventTorrentError,
			fmt.Errorf("failed to download torrent %d", i),
			map[string]interface{}{
				"torrent_id":                i,
				"error_type":                "network_error",
				"simulate_processing_error": i == 5, // Simulate processing error for torrent 5
			})

		time.Sleep(100 * time.Millisecond)
	}

	// Configuration change
	ens.PublishSessionEvent(rpc.EventSessionConfigChanged, map[string]interface{}{
		"changes": map[string]interface{}{
			"download_dir": "/new/downloads",
			"max_peers":    100,
		},
	})

	// Wait for dashboard updates
	fmt.Println("\n--- Waiting for Real-time Updates ---")
	time.Sleep(5 * time.Second)

	// Generate and print final report
	fmt.Println("\n--- Final Analytics Report ---")
	fmt.Println(analytics.GenerateReport())

	// Show event counts
	fmt.Println("Event Count Details:")
	eventCounts := analytics.GetEventCounts()
	for eventType, count := range eventCounts {
		fmt.Printf("  %s: %d\n", eventType, count)
	}

	// Show last event times
	fmt.Println("\nLast Event Times:")
	lastEvents := analytics.GetLastEventTimes()
	for eventType, lastTime := range lastEvents {
		fmt.Printf("  %s: %v\n", eventType, lastTime.Format(time.RFC3339))
	}

	// Cleanup
	fmt.Println("\n--- Cleanup ---")

	if err := ens.Shutdown(); err != nil {
		log.Printf("Event system shutdown failed: %v", err)
	}

	fmt.Println("Event-driven analytics demonstration completed!")
}

// demonstrateEventAnalytics can be called from a main function to run the example
// func main() {
//     demonstrateEventAnalytics()
// }
