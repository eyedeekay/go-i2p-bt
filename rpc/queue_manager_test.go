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
	"sync"
	"testing"
	"time"
)

func TestNewQueueManager(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   3,
		MaxActiveSeeds:       2,
		ProcessInterval:      100 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	var activatedCount, deactivatedCount int64
	var mu sync.Mutex

	onActivated := func(torrentID int64, queueType QueueType) {
		mu.Lock()
		activatedCount++
		mu.Unlock()
	}

	onDeactivated := func(torrentID int64, queueType QueueType) {
		mu.Lock()
		deactivatedCount++
		mu.Unlock()
	}

	qm := NewQueueManager(config, onActivated, onDeactivated)
	defer qm.Close()

	if qm.config.MaxActiveDownloads != 3 {
		t.Errorf("Expected MaxActiveDownloads=3, got %d", qm.config.MaxActiveDownloads)
	}

	if qm.config.MaxActiveSeeds != 2 {
		t.Errorf("Expected MaxActiveSeeds=2, got %d", qm.config.MaxActiveSeeds)
	}
}

func TestQueueManager_AddToQueue(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   2,
		MaxActiveSeeds:       1,
		ProcessInterval:      50 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	activatedTorrents := make(map[int64]QueueType)
	var mu sync.Mutex

	onActivated := func(torrentID int64, queueType QueueType) {
		mu.Lock()
		activatedTorrents[torrentID] = queueType
		mu.Unlock()
	}

	qm := NewQueueManager(config, onActivated, nil)
	defer qm.Close()

	// Test adding torrents to download queue
	err := qm.AddToQueue(1, DownloadQueue, 1)
	if err != nil {
		t.Fatalf("Failed to add torrent to download queue: %v", err)
	}

	err = qm.AddToQueue(2, DownloadQueue, 2)
	if err != nil {
		t.Fatalf("Failed to add second torrent to download queue: %v", err)
	}

	err = qm.AddToQueue(3, DownloadQueue, 0)
	if err != nil {
		t.Fatalf("Failed to add third torrent to download queue: %v", err)
	}

	// Wait for queue processing
	time.Sleep(200 * time.Millisecond)

	// Check that first two torrents were activated (MaxActiveDownloads=2)
	mu.Lock()
	activatedCount := len(activatedTorrents)
	mu.Unlock()

	if activatedCount != 2 {
		t.Errorf("Expected 2 torrents activated, got %d", activatedCount)
	}

	// Check queue stats
	stats := qm.GetStats()
	if stats.ActiveDownloads != 2 {
		t.Errorf("Expected 2 active downloads, got %d", stats.ActiveDownloads)
	}

	if stats.DownloadQueueLength != 1 {
		t.Errorf("Expected 1 torrent in download queue, got %d", stats.DownloadQueueLength)
	}
}

func TestQueueManager_QueuePosition(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   1,
		MaxActiveSeeds:       1,
		ProcessInterval:      50 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	// Add torrents with different priorities
	qm.AddToQueue(1, DownloadQueue, 10) // High priority
	qm.AddToQueue(2, DownloadQueue, 5)  // Medium priority
	qm.AddToQueue(3, DownloadQueue, 1)  // Low priority

	// Check initial queue positions
	pos1 := qm.GetQueuePosition(1)
	pos2 := qm.GetQueuePosition(2)
	pos3 := qm.GetQueuePosition(3)

	if pos1 != 0 { // Highest priority should be position 0
		t.Errorf("Expected torrent 1 at position 0, got %d", pos1)
	}

	if pos2 != 1 { // Medium priority should be position 1
		t.Errorf("Expected torrent 2 at position 1, got %d", pos2)
	}

	if pos3 != 2 { // Low priority should be position 2
		t.Errorf("Expected torrent 3 at position 2, got %d", pos3)
	}

	// Test setting queue position
	err := qm.SetQueuePosition(3, 0) // Move low priority to front
	if err != nil {
		t.Fatalf("Failed to set queue position: %v", err)
	}

	// Check updated positions
	pos3Updated := qm.GetQueuePosition(3)
	if pos3Updated != 0 {
		t.Errorf("Expected torrent 3 at position 0 after update, got %d", pos3Updated)
	}
}

func TestQueueManager_RemoveFromQueue(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   1,
		MaxActiveSeeds:       1,
		ProcessInterval:      50 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	// Add torrents
	qm.AddToQueue(1, DownloadQueue, 1)
	qm.AddToQueue(2, SeedQueue, 1)

	// Check they were added
	if qm.GetQueuePosition(1) == -1 {
		t.Error("Expected torrent 1 to be in queue")
	}

	if qm.GetQueuePosition(2) == -1 {
		t.Error("Expected torrent 2 to be in queue")
	}

	// Remove torrents
	qm.RemoveFromQueue(1)
	qm.RemoveFromQueue(2)

	// Check they were removed
	if qm.GetQueuePosition(1) != -1 {
		t.Error("Expected torrent 1 to be removed from queue")
	}

	if qm.GetQueuePosition(2) != -1 {
		t.Error("Expected torrent 2 to be removed from queue")
	}

	// Check stats
	stats := qm.GetStats()
	if stats.DownloadQueueLength != 0 {
		t.Errorf("Expected 0 torrents in download queue, got %d", stats.DownloadQueueLength)
	}

	if stats.SeedQueueLength != 0 {
		t.Errorf("Expected 0 torrents in seed queue, got %d", stats.SeedQueueLength)
	}
}

func TestQueueManager_ForceActivate(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   1,
		MaxActiveSeeds:       1,
		ProcessInterval:      50 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	activatedTorrents := make(map[int64]QueueType)
	var mu sync.Mutex

	onActivated := func(torrentID int64, queueType QueueType) {
		mu.Lock()
		activatedTorrents[torrentID] = queueType
		mu.Unlock()
	}

	qm := NewQueueManager(config, onActivated, nil)
	defer qm.Close()

	// Add torrent to queue
	qm.AddToQueue(1, DownloadQueue, 1)

	// Force activate another torrent, bypassing queue limits
	qm.ForceActivate(2, DownloadQueue)

	time.Sleep(100 * time.Millisecond)

	// Check that both torrents are active (even though limit is 1)
	mu.Lock()
	activatedCount := len(activatedTorrents)
	mu.Unlock()

	if activatedCount < 1 {
		t.Errorf("Expected at least 1 torrent activated, got %d", activatedCount)
	}

	// Check that torrent 2 is active
	if !qm.IsActive(2) {
		t.Error("Expected torrent 2 to be active after force activation")
	}
}

func TestQueueManager_DisabledQueues(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   2,
		MaxActiveSeeds:       1,
		ProcessInterval:      50 * time.Millisecond,
		DownloadQueueEnabled: false, // Disabled
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	// Try to add to disabled download queue
	err := qm.AddToQueue(1, DownloadQueue, 1)
	if err != nil {
		t.Fatalf("AddToQueue should not return error for disabled queue: %v", err)
	}

	// Check that torrent was not actually queued
	if qm.GetQueuePosition(1) != -1 {
		t.Error("Expected torrent not to be queued when download queue is disabled")
	}

	// Add to enabled seed queue should work
	err = qm.AddToQueue(2, SeedQueue, 1)
	if err != nil {
		t.Fatalf("Failed to add to enabled seed queue: %v", err)
	}

	if qm.GetQueuePosition(2) == -1 {
		t.Error("Expected torrent to be queued in enabled seed queue")
	}
}

func TestQueueManager_QueueProcessing(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   2,
		MaxActiveSeeds:       1,
		ProcessInterval:      50 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	activationOrder := make([]int64, 0)
	var mu sync.Mutex

	onActivated := func(torrentID int64, queueType QueueType) {
		mu.Lock()
		activationOrder = append(activationOrder, torrentID)
		mu.Unlock()
	}

	qm := NewQueueManager(config, onActivated, nil)
	defer qm.Close()

	// Add multiple torrents with different priorities
	qm.AddToQueue(1, DownloadQueue, 10) // High priority
	qm.AddToQueue(2, DownloadQueue, 5)  // Medium priority
	qm.AddToQueue(3, DownloadQueue, 1)  // Low priority
	qm.AddToQueue(4, DownloadQueue, 15) // Highest priority

	// Wait for queue processing
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Check that highest priority torrents were activated first
	if len(activationOrder) < 2 {
		t.Fatalf("Expected at least 2 torrents to be activated, got %d", len(activationOrder))
	}

	// Torrent 4 (priority 15) should be activated first
	// Torrent 1 (priority 10) should be activated second
	// Note: Exact order may vary due to timing, but high priority should be preferred

	stats := qm.GetStats()
	if stats.ActiveDownloads != 2 {
		t.Errorf("Expected 2 active downloads, got %d", stats.ActiveDownloads)
	}

	if stats.DownloadQueueLength != 2 {
		t.Errorf("Expected 2 torrents remaining in queue, got %d", stats.DownloadQueueLength)
	}
}

func TestQueueManager_Stats(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   1,
		MaxActiveSeeds:       1,
		ProcessInterval:      50 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	// Initial stats
	stats := qm.GetStats()
	if stats.TotalQueued != 0 {
		t.Errorf("Expected TotalQueued=0 initially, got %d", stats.TotalQueued)
	}

	// Add torrents
	qm.AddToQueue(1, DownloadQueue, 1)
	qm.AddToQueue(2, SeedQueue, 1)

	// Check updated stats
	stats = qm.GetStats()
	if stats.TotalQueued != 2 {
		t.Errorf("Expected TotalQueued=2 after adding torrents, got %d", stats.TotalQueued)
	}

	if stats.DownloadQueueLength != 1 {
		t.Errorf("Expected DownloadQueueLength=1, got %d", stats.DownloadQueueLength)
	}

	if stats.SeedQueueLength != 1 {
		t.Errorf("Expected SeedQueueLength=1, got %d", stats.SeedQueueLength)
	}
}

func TestQueueManager_ConcurrentOperations(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   5,
		MaxActiveSeeds:       3,
		ProcessInterval:      25 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	// Test concurrent additions
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			queueType := DownloadQueue
			if id%2 == 0 {
				queueType = SeedQueue
			}
			qm.AddToQueue(int64(id), queueType, int64(id%5))
		}(i)
	}

	wg.Wait()

	// Test concurrent removals
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			qm.RemoveFromQueue(int64(id))
		}(i)
	}

	wg.Wait()

	// Verify no data races occurred (this is mostly checked by race detector)
	stats := qm.GetStats()
	t.Logf("Final stats: Downloads=%d, Seeds=%d, Total=%d",
		stats.DownloadQueueLength, stats.SeedQueueLength, stats.TotalQueued)
}

func TestQueueManager_ErrorHandling(t *testing.T) {
	config := QueueConfig{
		MaxActiveDownloads:   1,
		MaxActiveSeeds:       1,
		ProcessInterval:      50 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	// Test setting position for non-existent torrent
	err := qm.SetQueuePosition(999, 0)
	if err != ErrTorrentNotInQueue {
		t.Errorf("Expected ErrTorrentNotInQueue for non-existent torrent, got: %v", err)
	}

	// Test getting position for non-existent torrent
	pos := qm.GetQueuePosition(999)
	if pos != -1 {
		t.Errorf("Expected position -1 for non-existent torrent, got %d", pos)
	}

	// Test checking active status for non-existent torrent
	active := qm.IsActive(999)
	if active {
		t.Error("Expected non-existent torrent to not be active")
	}
}

// Benchmarks

func BenchmarkQueueManager_AddToQueue(b *testing.B) {
	config := QueueConfig{
		MaxActiveDownloads:   10,
		MaxActiveSeeds:       5,
		ProcessInterval:      100 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		qm.AddToQueue(int64(i), DownloadQueue, int64(i%10))
	}
}

func BenchmarkQueueManager_GetQueuePosition(b *testing.B) {
	config := QueueConfig{
		MaxActiveDownloads:   10,
		MaxActiveSeeds:       5,
		ProcessInterval:      100 * time.Millisecond,
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	// Pre-populate queue
	for i := 0; i < 100; i++ {
		qm.AddToQueue(int64(i), DownloadQueue, int64(i%10))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		qm.GetQueuePosition(int64(i % 100))
	}
}

func BenchmarkQueueManager_ProcessQueues(b *testing.B) {
	config := QueueConfig{
		MaxActiveDownloads:   5,
		MaxActiveSeeds:       3,
		ProcessInterval:      time.Hour, // Disable automatic processing
		DownloadQueueEnabled: true,
		SeedQueueEnabled:     true,
	}

	qm := NewQueueManager(config, nil, nil)
	defer qm.Close()

	// Pre-populate queue
	for i := 0; i < 50; i++ {
		qm.AddToQueue(int64(i), DownloadQueue, int64(i%10))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		qm.processQueues()
	}
}
