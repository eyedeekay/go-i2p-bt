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

package persistence

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	"github.com/go-i2p/go-i2p-bt/rpc"
)

// MemoryPersistence implements TorrentPersistence using in-memory storage.
// This implementation is useful for:
//   - Testing and development
//   - Temporary sessions where persistence is not required
//   - High-performance scenarios where disk I/O should be minimized
//   - Applications that provide their own snapshotting mechanisms
//
// Features:
//   - Instant read/write operations
//   - Optional periodic snapshotting to backup persistence layer
//   - Deep copying to prevent external modifications
//   - Concurrent access safety
//   - Memory usage monitoring
type MemoryPersistence struct {
	mu            sync.RWMutex
	torrents      map[metainfo.Hash]*rpc.TorrentState
	sessionConfig *rpc.SessionConfiguration

	// Optional backup persistence for snapshotting
	backup           TorrentPersistence
	snapshotInterval time.Duration
	lastSnapshot     time.Time
	stopSnapshot     chan struct{}
	snapshotRunning  bool

	// Statistics
	readCount   int64
	writeCount  int64
	deleteCount int64
}

// NewMemoryPersistence creates a new in-memory persistence implementation.
// If backup is provided, periodic snapshots will be saved to the backup
// persistence layer at the specified interval.
func NewMemoryPersistence(backup TorrentPersistence, snapshotInterval time.Duration) *MemoryPersistence {
	mp := &MemoryPersistence{
		torrents:         make(map[metainfo.Hash]*rpc.TorrentState),
		backup:           backup,
		snapshotInterval: snapshotInterval,
		stopSnapshot:     make(chan struct{}),
	}

	// Start periodic snapshots if backup is provided
	if backup != nil && snapshotInterval > 0 {
		mp.startSnapshotRoutine()
	}

	return mp
}

// SaveTorrent persists a single torrent's state in memory
func (mp *MemoryPersistence) SaveTorrent(ctx context.Context, torrent *rpc.TorrentState) error {
	if torrent == nil {
		return fmt.Errorf("torrent cannot be nil")
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Deep copy to prevent external modifications
	mp.torrents[torrent.InfoHash] = mp.deepCopyTorrent(torrent)
	mp.writeCount++

	return nil
}

// LoadTorrent retrieves a torrent's state by info hash
func (mp *MemoryPersistence) LoadTorrent(ctx context.Context, infoHash metainfo.Hash) (*rpc.TorrentState, error) {
	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mp.mu.RLock()
	defer mp.mu.RUnlock()

	torrent, exists := mp.torrents[infoHash]
	if !exists {
		return nil, fmt.Errorf("torrent not found: %s", infoHash.String())
	}

	mp.readCount++
	// Return a deep copy to prevent external modifications
	return mp.deepCopyTorrent(torrent), nil
}

// SaveAllTorrents persists the state of multiple torrents in memory
func (mp *MemoryPersistence) SaveAllTorrents(ctx context.Context, torrents []*rpc.TorrentState) error {
	if len(torrents) == 0 {
		return nil
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Validate all torrents first
	for _, torrent := range torrents {
		if torrent == nil {
			return fmt.Errorf("torrent cannot be nil")
		}
	}

	// Save all torrents (this is atomic in memory)
	for _, torrent := range torrents {
		mp.torrents[torrent.InfoHash] = mp.deepCopyTorrent(torrent)
	}

	mp.writeCount += int64(len(torrents))
	return nil
}

// LoadAllTorrents retrieves all persisted torrent states
func (mp *MemoryPersistence) LoadAllTorrents(ctx context.Context) ([]*rpc.TorrentState, error) {
	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mp.mu.RLock()
	defer mp.mu.RUnlock()

	torrents := make([]*rpc.TorrentState, 0, len(mp.torrents))
	for _, torrent := range mp.torrents {
		torrents = append(torrents, mp.deepCopyTorrent(torrent))
	}

	mp.readCount++
	return torrents, nil
}

// DeleteTorrent removes a torrent's persisted state
func (mp *MemoryPersistence) DeleteTorrent(ctx context.Context, infoHash metainfo.Hash) error {
	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	if _, exists := mp.torrents[infoHash]; !exists {
		return fmt.Errorf("torrent not found: %s", infoHash.String())
	}

	delete(mp.torrents, infoHash)
	mp.deleteCount++
	return nil
}

// SaveSessionConfig persists session configuration in memory
func (mp *MemoryPersistence) SaveSessionConfig(ctx context.Context, config *rpc.SessionConfiguration) error {
	if config == nil {
		return fmt.Errorf("session configuration cannot be nil")
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Deep copy the configuration
	mp.sessionConfig = mp.deepCopySessionConfig(config)
	mp.writeCount++

	return nil
}

// LoadSessionConfig retrieves persisted session configuration
func (mp *MemoryPersistence) LoadSessionConfig(ctx context.Context) (*rpc.SessionConfiguration, error) {
	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mp.mu.RLock()
	defer mp.mu.RUnlock()

	if mp.sessionConfig == nil {
		return nil, fmt.Errorf("session configuration not found")
	}

	mp.readCount++
	return mp.deepCopySessionConfig(mp.sessionConfig), nil
}

// Close cleans up resources and stops background routines
func (mp *MemoryPersistence) Close() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Stop snapshot routine if running
	if mp.snapshotRunning {
		close(mp.stopSnapshot)
		mp.snapshotRunning = false
	}

	// Perform final snapshot if backup is available
	if mp.backup != nil {
		ctx := context.Background()
		mp.performSnapshot(ctx)
		return mp.backup.Close()
	}

	return nil
}

// GetStatistics returns usage statistics for monitoring
func (mp *MemoryPersistence) GetStatistics() map[string]interface{} {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	return map[string]interface{}{
		"torrent_count":     len(mp.torrents),
		"read_operations":   mp.readCount,
		"write_operations":  mp.writeCount,
		"delete_operations": mp.deleteCount,
		"last_snapshot":     mp.lastSnapshot,
		"has_backup":        mp.backup != nil,
		"snapshot_interval": mp.snapshotInterval.String(),
	}
}

// ForceSnapshot triggers an immediate snapshot to the backup persistence
func (mp *MemoryPersistence) ForceSnapshot(ctx context.Context) error {
	if mp.backup == nil {
		return fmt.Errorf("no backup persistence configured")
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	return mp.performSnapshot(ctx)
}

// LoadFromBackup restores state from the backup persistence layer
func (mp *MemoryPersistence) LoadFromBackup(ctx context.Context) error {
	if mp.backup == nil {
		return fmt.Errorf("no backup persistence configured")
	}

	// Load torrents from backup
	torrents, err := mp.backup.LoadAllTorrents(ctx)
	if err != nil {
		return fmt.Errorf("failed to load torrents from backup: %w", err)
	}

	// Load session config from backup
	sessionConfig, err := mp.backup.LoadSessionConfig(ctx)
	if err != nil && err.Error() != "session configuration not found" {
		return fmt.Errorf("failed to load session config from backup: %w", err)
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Clear current state
	mp.torrents = make(map[metainfo.Hash]*rpc.TorrentState)

	// Restore torrents
	for _, torrent := range torrents {
		mp.torrents[torrent.InfoHash] = mp.deepCopyTorrent(torrent)
	}

	// Restore session config
	if sessionConfig != nil {
		mp.sessionConfig = mp.deepCopySessionConfig(sessionConfig)
	}

	return nil
}

// Helper methods

func (mp *MemoryPersistence) startSnapshotRoutine() {
	mp.snapshotRunning = true
	go func() {
		ticker := time.NewTicker(mp.snapshotInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ctx := context.Background()
				mp.mu.Lock()
				mp.performSnapshot(ctx)
				mp.mu.Unlock()

			case <-mp.stopSnapshot:
				return
			}
		}
	}()
}

func (mp *MemoryPersistence) performSnapshot(ctx context.Context) error {
	if mp.backup == nil {
		return nil
	}

	// Save all torrents to backup
	var torrents []*rpc.TorrentState
	for _, torrent := range mp.torrents {
		torrents = append(torrents, torrent)
	}

	if len(torrents) > 0 {
		if err := mp.backup.SaveAllTorrents(ctx, torrents); err != nil {
			return err
		}
	}

	// Save session config to backup
	if mp.sessionConfig != nil {
		if err := mp.backup.SaveSessionConfig(ctx, mp.sessionConfig); err != nil {
			return err
		}
	}

	mp.lastSnapshot = time.Now()
	return nil
}

func (mp *MemoryPersistence) deepCopyTorrent(src *rpc.TorrentState) *rpc.TorrentState {
	if src == nil {
		return nil
	}

	dst := &rpc.TorrentState{
		ID:                  src.ID,
		InfoHash:            src.InfoHash,
		MetaInfo:            src.MetaInfo, // MetaInfo is typically immutable, so shallow copy is OK
		Status:              src.Status,
		Error:               src.Error,
		DownloadDir:         src.DownloadDir,
		AddedDate:           src.AddedDate,
		StartDate:           src.StartDate,
		Downloaded:          src.Downloaded,
		Uploaded:            src.Uploaded,
		Left:                src.Left,
		PercentDone:         src.PercentDone,
		PieceCount:          src.PieceCount,
		PiecesComplete:      src.PiecesComplete,
		PiecesAvailable:     src.PiecesAvailable,
		DownloadRate:        src.DownloadRate,
		UploadRate:          src.UploadRate,
		ETA:                 src.ETA,
		PeerCount:           src.PeerCount,
		PeerConnectedCount:  src.PeerConnectedCount,
		PeerSendingCount:    src.PeerSendingCount,
		PeerReceivingCount:  src.PeerReceivingCount,
		SeedRatioLimit:      src.SeedRatioLimit,
		SeedIdleLimit:       src.SeedIdleLimit,
		HonorsSessionLimits: src.HonorsSessionLimits,
	}

	// Deep copy slices
	if src.Labels != nil {
		dst.Labels = make([]string, len(src.Labels))
		copy(dst.Labels, src.Labels)
	}

	if src.Files != nil {
		dst.Files = make([]rpc.FileInfo, len(src.Files))
		copy(dst.Files, src.Files)
	}

	if src.Priorities != nil {
		dst.Priorities = make([]int64, len(src.Priorities))
		copy(dst.Priorities, src.Priorities)
	}

	if src.Wanted != nil {
		dst.Wanted = make([]bool, len(src.Wanted))
		copy(dst.Wanted, src.Wanted)
	}

	if src.TrackerList != nil {
		dst.TrackerList = make([]string, len(src.TrackerList))
		copy(dst.TrackerList, src.TrackerList)
	}

	if src.Peers != nil {
		dst.Peers = make([]rpc.PeerInfo, len(src.Peers))
		copy(dst.Peers, src.Peers)
	}

	return dst
}

func (mp *MemoryPersistence) deepCopySessionConfig(src *rpc.SessionConfiguration) *rpc.SessionConfiguration {
	if src == nil {
		return nil
	}

	// SessionConfiguration contains only basic types, so shallow copy is sufficient
	dst := *src
	return &dst
}
