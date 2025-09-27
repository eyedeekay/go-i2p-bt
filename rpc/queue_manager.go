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
	"sort"
	"sync"
	"time"
)

// QueueType represents the type of queue for torrents
type QueueType int

const (
	// DownloadQueue for torrents waiting to download
	DownloadQueue QueueType = iota
	// SeedQueue for torrents waiting to seed
	SeedQueue
)

// QueueItem represents a torrent item in a queue
type QueueItem struct {
	TorrentID     int64     // Unique torrent identifier
	QueuePosition int64     // Position in queue (0-based)
	AddedTime     time.Time // When the item was added to queue
	Priority      int64     // Priority level (higher = more important)
}

// QueueManager manages torrent queues for downloads and uploads
// It ensures that only a limited number of torrents are active at once,
// following Transmission RPC protocol queue management behavior.
type QueueManager struct {
	mu sync.RWMutex

	// Queue storage - maps torrent ID to queue item
	downloadQueue map[int64]*QueueItem
	seedQueue     map[int64]*QueueItem

	// Ordered queue positions for efficient sorting
	downloadOrder []int64
	seedOrder     []int64

	// Active torrent tracking
	activeDownloads map[int64]bool
	activeSeeds     map[int64]bool

	// Configuration
	config QueueConfig

	// Background processing
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Callbacks for queue state changes
	onTorrentActivated   func(torrentID int64, queueType QueueType)
	onTorrentDeactivated func(torrentID int64, queueType QueueType)

	// Statistics
	stats QueueStats
}

// QueueConfig contains configuration for the queue manager
type QueueConfig struct {
	// Maximum number of active downloads (0 = unlimited)
	MaxActiveDownloads int64
	// Maximum number of active seeds (0 = unlimited)
	MaxActiveSeeds int64
	// Queue processing interval
	ProcessInterval time.Duration
	// Enable download queue
	DownloadQueueEnabled bool
	// Enable seed queue
	SeedQueueEnabled bool
}

// QueueStats contains statistics about queue operations
type QueueStats struct {
	DownloadQueueLength   int64 // Number of torrents in download queue
	SeedQueueLength       int64 // Number of torrents in seed queue
	ActiveDownloads       int64 // Number of currently active downloads
	ActiveSeeds           int64 // Number of currently active seeds
	TotalQueued           int64 // Total items processed through queues
	TotalActivated        int64 // Total items activated from queues
	LastProcessTime       time.Time
	ProcessingTimeAverage time.Duration
}

// NewQueueManager creates a new queue manager with the given configuration
func NewQueueManager(config QueueConfig, onActivated, onDeactivated func(int64, QueueType)) *QueueManager {
	// Apply default configuration values
	if config.ProcessInterval == 0 {
		config.ProcessInterval = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	qm := &QueueManager{
		downloadQueue:        make(map[int64]*QueueItem),
		seedQueue:            make(map[int64]*QueueItem),
		downloadOrder:        make([]int64, 0),
		seedOrder:            make([]int64, 0),
		activeDownloads:      make(map[int64]bool),
		activeSeeds:          make(map[int64]bool),
		config:               config,
		ctx:                  ctx,
		cancel:               cancel,
		onTorrentActivated:   onActivated,
		onTorrentDeactivated: onDeactivated,
	}

	// Start background queue processing
	qm.startQueueProcessor()

	return qm
}

// AddToQueue adds a torrent to the specified queue with given priority
func (qm *QueueManager) AddToQueue(torrentID int64, queueType QueueType, priority int64) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	now := time.Now()
	item := &QueueItem{
		TorrentID: torrentID,
		AddedTime: now,
		Priority:  priority,
	}

	switch queueType {
	case DownloadQueue:
		if !qm.config.DownloadQueueEnabled {
			return nil // Queue disabled, don't add
		}

		// Remove from other queues if present
		qm.removeFromSeedQueueUnsafe(torrentID)

		// Add to download queue
		qm.downloadQueue[torrentID] = item
		qm.downloadOrder = append(qm.downloadOrder, torrentID)
		qm.sortDownloadQueue()

		// Update queue positions
		qm.updateDownloadQueuePositions()

		qm.stats.DownloadQueueLength = int64(len(qm.downloadQueue))
		qm.stats.TotalQueued++

	case SeedQueue:
		if !qm.config.SeedQueueEnabled {
			return nil // Queue disabled, don't add
		}

		// Remove from other queues if present
		qm.removeFromDownloadQueueUnsafe(torrentID)

		// Add to seed queue
		qm.seedQueue[torrentID] = item
		qm.seedOrder = append(qm.seedOrder, torrentID)
		qm.sortSeedQueue()

		// Update queue positions
		qm.updateSeedQueuePositions()

		qm.stats.SeedQueueLength = int64(len(qm.seedQueue))
		qm.stats.TotalQueued++
	}

	return nil
}

// RemoveFromQueue removes a torrent from all queues
func (qm *QueueManager) RemoveFromQueue(torrentID int64) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.removeFromDownloadQueueUnsafe(torrentID)
	qm.removeFromSeedQueueUnsafe(torrentID)

	// Also remove from active tracking
	delete(qm.activeDownloads, torrentID)
	delete(qm.activeSeeds, torrentID)

	qm.stats.ActiveDownloads = int64(len(qm.activeDownloads))
	qm.stats.ActiveSeeds = int64(len(qm.activeSeeds))
}

// SetQueuePosition sets the queue position for a torrent
func (qm *QueueManager) SetQueuePosition(torrentID, position int64) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Check if torrent is in download queue
	if _, exists := qm.downloadQueue[torrentID]; exists {
		return qm.moveInDownloadQueue(torrentID, position)
	}

	// Check if torrent is in seed queue
	if _, exists := qm.seedQueue[torrentID]; exists {
		return qm.moveInSeedQueue(torrentID, position)
	}

	return ErrTorrentNotInQueue
}

// GetQueuePosition returns the queue position for a torrent (-1 if not queued)
func (qm *QueueManager) GetQueuePosition(torrentID int64) int64 {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	if item, exists := qm.downloadQueue[torrentID]; exists {
		return item.QueuePosition
	}

	if item, exists := qm.seedQueue[torrentID]; exists {
		return item.QueuePosition
	}

	return -1
}

// IsActive returns true if the torrent is currently active
func (qm *QueueManager) IsActive(torrentID int64) bool {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	return qm.activeDownloads[torrentID] || qm.activeSeeds[torrentID]
}

// ForceActivate immediately activates a torrent, bypassing queue limits
func (qm *QueueManager) ForceActivate(torrentID int64, queueType QueueType) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Remove from queues but don't deactivate
	qm.removeFromDownloadQueueUnsafe(torrentID)
	qm.removeFromSeedQueueUnsafe(torrentID)

	// Mark as active
	switch queueType {
	case DownloadQueue:
		qm.activeDownloads[torrentID] = true
		qm.stats.ActiveDownloads = int64(len(qm.activeDownloads))
	case SeedQueue:
		qm.activeSeeds[torrentID] = true
		qm.stats.ActiveSeeds = int64(len(qm.activeSeeds))
	}

	qm.stats.TotalActivated++

	// Trigger callback
	if qm.onTorrentActivated != nil {
		go qm.onTorrentActivated(torrentID, queueType)
	}
}

// GetStats returns current queue statistics
func (qm *QueueManager) GetStats() QueueStats {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	// Update current counts
	stats := qm.stats
	stats.DownloadQueueLength = int64(len(qm.downloadQueue))
	stats.SeedQueueLength = int64(len(qm.seedQueue))
	stats.ActiveDownloads = int64(len(qm.activeDownloads))
	stats.ActiveSeeds = int64(len(qm.activeSeeds))

	return stats
}

// Close shuts down the queue manager and stops background processing
func (qm *QueueManager) Close() error {
	qm.cancel()
	qm.wg.Wait()
	return nil
}

// Internal helper methods

func (qm *QueueManager) removeFromDownloadQueueUnsafe(torrentID int64) {
	if _, exists := qm.downloadQueue[torrentID]; exists {
		delete(qm.downloadQueue, torrentID)

		// Remove from order slice
		for i, id := range qm.downloadOrder {
			if id == torrentID {
				qm.downloadOrder = append(qm.downloadOrder[:i], qm.downloadOrder[i+1:]...)
				break
			}
		}

		qm.updateDownloadQueuePositions()
		qm.stats.DownloadQueueLength = int64(len(qm.downloadQueue))
	}
}

func (qm *QueueManager) removeFromSeedQueueUnsafe(torrentID int64) {
	if _, exists := qm.seedQueue[torrentID]; exists {
		delete(qm.seedQueue, torrentID)

		// Remove from order slice
		for i, id := range qm.seedOrder {
			if id == torrentID {
				qm.seedOrder = append(qm.seedOrder[:i], qm.seedOrder[i+1:]...)
				break
			}
		}

		qm.updateSeedQueuePositions()
		qm.stats.SeedQueueLength = int64(len(qm.seedQueue))
	}
}

func (qm *QueueManager) sortDownloadQueue() {
	sort.Slice(qm.downloadOrder, func(i, j int) bool {
		itemI := qm.downloadQueue[qm.downloadOrder[i]]
		itemJ := qm.downloadQueue[qm.downloadOrder[j]]

		// Sort by priority (higher first), then by added time (earlier first)
		if itemI.Priority != itemJ.Priority {
			return itemI.Priority > itemJ.Priority
		}
		return itemI.AddedTime.Before(itemJ.AddedTime)
	})
}

func (qm *QueueManager) sortSeedQueue() {
	sort.Slice(qm.seedOrder, func(i, j int) bool {
		itemI := qm.seedQueue[qm.seedOrder[i]]
		itemJ := qm.seedQueue[qm.seedOrder[j]]

		// Sort by priority (higher first), then by added time (earlier first)
		if itemI.Priority != itemJ.Priority {
			return itemI.Priority > itemJ.Priority
		}
		return itemI.AddedTime.Before(itemJ.AddedTime)
	})
}

func (qm *QueueManager) updateDownloadQueuePositions() {
	for i, torrentID := range qm.downloadOrder {
		if item, exists := qm.downloadQueue[torrentID]; exists {
			item.QueuePosition = int64(i)
		}
	}
}

func (qm *QueueManager) updateSeedQueuePositions() {
	for i, torrentID := range qm.seedOrder {
		if item, exists := qm.seedQueue[torrentID]; exists {
			item.QueuePosition = int64(i)
		}
	}
}

// Background queue processing

func (qm *QueueManager) startQueueProcessor() {
	qm.wg.Add(1)
	go qm.queueProcessorLoop()
}

func (qm *QueueManager) queueProcessorLoop() {
	defer qm.wg.Done()

	ticker := time.NewTicker(qm.config.ProcessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-qm.ctx.Done():
			return
		case <-ticker.C:
			qm.processQueues()
		}
	}
}

func (qm *QueueManager) processQueues() {
	start := time.Now()

	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Process download queue
	if qm.config.DownloadQueueEnabled && qm.config.MaxActiveDownloads > 0 {
		qm.processDownloadQueue()
	}

	// Process seed queue
	if qm.config.SeedQueueEnabled && qm.config.MaxActiveSeeds > 0 {
		qm.processSeedQueue()
	}

	// Update processing time statistics
	processingTime := time.Since(start)
	if qm.stats.ProcessingTimeAverage == 0 {
		qm.stats.ProcessingTimeAverage = processingTime
	} else {
		// Simple moving average
		qm.stats.ProcessingTimeAverage = (qm.stats.ProcessingTimeAverage + processingTime) / 2
	}
	qm.stats.LastProcessTime = start
}

func (qm *QueueManager) processDownloadQueue() {
	activeCount := int64(len(qm.activeDownloads))

	// Activate torrents from queue if we have capacity
	for activeCount < qm.config.MaxActiveDownloads && len(qm.downloadOrder) > 0 {
		torrentID := qm.downloadOrder[0]

		// Remove from queue
		qm.removeFromDownloadQueueUnsafe(torrentID)

		// Add to active downloads
		qm.activeDownloads[torrentID] = true
		activeCount++
		qm.stats.TotalActivated++

		// Trigger callback
		if qm.onTorrentActivated != nil {
			go qm.onTorrentActivated(torrentID, DownloadQueue)
		}
	}

	qm.stats.ActiveDownloads = activeCount
}

func (qm *QueueManager) processSeedQueue() {
	activeCount := int64(len(qm.activeSeeds))

	// Activate torrents from queue if we have capacity
	for activeCount < qm.config.MaxActiveSeeds && len(qm.seedOrder) > 0 {
		torrentID := qm.seedOrder[0]

		// Remove from queue
		qm.removeFromSeedQueueUnsafe(torrentID)

		// Add to active seeds
		qm.activeSeeds[torrentID] = true
		activeCount++
		qm.stats.TotalActivated++

		// Trigger callback
		if qm.onTorrentActivated != nil {
			go qm.onTorrentActivated(torrentID, SeedQueue)
		}
	}

	qm.stats.ActiveSeeds = activeCount
}

// moveInDownloadQueue moves a torrent to a specific position in the download queue
func (qm *QueueManager) moveInDownloadQueue(torrentID, newPosition int64) error {
	// Find current position
	currentPos := -1
	for i, id := range qm.downloadOrder {
		if id == torrentID {
			currentPos = i
			break
		}
	}

	if currentPos == -1 {
		return ErrTorrentNotInQueue
	}

	// Clamp position to valid range
	queueLen := int64(len(qm.downloadOrder))
	if newPosition < 0 {
		newPosition = 0
	}
	if newPosition >= queueLen {
		newPosition = queueLen - 1
	}

	// If position unchanged, nothing to do
	if newPosition == int64(currentPos) {
		return nil
	}

	// Remove from current position
	qm.downloadOrder = append(qm.downloadOrder[:currentPos], qm.downloadOrder[currentPos+1:]...)

	// Insert at new position
	newPos := int(newPosition)
	qm.downloadOrder = append(qm.downloadOrder[:newPos], append([]int64{torrentID}, qm.downloadOrder[newPos:]...)...)

	// Update all positions
	qm.updateDownloadQueuePositions()

	return nil
}

// moveInSeedQueue moves a torrent to a specific position in the seed queue
func (qm *QueueManager) moveInSeedQueue(torrentID, newPosition int64) error {
	// Find current position
	currentPos := -1
	for i, id := range qm.seedOrder {
		if id == torrentID {
			currentPos = i
			break
		}
	}

	if currentPos == -1 {
		return ErrTorrentNotInQueue
	}

	// Clamp position to valid range
	queueLen := int64(len(qm.seedOrder))
	if newPosition < 0 {
		newPosition = 0
	}
	if newPosition >= queueLen {
		newPosition = queueLen - 1
	}

	// If position unchanged, nothing to do
	if newPosition == int64(currentPos) {
		return nil
	}

	// Remove from current position
	qm.seedOrder = append(qm.seedOrder[:currentPos], qm.seedOrder[currentPos+1:]...)

	// Insert at new position
	newPos := int(newPosition)
	qm.seedOrder = append(qm.seedOrder[:newPos], append([]int64{torrentID}, qm.seedOrder[newPos:]...)...)

	// Update all positions
	qm.updateSeedQueuePositions()

	return nil
}

// Custom error types
var (
	ErrTorrentNotInQueue = &RPCError{
		Code:    ErrCodeInvalidTorrent,
		Message: "torrent not found in queue",
	}
)
