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
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
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
	dhtConn    net.PacketConn
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

	// Initialize DHT server if enabled
	if config.SessionConfig.DHTEnabled {
		dhtAddr := fmt.Sprintf(":%d", config.PeerPort)
		dhtConn, err := net.ListenPacket("udp", dhtAddr)
		if err != nil {
			tm.log("Failed to create DHT connection on %s: %v", dhtAddr, err)
			// Continue without DHT - not a fatal error
		} else {
			tm.dhtConn = dhtConn

			dhtConfig := dht.Config{
				ID:        config.DHTConfig.ID,
				K:         config.DHTConfig.K,
				ReadOnly:  config.DHTConfig.ReadOnly,
				ErrorLog:  config.ErrorLog,
				OnSearch:  tm.onDHTSearch,
				OnTorrent: tm.onDHTTorrent,
			}

			tm.dhtServer = dht.NewServer(dhtConn, dhtConfig)
			go tm.dhtServer.Run()

			tm.log("DHT server initialized on %s", dhtAddr)
		}
	}

	// Initialize torrent downloader
	tm.downloader = downloader.NewTorrentDownloader(config.DownloaderConfig)

	// Set up DHT integration with downloader for peer discovery
	if tm.dhtServer != nil {
		tm.downloader.OnDHTNode(func(host string, port uint16) {
			// This callback receives DHT node information from peers
			// We could ping these nodes to add them to our DHT routing table
			addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
			if err == nil {
				_ = tm.dhtServer.Ping(addr)
			}
		})
	}

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
		tm.dhtServer.Close()
	}

	if tm.dhtConn != nil {
		tm.dhtConn.Close()
	}

	return nil
}

// onDHTSearch is called when someone searches for a torrent via DHT
func (tm *TorrentManager) onDHTSearch(infohash string, addr net.Addr) {
	tm.log("DHT search request for infohash %s from %s", infohash, addr)
	// This could trigger additional peer discovery for popular torrents
}

// onDHTTorrent is called when we discover a peer has a specific torrent
func (tm *TorrentManager) onDHTTorrent(infohash string, addr net.Addr) {
	tm.log("DHT discovered peer %s for infohash %s", addr, infohash)

	// Find if we have this torrent and are actively downloading
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, torrent := range tm.torrents {
		if torrent.InfoHash.HexString() == infohash {
			if torrent.Status == TorrentStatusDownloading ||
				torrent.Status == TorrentStatusQueuedVerify {
				// We could initiate a peer connection here
				tm.log("Found potential peer %s for active torrent %d", addr, torrent.ID)
			}
			break
		}
	}
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
				torrentState.TrackerList = append(torrentState.TrackerList, tierList...)
			}
		}
	}

	// Store torrent
	tm.torrents[torrentID] = torrentState

	// Start download if not paused
	if !req.Paused && tm.sessionConfig.StartAddedTorrents {
		go tm.startTorrent(torrentState)
	} else if metaInfo != nil {
		// Even if paused, verify existing data
		go tm.VerifyTorrent(torrentID)
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

// startTorrent initiates the download process for a torrent with actual BitTorrent operations
func (tm *TorrentManager) startTorrent(torrent *TorrentState) {
	tm.log("Starting torrent %s (ID: %d)", torrent.InfoHash.HexString(), torrent.ID)

	// If we don't have metadata, request it through DHT and downloader
	if torrent.MetaInfo == nil {
		tm.log("Starting metadata download for torrent %s", torrent.InfoHash.HexString())
		torrent.Status = TorrentStatusQueuedVerify

		// Use DHT to find peers that have this torrent
		if tm.dhtServer != nil {
			go tm.findPeersViaDHT(torrent)
		} else {
			tm.log("DHT not available for peer discovery")
		}

		return
	}

	// We have metadata, start the full download process
	tm.log("Starting file download for torrent %s", torrent.InfoHash.HexString())
	torrent.Status = TorrentStatusDownloading

	// Announce to trackers if available
	if len(torrent.TrackerList) > 0 {
		go tm.announceToTrackers(torrent)
	}

	// Continue peer discovery via DHT even when we have metadata
	if tm.dhtServer != nil {
		go tm.findPeersViaDHT(torrent)
	}
}

// findPeersViaDHT uses DHT to discover peers for a torrent
func (tm *TorrentManager) findPeersViaDHT(torrent *TorrentState) {
	tm.log("Searching for peers via DHT for torrent %s", torrent.InfoHash.HexString())

	// Use DHT GetPeers to find peers storing this torrent
	tm.dhtServer.GetPeers(torrent.InfoHash, func(result dht.Result) {
		if result.Code != 0 {
			tm.log("DHT peer discovery failed for %s: %s", torrent.InfoHash.HexString(), result.Reason)
			return
		}

		if result.Timeout {
			tm.log("DHT peer discovery timeout for %s", torrent.InfoHash.HexString())
			return
		}

		if len(result.Peers) > 0 {
			tm.log("DHT found %d peers for torrent %s", len(result.Peers), torrent.InfoHash.HexString())

			// If we don't have metadata yet, try to download it from these peers
			if torrent.MetaInfo == nil {
				for _, peer := range result.Peers {
					if peer.Port > 0 && peer.Port < 65535 {
						go tm.requestMetadataFromPeer(torrent, peer)
					}
				}
			} else {
				// Update peer list for active torrent
				tm.updateTorrentPeers(torrent, result.Peers)
			}
		} else {
			tm.log("No peers found via DHT for torrent %s", torrent.InfoHash.HexString())
		}
	})
}

// requestMetadataFromPeer attempts to download metadata from a specific peer
func (tm *TorrentManager) requestMetadataFromPeer(torrent *TorrentState, peer metainfo.Address) {
	tm.log("Requesting metadata from peer %s for torrent %s", peer.String(), torrent.InfoHash.HexString())

	// Use the downloader to request metadata from this peer
	tm.downloader.Request(peer.IP.String(), uint16(peer.Port), torrent.InfoHash)
}

// announceToTrackers announces the torrent to its tracker list
func (tm *TorrentManager) announceToTrackers(torrent *TorrentState) {
	tm.log("Announcing to trackers for torrent %s", torrent.InfoHash.HexString())

	// This is a simplified tracker announce - in a full implementation,
	// we would create proper tracker clients and handle the announce protocol
	for _, trackerURL := range torrent.TrackerList {
		tm.log("Would announce to tracker: %s", trackerURL)
		// TODO: Implement actual tracker announcing
		// This would involve creating HTTP or UDP tracker clients
		// and sending announce requests
	}
}

// updateTorrentPeers updates the peer list for a torrent
func (tm *TorrentManager) updateTorrentPeers(torrent *TorrentState, peers []metainfo.Address) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Update peer information - convert metainfo.Address to PeerInfo
	newPeers := make([]PeerInfo, 0, len(peers))
	for _, peer := range peers {
		peerInfo := PeerInfo{
			Address:   peer.IP.String(),
			Port:      int64(peer.Port),
			Direction: "outgoing", // We're connecting to them
		}
		newPeers = append(newPeers, peerInfo)
	}

	// Add new peers to existing list (avoiding duplicates in a simple way)
	existingAddrs := make(map[string]bool)
	for _, existing := range torrent.Peers {
		key := fmt.Sprintf("%s:%d", existing.Address, existing.Port)
		existingAddrs[key] = true
	}

	for _, newPeer := range newPeers {
		key := fmt.Sprintf("%s:%d", newPeer.Address, newPeer.Port)
		if !existingAddrs[key] {
			torrent.Peers = append(torrent.Peers, newPeer)
		}
	}

	tm.log("Updated peer list for torrent %s: %d total peers",
		torrent.InfoHash.HexString(), len(torrent.Peers))
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

	// Update status and start the download process
	torrent.Status = TorrentStatusDownloading

	tm.log("Metadata downloaded for torrent %s, starting file download", response.InfoHash.HexString())

	// Now that we have metadata, start the full download process
	go tm.startTorrent(torrent)
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

	now := time.Now()

	for _, torrent := range tm.torrents {
		tm.updateTorrentStatistics(torrent, now)
	}
}

// updateTorrentStatistics calculates and updates statistics for a single torrent
// Implements real-time transfer rate calculations, completion percentage, ETA, and peer counts
func (tm *TorrentManager) updateTorrentStatistics(torrent *TorrentState, now time.Time) {
	// Calculate transfer rates based on bytes transferred since last update
	if !torrent.lastStatsUpdate.IsZero() {
		timeDelta := now.Sub(torrent.lastStatsUpdate).Seconds()
		if timeDelta > 0 {
			downloadDelta := torrent.Downloaded - torrent.lastDownloaded
			uploadDelta := torrent.Uploaded - torrent.lastUploaded

			torrent.DownloadRate = int64(float64(downloadDelta) / timeDelta)
			torrent.UploadRate = int64(float64(uploadDelta) / timeDelta)
		}
	}

	// Update tracking fields for next calculation
	torrent.lastStatsUpdate = now
	torrent.lastDownloaded = torrent.Downloaded
	torrent.lastUploaded = torrent.Uploaded

	// Calculate completion percentage and piece statistics
	if torrent.MetaInfo != nil {
		info, err := torrent.MetaInfo.Info()
		if err == nil {
			totalSize := info.TotalLength()
			if totalSize > 0 {
				torrent.PercentDone = float64(torrent.Downloaded) / float64(totalSize)
				if torrent.PercentDone > 1.0 {
					torrent.PercentDone = 1.0
				}
				torrent.Left = totalSize - torrent.Downloaded
				if torrent.Left < 0 {
					torrent.Left = 0
				}
			}

			// Calculate piece statistics
			torrent.PieceCount = int64(info.CountPieces())
			torrent.PiecesComplete = tm.calculateCompletedPieces(torrent)
			torrent.PiecesAvailable = tm.calculateAvailablePieces(torrent)
		}
	}

	// Calculate ETA (Estimated Time to Arrival)
	torrent.ETA = tm.calculateETA(torrent)

	// Update peer statistics
	torrent.PeerCount = int64(len(torrent.Peers))
	torrent.PeerConnectedCount = tm.countConnectedPeers(torrent)
	torrent.PeerSendingCount = tm.countSendingPeers(torrent)
	torrent.PeerReceivingCount = tm.countReceivingPeers(torrent)
}

// calculateTotalSize returns the total size of all files in the torrent
func (tm *TorrentManager) calculateTotalSize(torrent *TorrentState) int64 {
	if torrent.MetaInfo == nil {
		return 0
	}

	info, err := torrent.MetaInfo.Info()
	if err != nil {
		return 0
	}

	return info.TotalLength()
}

// calculateCompletedPieces returns the number of completed pieces
// In a real implementation, this would check piece completion status
func (tm *TorrentManager) calculateCompletedPieces(torrent *TorrentState) int64 {
	if torrent.MetaInfo == nil || torrent.PercentDone <= 0 {
		return 0
	}

	info, err := torrent.MetaInfo.Info()
	if err != nil {
		return 0
	}

	// Estimate based on completion percentage
	totalPieces := int64(info.CountPieces())
	return int64(float64(totalPieces) * torrent.PercentDone)
}

// calculateAvailablePieces returns the number of pieces available from peers
// In a real implementation, this would track piece availability from connected peers
func (tm *TorrentManager) calculateAvailablePieces(torrent *TorrentState) int64 {
	if torrent.MetaInfo == nil || len(torrent.Peers) == 0 {
		return 0
	}

	info, err := torrent.MetaInfo.Info()
	if err != nil {
		return 0
	}

	// Estimate based on connected peers (assumes peers have most pieces)
	totalPieces := int64(info.CountPieces())
	if torrent.PeerCount > 0 {
		// Assume each peer has at least 50% of pieces on average
		return int64(float64(totalPieces) * 0.5 * float64(torrent.PeerCount))
	}

	return 0
}

// calculateETA calculates estimated time to completion in seconds
// Returns -1 if ETA cannot be determined, 0 if already complete
func (tm *TorrentManager) calculateETA(torrent *TorrentState) int64 {
	// If nothing left to download, return 0 (complete)
	if torrent.Left <= 0 {
		return 0
	}

	// If no download rate, return -1 (unknown)
	if torrent.DownloadRate <= 0 {
		return -1
	}

	// ETA = remaining bytes / download rate
	return torrent.Left / torrent.DownloadRate
}

// countConnectedPeers returns the number of peers we're actually connected to
func (tm *TorrentManager) countConnectedPeers(torrent *TorrentState) int64 {
	// In a real implementation, this would check connection status
	// For now, assume all peers in the list are connected
	return int64(len(torrent.Peers))
}

// countSendingPeers returns the number of peers currently sending data to us
func (tm *TorrentManager) countSendingPeers(torrent *TorrentState) int64 {
	if torrent.Status != TorrentStatusDownloading || torrent.DownloadRate <= 0 {
		return 0
	}

	// Estimate based on download activity and peer count
	peerCount := int64(len(torrent.Peers))
	if peerCount > 0 {
		// Assume roughly 30% of connected peers are actively sending
		return int64(float64(peerCount) * 0.3)
	}

	return 0
}

// countReceivingPeers returns the number of peers currently receiving data from us
func (tm *TorrentManager) countReceivingPeers(torrent *TorrentState) int64 {
	if torrent.Status != TorrentStatusSeeding || torrent.UploadRate <= 0 {
		return 0
	}

	// Estimate based on upload activity and peer count
	peerCount := int64(len(torrent.Peers))
	if peerCount > 0 {
		// Assume roughly 20% of connected peers are actively receiving
		return int64(float64(peerCount) * 0.2)
	}

	return 0
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

// Torrent verification methods

// VerifyTorrent verifies all pieces of a torrent and updates completion status
func (tm *TorrentManager) VerifyTorrent(id int64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	torrent, exists := tm.torrents[id]
	if !exists {
		return fmt.Errorf("torrent with ID %d not found", id)
	}

	if torrent.MetaInfo == nil {
		return fmt.Errorf("cannot verify torrent %d: no metadata available", id)
	}

	tm.log("Starting verification for torrent %s", torrent.InfoHash.HexString())
	torrent.Status = TorrentStatusVerifying

	go tm.verifyTorrentAsync(torrent)
	return nil
}

// verifyTorrentAsync performs piece verification in a background goroutine
func (tm *TorrentManager) verifyTorrentAsync(torrent *TorrentState) {
	info, err := torrent.MetaInfo.Info()
	if err != nil {
		tm.log("Failed to get info for torrent %s verification: %v", torrent.InfoHash.HexString(), err)
		torrent.Status = TorrentStatusStopped
		return
	}

	totalPieces := info.CountPieces()
	verifiedPieces := 0
	totalBytes := int64(0)
	completedBytes := int64(0)

	tm.log("Verifying %d pieces for torrent %s", totalPieces, torrent.InfoHash.HexString())

	// Verify each piece
	for i := 0; i < totalPieces; i++ {
		piece := info.Piece(i)
		verified, err := tm.verifyPiece(torrent, piece)
		if err != nil {
			tm.log("Error verifying piece %d for torrent %s: %v", i, torrent.InfoHash.HexString(), err)
			continue
		}

		totalBytes += piece.Length()
		if verified {
			verifiedPieces++
			completedBytes += piece.Length()
		}
	}

	// Update torrent statistics
	tm.mu.Lock()
	torrent.Downloaded = completedBytes
	torrent.Left = totalBytes - completedBytes
	tm.mu.Unlock()

	// Update file completion stats
	tm.updateFileCompletion(torrent, info, completedBytes)

	// Update status based on verification results
	if verifiedPieces == totalPieces {
		torrent.Status = TorrentStatusSeeding
		tm.log("Torrent %s is 100%% complete (%d/%d pieces)", torrent.InfoHash.HexString(), verifiedPieces, totalPieces)
	} else if verifiedPieces > 0 {
		torrent.Status = TorrentStatusDownloading
		tm.log("Torrent %s is %.1f%% complete (%d/%d pieces)",
			torrent.InfoHash.HexString(),
			float64(verifiedPieces)*100.0/float64(totalPieces),
			verifiedPieces, totalPieces)
	} else {
		torrent.Status = TorrentStatusDownloading
		tm.log("Torrent %s has no verified pieces, starting download", torrent.InfoHash.HexString())
	}
}

// verifyPiece verifies a single piece by reading it from disk and checking its hash
func (tm *TorrentManager) verifyPiece(torrent *TorrentState, piece metainfo.Piece) (bool, error) {
	// Get torrent info
	info, err := torrent.MetaInfo.Info()
	if err != nil {
		return false, err
	}

	// Construct the file path for this torrent
	torrentDir := filepath.Join(torrent.DownloadDir, info.Name)

	// Read the piece data from the appropriate files
	pieceData, err := tm.readPieceFromFiles(torrent, piece, torrentDir)
	if err != nil {
		return false, err
	}

	// If piece data is incomplete (file doesn't exist or is truncated), it's not verified
	if len(pieceData) != int(piece.Length()) {
		return false, nil
	}

	// Calculate hash and compare with expected hash
	actualHash := sha1.Sum(pieceData)
	expectedHash := piece.Hash()

	return actualHash == [20]byte(expectedHash), nil
}

// readPieceFromFiles reads piece data from the actual files on disk
func (tm *TorrentManager) readPieceFromFiles(torrent *TorrentState, piece metainfo.Piece, torrentDir string) ([]byte, error) {
	info, err := torrent.MetaInfo.Info()
	if err != nil {
		return nil, err
	}

	pieceOffset := piece.Offset()
	pieceLength := piece.Length()
	pieceData := make([]byte, pieceLength)

	var currentOffset int64
	var piecePos int64

	// Read data from files that contribute to this piece
	for _, file := range info.Files {
		filePath := filepath.Join(torrentDir, file.Path(info))
		fileSize := file.Length

		// Check if this file contributes to the current piece
		if currentOffset+fileSize <= pieceOffset {
			// File is entirely before this piece
			currentOffset += fileSize
			continue
		}

		if currentOffset >= pieceOffset+pieceLength {
			// File is entirely after this piece
			break
		}

		// This file contributes to the piece
		fileStartInPiece := int64(0)
		if currentOffset < pieceOffset {
			fileStartInPiece = pieceOffset - currentOffset
		}

		readStart := currentOffset + fileStartInPiece
		readLength := fileSize - fileStartInPiece
		if readStart+readLength > pieceOffset+pieceLength {
			readLength = pieceOffset + pieceLength - readStart
		}

		// Try to read from the file
		fileData, err := tm.readFileSegment(filePath, fileStartInPiece, readLength)
		if err != nil {
			// File doesn't exist or can't be read - piece is incomplete
			return nil, err
		}

		// Copy data to piece buffer
		copy(pieceData[piecePos:], fileData)
		piecePos += readLength
		currentOffset += fileSize
	}

	return pieceData[:piecePos], nil
}

// readFileSegment reads a segment of a file
func (tm *TorrentManager) readFileSegment(filePath string, offset, length int64) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	data := make([]byte, length)
	n, err := io.ReadFull(file, data)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	return data[:n], nil
}

// updateFileCompletion updates individual file completion statistics
func (tm *TorrentManager) updateFileCompletion(torrent *TorrentState, info metainfo.Info, totalCompleted int64) {
	var currentOffset int64

	for i, file := range info.Files {
		fileStart := currentOffset
		fileEnd := currentOffset + file.Length

		// Calculate how much of this file is completed
		var fileCompleted int64
		if totalCompleted >= fileEnd {
			// Entire file is completed
			fileCompleted = file.Length
		} else if totalCompleted > fileStart {
			// Partial file completion
			fileCompleted = totalCompleted - fileStart
		}
		// else fileCompleted remains 0

		// Update file info
		if i < len(torrent.Files) {
			torrent.Files[i].BytesCompleted = fileCompleted
		}

		currentOffset += file.Length
	}
}
