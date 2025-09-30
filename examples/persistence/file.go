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
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	"github.com/go-i2p/go-i2p-bt/rpc"
)

// FilePersistence implements TorrentPersistence using JSON files on disk.
// This is a simple persistence strategy suitable for small to medium deployments
// where database overhead is not desired.
//
// Structure:
//   - dataDir/torrents/[infohash].json - Individual torrent state files
//   - dataDir/session.json - Session configuration
//   - dataDir/metadata/ - MetaInfo files for reconstruction
//
// This implementation provides:
//   - Atomic writes using temporary files
//   - Concurrent access safety with file locking simulation
//   - Automatic directory structure creation
//   - MetaInfo preservation for complete state restoration
type FilePersistence struct {
	dataDir     string
	torrentsDir string
	metadataDir string
	sessionFile string
	mu          sync.RWMutex
}

// NewFilePersistence creates a new file-based persistence implementation.
// The dataDir will be created if it doesn't exist, along with required subdirectories.
func NewFilePersistence(dataDir string) (*FilePersistence, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("data directory cannot be empty")
	}

	fp := &FilePersistence{
		dataDir:     dataDir,
		torrentsDir: filepath.Join(dataDir, "torrents"),
		metadataDir: filepath.Join(dataDir, "metadata"),
		sessionFile: filepath.Join(dataDir, "session.json"),
	}

	// Create directory structure
	if err := os.MkdirAll(fp.torrentsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create torrents directory: %w", err)
	}
	if err := os.MkdirAll(fp.metadataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create metadata directory: %w", err)
	}

	return fp, nil
}

// SaveTorrent persists a single torrent's state to a JSON file
func (fp *FilePersistence) SaveTorrent(ctx context.Context, torrent *rpc.TorrentState) error {
	if torrent == nil {
		return fmt.Errorf("torrent cannot be nil")
	}

	fp.mu.Lock()
	defer fp.mu.Unlock()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Create a serializable copy (exclude non-serializable fields)
	serializable := fp.createSerializableTorrent(torrent)

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(serializable, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal torrent state: %w", err)
	}

	// Write atomically using temporary file
	torrentFile := fp.getTorrentFilePath(torrent.InfoHash)
	tempFile := torrentFile + ".tmp"

	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, torrentFile); err != nil {
		os.Remove(tempFile) // Clean up on failure
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	// Save MetaInfo separately if available
	if torrent.MetaInfo != nil {
		if err := fp.saveMetaInfo(torrent.InfoHash, torrent.MetaInfo); err != nil {
			// Log error but don't fail the operation
			// MetaInfo can be reconstructed from other sources
		}
	}

	return nil
}

// LoadTorrent retrieves a torrent's state by info hash
func (fp *FilePersistence) LoadTorrent(ctx context.Context, infoHash metainfo.Hash) (*rpc.TorrentState, error) {
	fp.mu.RLock()
	defer fp.mu.RUnlock()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	torrentFile := fp.getTorrentFilePath(infoHash)
	data, err := os.ReadFile(torrentFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("torrent not found: %s", infoHash.String())
		}
		return nil, fmt.Errorf("failed to read torrent file: %w", err)
	}

	var torrent rpc.TorrentState
	if err := json.Unmarshal(data, &torrent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal torrent state: %w", err)
	}

	// Load MetaInfo if available
	if metaInfo, err := fp.loadMetaInfo(infoHash); err == nil {
		torrent.MetaInfo = metaInfo
	}

	return &torrent, nil
}

// SaveAllTorrents persists the state of multiple torrents atomically
// by writing to temporary files first, then renaming all at once
func (fp *FilePersistence) SaveAllTorrents(ctx context.Context, torrents []*rpc.TorrentState) error {
	if len(torrents) == 0 {
		return nil
	}

	fp.mu.Lock()
	defer fp.mu.Unlock()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Prepare all temporary files first
	tempFiles := make(map[string]string) // target -> temp
	cleanup := func() {
		for _, tempFile := range tempFiles {
			os.Remove(tempFile)
		}
	}

	// Write all torrents to temporary files
	for _, torrent := range torrents {
		if torrent == nil {
			cleanup()
			return fmt.Errorf("torrent cannot be nil")
		}

		serializable := fp.createSerializableTorrent(torrent)
		data, err := json.MarshalIndent(serializable, "", "  ")
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to marshal torrent %s: %w", torrent.InfoHash.String(), err)
		}

		torrentFile := fp.getTorrentFilePath(torrent.InfoHash)
		tempFile := torrentFile + ".tmp"
		tempFiles[torrentFile] = tempFile

		if err := os.WriteFile(tempFile, data, 0644); err != nil {
			cleanup()
			return fmt.Errorf("failed to write temporary file for %s: %w", torrent.InfoHash.String(), err)
		}

		// Save MetaInfo if available
		if torrent.MetaInfo != nil {
			fp.saveMetaInfo(torrent.InfoHash, torrent.MetaInfo)
		}
	}

	// Atomically rename all files
	for target, temp := range tempFiles {
		if err := os.Rename(temp, target); err != nil {
			cleanup()
			return fmt.Errorf("failed to rename temporary file %s: %w", temp, err)
		}
	}

	return nil
}

// LoadAllTorrents retrieves all persisted torrent states
func (fp *FilePersistence) LoadAllTorrents(ctx context.Context) ([]*rpc.TorrentState, error) {
	fp.mu.RLock()
	defer fp.mu.RUnlock()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(fp.torrentsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read torrents directory: %w", err)
	}

	var torrents []*rpc.TorrentState
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Extract info hash from filename
		hashStr := strings.TrimSuffix(entry.Name(), ".json")
		infoHash := metainfo.NewHashFromString(hashStr)

		torrent, err := fp.loadTorrentFile(filepath.Join(fp.torrentsDir, entry.Name()))
		if err != nil {
			// Skip corrupted files, but continue with others
			continue
		}

		// Ensure info hash matches
		torrent.InfoHash = infoHash

		// Load MetaInfo if available
		if metaInfo, err := fp.loadMetaInfo(infoHash); err == nil {
			torrent.MetaInfo = metaInfo
		}

		torrents = append(torrents, torrent)
	}

	return torrents, nil
}

// DeleteTorrent removes a torrent's persisted state
func (fp *FilePersistence) DeleteTorrent(ctx context.Context, infoHash metainfo.Hash) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	torrentFile := fp.getTorrentFilePath(infoHash)
	if err := os.Remove(torrentFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove torrent file: %w", err)
	}

	// Remove MetaInfo file if it exists
	metaFile := fp.getMetaInfoFilePath(infoHash)
	os.Remove(metaFile) // Ignore errors for metadata cleanup

	return nil
}

// SaveSessionConfig persists session configuration
func (fp *FilePersistence) SaveSessionConfig(ctx context.Context, config *rpc.SessionConfiguration) error {
	if config == nil {
		return fmt.Errorf("session configuration cannot be nil")
	}

	fp.mu.Lock()
	defer fp.mu.Unlock()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session configuration: %w", err)
	}

	// Write atomically using temporary file
	tempFile := fp.sessionFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary session file: %w", err)
	}

	if err := os.Rename(tempFile, fp.sessionFile); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename temporary session file: %w", err)
	}

	return nil
}

// LoadSessionConfig retrieves persisted session configuration
func (fp *FilePersistence) LoadSessionConfig(ctx context.Context) (*rpc.SessionConfiguration, error) {
	fp.mu.RLock()
	defer fp.mu.RUnlock()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(fp.sessionFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session configuration not found")
		}
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var config rpc.SessionConfiguration
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session configuration: %w", err)
	}

	return &config, nil
}

// Close cleans up any resources (no-op for file persistence)
func (fp *FilePersistence) Close() error {
	return nil
}

// Helper methods

func (fp *FilePersistence) getTorrentFilePath(infoHash metainfo.Hash) string {
	return filepath.Join(fp.torrentsDir, infoHash.String()+".json")
}

func (fp *FilePersistence) getMetaInfoFilePath(infoHash metainfo.Hash) string {
	return filepath.Join(fp.metadataDir, infoHash.String()+".torrent")
}

func (fp *FilePersistence) createSerializableTorrent(torrent *rpc.TorrentState) map[string]interface{} {
	// Create a map excluding non-serializable fields and internal state
	return map[string]interface{}{
		"id":                    torrent.ID,
		"info_hash":             torrent.InfoHash.String(), // Serialize as string
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
		"files":                 torrent.Files,
		"priorities":            torrent.Priorities,
		"wanted":                torrent.Wanted,
		"tracker_list":          torrent.TrackerList,
		"seed_ratio_limit":      torrent.SeedRatioLimit,
		"seed_idle_limit":       torrent.SeedIdleLimit,
		"honors_session_limits": torrent.HonorsSessionLimits,
	}
}

func (fp *FilePersistence) loadTorrentFile(filename string) (*rpc.TorrentState, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	// Unmarshal into a map first to handle the string info_hash
	var dataMap map[string]interface{}
	if err := json.Unmarshal(data, &dataMap); err != nil {
		return nil, err
	}

	// Convert info_hash from string to Hash
	if hashStr, ok := dataMap["info_hash"].(string); ok {
		dataMap["info_hash"] = metainfo.NewHashFromString(hashStr)
	}

	// Re-marshal and unmarshal into TorrentState
	fixedData, err := json.Marshal(dataMap)
	if err != nil {
		return nil, err
	}

	var torrent rpc.TorrentState
	if err := json.Unmarshal(fixedData, &torrent); err != nil {
		return nil, err
	}

	return &torrent, nil
}

func (fp *FilePersistence) saveMetaInfo(infoHash metainfo.Hash, metaInfo *metainfo.MetaInfo) error {
	metaFile := fp.getMetaInfoFilePath(infoHash)

	// Create file and write MetaInfo using its Write method
	file, err := os.Create(metaFile)
	if err != nil {
		return err
	}
	defer file.Close()

	return metaInfo.Write(file)
}

func (fp *FilePersistence) loadMetaInfo(infoHash metainfo.Hash) (*metainfo.MetaInfo, error) {
	metaFile := fp.getMetaInfoFilePath(infoHash)
	file, err := os.Open(metaFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	metaInfo, err := metainfo.Load(file)
	if err != nil {
		return nil, err
	}

	return &metaInfo, nil
}
