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

	// Incomplete directory for downloads in progress (optional)
	IncompleteDir string

	// Whether to use incomplete directory for downloads
	IncompleteDirEnabled bool

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
	dhtServer        *dht.Server
	dhtConn          net.PacketConn
	downloader       *downloader.TorrentDownloader
	queueManager     *QueueManager
	bandwidthManager *BandwidthManager
	blocklistManager *BlocklistManager

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
	// Apply default values to configuration
	applyConfigDefaults(&config)

	// Create base TorrentManager instance
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

	// Initialize queue manager with session configuration
	queueConfig := QueueConfig{
		MaxActiveDownloads:   config.SessionConfig.DownloadQueueSize,
		MaxActiveSeeds:       config.SessionConfig.SeedQueueSize,
		ProcessInterval:      5 * time.Second,
		DownloadQueueEnabled: config.SessionConfig.DownloadQueueEnabled,
		SeedQueueEnabled:     config.SessionConfig.SeedQueueEnabled,
	}

	tm.queueManager = NewQueueManager(queueConfig, tm.onTorrentActivated, tm.onTorrentDeactivated)

	// Initialize bandwidth manager with session configuration
	tm.bandwidthManager = NewBandwidthManager(config.SessionConfig)

	// Initialize blocklist manager with session configuration
	tm.blocklistManager = NewBlocklistManager()
	tm.blocklistManager.SetEnabled(config.SessionConfig.BlocklistEnabled)
	if config.SessionConfig.BlocklistURL != "" {
		if err := tm.blocklistManager.SetURL(config.SessionConfig.BlocklistURL); err != nil {
			tm.log("Blocklist initialization warning: %v", err)
		}
	}

	// Initialize DHT server if enabled
	if err := tm.initializeDHTServer(); err != nil {
		tm.log("DHT initialization warning: %v", err)
		// Continue without DHT - not a fatal error
	}

	// Initialize downloader and DHT integration
	tm.initializeDownloader()

	// Start background processes
	tm.startBackgroundProcesses()

	return tm, nil
}

// applyConfigDefaults sets default values for unspecified configuration options
func applyConfigDefaults(config *TorrentManagerConfig) {
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

	// Set default session configuration if not provided
	if config.SessionConfig.Version == "" {
		config.SessionConfig = createDefaultSessionConfig(*config)
	}
}

// createDefaultSessionConfig creates a default session configuration with the given base config
func createDefaultSessionConfig(config TorrentManagerConfig) SessionConfiguration {
	return SessionConfiguration{
		DownloadDir:           config.DownloadDir,
		IncompleteDir:         config.IncompleteDir,
		IncompleteDirEnabled:  config.IncompleteDirEnabled,
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

// initializeDHTServer sets up the DHT server if enabled in configuration
func (tm *TorrentManager) initializeDHTServer() error {
	if !tm.config.SessionConfig.DHTEnabled {
		return nil
	}

	dhtAddr := fmt.Sprintf(":%d", tm.config.PeerPort)
	dhtConn, err := net.ListenPacket("udp", dhtAddr)
	if err != nil {
		return fmt.Errorf("failed to create DHT connection on %s: %w", dhtAddr, err)
	}

	tm.dhtConn = dhtConn

	dhtConfig := dht.Config{
		ID:        tm.config.DHTConfig.ID,
		K:         tm.config.DHTConfig.K,
		ReadOnly:  tm.config.DHTConfig.ReadOnly,
		ErrorLog:  tm.config.ErrorLog,
		OnSearch:  tm.onDHTSearch,
		OnTorrent: tm.onDHTTorrent,
	}

	tm.dhtServer = dht.NewServer(dhtConn, dhtConfig)
	go tm.dhtServer.Run()

	tm.log("DHT server initialized on %s", dhtAddr)
	return nil
}

// initializeDownloader creates the torrent downloader and sets up DHT integration
func (tm *TorrentManager) initializeDownloader() {
	tm.downloader = downloader.NewTorrentDownloader(tm.config.DownloaderConfig)

	// Set up DHT integration with downloader for peer discovery
	if tm.dhtServer != nil {
		tm.setupDHTIntegration()
	}
}

// setupDHTIntegration configures the downloader to work with the DHT server for peer discovery
func (tm *TorrentManager) setupDHTIntegration() {
	tm.downloader.OnDHTNode(func(host string, port uint16) {
		// This callback receives DHT node information from peers
		// We could ping these nodes to add them to our DHT routing table
		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
		if err == nil {
			_ = tm.dhtServer.Ping(addr)
		}
	})
}

// startBackgroundProcesses launches the background goroutines for torrent management
func (tm *TorrentManager) startBackgroundProcesses() {
	go tm.processDownloaderResponses()
	go tm.updateTorrentStats()
}

// Close shuts down the TorrentManager and releases resources
func (tm *TorrentManager) Close() error {
	tm.cancel()

	if tm.queueManager != nil {
		tm.queueManager.Close()
	}

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
	if err := tm.validateTorrentLimit(); err != nil {
		return nil, err
	}

	// Parse torrent data to get metainfo and info hash
	metaInfo, infoHash, err := tm.parseAddTorrentRequest(req)
	if err != nil {
		return nil, err
	}

	// Check for duplicate torrents
	if existing := tm.findDuplicateTorrent(infoHash); existing != nil {
		return existing, fmt.Errorf("torrent already exists")
	}

	// Create and initialize torrent state
	torrentState := tm.createTorrentState(req, metaInfo, infoHash)

	// Set initial status based on request and metadata availability
	tm.setInitialTorrentStatus(torrentState, req.Paused, metaInfo != nil)

	// Initialize file information if we have metadata
	if metaInfo != nil {
		if err := tm.initializeFileInfo(torrentState, metaInfo); err != nil {
			tm.log("Failed to initialize file info for torrent %s: %v", infoHash.HexString(), err)
		}
	}

	// Store torrent
	tm.torrents[torrentState.ID] = torrentState

	// Start download process if appropriate
	tm.handleTorrentStartup(torrentState, req.Paused, metaInfo != nil)

	atomic.AddInt64(&tm.stats.TorrentsAdded, 1)

	return torrentState, nil
}

// validateTorrentLimit checks if adding a new torrent would exceed the maximum limit
func (tm *TorrentManager) validateTorrentLimit() error {
	if len(tm.torrents) >= tm.config.MaxTorrents {
		return fmt.Errorf("maximum number of torrents (%d) reached", tm.config.MaxTorrents)
	}
	return nil
}

// parseAddTorrentRequest parses the torrent request and returns metainfo and info hash
func (tm *TorrentManager) parseAddTorrentRequest(req TorrentAddRequest) (*metainfo.MetaInfo, metainfo.Hash, error) {
	if req.Metainfo != "" {
		return tm.parseMetainfoData(req.Metainfo)
	} else if req.Filename != "" {
		return tm.parseMagnetURI(req.Filename)
	} else {
		return nil, metainfo.Hash{}, fmt.Errorf("either metainfo or filename (magnet URI) must be provided")
	}
}

// parseMetainfoData decodes and parses base64 encoded .torrent file data
func (tm *TorrentManager) parseMetainfoData(metainfoData string) (*metainfo.MetaInfo, metainfo.Hash, error) {
	// Decode base64 encoded .torrent file
	torrentData, err := base64.StdEncoding.DecodeString(metainfoData)
	if err != nil {
		return nil, metainfo.Hash{}, fmt.Errorf("failed to decode torrent data: %w", err)
	}

	// Parse .torrent file
	var metaInfo metainfo.MetaInfo
	err = bencode.DecodeBytes(torrentData, &metaInfo)
	if err != nil {
		return nil, metainfo.Hash{}, fmt.Errorf("failed to parse torrent file: %w", err)
	}

	infoHash := metaInfo.InfoHash()
	return &metaInfo, infoHash, nil
}

// parseMagnetURI parses a magnet URI and returns the info hash
func (tm *TorrentManager) parseMagnetURI(magnetURI string) (*metainfo.MetaInfo, metainfo.Hash, error) {
	// Parse magnet URI
	magnet, err := metainfo.ParseMagnetURI(magnetURI)
	if err != nil {
		return nil, metainfo.Hash{}, fmt.Errorf("failed to parse magnet URI: %w", err)
	}

	// For magnet links, we need to download the metadata asynchronously
	return nil, magnet.InfoHash, nil
}

// findDuplicateTorrent finds an existing torrent with the given info hash
func (tm *TorrentManager) findDuplicateTorrent(infoHash metainfo.Hash) *TorrentState {
	for _, existing := range tm.torrents {
		if existing.InfoHash == infoHash {
			return existing
		}
	}
	return nil
}

// createTorrentState creates a new TorrentState with basic initialization
// determineDownloadDirectory returns the appropriate download directory based on completion status and configuration
func (tm *TorrentManager) determineDownloadDirectory(requestedDir string, isComplete bool) string {
	// Use explicitly requested directory if provided
	if requestedDir != "" {
		return requestedDir
	}

	// If incomplete directory is enabled and torrent is not complete, use incomplete directory
	if tm.sessionConfig.IncompleteDirEnabled && !isComplete && tm.sessionConfig.IncompleteDir != "" {
		return tm.sessionConfig.IncompleteDir
	}

	// Otherwise, use the default download directory
	return tm.sessionConfig.DownloadDir
}

func (tm *TorrentManager) createTorrentState(req TorrentAddRequest, metaInfo *metainfo.MetaInfo, infoHash metainfo.Hash) *TorrentState {
	torrentID := tm.nextID
	tm.nextID++

	downloadDir := tm.determineDownloadDirectory(req.DownloadDir, false) // false = not complete yet

	return &TorrentState{
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
}

// setInitialTorrentStatus sets the initial status of a torrent based on pause state and metadata availability
func (tm *TorrentManager) setInitialTorrentStatus(torrentState *TorrentState, paused, hasMetadata bool) {
	if paused {
		torrentState.Status = TorrentStatusStopped
	} else {
		if hasMetadata {
			torrentState.Status = TorrentStatusDownloading
		} else {
			torrentState.Status = TorrentStatusQueuedVerify // Downloading metadata
		}
	}
}

// initializeFileInfo initializes file information, priorities, and tracker list when metadata is available
func (tm *TorrentManager) initializeFileInfo(torrentState *TorrentState, metaInfo *metainfo.MetaInfo) error {
	info, err := metaInfo.Info()
	if err != nil {
		return err
	}

	tm.setupFileArrays(torrentState, info)
	tm.populateFileInfo(torrentState, info)
	tm.extractTrackerList(torrentState, metaInfo)

	return nil
}

// setupFileArrays initializes the file-related arrays in the torrent state
func (tm *TorrentManager) setupFileArrays(torrentState *TorrentState, info metainfo.Info) {
	fileCount := len(info.Files)
	torrentState.Files = make([]FileInfo, fileCount)
	torrentState.Priorities = make([]int64, fileCount)
	torrentState.Wanted = make([]bool, fileCount)
}

// populateFileInfo populates file information for each file in the torrent
func (tm *TorrentManager) populateFileInfo(torrentState *TorrentState, info metainfo.Info) {
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
}

// extractTrackerList extracts and flattens the tracker list from metainfo announces
func (tm *TorrentManager) extractTrackerList(torrentState *TorrentState, metaInfo *metainfo.MetaInfo) {
	announces := metaInfo.Announces()
	for _, tierList := range announces {
		torrentState.TrackerList = append(torrentState.TrackerList, tierList...)
	}
}

// handleTorrentStartup handles the startup process for a newly added torrent
func (tm *TorrentManager) handleTorrentStartup(torrentState *TorrentState, paused, hasMetadata bool) {
	if !paused && tm.sessionConfig.StartAddedTorrents {
		go tm.startTorrent(torrentState)
	} else if hasMetadata {
		// Even if paused, verify existing data
		go tm.VerifyTorrent(torrentState.ID)
	}
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

// StartTorrent starts downloading/seeding a torrent using queue management
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

	// Determine queue type based on completion status
	var queueType QueueType
	if torrent.PercentDone >= 1.0 {
		// Torrent is complete, queue for seeding
		queueType = SeedQueue
		torrent.Status = TorrentStatusQueuedSeed
	} else {
		// Torrent is incomplete, queue for downloading
		queueType = DownloadQueue
		torrent.Status = TorrentStatusQueuedDown
	}

	// Add to appropriate queue with normal priority
	if err := tm.queueManager.AddToQueue(id, queueType, 0); err != nil {
		return fmt.Errorf("failed to add torrent to queue: %v", err)
	}

	return nil
}

// StartTorrentNow starts downloading/seeding a torrent immediately, bypassing any queue
// This implements the "torrent-start-now" RPC method which should bypass download queues
func (tm *TorrentManager) StartTorrentNow(id int64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	torrent, exists := tm.torrents[id]
	if !exists {
		return fmt.Errorf("torrent with ID %d not found", id)
	}

	// Force immediate start regardless of current status (except if already active)
	if torrent.Status == TorrentStatusDownloading || torrent.Status == TorrentStatusSeeding {
		return nil // Already started
	}

	// Determine queue type based on completion status
	var queueType QueueType
	if torrent.PercentDone >= 1.0 {
		queueType = SeedQueue
		torrent.Status = TorrentStatusSeeding
	} else {
		queueType = DownloadQueue
		torrent.Status = TorrentStatusDownloading
	}

	torrent.StartDate = time.Now()

	// Force immediate activation bypassing queue limits
	tm.queueManager.ForceActivate(id, queueType)

	// Start torrent immediately without queue delays
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

	// Remove from queue management if present
	tm.queueManager.RemoveFromQueue(id)

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

	// Remove from queue management
	tm.queueManager.RemoveFromQueue(id)

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

	config := tm.sessionConfig
	// Update blocklist size from current manager state
	config.BlocklistSize = tm.blocklistManager.GetSize()
	
	return config
}

// UpdateSessionConfig updates the session configuration
func (tm *TorrentManager) UpdateSessionConfig(config SessionConfiguration) error {
	tm.sessionMu.Lock()
	defer tm.sessionMu.Unlock()

	// Validate configuration
	if config.PeerPort <= 0 || config.PeerPort > 65535 {
		return fmt.Errorf("invalid peer port: %d", config.PeerPort)
	}

	if config.PeerLimitGlobal < 0 {
		return fmt.Errorf("peer limit global cannot be negative: %d", config.PeerLimitGlobal)
	}

	if config.PeerLimitPerTorrent < 0 {
		return fmt.Errorf("peer limit per torrent cannot be negative: %d", config.PeerLimitPerTorrent)
	}

	if config.DownloadQueueSize < 0 {
		return fmt.Errorf("download queue size cannot be negative: %d", config.DownloadQueueSize)
	}

	if config.SeedQueueSize < 0 {
		return fmt.Errorf("seed queue size cannot be negative: %d", config.SeedQueueSize)
	}

	// Validate and create download directory if necessary
	if config.DownloadDir != "" {
		if err := tm.validateAndCreateDownloadDir(config.DownloadDir); err != nil {
			return fmt.Errorf("invalid download directory: %w", err)
		}
	}

	// Validate and create incomplete directory if enabled and specified
	if config.IncompleteDirEnabled && config.IncompleteDir != "" {
		if err := tm.validateAndCreateDownloadDir(config.IncompleteDir); err != nil {
			return fmt.Errorf("invalid incomplete directory: %w", err)
		}
	}

	// Store previous configuration for comparison
	oldConfig := tm.sessionConfig
	tm.sessionConfig = config

	// Apply configuration changes to running components
	if err := tm.applyRuntimeConfigChanges(oldConfig, config); err != nil {
		// Revert configuration on failure
		tm.sessionConfig = oldConfig
		return fmt.Errorf("failed to apply configuration changes: %w", err)
	}

	return nil
}

// validateAndCreateDownloadDir validates and creates the download directory if needed
func (tm *TorrentManager) validateAndCreateDownloadDir(downloadDir string) error {
	// Check if directory exists
	if _, err := os.Stat(downloadDir); err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist, try to create it
			if err := os.MkdirAll(downloadDir, 0755); err != nil {
				return fmt.Errorf("cannot create download directory: %w", err)
			}
		} else {
			return fmt.Errorf("cannot access download directory: %w", err)
		}
	}

	// Check if it's actually a directory
	if info, err := os.Stat(downloadDir); err != nil {
		return fmt.Errorf("cannot stat download directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("download path is not a directory: %s", downloadDir)
	}

	// Check if directory is writable by attempting to create a temp file
	testFile := filepath.Join(downloadDir, ".write_test")
	if file, err := os.Create(testFile); err != nil {
		return fmt.Errorf("download directory is not writable: %w", err)
	} else {
		file.Close()
		os.Remove(testFile) // Clean up test file
	}

	return nil
}

// applyRuntimeConfigChanges applies configuration changes to running components
func (tm *TorrentManager) applyRuntimeConfigChanges(oldConfig, newConfig SessionConfiguration) error {
	// Update queue manager configuration if queue settings changed
	if oldConfig.DownloadQueueEnabled != newConfig.DownloadQueueEnabled ||
		oldConfig.DownloadQueueSize != newConfig.DownloadQueueSize ||
		oldConfig.SeedQueueEnabled != newConfig.SeedQueueEnabled ||
		oldConfig.SeedQueueSize != newConfig.SeedQueueSize {

		if err := tm.updateQueueConfiguration(newConfig); err != nil {
			return fmt.Errorf("failed to update queue configuration: %w", err)
		}
	}

	// Update download directory for existing torrents if changed
	if oldConfig.DownloadDir != newConfig.DownloadDir && newConfig.DownloadDir != "" {
		if err := tm.updateDownloadDirectory(newConfig.DownloadDir); err != nil {
			return fmt.Errorf("failed to update download directory: %w", err)
		}
	}

	// Update incomplete directory settings if changed
	if oldConfig.IncompleteDir != newConfig.IncompleteDir ||
		oldConfig.IncompleteDirEnabled != newConfig.IncompleteDirEnabled {
		if err := tm.updateIncompleteDirConfiguration(oldConfig, newConfig); err != nil {
			return fmt.Errorf("failed to update incomplete directory configuration: %w", err)
		}
	}

	// Update peer limits if changed
	if oldConfig.PeerLimitGlobal != newConfig.PeerLimitGlobal ||
		oldConfig.PeerLimitPerTorrent != newConfig.PeerLimitPerTorrent {
		tm.updatePeerLimits(newConfig)
	}

	// Update speed limits if changed
	if oldConfig.SpeedLimitDown != newConfig.SpeedLimitDown ||
		oldConfig.SpeedLimitDownEnabled != newConfig.SpeedLimitDownEnabled ||
		oldConfig.SpeedLimitUp != newConfig.SpeedLimitUp ||
		oldConfig.SpeedLimitUpEnabled != newConfig.SpeedLimitUpEnabled {
		tm.updateSpeedLimits(newConfig)
	}

	// Update blocklist configuration if changed
	if oldConfig.BlocklistEnabled != newConfig.BlocklistEnabled ||
		oldConfig.BlocklistURL != newConfig.BlocklistURL {
		if err := tm.updateBlocklistConfiguration(newConfig); err != nil {
			return fmt.Errorf("failed to update blocklist configuration: %w", err)
		}
	}

	return nil
}

// updateQueueConfiguration updates the queue manager with new settings
func (tm *TorrentManager) updateQueueConfiguration(config SessionConfiguration) error {
	// Update queue manager limits by modifying its configuration
	tm.queueManager.mu.Lock()
	tm.queueManager.config.MaxActiveDownloads = config.DownloadQueueSize
	tm.queueManager.config.MaxActiveSeeds = config.SeedQueueSize
	tm.queueManager.config.DownloadQueueEnabled = config.DownloadQueueEnabled
	tm.queueManager.config.SeedQueueEnabled = config.SeedQueueEnabled
	tm.queueManager.mu.Unlock()

	// If queues are disabled, force activate all queued torrents
	if !config.DownloadQueueEnabled {
		tm.queueManager.mu.RLock()
		queuedTorrents := make([]int64, 0, len(tm.queueManager.downloadQueue))
		for torrentID := range tm.queueManager.downloadQueue {
			queuedTorrents = append(queuedTorrents, torrentID)
		}
		tm.queueManager.mu.RUnlock()

		for _, torrentID := range queuedTorrents {
			tm.queueManager.ForceActivate(torrentID, DownloadQueue)
			tm.log("Activated download torrent %d when disabling download queue", torrentID)
		}
	}

	if !config.SeedQueueEnabled {
		tm.queueManager.mu.RLock()
		queuedTorrents := make([]int64, 0, len(tm.queueManager.seedQueue))
		for torrentID := range tm.queueManager.seedQueue {
			queuedTorrents = append(queuedTorrents, torrentID)
		}
		tm.queueManager.mu.RUnlock()

		for _, torrentID := range queuedTorrents {
			tm.queueManager.ForceActivate(torrentID, SeedQueue)
			tm.log("Activated seed torrent %d when disabling seed queue", torrentID)
		}
	}

	return nil
}

// updateDownloadDirectory updates the download directory for torrents
func (tm *TorrentManager) updateDownloadDirectory(newDir string) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Update the base configuration
	tm.config.DownloadDir = newDir

	// Note: Individual torrent download paths are typically set when torrents are added
	// and shouldn't be changed for active downloads. This mainly affects new torrents.
	tm.log("Download directory updated to: %s (affects new torrents)", newDir)

	return nil
}

// updatePeerLimits updates peer connection limits
func (tm *TorrentManager) updatePeerLimits(config SessionConfiguration) {
	// Update the configuration that will be used for new torrents
	tm.config.PeerLimitGlobal = config.PeerLimitGlobal
	tm.config.PeerLimitPerTorrent = config.PeerLimitPerTorrent

	tm.log("Peer limits updated: global=%d, per_torrent=%d",
		config.PeerLimitGlobal, config.PeerLimitPerTorrent)

	// Note: Existing peer connections are typically managed by the peer protocol
	// and downloader components. Runtime limit changes would require component
	// support for dynamic reconfiguration.
}

// updateSpeedLimits updates transfer speed limits
func (tm *TorrentManager) updateSpeedLimits(config SessionConfiguration) {
	// Update bandwidth manager with new limits
	tm.bandwidthManager.UpdateConfiguration(config)

	tm.log("Speed limits updated: down=%d (enabled=%t), up=%d (enabled=%t)",
		config.SpeedLimitDown, config.SpeedLimitDownEnabled,
		config.SpeedLimitUp, config.SpeedLimitUpEnabled)

	tm.log("Bandwidth manager: %s", tm.bandwidthManager.String())
}

// updateBlocklistConfiguration updates blocklist settings and triggers URL refresh if needed
func (tm *TorrentManager) updateBlocklistConfiguration(config SessionConfiguration) error {
	// Update blocklist enabled status
	tm.blocklistManager.SetEnabled(config.BlocklistEnabled)
	
	// Update blocklist URL if changed
	if config.BlocklistURL != "" {
		if err := tm.blocklistManager.SetURL(config.BlocklistURL); err != nil {
			return fmt.Errorf("failed to set blocklist URL: %w", err)
		}
	}
	
	tm.log("Blocklist configuration updated: enabled=%t, url=%s, size=%d",
		config.BlocklistEnabled, config.BlocklistURL, tm.blocklistManager.GetSize())
	
	return nil
}

// updateIncompleteDirConfiguration updates incomplete directory settings and handles migrations
func (tm *TorrentManager) updateIncompleteDirConfiguration(oldConfig, newConfig SessionConfiguration) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Update the base configuration
	tm.config.IncompleteDir = newConfig.IncompleteDir
	tm.config.IncompleteDirEnabled = newConfig.IncompleteDirEnabled

	// Log the configuration change
	if newConfig.IncompleteDirEnabled {
		tm.log("Incomplete directory enabled: %s", newConfig.IncompleteDir)
	} else {
		tm.log("Incomplete directory disabled")
	}

	// Note: For this initial implementation, we don't automatically move existing torrents
	// between directories. This would require file management operations that are better
	// handled explicitly by users or through separate file management functionality.
	// Future enhancement: Add automatic file migration when changing incomplete directory settings.

	return nil
}

// Queue Management Methods

// SetTorrentQueuePosition sets the queue position for a torrent
func (tm *TorrentManager) SetTorrentQueuePosition(id int64, position int64) error {
	tm.mu.RLock()
	_, exists := tm.torrents[id]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("torrent with ID %d not found", id)
	}

	return tm.queueManager.SetQueuePosition(id, position)
}

// GetTorrentQueuePosition returns the queue position for a torrent (-1 if not queued)
func (tm *TorrentManager) GetTorrentQueuePosition(id int64) int64 {
	return tm.queueManager.GetQueuePosition(id)
}

// GetQueueStats returns current queue statistics
func (tm *TorrentManager) GetQueueStats() QueueStats {
	return tm.queueManager.GetStats()
}

// IsActiveTorrent returns true if the torrent is currently active (not queued)
func (tm *TorrentManager) IsActiveTorrent(id int64) bool {
	return tm.queueManager.IsActive(id)
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
		// Check if peer is blocked by the blocklist
		if tm.blocklistManager.IsBlocked(peer.IP.String()) {
			tm.log("Blocked peer %s:%d by blocklist", peer.IP.String(), peer.Port)
			continue
		}
		
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

	torrent := tm.findTorrentByInfoHash(response.InfoHash)
	if torrent == nil {
		tm.log("Received metadata for unknown torrent: %s", response.InfoHash.HexString())
		return
	}

	metaInfo, err := tm.parseAndValidateMetadata(response)
	if err != nil {
		tm.log("Failed to process metadata for torrent %s: %v", response.InfoHash.HexString(), err)
		return
	}

	torrent.MetaInfo = metaInfo

	if err := tm.initializeTorrentFileStructure(torrent, metaInfo); err != nil {
		tm.log("Failed to initialize file structure for torrent %s: %v", response.InfoHash.HexString(), err)
		return
	}

	tm.finalizeTorrentMetadataSetup(torrent)
}

// findTorrentByInfoHash locates a torrent in the manager by its info hash.
func (tm *TorrentManager) findTorrentByInfoHash(infoHash metainfo.Hash) *TorrentState {
	for _, t := range tm.torrents {
		if t.InfoHash == infoHash {
			return t
		}
	}
	return nil
}

// parseAndValidateMetadata parses and validates the metadata from a downloader response.
func (tm *TorrentManager) parseAndValidateMetadata(response downloader.TorrentResponse) (*metainfo.MetaInfo, error) {
	var metaInfo metainfo.MetaInfo
	if err := bencode.DecodeBytes(response.InfoBytes, &metaInfo.InfoBytes); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	if sha1.Sum(response.InfoBytes) != [20]byte(response.InfoHash) {
		return nil, fmt.Errorf("invalid metadata: hash mismatch")
	}

	return &metaInfo, nil
}

// initializeTorrentFileStructure sets up the file arrays and information for a torrent.
func (tm *TorrentManager) initializeTorrentFileStructure(torrent *TorrentState, metaInfo *metainfo.MetaInfo) error {
	info, err := metaInfo.Info()
	if err != nil {
		return fmt.Errorf("failed to parse info: %w", err)
	}

	fileCount := len(info.Files)
	torrent.Files = make([]FileInfo, fileCount)
	torrent.Priorities = make([]int64, fileCount)
	torrent.Wanted = make([]bool, fileCount)

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

	return nil
}

// finalizeTorrentMetadataSetup completes the metadata setup and starts the download process.
func (tm *TorrentManager) finalizeTorrentMetadataSetup(torrent *TorrentState) {
	torrent.Status = TorrentStatusDownloading
	tm.log("Metadata downloaded for torrent %s, starting file download", torrent.InfoHash.HexString())
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
	tm.updateTransferRates(torrent, now)
	tm.updateTrackingFields(torrent, now)
	tm.updateCompletionStatistics(torrent)
	tm.updatePeerStatistics(torrent)
}

// updateTransferRates calculates download and upload rates based on bytes transferred since last update
func (tm *TorrentManager) updateTransferRates(torrent *TorrentState, now time.Time) {
	if !torrent.lastStatsUpdate.IsZero() {
		timeDelta := now.Sub(torrent.lastStatsUpdate).Seconds()
		if timeDelta > 0 {
			downloadDelta := torrent.Downloaded - torrent.lastDownloaded
			uploadDelta := torrent.Uploaded - torrent.lastUploaded

			torrent.DownloadRate = int64(float64(downloadDelta) / timeDelta)
			torrent.UploadRate = int64(float64(uploadDelta) / timeDelta)
		}
	}
}

// updateTrackingFields updates the tracking fields used for next statistics calculation
func (tm *TorrentManager) updateTrackingFields(torrent *TorrentState, now time.Time) {
	torrent.lastStatsUpdate = now
	torrent.lastDownloaded = torrent.Downloaded
	torrent.lastUploaded = torrent.Uploaded
}

// updateCompletionStatistics calculates completion percentage, remaining bytes, and piece statistics
func (tm *TorrentManager) updateCompletionStatistics(torrent *TorrentState) {
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
}

// updatePeerStatistics calculates ETA and updates all peer-related statistics
func (tm *TorrentManager) updatePeerStatistics(torrent *TorrentState) {
	torrent.ETA = tm.calculateETA(torrent)
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

		if tm.shouldSkipFile(currentOffset, fileSize, pieceOffset, pieceLength) {
			currentOffset += fileSize
			continue
		}

		if tm.isFileAfterPiece(currentOffset, pieceOffset, pieceLength) {
			break
		}

		fileStartInPiece, readLength := tm.calculateReadParameters(currentOffset, fileSize, pieceOffset, pieceLength)

		fileData, err := tm.readFileSegment(filePath, fileStartInPiece, readLength)
		if err != nil {
			return nil, err
		}

		copy(pieceData[piecePos:], fileData)
		piecePos += readLength
		currentOffset += fileSize
	}

	return pieceData[:piecePos], nil
}

// shouldSkipFile determines if a file is entirely before the piece and should be skipped
func (tm *TorrentManager) shouldSkipFile(currentOffset, fileSize, pieceOffset, pieceLength int64) bool {
	return currentOffset+fileSize <= pieceOffset
}

// isFileAfterPiece determines if a file is entirely after the piece
func (tm *TorrentManager) isFileAfterPiece(currentOffset, pieceOffset, pieceLength int64) bool {
	return currentOffset >= pieceOffset+pieceLength
}

// calculateReadParameters calculates the file start position and read length for a piece
func (tm *TorrentManager) calculateReadParameters(currentOffset, fileSize, pieceOffset, pieceLength int64) (fileStartInPiece, readLength int64) {
	fileStartInPiece = int64(0)
	if currentOffset < pieceOffset {
		fileStartInPiece = pieceOffset - currentOffset
	}

	readStart := currentOffset + fileStartInPiece
	readLength = fileSize - fileStartInPiece
	if readStart+readLength > pieceOffset+pieceLength {
		readLength = pieceOffset + pieceLength - readStart
	}

	return fileStartInPiece, readLength
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

// Queue management callback methods

// onTorrentActivated is called when a torrent is activated from the queue
func (tm *TorrentManager) onTorrentActivated(torrentID int64, queueType QueueType) {
	tm.mu.Lock()
	torrent, exists := tm.torrents[torrentID]
	if !exists {
		tm.mu.Unlock()
		tm.log("Queue activated unknown torrent ID: %d", torrentID)
		return
	}

	// Update torrent status based on queue type
	switch queueType {
	case DownloadQueue:
		if torrent.Status == TorrentStatusQueuedDown {
			torrent.Status = TorrentStatusDownloading
			tm.log("Torrent %d activated for downloading", torrentID)
			// Start actual download operation
			go tm.startTorrentDownload(torrent)
		}
	case SeedQueue:
		if torrent.Status == TorrentStatusQueuedSeed {
			torrent.Status = TorrentStatusSeeding
			tm.log("Torrent %d activated for seeding", torrentID)
			// Start actual seeding operation
			go tm.startTorrentSeeding(torrent)
		}
	}

	tm.mu.Unlock()
}

// onTorrentDeactivated is called when a torrent should be deactivated
func (tm *TorrentManager) onTorrentDeactivated(torrentID int64, queueType QueueType) {
	tm.mu.Lock()
	torrent, exists := tm.torrents[torrentID]
	if !exists {
		tm.mu.Unlock()
		tm.log("Queue deactivated unknown torrent ID: %d", torrentID)
		return
	}

	// Update torrent status to queued state
	switch queueType {
	case DownloadQueue:
		if torrent.Status == TorrentStatusDownloading {
			torrent.Status = TorrentStatusQueuedDown
			tm.log("Torrent %d deactivated to download queue", torrentID)
		}
	case SeedQueue:
		if torrent.Status == TorrentStatusSeeding {
			torrent.Status = TorrentStatusQueuedSeed
			tm.log("Torrent %d deactivated to seed queue", torrentID)
		}
	}

	tm.mu.Unlock()
}

// startTorrentDownload starts the actual download process for a torrent
func (tm *TorrentManager) startTorrentDownload(torrent *TorrentState) {
	// Use the existing private startTorrent method
	tm.startTorrent(torrent)
	tm.log("Started downloading torrent %d", torrent.ID)
}

// startTorrentSeeding starts the actual seeding process for a torrent
func (tm *TorrentManager) startTorrentSeeding(torrent *TorrentState) {
	// Implementation would set up seeding operations
	// For now, we'll just log and mark as seeding
	tm.mu.Lock()
	torrent.Status = TorrentStatusSeeding
	tm.mu.Unlock()

	tm.log("Started seeding torrent %d", torrent.ID)
}

// Bandwidth Management Methods

// WaitForDownloadBandwidth blocks until the specified number of download bytes can be transferred
// This method should be called by downloader components before transferring data
func (tm *TorrentManager) WaitForDownloadBandwidth(ctx context.Context, bytes int64) error {
	return tm.bandwidthManager.WaitForDownload(ctx, bytes)
}

// WaitForUploadBandwidth blocks until the specified number of upload bytes can be transferred
// This method should be called by uploader components before transferring data
func (tm *TorrentManager) WaitForUploadBandwidth(ctx context.Context, bytes int64) error {
	return tm.bandwidthManager.WaitForUpload(ctx, bytes)
}

// GetBandwidthStats returns current bandwidth limiter statistics
func (tm *TorrentManager) GetBandwidthStats() (downloadTokens, downloadMax, uploadTokens, uploadMax int64) {
	return tm.bandwidthManager.GetStats()
}
