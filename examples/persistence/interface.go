// Copyright 2025 go-i2ppackage persistence

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

// Package persistence provides example implementations of different persistence
// strategies for torrent state management in downstream applications.
//
// The go-i2p-bt library intentionally does not include opinionated persistence
// mechanisms. Instead, it provides hooks and extensibility points that allow
// downstream applications to implement their own state management strategies
// based on their specific requirements.
//
// This package demonstrates three common approaches:
//   - File-based persistence using JSON
//   - Database persistence using SQLite
//   - In-memory persistence with optional snapshotting
//
// Each implementation can be used as a starting point for more sophisticated
// persistence strategies in production applications.
package persistence

import (
	"context"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	"github.com/go-i2p/go-i2p-bt/rpc"
)

// TorrentPersistence defines the interface that all persistence implementations
// should implement for managing torrent state across application restarts.
//
// This interface provides the basic operations needed to persist and restore
// torrent state, configuration, and statistics. Implementations can choose
// to persist all fields or a subset based on their requirements.
type TorrentPersistence interface {
	// SaveTorrent persists a single torrent's state
	SaveTorrent(ctx context.Context, torrent *rpc.TorrentState) error

	// LoadTorrent retrieves a torrent's state by info hash
	LoadTorrent(ctx context.Context, infoHash metainfo.Hash) (*rpc.TorrentState, error)

	// SaveAllTorrents persists the state of multiple torrents atomically
	SaveAllTorrents(ctx context.Context, torrents []*rpc.TorrentState) error

	// LoadAllTorrents retrieves all persisted torrent states
	LoadAllTorrents(ctx context.Context) ([]*rpc.TorrentState, error)

	// DeleteTorrent removes a torrent's persisted state
	DeleteTorrent(ctx context.Context, infoHash metainfo.Hash) error

	// SaveSessionConfig persists session configuration
	SaveSessionConfig(ctx context.Context, config *rpc.SessionConfiguration) error

	// LoadSessionConfig retrieves persisted session configuration
	LoadSessionConfig(ctx context.Context) (*rpc.SessionConfiguration, error)

	// Close cleans up any resources used by the persistence layer
	Close() error
}

// TorrentMetrics represents performance and usage metrics that can be
// persisted for historical analysis and monitoring.
type TorrentMetrics struct {
	InfoHash        metainfo.Hash `json:"info_hash"`
	Timestamp       time.Time     `json:"timestamp"`
	Downloaded      int64         `json:"downloaded"`
	Uploaded        int64         `json:"uploaded"`
	DownloadRate    int64         `json:"download_rate"`
	UploadRate      int64         `json:"upload_rate"`
	PeerCount       int64         `json:"peer_count"`
	SeedingTime     time.Duration `json:"seeding_time"`
	DownloadingTime time.Duration `json:"downloading_time"`
}

// MetricsPersistence defines optional interface for persisting historical
// metrics data. This can be implemented separately from torrent state
// persistence for applications that need detailed analytics.
type MetricsPersistence interface {
	// SaveMetrics persists torrent metrics for a specific timestamp
	SaveMetrics(ctx context.Context, metrics []TorrentMetrics) error

	// LoadMetrics retrieves metrics for a specific torrent within a time range
	LoadMetrics(ctx context.Context, infoHash metainfo.Hash, from, to time.Time) ([]TorrentMetrics, error)

	// LoadMetricsSummary retrieves aggregated metrics for reporting
	LoadMetricsSummary(ctx context.Context, from, to time.Time) (map[metainfo.Hash]TorrentMetrics, error)

	// PurgeOldMetrics removes metrics older than the specified duration
	PurgeOldMetrics(ctx context.Context, olderThan time.Duration) error
}

// PersistenceHook is a convenience type that implements hook callbacks
// for automatic persistence of torrent state changes.
//
// Usage:
//
//	persistence := NewFilePersistence("/data/torrents")
//	hook := NewPersistenceHook(persistence)
//	hookManager.RegisterHook(&rpc.Hook{
//	  ID: "persistence",
//	  Callback: hook.HandleTorrentEvent,
//	  Events: []rpc.HookEvent{
//	    rpc.HookEventTorrentAdded,
//	    rpc.HookEventTorrentRemoved,
//	    rpc.HookEventTorrentCompleted,
//	  },
//	})
type PersistenceHook struct {
	persistence TorrentPersistence
}

// NewPersistenceHook creates a hook that automatically persists torrent
// state changes using the provided persistence implementation.
func NewPersistenceHook(persistence TorrentPersistence) *PersistenceHook {
	return &PersistenceHook{
		persistence: persistence,
	}
}

// HandleTorrentEvent is a hook callback that persists torrent state changes
func (ph *PersistenceHook) HandleTorrentEvent(ctx *rpc.HookContext) error {
	switch ctx.Event {
	case rpc.HookEventTorrentAdded, rpc.HookEventTorrentStarted,
		rpc.HookEventTorrentStopped, rpc.HookEventTorrentCompleted:
		// Save torrent state for these events
		return ph.persistence.SaveTorrent(ctx.Context, ctx.Torrent)

	case rpc.HookEventTorrentRemoved:
		// Delete torrent state when removed
		return ph.persistence.DeleteTorrent(ctx.Context, ctx.Torrent.InfoHash)

	default:
		// No action needed for other events
		return nil
	}
}
