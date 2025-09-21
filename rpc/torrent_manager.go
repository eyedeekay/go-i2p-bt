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
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-i2p/go-i2p-bt/bencode"
	"github.com/go-i2p/go-i2p-bt/dht"
	"github.com/go-i2p/go-i2p-bt/downloader"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// TorrentManagerConfig configures the TorrentManager
type TorrentManagerConfig struct {
	// DHT configuration
	DHTConfig dht.Config

	// Downloader configuration
	DownloaderConfig downloader.TorrentDownloaderConfig

	// Default download directory
	DownloadDir string

	// Logger for error reporting
	ErrorLog func(format string, args ...interface{})

	// Maximum number of concurrent torrents
	MaxTorrents int

	// Peer limits
	PeerLimitGlobal     int64
	PeerLimitPerTorrent int64

	// Default port for peer connections
	PeerPort int64

	// Session configuration
	SessionConfig SessionConfiguration
}

// TorrentManager manages torrent state and operations using go-i2p-bt components
type TorrentManager struct {
	config TorrentManagerConfig

	// Core components
	dhtServer  *dht.Server
	downloader *downloader.TorrentDownloader

	// Thread-safe torrent storage
	mu       sync.RWMutex
	torrents map[int64]*TorrentState
	nextID   int64

	// Session configuration
	sessionConfig SessionConfiguration
	sessionMu     sync.RWMutex

	// Background context for operations
	ctx    context.Context
	cancel context.CancelFunc

	// Statistics tracking
	stats struct {
		TorrentsAdded   int64
		TorrentsRemoved int64
		BytesDownloaded int64
		BytesUploaded   int64
	}

	// Logger
	log func(format string, args ...interface{})
}

// NewTorrentManager creates a new TorrentManager with the given configuration
func NewTorrentManager(config TorrentManagerConfig) (*TorrentManager, error) {
	if config.ErrorLog == nil {
		config.ErrorLog = log.Printf
	}

	if config.DownloadDir == "" {
		config.DownloadDir = "downloads"
	}

	if config.MaxTorrents <= 0 {
		config.MaxTorrents = 100
	}

	if config.PeerLimitGlobal <= 0 {
		config.PeerLimitGlobal = 200
	}

	if config.PeerLimitPerTorrent <= 0 {
		config.PeerLimitPerTorrent = 50
	}

	if config.PeerPort <= 0 {
		config.PeerPort = 51413
	}

	// Set default session configuration
	if config.SessionConfig.Version == "" {
		config.SessionConfig = SessionConfiguration{
			DownloadDir:           config.DownloadDir,
			PeerPort:              config.PeerPort,
			PeerLimitGlobal:       config.PeerLimitGlobal,
			PeerLimitPerTorrent:   config.PeerLimitPerTorrent,
			DHTEnabled:            true,
			PEXEnabled:            true,
			LPDEnabled:            false,
			UTPEnabled:            true,
			Encryption:            "preferred",
			SpeedLimitDown:        100,
			SpeedLimitDownEnabled: false,
			SpeedLimitUp:          100,
			SpeedLimitUpEnabled:   false,
			SeedRatioLimit:        2.0,
			SeedRatioLimited:      false,
			StartAddedTorrents:    true,
			CacheSizeMB:           4,
			Version:               "go-i2p-bt/1.0.0",
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	tm := &TorrentManager{
		config:        config,
		torrents:      make(map[int64]*TorrentState),
		nextID:        1,
		sessionConfig: config.SessionConfig,
		ctx:           ctx,
		cancel:        cancel,
		log:           config.ErrorLog,
	}

	// Initialize DHT server if enabled (simplified - would need proper network connection)
	// Note: In a full implementation, you'd create a UDP connection for DHT
	// For now, we'll leave DHT integration for future enhancement
	if config.SessionConfig.DHTEnabled {
		tm.log("DHT enabled but not fully implemented in this version")
	}

	// Initialize torrent downloader
	tm.downloader = downloader.NewTorrentDownloader(config.DownloaderConfig)

	// Start background processes
	go tm.processDownloaderResponses()
	go tm.updateTorrentStats()

	return tm, nil
}

// Close shuts down the TorrentManager and releases resources
func (tm *TorrentManager) Close() error {
	tm.cancel()

	if tm.downloader != nil {
		tm.downloader.Close()
	}

	if tm.dhtServer != nil {
		// Note: DHT Close() doesn't return an error in the current implementation
		tm.dhtServer.Close()
	}

	return nil
}

// AddTorrent adds a new torrent from either a .torrent file (base64 encoded) or magnet URI
func (tm *TorrentManager) AddTorrent(req TorrentAddRequest) (*TorrentState, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check torrent limit
	if len(tm.torrents) >= tm.config.MaxTorrents {
		return nil, fmt.Errorf("maximum number of torrents (%d) reached", tm.config.MaxTorrents)
	}

	var metaInfo *metainfo.MetaInfo
	var infoHash metainfo.Hash

	// Parse torrent data
	if req.Metainfo != "" {
		// Decode base64 encoded .torrent file
		torrentData, err := base64.StdEncoding.DecodeString(req.Metainfo)
		if err != nil {
			return nil, fmt.Errorf("failed to decode torrent data: %w", err)
		}

		// Parse .torrent file
		err = bencode.DecodeBytes(torrentData, &metaInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to parse torrent file: %w", err)
		}

		infoHash = metaInfo.InfoHash()

	} else if req.Filename != "" {
		// Parse magnet URI
		magnet, err := metainfo.ParseMagnetURI(req.Filename)
		if err != nil {
			return nil, fmt.Errorf("failed to parse magnet URI: %w", err)
		}

		infoHash = magnet.InfoHash

		// For magnet links, we need to download the metadata
		// This will be handled asynchronously

	} else {
		return nil, fmt.Errorf("either metainfo or filename (magnet URI) must be provided")
	}

	// Check for duplicate torrents
	for _, existing := range tm.torrents {
		if existing.InfoHash == infoHash {
			return existing, fmt.Errorf("torrent already exists")
		}
	}

	// Create torrent state
	torrentID := tm.nextID
	tm.nextID++

	downloadDir := req.DownloadDir
	if downloadDir == "" {
		downloadDir = tm.sessionConfig.DownloadDir
	}

	torrentState := &TorrentState{
		ID:                  torrentID,
		InfoHash:            infoHash,
		MetaInfo:            metaInfo,
		DownloadDir:         downloadDir,
		AddedDate:           time.Now(),
		Labels:              req.Labels,
		Priorities:          []int64{},
		Wanted:              []bool{},
		TrackerList:         []string{},
		Peers:               []PeerInfo{},
		HonorsSessionLimits: true,
	}

	// Set initial status
	if req.Paused {
		torrentState.Status = TorrentStatusStopped
	} else {
		if metaInfo != nil {
			torrentState.Status = TorrentStatusDownloading
		} else {
			torrentState.Status = TorrentStatusQueuedVerify // Downloading metadata
		}
	}

	// Initialize file information if we have metadata
	if metaInfo != nil {
		info, err := metaInfo.Info()
		if err == nil {
			torrentState.Files = make([]FileInfo, len(info.Files))
			torrentState.Priorities = make([]int64, len(info.Files))
			torrentState.Wanted = make([]bool, len(info.Files))

			for i, file := range info.Files {
				torrentState.Files[i] = FileInfo{
					Name:           file.Path(info),
					Length:         file.Length,
					BytesCompleted: 0,
					Priority:       0, // Normal priority
					Wanted:         true,
				}
				torrentState.Priorities[i] = 0
				torrentState.Wanted[i] = true
			}

			// Get tracker list
			announces := metaInfo.Announces()
			for _, tierList := range announces {
				for _, announce := range tierList {
					torrentState.TrackerList = append(torrentState.TrackerList, announce)
				}
			}
		}
	}

	// Store torrent
	tm.torrents[torrentID] = torrentState

	// Start download if not paused
	if !req.Paused && tm.sessionConfig.StartAddedTorrents {
		go tm.startTorrent(torrentState)
	}

	atomic.AddInt64(&tm.stats.TorrentsAdded, 1)

	return torrentState, nil
}

// GetTorrent retrieves torrent information by ID
func (tm *TorrentManager) GetTorrent(id int64) (*TorrentState, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	torrent, exists := tm.torrents[id]
	if !exists {
		return nil, fmt.Errorf("torrent with ID %d not found", id)
	}

	return torrent, nil
}

// GetTorrentByHash retrieves torrent information by info hash
func (tm *TorrentManager) GetTorrentByHash(hash string) (*TorrentState, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, torrent := range tm.torrents {
		if torrent.InfoHash.HexString() == hash {
			return torrent, nil
		}
	}

	return nil, fmt.Errorf("torrent with hash %s not found", hash)
}

// GetAllTorrents returns all torrents
func (tm *TorrentManager) GetAllTorrents() []*TorrentState {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	torrents := make([]*TorrentState, 0, len(tm.torrents))
	for _, torrent := range tm.torrents {
		torrents = append(torrents, torrent)
	}

	return torrents
}

// StartTorrent starts downloading/seeding a torrent
func (tm *TorrentManager) StartTorrent(id int64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	torrent, exists := tm.torrents[id]
	if !exists {
		return fmt.Errorf("torrent with ID %d not found", id)
	}

	if torrent.Status == TorrentStatusDownloading || torrent.Status == TorrentStatusSeeding {
		return nil // Already started
	}

	torrent.Status = TorrentStatusDownloading
	torrent.StartDate = time.Now()

	go tm.startTorrent(torrent)

	return nil
}

// StopTorrent stops downloading/seeding a torrent
func (tm *TorrentManager) StopTorrent(id int64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	torrent, exists := tm.torrents[id]
	if !exists {
		return fmt.Errorf("torrent with ID %d not found", id)
	}

	torrent.Status = TorrentStatusStopped

	// TODO: Implement actual stopping of download/upload operations

	return nil
}

// RemoveTorrent removes a torrent from the manager
func (tm *TorrentManager) RemoveTorrent(id int64, deleteData bool) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	torrent, exists := tm.torrents[id]
	if !exists {
		return fmt.Errorf("torrent with ID %d not found", id)
	}

	// Stop the torrent first
	torrent.Status = TorrentStatusStopped

	// TODO: If deleteData is true, delete the downloaded files

	// Remove from torrents map
	delete(tm.torrents, id)

	atomic.AddInt64(&tm.stats.TorrentsRemoved, 1)

	return nil
}

// GetSessionConfig returns the current session configuration
func (tm *TorrentManager) GetSessionConfig() SessionConfiguration {
	tm.sessionMu.RLock()
	defer tm.sessionMu.RUnlock()

	return tm.sessionConfig
}

// UpdateSessionConfig updates the session configuration
func (tm *TorrentManager) UpdateSessionConfig(config SessionConfiguration) error {
	tm.sessionMu.Lock()
	defer tm.sessionMu.Unlock()

	// Validate configuration
	if config.PeerPort <= 0 || config.PeerPort > 65535 {
		return fmt.Errorf("invalid peer port: %d", config.PeerPort)
	}

	tm.sessionConfig = config

	// TODO: Apply configuration changes to running components

	return nil
}

// Private methods

// startTorrent initiates the download process for a torrent
func (tm *TorrentManager) startTorrent(torrent *TorrentState) {
	// If we don't have metadata, request it
	if torrent.MetaInfo == nil {
		// Try to find peers from DHT or other sources
		// This is a simplified implementation
		tm.log("Starting metadata download for torrent %s", torrent.InfoHash.HexString())

		// TODO: Implement peer discovery and metadata download
		// For now, just mark as downloading
		torrent.Status = TorrentStatusVerifying
		return
	}

	// We have metadata, start downloading the actual files
	tm.log("Starting file download for torrent %s", torrent.InfoHash.HexString())

	// TODO: Implement actual file downloading using the existing components
	// This would involve:
	// 1. Announcing to trackers
	// 2. Finding peers through DHT
	// 3. Connecting to peers and downloading pieces
	// 4. Writing pieces to disk

	torrent.Status = TorrentStatusDownloading
}

// processDownloaderResponses handles responses from the torrent downloader
func (tm *TorrentManager) processDownloaderResponses() {
	for {
		select {
		case <-tm.ctx.Done():
			return
		case response := <-tm.downloader.Response():
			tm.handleDownloaderResponse(response)
		}
	}
}

// handleDownloaderResponse processes a single downloader response
func (tm *TorrentManager) handleDownloaderResponse(response downloader.TorrentResponse) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find the torrent by info hash
	var torrent *TorrentState
	for _, t := range tm.torrents {
		if t.InfoHash == response.InfoHash {
			torrent = t
			break
		}
	}

	if torrent == nil {
		tm.log("Received metadata for unknown torrent: %s", response.InfoHash.HexString())
		return
	}

	// Parse the metadata
	var metaInfo metainfo.MetaInfo
	if err := bencode.DecodeBytes(response.InfoBytes, &metaInfo.InfoBytes); err != nil {
		tm.log("Failed to parse metadata for torrent %s: %v", response.InfoHash.HexString(), err)
		return
	}

	// Verify info hash
	if sha1.Sum(response.InfoBytes) != [20]byte(response.InfoHash) {
		tm.log("Invalid metadata for torrent %s: hash mismatch", response.InfoHash.HexString())
		return
	}

	torrent.MetaInfo = &metaInfo

	// Update file information
	info, err := metaInfo.Info()
	if err != nil {
		tm.log("Failed to parse info for torrent %s: %v", response.InfoHash.HexString(), err)
		return
	}

	torrent.Files = make([]FileInfo, len(info.Files))
	torrent.Priorities = make([]int64, len(info.Files))
	torrent.Wanted = make([]bool, len(info.Files))

	for i, file := range info.Files {
		torrent.Files[i] = FileInfo{
			Name:           file.Path(info),
			Length:         file.Length,
			BytesCompleted: 0,
			Priority:       0,
			Wanted:         true,
		}
		torrent.Priorities[i] = 0
		torrent.Wanted[i] = true
	}

	// Update status
	torrent.Status = TorrentStatusDownloading

	tm.log("Metadata downloaded for torrent %s", response.InfoHash.HexString())
}

// updateTorrentStats periodically updates torrent statistics
func (tm *TorrentManager) updateTorrentStats() {
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.updateStats()
		}
	}
}

// updateStats updates statistics for all torrents
func (tm *TorrentManager) updateStats() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// TODO: Update transfer rates, peer counts, completion percentages, etc.
	// This would require integration with the actual transfer components

	for _, torrent := range tm.torrents {
		// Update activity date
		if torrent.Status == TorrentStatusDownloading || torrent.Status == TorrentStatusSeeding {
			// Would normally check if there's actual activity
			// For now, just update if status indicates activity
		}
	}
}

// Helper methods for ID resolution

// ResolveTorrentIDs resolves a list of torrent identifiers to actual torrent IDs
func (tm *TorrentManager) ResolveTorrentIDs(ids []interface{}) ([]int64, error) {
	if len(ids) == 0 {
		// Return all torrent IDs
		tm.mu.RLock()
		defer tm.mu.RUnlock()

		result := make([]int64, 0, len(tm.torrents))
		for id := range tm.torrents {
			result = append(result, id)
		}
		return result, nil
	}

	var result []int64

	for _, id := range ids {
		switch v := id.(type) {
		case float64:
			// JSON numbers are parsed as float64
			result = append(result, int64(v))
		case int64:
			result = append(result, v)
		case int:
			result = append(result, int64(v))
		case string:
			if v == "recently-active" {
				// Return torrents that have been active recently
				recentIDs := tm.getRecentlyActiveTorrents()
				result = append(result, recentIDs...)
			} else {
				// Assume it's a hash string
				torrent, err := tm.GetTorrentByHash(v)
				if err != nil {
					return nil, fmt.Errorf("torrent not found: %s", v)
				}
				result = append(result, torrent.ID)
			}
		default:
			return nil, fmt.Errorf("invalid torrent ID type: %T", id)
		}
	}

	return result, nil
}

// getRecentlyActiveTorrents returns IDs of torrents that have been active recently
func (tm *TorrentManager) getRecentlyActiveTorrents() []int64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var result []int64
	cutoff := time.Now().Add(-5 * time.Minute)

	for _, torrent := range tm.torrents {
		// Consider a torrent recently active if it has been active in the last 5 minutes
		// This is a simplified check - in a real implementation, we'd track actual activity
		if torrent.Status == TorrentStatusDownloading || torrent.Status == TorrentStatusSeeding {
			result = append(result, torrent.ID)
		} else if !torrent.StartDate.IsZero() && torrent.StartDate.After(cutoff) {
			result = append(result, torrent.ID)
		}
	}

	return result
}
