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
	"errors"
	"sync"
	"testing"
	"time"
)

// Test subscriber implementations

// testEventSubscriber implements EventSubscriber interface for testing
type testEventSubscriber struct {
	id        string
	filters   []EventType
	events    []*Event
	errors    []error
	mu        sync.Mutex
	shouldErr bool
	errMsg    string
}

func (s *testEventSubscriber) ID() string {
	return s.id
}

func (s *testEventSubscriber) GetSubscriptionFilters() []EventType {
	return s.filters
}

func (s *testEventSubscriber) HandleEvent(ctx context.Context, event *Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shouldErr {
		err := errors.New(s.errMsg)
		s.errors = append(s.errors, err)
		return err
	}

	s.events = append(s.events, event)
	return nil
}

func (s *testEventSubscriber) GetReceivedEvents() []*Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]*Event, len(s.events))
	copy(result, s.events)
	return result
}

func (s *testEventSubscriber) GetErrors() []error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]error, len(s.errors))
	copy(result, s.errors)
	return result
}

func (s *testEventSubscriber) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = make([]*Event, 0)
	s.errors = make([]error, 0)
}

func (s *testEventSubscriber) SetShouldError(shouldErr bool, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.shouldErr = shouldErr
	s.errMsg = errMsg
}

// Test helper functions

func createTestSubscriber(id string, filters []EventType) *testEventSubscriber {
	return &testEventSubscriber{
		id:      id,
		filters: filters,
		events:  make([]*Event, 0),
		errors:  make([]error, 0),
	}
}

func createTestEvent(eventType EventType, source string) *Event {
	return &Event{
		Type:      eventType,
		ID:        "test-event-id",
		Timestamp: time.Now(),
		Source:    source,
		Data:      map[string]interface{}{"test": "data"},
	}
}

func waitForEventProcessing(ens *EventNotificationSystem) {
	// Give some time for background processing
	time.Sleep(50 * time.Millisecond)
}

// Test cases

func TestNewEventNotificationSystem(t *testing.T) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	if ens == nil {
		t.Fatal("NewEventNotificationSystem returned nil")
	}

	if ens.subscribers == nil {
		t.Error("subscribers map not initialized")
	}

	if ens.subscriberInfo == nil {
		t.Error("subscriberInfo map not initialized")
	}

	if ens.typeSubscribers == nil {
		t.Error("typeSubscribers map not initialized")
	}

	if ens.eventChan == nil {
		t.Error("eventChan not initialized")
	}

	if ens.bufferSize != 1000 {
		t.Error("default buffer size not set correctly")
	}

	if ens.timeout != 30*time.Second {
		t.Error("default timeout not set correctly")
	}
}

func TestEventNotificationSystemSetters(t *testing.T) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Test SetBufferSize
	newBufferSize := 2000
	ens.SetBufferSize(newBufferSize)

	if ens.bufferSize != newBufferSize {
		t.Errorf("SetBufferSize failed: expected %d, got %d", newBufferSize, ens.bufferSize)
	}

	// Test SetTimeout
	newTimeout := 60 * time.Second
	ens.SetTimeout(newTimeout)

	if ens.timeout != newTimeout {
		t.Errorf("SetTimeout failed: expected %v, got %v", newTimeout, ens.timeout)
	}

	// Test SetLogger
	logCalled := false
	testLogger := func(format string, args ...interface{}) {
		logCalled = true
	}
	ens.SetLogger(testLogger)

	// Test logger by subscribing a subscriber
	subscriber := createTestSubscriber("test", []EventType{EventTorrentAdded})
	ens.Subscribe(subscriber)

	if !logCalled {
		t.Error("Custom logger was not called")
	}
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Test successful subscription
	subscriber := createTestSubscriber("test-subscriber", []EventType{EventTorrentAdded, EventTorrentStarted})
	err := ens.Subscribe(subscriber)

	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Check subscriber info
	info := ens.GetSubscriberInfo()
	if subInfo, exists := info["test-subscriber"]; !exists {
		t.Error("Subscriber info not stored")
	} else {
		if subInfo.ID != "test-subscriber" {
			t.Error("Subscriber ID not stored correctly")
		}
		if subInfo.Status != "active" {
			t.Error("Subscriber status not set to active")
		}
		if len(subInfo.Filters) != 2 {
			t.Error("Subscriber filters not stored correctly")
		}
	}

	// Check type-based lookup
	typeSubscribers := ens.ListSubscribersByType()
	if subscribers, exists := typeSubscribers[EventTorrentAdded]; !exists {
		t.Error("Subscriber not registered for EventTorrentAdded")
	} else if len(subscribers) != 1 || subscribers[0] != "test-subscriber" {
		t.Error("Subscriber not registered correctly for EventTorrentAdded")
	}

	// Test nil subscriber
	err = ens.Subscribe(nil)
	if err == nil {
		t.Error("Subscribe should fail with nil subscriber")
	}

	// Test duplicate subscription
	duplicate := createTestSubscriber("test-subscriber", []EventType{EventTorrentCompleted})
	err = ens.Subscribe(duplicate)
	if err == nil {
		t.Error("Subscribe should fail with duplicate ID")
	}

	// Test successful unsubscription
	err = ens.Unsubscribe("test-subscriber")
	if err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// Check subscriber is removed
	info = ens.GetSubscriberInfo()
	if _, exists := info["test-subscriber"]; exists {
		t.Error("Subscriber info not removed")
	}

	// Test unsubscribing non-existent subscriber
	err = ens.Unsubscribe("non-existent")
	if err == nil {
		t.Error("Unsubscribe should fail with non-existent subscriber")
	}

	// Test empty subscriber ID
	err = ens.Unsubscribe("")
	if err == nil {
		t.Error("Unsubscribe should fail with empty subscriber ID")
	}
}

func TestEventPublishing(t *testing.T) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Subscribe to specific event types
	subscriber1 := createTestSubscriber("subscriber1", []EventType{EventTorrentAdded})
	subscriber2 := createTestSubscriber("subscriber2", []EventType{EventTorrentStarted})

	ens.Subscribe(subscriber1)
	ens.Subscribe(subscriber2)

	// Publish EventTorrentAdded
	event1 := createTestEvent(EventTorrentAdded, "test")
	err := ens.PublishEvent(event1)
	if err != nil {
		t.Fatalf("PublishEvent failed: %v", err)
	}

	waitForEventProcessing(ens)

	// Check subscriber1 received the event
	events1 := subscriber1.GetReceivedEvents()
	if len(events1) != 1 {
		t.Errorf("Subscriber1 should have received 1 event, got %d", len(events1))
	} else if events1[0].Type != EventTorrentAdded {
		t.Error("Subscriber1 received wrong event type")
	}

	// Check subscriber2 did not receive the event
	events2 := subscriber2.GetReceivedEvents()
	if len(events2) != 0 {
		t.Errorf("Subscriber2 should not have received any events, got %d", len(events2))
	}

	// Publish EventTorrentStarted
	event2 := createTestEvent(EventTorrentStarted, "test")
	err = ens.PublishEvent(event2)
	if err != nil {
		t.Fatalf("PublishEvent failed: %v", err)
	}

	waitForEventProcessing(ens)

	// Check subscriber2 received the event
	events2 = subscriber2.GetReceivedEvents()
	if len(events2) != 1 {
		t.Errorf("Subscriber2 should have received 1 event, got %d", len(events2))
	} else if events2[0].Type != EventTorrentStarted {
		t.Error("Subscriber2 received wrong event type")
	}

	// Check subscriber1 still has only 1 event
	events1 = subscriber1.GetReceivedEvents()
	if len(events1) != 1 {
		t.Errorf("Subscriber1 should still have 1 event, got %d", len(events1))
	}

	// Test nil event
	err = ens.PublishEvent(nil)
	if err == nil {
		t.Error("PublishEvent should fail with nil event")
	}
}

func TestGlobalSubscriber(t *testing.T) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Subscribe to all events (empty filters)
	globalSubscriber := createTestSubscriber("global", []EventType{})
	err := ens.Subscribe(globalSubscriber)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Publish multiple different events
	events := []*Event{
		createTestEvent(EventTorrentAdded, "test"),
		createTestEvent(EventTorrentStarted, "test"),
		createTestEvent(EventSessionStarted, "test"),
	}

	for _, event := range events {
		err := ens.PublishEvent(event)
		if err != nil {
			t.Fatalf("PublishEvent failed: %v", err)
		}
	}

	waitForEventProcessing(ens)

	// Check global subscriber received all events
	receivedEvents := globalSubscriber.GetReceivedEvents()
	if len(receivedEvents) != len(events) {
		t.Errorf("Global subscriber should have received %d events, got %d", len(events), len(receivedEvents))
	}
}

func TestEventSubscriberErrors(t *testing.T) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Create subscriber that will error
	subscriber := createTestSubscriber("error-subscriber", []EventType{EventTorrentAdded})
	subscriber.SetShouldError(true, "test error")

	err := ens.Subscribe(subscriber)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Publish event
	event := createTestEvent(EventTorrentAdded, "test")
	err = ens.PublishEvent(event)
	if err != nil {
		t.Fatalf("PublishEvent failed: %v", err)
	}

	waitForEventProcessing(ens)

	// Check subscriber received error
	errors := subscriber.GetErrors()
	if len(errors) != 1 {
		t.Errorf("Subscriber should have 1 error, got %d", len(errors))
	}

	// Check subscriber info reflects error
	info := ens.GetSubscriberInfo()
	if subInfo, exists := info["error-subscriber"]; !exists {
		t.Error("Subscriber info not found")
	} else if subInfo.ErrorCount != 1 {
		t.Errorf("Subscriber error count should be 1, got %d", subInfo.ErrorCount)
	}
}

func TestConveniencePublishMethods(t *testing.T) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Subscribe to all events
	subscriber := createTestSubscriber("test", []EventType{})
	ens.Subscribe(subscriber)

	// Test PublishTorrentEvent
	torrent := &TorrentState{ID: 1}
	data := map[string]interface{}{"key": "value"}

	err := ens.PublishTorrentEvent(EventTorrentAdded, torrent, data)
	if err != nil {
		t.Fatalf("PublishTorrentEvent failed: %v", err)
	}

	// Test PublishSessionEvent
	err = ens.PublishSessionEvent(EventSessionStarted, data)
	if err != nil {
		t.Fatalf("PublishSessionEvent failed: %v", err)
	}

	// Test PublishErrorEvent
	testErr := errors.New("test error")
	err = ens.PublishErrorEvent(EventSystemError, testErr, data)
	if err != nil {
		t.Fatalf("PublishErrorEvent failed: %v", err)
	}

	waitForEventProcessing(ens)

	// Check events were received
	events := subscriber.GetReceivedEvents()
	if len(events) != 3 {
		t.Errorf("Should have received 3 events, got %d", len(events))
	}

	// Check torrent event
	if events[0].Type != EventTorrentAdded || events[0].Torrent == nil {
		t.Error("Torrent event not published correctly")
	}

	// Check session event
	if events[1].Type != EventSessionStarted || events[1].Source != "session_manager" {
		t.Error("Session event not published correctly")
	}

	// Check error event
	if events[2].Type != EventSystemError || events[2].Error == nil {
		t.Error("Error event not published correctly")
	}
}

func TestEventMetrics(t *testing.T) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Subscribe a subscriber
	subscriber := createTestSubscriber("test", []EventType{EventTorrentAdded})
	ens.Subscribe(subscriber)

	// Publish some events
	for i := 0; i < 5; i++ {
		event := createTestEvent(EventTorrentAdded, "test")
		ens.PublishEvent(event)
	}

	waitForEventProcessing(ens)

	// Check metrics
	metrics := ens.GetMetrics()
	if metrics.TotalEvents != 5 {
		t.Errorf("Total events should be 5, got %d", metrics.TotalEvents)
	}

	if metrics.ActiveSubscribers != 1 {
		t.Errorf("Active subscribers should be 1, got %d", metrics.ActiveSubscribers)
	}

	if metrics.TotalSubscribers != 1 {
		t.Errorf("Total subscribers should be 1, got %d", metrics.TotalSubscribers)
	}
}

func TestEventHelperFunctions(t *testing.T) {
	// Test CreateEvent
	event := CreateEvent(EventTorrentAdded, "test_source", map[string]interface{}{"key": "value"})

	if event.Type != EventTorrentAdded {
		t.Error("Event type not set correctly")
	}

	if event.Source != "test_source" {
		t.Error("Event source not set correctly")
	}

	if event.ID == "" {
		t.Error("Event ID not generated")
	}

	if event.Timestamp.IsZero() {
		t.Error("Event timestamp not set")
	}

	// Test CreateTorrentEvent
	torrent := &TorrentState{ID: 1}
	torrentEvent := CreateTorrentEvent(EventTorrentCompleted, torrent, map[string]interface{}{"completed": true})

	if torrentEvent.Type != EventTorrentCompleted {
		t.Error("Torrent event type not set correctly")
	}

	if torrentEvent.Torrent == nil || torrentEvent.Torrent.ID != 1 {
		t.Error("Torrent not attached to event")
	}

	// Test CreateErrorEvent
	testErr := errors.New("test error")
	errorEvent := CreateErrorEvent(EventSystemError, "error_source", testErr, map[string]interface{}{"severity": "high"})

	if errorEvent.Type != EventSystemError {
		t.Error("Error event type not set correctly")
	}

	if errorEvent.Error == nil || errorEvent.Error.Error() != "test error" {
		t.Error("Error not attached to event")
	}

	if errorEvent.Source != "error_source" {
		t.Error("Error event source not set correctly")
	}
}

func TestEventNotificationSystemShutdown(t *testing.T) {
	ens := NewEventNotificationSystem()

	// Subscribe multiple subscribers
	subscriber1 := createTestSubscriber("sub1", []EventType{EventTorrentAdded})
	subscriber2 := createTestSubscriber("sub2", []EventType{EventTorrentStarted})

	ens.Subscribe(subscriber1)
	ens.Subscribe(subscriber2)

	// Shutdown
	err := ens.Shutdown()
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Check subscribers were cleared
	info := ens.GetSubscriberInfo()
	if len(info) != 0 {
		t.Error("Subscriber info was not cleared")
	}

	// Check metrics were reset
	metrics := ens.GetMetrics()
	if metrics.ActiveSubscribers != 0 {
		t.Error("Active subscribers count was not reset")
	}

	// Try to publish event after shutdown (should not panic)
	event := createTestEvent(EventTorrentAdded, "test")
	// This might return an error or not depending on timing, but shouldn't panic
	ens.PublishEvent(event)
}

// Benchmark tests

func BenchmarkEventPublishing(b *testing.B) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Subscribe a subscriber
	subscriber := createTestSubscriber("benchmark", []EventType{EventTorrentAdded})
	ens.Subscribe(subscriber)

	event := createTestEvent(EventTorrentAdded, "benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ens.PublishEvent(event)
	}
}

func BenchmarkSubscriberManagement(b *testing.B) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		subscriber := createTestSubscriber("benchmark", []EventType{EventTorrentAdded})
		ens.Subscribe(subscriber)
		ens.Unsubscribe("benchmark")
	}
}

func BenchmarkEventProcessing(b *testing.B) {
	ens := NewEventNotificationSystem()
	defer ens.Shutdown()

	// Subscribe multiple subscribers
	for i := 0; i < 100; i++ {
		subscriber := createTestSubscriber("subscriber", []EventType{EventTorrentAdded})
		ens.Subscribe(subscriber)
	}

	event := createTestEvent(EventTorrentAdded, "benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ens.PublishEvent(event)
	}
}
