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

// EventType represents the type of event being broadcast
type EventType string

const (
	// Torrent lifecycle events
	EventTorrentAdded     EventType = "torrent.added"
	EventTorrentStarted   EventType = "torrent.started"
	EventTorrentStopped   EventType = "torrent.stopped"
	EventTorrentCompleted EventType = "torrent.completed"
	EventTorrentRemoved   EventType = "torrent.removed"
	EventTorrentError     EventType = "torrent.error"
	EventTorrentMetadata  EventType = "torrent.metadata"

	// Session events
	EventSessionStarted       EventType = "session.started"
	EventSessionStopped       EventType = "session.stopped"
	EventSessionConfigChanged EventType = "session.config_changed"

	// Peer events
	EventPeerConnected    EventType = "peer.connected"
	EventPeerDisconnected EventType = "peer.disconnected"

	// Download events
	EventDownloadStarted  EventType = "download.started"
	EventDownloadPaused   EventType = "download.paused"
	EventDownloadFinished EventType = "download.finished"

	// System events
	EventSystemShutdown EventType = "system.shutdown"
	EventSystemError    EventType = "system.error"
)

// getAllEventTypes returns all defined event types for global subscriber registration
func getAllEventTypes() []EventType {
	return []EventType{
		// Torrent lifecycle events
		EventTorrentAdded,
		EventTorrentStarted,
		EventTorrentStopped,
		EventTorrentCompleted,
		EventTorrentRemoved,
		EventTorrentError,
		EventTorrentMetadata,
		// Session events
		EventSessionStarted,
		EventSessionStopped,
		EventSessionConfigChanged,
		// Peer events
		EventPeerConnected,
		EventPeerDisconnected,
		// Download events
		EventDownloadStarted,
		EventDownloadPaused,
		EventDownloadFinished,
		// System events
		EventSystemShutdown,
		EventSystemError,
	}
}

// Event represents a notification event with metadata
type Event struct {
	// Type of the event
	Type EventType `json:"type"`
	// Unique identifier for this event instance
	ID string `json:"id"`
	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`
	// Source component that generated the event
	Source string `json:"source"`
	// Event payload data
	Data map[string]interface{} `json:"data"`
	// Torrent associated with this event (if applicable)
	Torrent *TorrentState `json:"torrent,omitempty"`
	// Error associated with this event (if applicable)
	Error error `json:"error,omitempty"`
}

// EventSubscriber represents a subscriber to event notifications
type EventSubscriber interface {
	// ID returns a unique identifier for this subscriber
	ID() string
	// HandleEvent processes an event notification
	// Returns an error if processing fails
	HandleEvent(ctx context.Context, event *Event) error
	// GetSubscriptionFilters returns event types this subscriber is interested in
	// Empty slice means subscribe to all events
	GetSubscriptionFilters() []EventType
}

// EventFilter represents criteria for filtering events
type EventFilter struct {
	// Event types to include (empty means all types)
	Types []EventType
	// Source components to include (empty means all sources)
	Sources []string
	// Minimum severity level for error events
	MinSeverity string
	// Custom filter function
	CustomFilter func(event *Event) bool
}

// SubscriberInfo contains metadata about a subscriber
type SubscriberInfo struct {
	ID            string      `json:"id"`
	Filters       []EventType `json:"filters"`
	Status        string      `json:"status"` // "active", "error", "removed"
	SubscribeTime time.Time   `json:"subscribe_time"`
	EventCount    int64       `json:"event_count"`
	ErrorCount    int64       `json:"error_count"`
	LastEvent     time.Time   `json:"last_event"`
	LastError     time.Time   `json:"last_error"`
}

// EventNotificationSystem manages event broadcasting and subscription
type EventNotificationSystem struct {
	mu sync.RWMutex

	// Subscriber management
	subscribers    map[string]EventSubscriber
	subscriberInfo map[string]*SubscriberInfo

	// Event filtering and routing
	eventFilters    map[string]*EventFilter
	typeSubscribers map[EventType][]string

	// Configuration
	bufferSize int
	timeout    time.Duration
	logger     func(format string, args ...interface{})

	// Metrics and monitoring
	metrics EventMetrics

	// Lifecycle management
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	eventChan      chan *Event
	workerWg       sync.WaitGroup
}

// EventMetrics tracks event system performance
type EventMetrics struct {
	TotalEvents       int64         `json:"total_events"`
	TotalSubscribers  int64         `json:"total_subscribers"`
	ActiveSubscribers int64         `json:"active_subscribers"`
	EventsDropped     int64         `json:"events_dropped"`
	AverageLatency    time.Duration `json:"average_latency"`
	LastEvent         time.Time     `json:"last_event"`
}

// NewEventNotificationSystem creates a new event notification system
func NewEventNotificationSystem() *EventNotificationSystem {
	ctx, cancel := context.WithCancel(context.Background())

	ens := &EventNotificationSystem{
		subscribers:     make(map[string]EventSubscriber),
		subscriberInfo:  make(map[string]*SubscriberInfo),
		eventFilters:    make(map[string]*EventFilter),
		typeSubscribers: make(map[EventType][]string),
		bufferSize:      1000,
		timeout:         30 * time.Second,
		logger:          func(format string, args ...interface{}) {}, // No-op logger by default
		shutdownCtx:     ctx,
		shutdownCancel:  cancel,
		eventChan:       make(chan *Event, 1000),
	}

	// Start event processing worker
	ens.startEventWorker()

	return ens
}

// SetLogger sets a custom logger for event system
func (ens *EventNotificationSystem) SetLogger(logger func(format string, args ...interface{})) {
	ens.mu.Lock()
	defer ens.mu.Unlock()
	ens.logger = logger
}

// SetBufferSize sets the event buffer size
func (ens *EventNotificationSystem) SetBufferSize(size int) {
	ens.mu.Lock()
	defer ens.mu.Unlock()
	ens.bufferSize = size
}

// SetTimeout sets the timeout for event processing
func (ens *EventNotificationSystem) SetTimeout(timeout time.Duration) {
	ens.mu.Lock()
	defer ens.mu.Unlock()
	ens.timeout = timeout
}

// Subscribe registers a new event subscriber
func (ens *EventNotificationSystem) Subscribe(subscriber EventSubscriber) error {
	if subscriber == nil {
		return fmt.Errorf("subscriber cannot be nil")
	}

	id := subscriber.ID()
	if id == "" {
		return fmt.Errorf("subscriber ID cannot be empty")
	}

	ens.mu.Lock()
	defer ens.mu.Unlock()

	// Check for duplicate subscriber IDs
	if _, exists := ens.subscribers[id]; exists {
		return fmt.Errorf("subscriber with ID '%s' already exists", id)
	}

	// Store subscriber and metadata
	ens.subscribers[id] = subscriber
	ens.subscriberInfo[id] = &SubscriberInfo{
		ID:            id,
		Filters:       subscriber.GetSubscriptionFilters(),
		Status:        "active",
		SubscribeTime: time.Now(),
	}

	// Register in type-based lookup for efficient routing
	filters := subscriber.GetSubscriptionFilters()
	if len(filters) == 0 {
		// Subscribe to all event types (global subscriber)
		allEventTypes := getAllEventTypes()
		for _, eventType := range allEventTypes {
			if ens.typeSubscribers[eventType] == nil {
				ens.typeSubscribers[eventType] = make([]string, 0)
			}
			ens.typeSubscribers[eventType] = append(ens.typeSubscribers[eventType], id)
		}
	} else {
		// Subscribe to specific event types
		for _, eventType := range filters {
			if ens.typeSubscribers[eventType] == nil {
				ens.typeSubscribers[eventType] = make([]string, 0)
			}
			ens.typeSubscribers[eventType] = append(ens.typeSubscribers[eventType], id)
		}
	}

	ens.metrics.TotalSubscribers++
	ens.metrics.ActiveSubscribers++

	ens.logger("Subscribed event listener '%s' with filters: %v", id, filters)
	return nil
}

// Unsubscribe removes an event subscriber
func (ens *EventNotificationSystem) Unsubscribe(subscriberID string) error {
	if subscriberID == "" {
		return fmt.Errorf("subscriber ID cannot be empty")
	}

	ens.mu.Lock()
	defer ens.mu.Unlock()

	subscriber, exists := ens.subscribers[subscriberID]
	if !exists {
		return fmt.Errorf("subscriber with ID '%s' not found", subscriberID)
	}

	// Remove from type-based lookup
	filters := subscriber.GetSubscriptionFilters()
	if len(filters) == 0 {
		// Was subscribed to all types
		for eventType := range ens.typeSubscribers {
			ens.removeSubscriberFromType(eventType, subscriberID)
		}
	} else {
		// Was subscribed to specific types
		for _, eventType := range filters {
			ens.removeSubscriberFromType(eventType, subscriberID)
		}
	}

	// Remove from main registry
	delete(ens.subscribers, subscriberID)
	if info, exists := ens.subscriberInfo[subscriberID]; exists {
		info.Status = "removed"
		delete(ens.subscriberInfo, subscriberID)
	}

	ens.metrics.ActiveSubscribers--

	ens.logger("Unsubscribed event listener '%s'", subscriberID)
	return nil
}

// PublishEvent broadcasts an event to all relevant subscribers
func (ens *EventNotificationSystem) PublishEvent(event *Event) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}

	// Check if system is shut down
	select {
	case <-ens.shutdownCtx.Done():
		return fmt.Errorf("event notification system is shut down")
	default:
	}

	// Generate ID if not provided
	if event.ID == "" {
		event.ID = fmt.Sprintf("%d-%s", time.Now().UnixNano(), event.Type)
	}

	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case ens.eventChan <- event:
		ens.mu.Lock()
		ens.metrics.TotalEvents++
		ens.metrics.LastEvent = time.Now()
		ens.mu.Unlock()
		return nil
	case <-ens.shutdownCtx.Done():
		return fmt.Errorf("event notification system is shut down")
	default:
		// Channel is full, drop event
		ens.mu.Lock()
		ens.metrics.EventsDropped++
		ens.mu.Unlock()
		ens.logger("Event dropped due to full buffer: %s", event.Type)
		return fmt.Errorf("event buffer full, event dropped")
	}
}

// PublishTorrentEvent is a convenience method for publishing torrent-related events
func (ens *EventNotificationSystem) PublishTorrentEvent(eventType EventType, torrent *TorrentState, data map[string]interface{}) error {
	event := &Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    "torrent_manager",
		Data:      data,
		Torrent:   torrent,
	}
	return ens.PublishEvent(event)
}

// PublishSessionEvent is a convenience method for publishing session-related events
func (ens *EventNotificationSystem) PublishSessionEvent(eventType EventType, data map[string]interface{}) error {
	event := &Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    "session_manager",
		Data:      data,
	}
	return ens.PublishEvent(event)
}

// PublishErrorEvent is a convenience method for publishing error events
func (ens *EventNotificationSystem) PublishErrorEvent(eventType EventType, err error, data map[string]interface{}) error {
	event := &Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    "system",
		Data:      data,
		Error:     err,
	}
	return ens.PublishEvent(event)
}

// GetSubscriberInfo returns information about all subscribers
func (ens *EventNotificationSystem) GetSubscriberInfo() map[string]*SubscriberInfo {
	ens.mu.RLock()
	defer ens.mu.RUnlock()

	result := make(map[string]*SubscriberInfo)
	for id, info := range ens.subscriberInfo {
		// Create a copy to avoid data races
		infoCopy := *info
		result[id] = &infoCopy
	}
	return result
}

// GetMetrics returns current event system metrics
func (ens *EventNotificationSystem) GetMetrics() EventMetrics {
	ens.mu.RLock()
	defer ens.mu.RUnlock()
	return ens.metrics
}

// ListSubscribersByType returns subscriber IDs grouped by event type
func (ens *EventNotificationSystem) ListSubscribersByType() map[EventType][]string {
	ens.mu.RLock()
	defer ens.mu.RUnlock()

	result := make(map[EventType][]string)
	for eventType, subscribers := range ens.typeSubscribers {
		if len(subscribers) > 0 {
			// Create a copy to avoid data races
			subscribersCopy := make([]string, len(subscribers))
			copy(subscribersCopy, subscribers)
			result[eventType] = subscribersCopy
		}
	}
	return result
}

// Shutdown gracefully shuts down the event notification system
func (ens *EventNotificationSystem) Shutdown() error {
	ens.logger("Shutting down event notification system...")

	// Cancel context to signal shutdown
	ens.shutdownCancel()

	// Close event channel to stop accepting new events
	close(ens.eventChan)

	// Wait for worker to finish processing remaining events
	ens.workerWg.Wait()

	ens.mu.Lock()
	defer ens.mu.Unlock()

	// Clear all registries
	ens.subscribers = make(map[string]EventSubscriber)
	ens.subscriberInfo = make(map[string]*SubscriberInfo)
	ens.eventFilters = make(map[string]*EventFilter)
	ens.typeSubscribers = make(map[EventType][]string)
	ens.metrics.ActiveSubscribers = 0

	ens.logger("Event notification system shutdown completed")
	return nil
}

// startEventWorker starts the background worker for processing events
func (ens *EventNotificationSystem) startEventWorker() {
	ens.workerWg.Add(1)
	go func() {
		defer ens.workerWg.Done()

		for {
			select {
			case event, ok := <-ens.eventChan:
				if !ok {
					// Channel closed, exit
					return
				}
				ens.processEvent(event)
			case <-ens.shutdownCtx.Done():
				// Shutdown requested
				return
			}
		}
	}()
}

// processEvent delivers an event to all relevant subscribers
func (ens *EventNotificationSystem) processEvent(event *Event) {
	start := time.Now()
	defer func() {
		ens.mu.Lock()
		ens.metrics.AverageLatency = (ens.metrics.AverageLatency + time.Since(start)) / 2
		ens.mu.Unlock()
	}()

	ens.mu.RLock()
	// Get subscribers for this event type
	subscriberIDs := ens.typeSubscribers[event.Type]
	subscribers := make([]EventSubscriber, 0, len(subscriberIDs))
	subscriberInfos := make([]*SubscriberInfo, 0, len(subscriberIDs))

	for _, id := range subscriberIDs {
		if subscriber, exists := ens.subscribers[id]; exists {
			subscribers = append(subscribers, subscriber)
			if info, exists := ens.subscriberInfo[id]; exists {
				subscriberInfos = append(subscriberInfos, info)
			}
		}
	}
	ens.mu.RUnlock()

	// Deliver event to subscribers concurrently
	var wg sync.WaitGroup
	for i, subscriber := range subscribers {
		wg.Add(1)
		go func(sub EventSubscriber, info *SubscriberInfo) {
			defer wg.Done()
			ens.deliverEventToSubscriber(event, sub, info)
		}(subscriber, subscriberInfos[i])
	}

	wg.Wait()
}

// deliverEventToSubscriber delivers an event to a single subscriber
func (ens *EventNotificationSystem) deliverEventToSubscriber(event *Event, subscriber EventSubscriber, info *SubscriberInfo) {
	ctx, cancel := context.WithTimeout(ens.shutdownCtx, ens.timeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			ens.mu.Lock()
			info.ErrorCount++
			info.LastError = time.Now()
			ens.mu.Unlock()
			ens.logger("Event subscriber '%s' panicked: %v", subscriber.ID(), r)
		}
	}()

	if err := subscriber.HandleEvent(ctx, event); err != nil {
		ens.mu.Lock()
		info.ErrorCount++
		info.LastError = time.Now()
		ens.mu.Unlock()
		ens.logger("Event subscriber '%s' failed to handle event '%s': %v", subscriber.ID(), event.Type, err)
	} else {
		ens.mu.Lock()
		info.EventCount++
		info.LastEvent = time.Now()
		ens.mu.Unlock()
	}
}

// removeSubscriberFromType removes a subscriber from a specific event type
func (ens *EventNotificationSystem) removeSubscriberFromType(eventType EventType, subscriberID string) {
	subscribers := ens.typeSubscribers[eventType]
	for i, id := range subscribers {
		if id == subscriberID {
			ens.typeSubscribers[eventType] = append(subscribers[:i], subscribers[i+1:]...)
			break
		}
	}
}

// CreateEvent is a helper function to create a new event
func CreateEvent(eventType EventType, source string, data map[string]interface{}) *Event {
	return &Event{
		Type:      eventType,
		ID:        fmt.Sprintf("%d-%s", time.Now().UnixNano(), eventType),
		Timestamp: time.Now(),
		Source:    source,
		Data:      data,
	}
}

// CreateTorrentEvent is a helper function to create a torrent-related event
func CreateTorrentEvent(eventType EventType, torrent *TorrentState, data map[string]interface{}) *Event {
	event := CreateEvent(eventType, "torrent_manager", data)
	event.Torrent = torrent
	return event
}

// CreateErrorEvent is a helper function to create an error event
func CreateErrorEvent(eventType EventType, source string, err error, data map[string]interface{}) *Event {
	event := CreateEvent(eventType, source, data)
	event.Error = err
	return event
}
