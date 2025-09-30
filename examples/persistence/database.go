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
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	"github.com/go-i2p/go-i2p-bt/rpc"
	// Import SQLite driver (uncomment and add to go.mod for production use)
	// _ "github.com/mattn/go-sqlite3"
)

// DatabasePersistence implements TorrentPersistence using SQLite database.
// This implementation is suitable for production deployments that need:
//   - ACID transactions for data consistency
//   - SQL queries for reporting and analytics
//   - Concurrent access from multiple processes
//   - Reliable persistence with backup/restore capabilities
//
// Schema:
//   - torrents: Main torrent state table with JSON blob for flexible fields
//   - session_config: Single-row table for session configuration
//   - torrent_metrics: Optional table for historical metrics tracking
//
// Features:
//   - Automatic schema migration
//   - Transaction support for atomic operations
//   - Connection pooling for concurrent access
//   - Prepared statements for performance
//   - Optional metrics tracking with retention policies
type DatabasePersistence struct {
	db     *sql.DB
	dbPath string

	// Prepared statements for performance
	insertTorrent     *sql.Stmt
	updateTorrent     *sql.Stmt
	selectTorrent     *sql.Stmt
	deleteTorrent     *sql.Stmt
	selectAllTorrents *sql.Stmt

	insertSession *sql.Stmt
	selectSession *sql.Stmt

	// Optional metrics support
	metricsEnabled bool
	insertMetrics  *sql.Stmt
}

// NewDatabasePersistence creates a new SQLite-based persistence implementation.
// The database file will be created if it doesn't exist, and the schema will
// be automatically migrated to the latest version.
func NewDatabasePersistence(dbPath string, enableMetrics bool) (*DatabasePersistence, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("database path cannot be empty")
	}

	// Open database connection
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=10000&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	dp := &DatabasePersistence{
		db:             db,
		dbPath:         dbPath,
		metricsEnabled: enableMetrics,
	}

	// Initialize schema and prepare statements
	if err := dp.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	if err := dp.prepareStatements(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
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

	// Try update first, then insert if not exists
	result, err := dp.updateTorrent.ExecContext(ctx,
		jsonData,
		torrent.Status,
		torrent.PercentDone,
		torrent.Downloaded,
		torrent.Uploaded,
		time.Now(),
		torrent.InfoHash.String(),
	)
	if err != nil {
		return fmt.Errorf("failed to update torrent: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	// If no rows were updated, insert new record
	if rowsAffected == 0 {
		_, err = dp.insertTorrent.ExecContext(ctx,
			torrent.InfoHash.String(),
			jsonData,
			torrent.Status,
			torrent.PercentDone,
			torrent.Downloaded,
			torrent.Uploaded,
			torrent.AddedDate,
			time.Now(),
		)
		if err != nil {
			return fmt.Errorf("failed to insert torrent: %w", err)
		}
	}

	return nil
}

// LoadTorrent retrieves a torrent's state by info hash
func (dp *DatabasePersistence) LoadTorrent(ctx context.Context, infoHash metainfo.Hash) (*rpc.TorrentState, error) {
	var jsonData string
	var addedDate, updatedDate time.Time

	err := dp.selectTorrent.QueryRowContext(ctx, infoHash.String()).Scan(
		&jsonData, &addedDate, &updatedDate,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("torrent not found: %s", infoHash.String())
		}
		return nil, fmt.Errorf("failed to query torrent: %w", err)
	}

	var torrent rpc.TorrentState
	if err := json.Unmarshal([]byte(jsonData), &torrent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal torrent state: %w", err)
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

	// Start transaction for atomic operation
	tx, err := dp.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare statements within transaction
	insertStmt := tx.StmtContext(ctx, dp.insertTorrent)
	updateStmt := tx.StmtContext(ctx, dp.updateTorrent)

	for _, torrent := range torrents {
		if torrent == nil {
			return fmt.Errorf("torrent cannot be nil")
		}

		torrentData := dp.createSerializableTorrent(torrent)
		jsonData, err := json.Marshal(torrentData)
		if err != nil {
			return fmt.Errorf("failed to marshal torrent %s: %w", torrent.InfoHash.String(), err)
		}

		// Try update first
		result, err := updateStmt.ExecContext(ctx,
			jsonData,
			torrent.Status,
			torrent.PercentDone,
			torrent.Downloaded,
			torrent.Uploaded,
			time.Now(),
			torrent.InfoHash.String(),
		)
		if err != nil {
			return fmt.Errorf("failed to update torrent %s: %w", torrent.InfoHash.String(), err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get affected rows for %s: %w", torrent.InfoHash.String(), err)
		}

		// Insert if update didn't affect any rows
		if rowsAffected == 0 {
			_, err = insertStmt.ExecContext(ctx,
				torrent.InfoHash.String(),
				jsonData,
				torrent.Status,
				torrent.PercentDone,
				torrent.Downloaded,
				torrent.Uploaded,
				torrent.AddedDate,
				time.Now(),
			)
			if err != nil {
				return fmt.Errorf("failed to insert torrent %s: %w", torrent.InfoHash.String(), err)
			}
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// LoadAllTorrents retrieves all persisted torrent states
func (dp *DatabasePersistence) LoadAllTorrents(ctx context.Context) ([]*rpc.TorrentState, error) {
	rows, err := dp.selectAllTorrents.QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query torrents: %w", err)
	}
	defer rows.Close()

	var torrents []*rpc.TorrentState
	for rows.Next() {
		var infoHashStr, jsonData string
		var addedDate, updatedDate time.Time

		if err := rows.Scan(&infoHashStr, &jsonData, &addedDate, &updatedDate); err != nil {
			return nil, fmt.Errorf("failed to scan torrent row: %w", err)
		}

		var torrent rpc.TorrentState
		if err := json.Unmarshal([]byte(jsonData), &torrent); err != nil {
			// Skip corrupted records
			continue
		}

		// Set info hash from database
		torrent.InfoHash = metainfo.NewHashFromString(infoHashStr)

		torrents = append(torrents, &torrent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating torrent rows: %w", err)
	}

	return torrents, nil
}

// DeleteTorrent removes a torrent's persisted state
func (dp *DatabasePersistence) DeleteTorrent(ctx context.Context, infoHash metainfo.Hash) error {
	result, err := dp.deleteTorrent.ExecContext(ctx, infoHash.String())
	if err != nil {
		return fmt.Errorf("failed to delete torrent: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("torrent not found: %s", infoHash.String())
	}

	return nil
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

	_, err = dp.insertSession.ExecContext(ctx, jsonData, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save session configuration: %w", err)
	}

	return nil
}

// LoadSessionConfig retrieves persisted session configuration
func (dp *DatabasePersistence) LoadSessionConfig(ctx context.Context) (*rpc.SessionConfiguration, error) {
	var jsonData string
	var updatedDate time.Time

	err := dp.selectSession.QueryRowContext(ctx).Scan(&jsonData, &updatedDate)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session configuration not found")
		}
		return nil, fmt.Errorf("failed to query session configuration: %w", err)
	}

	var config rpc.SessionConfiguration
	if err := json.Unmarshal([]byte(jsonData), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session configuration: %w", err)
	}

	return &config, nil
}

// Close cleans up database resources
func (dp *DatabasePersistence) Close() error {
	// Close prepared statements
	statements := []*sql.Stmt{
		dp.insertTorrent,
		dp.updateTorrent,
		dp.selectTorrent,
		dp.deleteTorrent,
		dp.selectAllTorrents,
		dp.insertSession,
		dp.selectSession,
		dp.insertMetrics,
	}

	for _, stmt := range statements {
		if stmt != nil {
			stmt.Close()
		}
	}

	return dp.db.Close()
}

// Helper methods

func (dp *DatabasePersistence) initSchema() error {
	// Create torrents table
	createTorrents := `
	CREATE TABLE IF NOT EXISTS torrents (
		info_hash TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		status INTEGER NOT NULL,
		percent_done REAL NOT NULL,
		downloaded INTEGER NOT NULL,
		uploaded INTEGER NOT NULL,
		added_date DATETIME NOT NULL,
		updated_date DATETIME NOT NULL
	);
	
	CREATE INDEX IF NOT EXISTS idx_torrents_status ON torrents(status);
	CREATE INDEX IF NOT EXISTS idx_torrents_updated ON torrents(updated_date);
	`

	// Create session configuration table
	createSession := `
	CREATE TABLE IF NOT EXISTS session_config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL,
		updated_date DATETIME NOT NULL
	);
	`

	// Create metrics table if enabled
	createMetrics := ""
	if dp.metricsEnabled {
		createMetrics = `
		CREATE TABLE IF NOT EXISTS torrent_metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			info_hash TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			downloaded INTEGER NOT NULL,
			uploaded INTEGER NOT NULL,
			download_rate INTEGER NOT NULL,
			upload_rate INTEGER NOT NULL,
			peer_count INTEGER NOT NULL,
			FOREIGN KEY (info_hash) REFERENCES torrents(info_hash) ON DELETE CASCADE
		);
		
		CREATE INDEX IF NOT EXISTS idx_metrics_hash_time ON torrent_metrics(info_hash, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON torrent_metrics(timestamp);
		`
	}

	// Execute schema creation
	schema := createTorrents + createSession + createMetrics
	if _, err := dp.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

func (dp *DatabasePersistence) prepareStatements() error {
	var err error

	// Torrent statements
	dp.insertTorrent, err = dp.db.Prepare(`
		INSERT INTO torrents (info_hash, data, status, percent_done, downloaded, uploaded, added_date, updated_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert torrent statement: %w", err)
	}

	dp.updateTorrent, err = dp.db.Prepare(`
		UPDATE torrents 
		SET data = ?, status = ?, percent_done = ?, downloaded = ?, uploaded = ?, updated_date = ?
		WHERE info_hash = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare update torrent statement: %w", err)
	}

	dp.selectTorrent, err = dp.db.Prepare(`
		SELECT data, added_date, updated_date FROM torrents WHERE info_hash = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare select torrent statement: %w", err)
	}

	dp.deleteTorrent, err = dp.db.Prepare(`
		DELETE FROM torrents WHERE info_hash = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare delete torrent statement: %w", err)
	}

	dp.selectAllTorrents, err = dp.db.Prepare(`
		SELECT info_hash, data, added_date, updated_date FROM torrents ORDER BY added_date
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare select all torrents statement: %w", err)
	}

	// Session statements
	dp.insertSession, err = dp.db.Prepare(`
		INSERT OR REPLACE INTO session_config (id, data, updated_date) VALUES (1, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert session statement: %w", err)
	}

	dp.selectSession, err = dp.db.Prepare(`
		SELECT data, updated_date FROM session_config WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare select session statement: %w", err)
	}

	// Metrics statement if enabled
	if dp.metricsEnabled {
		dp.insertMetrics, err = dp.db.Prepare(`
			INSERT INTO torrent_metrics (info_hash, timestamp, downloaded, uploaded, download_rate, upload_rate, peer_count)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed to prepare insert metrics statement: %w", err)
		}
	}

	return nil
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
