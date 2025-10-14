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
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	"github.com/go-i2p/go-i2p-bt/rpc"
	"go.etcd.io/bbolt"
)

// DatabasePersistence implements TorrentPersistence using bbolt (BoltDB) database.
// This implementation is suitable for production deployments that need:
//   - ACID transactions for data consistency
//   - Embedded key-value storage without external dependencies
//   - Concurrent read access with single-writer transactions
//   - Reliable persistence with crash recovery
//
// Buckets:
//   - torrents: Main torrent state bucket with JSON values
//   - session: Single-key bucket for session configuration
//   - metrics: Optional bucket for historical metrics tracking
//
// Features:
//   - Automatic bucket initialization
//   - ACID transaction support for atomic operations
//   - MVCC for concurrent readers
//   - No external database dependencies
//   - Optional metrics tracking with retention policies
type DatabasePersistence struct {
	db     *bbolt.DB
	dbPath string

	// Optional metrics support
	metricsEnabled bool
}

// NewDatabasePersistence creates a new bbolt-based persistence implementation.
// The database file will be created if it doesn't exist, and the required buckets
// will be automatically initialized.
func NewDatabasePersistence(dbPath string, enableMetrics bool) (*DatabasePersistence, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("database path cannot be empty")
	}

	// Open database connection with default options
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	dp := &DatabasePersistence{
		db:             db,
		dbPath:         dbPath,
		metricsEnabled: enableMetrics,
	}

	// Initialize buckets
	if err := dp.initBuckets(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	return dp, nil
}

// SaveTorrent persists a single torrent's state to the database
func (dp *DatabasePersistence) SaveTorrent(ctx context.Context, torrent *rpc.TorrentState) error {
	if torrent == nil {
		return fmt.Errorf("torrent cannot be nil")
	}

	// Serialize torrent state to JSON (excluding MetaInfo for size)
	torrentData := dp.createSerializableTorrent(torrent)
	jsonData, err := json.Marshal(torrentData)
	if err != nil {
		return fmt.Errorf("failed to marshal torrent state: %w", err)
	}

	// Store in bbolt
	return dp.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("torrents"))
		if bucket == nil {
			return fmt.Errorf("torrents bucket not found")
		}

		return bucket.Put([]byte(torrent.InfoHash.String()), jsonData)
	})
}

// LoadTorrent retrieves a torrent's state by info hash
func (dp *DatabasePersistence) LoadTorrent(ctx context.Context, infoHash metainfo.Hash) (*rpc.TorrentState, error) {
	var torrent rpc.TorrentState

	err := dp.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("torrents"))
		if bucket == nil {
			return fmt.Errorf("torrents bucket not found")
		}

		jsonData := bucket.Get([]byte(infoHash.String()))
		if jsonData == nil {
			return fmt.Errorf("torrent not found: %s", infoHash.String())
		}

		return json.Unmarshal(jsonData, &torrent)
	})

	if err != nil {
		return nil, err
	}

	// Ensure info hash is set correctly
	torrent.InfoHash = infoHash

	return &torrent, nil
}

// SaveAllTorrents persists the state of multiple torrents atomically using a transaction
func (dp *DatabasePersistence) SaveAllTorrents(ctx context.Context, torrents []*rpc.TorrentState) error {
	if len(torrents) == 0 {
		return nil
	}

	return dp.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("torrents"))
		if bucket == nil {
			return fmt.Errorf("torrents bucket not found")
		}

		for _, torrent := range torrents {
			if torrent == nil {
				return fmt.Errorf("torrent cannot be nil")
			}

			torrentData := dp.createSerializableTorrent(torrent)
			jsonData, err := json.Marshal(torrentData)
			if err != nil {
				return fmt.Errorf("failed to marshal torrent %s: %w", torrent.InfoHash.String(), err)
			}

			if err := bucket.Put([]byte(torrent.InfoHash.String()), jsonData); err != nil {
				return fmt.Errorf("failed to save torrent %s: %w", torrent.InfoHash.String(), err)
			}
		}

		return nil
	})
}

// LoadAllTorrents retrieves all persisted torrent states
func (dp *DatabasePersistence) LoadAllTorrents(ctx context.Context) ([]*rpc.TorrentState, error) {
	var torrents []*rpc.TorrentState

	err := dp.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("torrents"))
		if bucket == nil {
			return fmt.Errorf("torrents bucket not found")
		}

		return bucket.ForEach(func(k, v []byte) error {
			var torrent rpc.TorrentState
			if err := json.Unmarshal(v, &torrent); err != nil {
				// Skip corrupted records
				return nil
			}

			// Set info hash from key
			torrent.InfoHash = metainfo.NewHashFromString(string(k))
			torrents = append(torrents, &torrent)

			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return torrents, nil
}

// DeleteTorrent removes a torrent's persisted state
func (dp *DatabasePersistence) DeleteTorrent(ctx context.Context, infoHash metainfo.Hash) error {
	return dp.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("torrents"))
		if bucket == nil {
			return fmt.Errorf("torrents bucket not found")
		}

		// Check if torrent exists first
		if bucket.Get([]byte(infoHash.String())) == nil {
			return fmt.Errorf("torrent not found: %s", infoHash.String())
		}

		return bucket.Delete([]byte(infoHash.String()))
	})
}

// SaveSessionConfig persists session configuration
func (dp *DatabasePersistence) SaveSessionConfig(ctx context.Context, config *rpc.SessionConfiguration) error {
	if config == nil {
		return fmt.Errorf("session configuration cannot be nil")
	}

	jsonData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal session configuration: %w", err)
	}

	return dp.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("session"))
		if bucket == nil {
			return fmt.Errorf("session bucket not found")
		}

		return bucket.Put([]byte("config"), jsonData)
	})
}

// LoadSessionConfig retrieves persisted session configuration
func (dp *DatabasePersistence) LoadSessionConfig(ctx context.Context) (*rpc.SessionConfiguration, error) {
	var config rpc.SessionConfiguration

	err := dp.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("session"))
		if bucket == nil {
			return fmt.Errorf("session bucket not found")
		}

		jsonData := bucket.Get([]byte("config"))
		if jsonData == nil {
			return fmt.Errorf("session configuration not found")
		}

		return json.Unmarshal(jsonData, &config)
	})

	if err != nil {
		return nil, err
	}

	return &config, nil
}

// Close cleans up database resources
func (dp *DatabasePersistence) Close() error {
	return dp.db.Close()
}

// initBuckets initializes all required bbolt buckets
func (dp *DatabasePersistence) initBuckets() error {
	return dp.db.Update(func(tx *bbolt.Tx) error {
		// Create torrents bucket
		if _, err := tx.CreateBucketIfNotExists([]byte("torrents")); err != nil {
			return fmt.Errorf("failed to create torrents bucket: %w", err)
		}

		// Create session bucket
		if _, err := tx.CreateBucketIfNotExists([]byte("session")); err != nil {
			return fmt.Errorf("failed to create session bucket: %w", err)
		}

		// Create metrics bucket if enabled
		if dp.metricsEnabled {
			if _, err := tx.CreateBucketIfNotExists([]byte("metrics")); err != nil {
				return fmt.Errorf("failed to create metrics bucket: %w", err)
			}
		}

		return nil
	})
}

func (dp *DatabasePersistence) createSerializableTorrent(torrent *rpc.TorrentState) map[string]interface{} {
	// Create a map excluding non-serializable fields and large binary data
	return map[string]interface{}{
		"id":                    torrent.ID,
		"info_hash":             torrent.InfoHash.String(),
		"status":                torrent.Status,
		"download_dir":          torrent.DownloadDir,
		"added_date":            torrent.AddedDate,
		"start_date":            torrent.StartDate,
		"labels":                torrent.Labels,
		"downloaded":            torrent.Downloaded,
		"uploaded":              torrent.Uploaded,
		"left":                  torrent.Left,
		"percent_done":          torrent.PercentDone,
		"piece_count":           torrent.PieceCount,
		"pieces_complete":       torrent.PiecesComplete,
		"pieces_available":      torrent.PiecesAvailable,
		"download_rate":         torrent.DownloadRate,
		"upload_rate":           torrent.UploadRate,
		"eta":                   torrent.ETA,
		"peer_count":            torrent.PeerCount,
		"peer_connected_count":  torrent.PeerConnectedCount,
		"peer_sending_count":    torrent.PeerSendingCount,
		"peer_receiving_count":  torrent.PeerReceivingCount,
		"files":                 torrent.Files,
		"priorities":            torrent.Priorities,
		"wanted":                torrent.Wanted,
		"tracker_list":          torrent.TrackerList,
		"seed_ratio_limit":      torrent.SeedRatioLimit,
		"seed_idle_limit":       torrent.SeedIdleLimit,
		"honors_session_limits": torrent.HonorsSessionLimits,
	}
}
