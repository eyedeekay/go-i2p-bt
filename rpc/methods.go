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
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RPCMethods contains all the RPC method implementations
type RPCMethods struct {
	manager *TorrentManager
}

// NewRPCMethods creates a new RPCMethods instance
func NewRPCMethods(manager *TorrentManager) *RPCMethods {
	return &RPCMethods{
		manager: manager,
	}
}

// TorrentAdd implements the torrent-add RPC method
func (m *RPCMethods) TorrentAdd(req TorrentAddRequest) (TorrentAddResponse, error) {
	// Add the torrent using the manager
	torrentState, err := m.manager.AddTorrent(req)
	if err != nil {
		// Check if it's a duplicate torrent error
		if strings.Contains(err.Error(), "already exists") {
			// Try to find the existing torrent
			if req.Metainfo != "" {
				// For .torrent files, we can calculate the hash
				// This is simplified - in practice you'd decode and hash
				existing := m.manager.GetAllTorrents()
				if len(existing) > 0 {
					torrent := m.convertTorrentStateToTorrent(existing[len(existing)-1])
					return TorrentAddResponse{
						TorrentDuplicate: &torrent,
					}, nil
				}
			}
			return TorrentAddResponse{}, &RPCError{
				Code:    ErrCodeDuplicateTorrent,
				Message: "torrent already exists",
			}
		}

		return TorrentAddResponse{}, &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	// Convert to Transmission format
	torrent := m.convertTorrentStateToTorrent(torrentState)

	return TorrentAddResponse{
		TorrentAdded: &torrent,
	}, nil
}

// TorrentGet implements the torrent-get RPC method
func (m *RPCMethods) TorrentGet(req TorrentGetRequest) (TorrentGetResponse, error) {
	// Resolve torrent IDs
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return TorrentGetResponse{}, &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	var torrents []Torrent
	var removed []int64

	// Get torrents by ID
	for _, id := range ids {
		torrentState, err := m.manager.GetTorrent(id)
		if err != nil {
			// Torrent might have been removed
			removed = append(removed, id)
			continue
		}

		torrent := m.convertTorrentStateToTorrent(torrentState)

		// Filter fields if specified
		if len(req.Fields) > 0 {
			torrent = m.filterTorrentFields(torrent, req.Fields)
		}

		torrents = append(torrents, torrent)
	}

	response := TorrentGetResponse{
		Torrents: torrents,
	}

	if len(removed) > 0 {
		response.Removed = removed
	}

	return response, nil
}

// TorrentStart implements the torrent-start RPC method
func (m *RPCMethods) TorrentStart(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	for _, id := range ids {
		if err := m.manager.StartTorrent(id); err != nil {
			return &RPCError{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("failed to start torrent %d: %v", id, err),
			}
		}
	}

	return nil
}

// TorrentStartNow implements the torrent-start-now RPC method
func (m *RPCMethods) TorrentStartNow(req TorrentActionRequest) error {
	// For simplicity, treat torrent-start-now the same as torrent-start
	// In a full implementation, this would bypass the queue
	return m.TorrentStart(req)
}

// TorrentStop implements the torrent-stop RPC method
func (m *RPCMethods) TorrentStop(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	for _, id := range ids {
		if err := m.manager.StopTorrent(id); err != nil {
			return &RPCError{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("failed to stop torrent %d: %v", id, err),
			}
		}
	}

	return nil
}

// TorrentVerify implements the torrent-verify RPC method
func (m *RPCMethods) TorrentVerify(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	// For each torrent, set status to verifying
	// In a full implementation, this would actually verify the data
	for _, id := range ids {
		torrent, err := m.manager.GetTorrent(id)
		if err != nil {
			continue
		}

		// Set status to verifying
		torrent.Status = TorrentStatusVerifying
	}

	return nil
}

// TorrentRemove implements the torrent-remove RPC method
func (m *RPCMethods) TorrentRemove(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	for _, id := range ids {
		if err := m.manager.RemoveTorrent(id, req.DeleteLocalData); err != nil {
			return &RPCError{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("failed to remove torrent %d: %v", id, err),
			}
		}
	}

	return nil
}

// TorrentSet implements the torrent-set RPC method
func (m *RPCMethods) TorrentSet(req TorrentActionRequest) error {
	ids, err := m.manager.ResolveTorrentIDs(req.IDs)
	if err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	// Update torrent properties
	for _, id := range ids {
		torrent, err := m.manager.GetTorrent(id)
		if err != nil {
			continue
		}

		// Apply changes based on request
		if req.BandwidthPriority != 0 {
			// Set bandwidth priority
			// In a full implementation, this would affect transfer scheduling
		}

		if len(req.FilesWanted) > 0 {
			// Mark files as wanted
			for _, fileIndex := range req.FilesWanted {
				if int(fileIndex) < len(torrent.Wanted) {
					torrent.Wanted[fileIndex] = true
				}
			}
		}

		if len(req.FilesUnwanted) > 0 {
			// Mark files as unwanted
			for _, fileIndex := range req.FilesUnwanted {
				if int(fileIndex) < len(torrent.Wanted) {
					torrent.Wanted[fileIndex] = false
				}
			}
		}

		if len(req.PriorityHigh) > 0 {
			for _, fileIndex := range req.PriorityHigh {
				if int(fileIndex) < len(torrent.Priorities) {
					torrent.Priorities[fileIndex] = 1 // High priority
				}
			}
		}

		if len(req.PriorityNormal) > 0 {
			for _, fileIndex := range req.PriorityNormal {
				if int(fileIndex) < len(torrent.Priorities) {
					torrent.Priorities[fileIndex] = 0 // Normal priority
				}
			}
		}

		if len(req.PriorityLow) > 0 {
			for _, fileIndex := range req.PriorityLow {
				if int(fileIndex) < len(torrent.Priorities) {
					torrent.Priorities[fileIndex] = -1 // Low priority
				}
			}
		}

		if req.SeedRatioLimit > 0 {
			torrent.SeedRatioLimit = req.SeedRatioLimit
		}

		if req.SeedIdleLimit > 0 {
			torrent.SeedIdleLimit = req.SeedIdleLimit
		}

		if len(req.Labels) > 0 {
			torrent.Labels = req.Labels
		}

		if req.PeerLimit > 0 {
			// Set peer limit for this torrent
		}
	}

	return nil
}

// SessionGet implements the session-get RPC method
func (m *RPCMethods) SessionGet() (SessionGetResponse, error) {
	config := m.manager.GetSessionConfig()

	response := SessionGetResponse{
		AltSpeedDown:              config.AltSpeedDown,
		AltSpeedEnabled:           config.AltSpeedEnabled,
		AltSpeedTimeBegin:         0,     // Not implemented
		AltSpeedTimeDay:           0,     // Not implemented
		AltSpeedTimeEnabled:       false, // Not implemented
		AltSpeedTimeEnd:           0,     // Not implemented
		AltSpeedUp:                config.AltSpeedUp,
		BlocklistEnabled:          config.BlocklistEnabled,
		BlocklistSize:             config.BlocklistSize,
		BlocklistURL:              config.BlocklistURL,
		CacheSizeMB:               config.CacheSizeMB,
		ConfigDir:                 "", // Not implemented
		DHT:                       config.DHTEnabled,
		DownloadDir:               config.DownloadDir,
		DownloadDirFreeSpace:      0, // Would require filesystem check
		DownloadQueueEnabled:      config.DownloadQueueEnabled,
		DownloadQueueSize:         config.DownloadQueueSize,
		Encryption:                config.Encryption,
		IdleSeedingLimit:          config.IdleSeedingLimit,
		IdleSeedingLimitEnabled:   config.IdleSeedingLimitEnabled,
		IncompleteDir:             "",    // Not implemented
		IncompleteDirEnabled:      false, // Not implemented
		LPD:                       config.LPDEnabled,
		PeerLimitGlobal:           config.PeerLimitGlobal,
		PeerLimitPerTorrent:       config.PeerLimitPerTorrent,
		PeerPort:                  config.PeerPort,
		PeerPortRandomOnStart:     false, // Not implemented
		PEX:                       config.PEXEnabled,
		PortForwardingEnabled:     false, // Not implemented
		QueueStalledEnabled:       false, // Not implemented
		QueueStalledMinutes:       0,     // Not implemented
		RenamePartialFiles:        false, // Not implemented
		RPCVersion:                17,    // Supporting Transmission RPC version 17
		RPCVersionMinimum:         1,
		ScriptTorrentDoneEnabled:  false, // Not implemented
		ScriptTorrentDoneFilename: "",    // Not implemented
		SeedQueueEnabled:          config.SeedQueueEnabled,
		SeedQueueSize:             config.SeedQueueSize,
		SeedRatioLimit:            config.SeedRatioLimit,
		SeedRatioLimited:          config.SeedRatioLimited,
		SpeedLimitDown:            config.SpeedLimitDown,
		SpeedLimitDownEnabled:     config.SpeedLimitDownEnabled,
		SpeedLimitUp:              config.SpeedLimitUp,
		SpeedLimitUpEnabled:       config.SpeedLimitUpEnabled,
		StartAddedTorrents:        config.StartAddedTorrents,
		TrashOriginalTorrentFiles: false, // Not implemented
		UTP:                       config.UTPEnabled,
		Version:                   config.Version,
	}

	return response, nil
}

// SessionSet implements the session-set RPC method
func (m *RPCMethods) SessionSet(req SessionSetRequest) error {
	config := m.manager.GetSessionConfig()

	// Update configuration based on request
	if req.AltSpeedDown != nil {
		config.AltSpeedDown = *req.AltSpeedDown
	}
	if req.AltSpeedEnabled != nil {
		config.AltSpeedEnabled = *req.AltSpeedEnabled
	}
	if req.AltSpeedUp != nil {
		config.AltSpeedUp = *req.AltSpeedUp
	}
	if req.BlocklistEnabled != nil {
		config.BlocklistEnabled = *req.BlocklistEnabled
	}
	if req.BlocklistURL != nil {
		config.BlocklistURL = *req.BlocklistURL
	}
	if req.CacheSizeMB != nil {
		config.CacheSizeMB = *req.CacheSizeMB
	}
	if req.DHT != nil {
		config.DHTEnabled = *req.DHT
	}
	if req.DownloadDir != nil {
		config.DownloadDir = *req.DownloadDir
	}
	if req.DownloadQueueEnabled != nil {
		config.DownloadQueueEnabled = *req.DownloadQueueEnabled
	}
	if req.DownloadQueueSize != nil {
		config.DownloadQueueSize = *req.DownloadQueueSize
	}
	if req.Encryption != nil {
		config.Encryption = *req.Encryption
	}
	if req.IdleSeedingLimit != nil {
		config.IdleSeedingLimit = *req.IdleSeedingLimit
	}
	if req.IdleSeedingLimitEnabled != nil {
		config.IdleSeedingLimitEnabled = *req.IdleSeedingLimitEnabled
	}
	if req.LPD != nil {
		config.LPDEnabled = *req.LPD
	}
	if req.PeerLimitGlobal != nil {
		config.PeerLimitGlobal = *req.PeerLimitGlobal
	}
	if req.PeerLimitPerTorrent != nil {
		config.PeerLimitPerTorrent = *req.PeerLimitPerTorrent
	}
	if req.PeerPort != nil {
		config.PeerPort = *req.PeerPort
	}
	if req.PEX != nil {
		config.PEXEnabled = *req.PEX
	}
	if req.SeedQueueEnabled != nil {
		config.SeedQueueEnabled = *req.SeedQueueEnabled
	}
	if req.SeedQueueSize != nil {
		config.SeedQueueSize = *req.SeedQueueSize
	}
	if req.SeedRatioLimit != nil {
		config.SeedRatioLimit = *req.SeedRatioLimit
	}
	if req.SeedRatioLimited != nil {
		config.SeedRatioLimited = *req.SeedRatioLimited
	}
	if req.SpeedLimitDown != nil {
		config.SpeedLimitDown = *req.SpeedLimitDown
	}
	if req.SpeedLimitDownEnabled != nil {
		config.SpeedLimitDownEnabled = *req.SpeedLimitDownEnabled
	}
	if req.SpeedLimitUp != nil {
		config.SpeedLimitUp = *req.SpeedLimitUp
	}
	if req.SpeedLimitUpEnabled != nil {
		config.SpeedLimitUpEnabled = *req.SpeedLimitUpEnabled
	}
	if req.StartAddedTorrents != nil {
		config.StartAddedTorrents = *req.StartAddedTorrents
	}
	if req.UTP != nil {
		config.UTPEnabled = *req.UTP
	}

	// Apply the updated configuration
	if err := m.manager.UpdateSessionConfig(config); err != nil {
		return &RPCError{
			Code:    ErrCodeInvalidArgument,
			Message: err.Error(),
		}
	}

	return nil
}

// SessionStats implements the session-stats RPC method
func (m *RPCMethods) SessionStats() (map[string]interface{}, error) {
	// Return basic session statistics
	// In a full implementation, this would include detailed transfer stats
	stats := map[string]interface{}{
		"activeTorrentCount": len(m.manager.GetAllTorrents()),
		"downloadSpeed":      0, // Would calculate current download speed
		"pausedTorrentCount": 0, // Would count paused torrents
		"torrentCount":       len(m.manager.GetAllTorrents()),
		"uploadSpeed":        0, // Would calculate current upload speed
		"cumulative-stats": map[string]interface{}{
			"downloadedBytes": 0, // Total downloaded since start
			"uploadedBytes":   0, // Total uploaded since start
			"filesAdded":      0, // Total files added
			"sessionCount":    1, // Session count
			"secondsActive":   0, // Seconds active
		},
		"current-stats": map[string]interface{}{
			"downloadedBytes": 0, // Downloaded this session
			"uploadedBytes":   0, // Uploaded this session
			"filesAdded":      0, // Files added this session
			"sessionCount":    1, // Always 1 for current
			"secondsActive":   0, // Seconds active this session
		},
	}

	return stats, nil
}

// Helper methods

// convertTorrentStateToTorrent converts internal TorrentState to Transmission Torrent format
func (m *RPCMethods) convertTorrentStateToTorrent(state *TorrentState) Torrent {
	var files []File
	var fileStats []FileStat
	var priorities []int64
	var wanted []bool

	// Convert files
	for i, f := range state.Files {
		files = append(files, File{
			BytesCompleted: f.BytesCompleted,
			Length:         f.Length,
			Name:           f.Name,
		})

		fileStats = append(fileStats, FileStat{
			BytesCompleted: f.BytesCompleted,
			Wanted:         f.Wanted,
			Priority:       f.Priority,
		})

		if i < len(state.Priorities) {
			priorities = append(priorities, state.Priorities[i])
		}
		if i < len(state.Wanted) {
			wanted = append(wanted, state.Wanted[i])
		}
	}

	// Convert peers
	var peers []Peer
	for _, p := range state.Peers {
		peers = append(peers, Peer{
			Address:      p.Address,
			Port:         p.Port,
			ClientName:   "Unknown",
			IsIncoming:   p.Direction == "incoming",
			RateToClient: 0, // Would need to track transfer rates
			RateToPeer:   0,
		})
	}

	// Convert trackers
	var trackers []Tracker
	var trackerStats []TrackerStat
	for i, announce := range state.TrackerList {
		trackers = append(trackers, Tracker{
			Announce: announce,
			ID:       int64(i),
			Tier:     int64(i), // Simplified
		})

		trackerStats = append(trackerStats, TrackerStat{
			Announce:              announce,
			ID:                    int64(i),
			Tier:                  int64(i),
			LastAnnounceSucceeded: false, // Would track actual status
			AnnounceState:         0,     // Would track actual state
		})
	}

	// Calculate progress
	var totalSize int64
	var completed int64
	for _, f := range files {
		totalSize += f.Length
		completed += f.BytesCompleted
	}

	var percentDone float64
	if totalSize > 0 {
		percentDone = float64(completed) / float64(totalSize)
	}

	// Create magnet link
	magnetLink := ""
	if state.MetaInfo != nil {
		magnet := state.MetaInfo.Magnet("", state.InfoHash)
		magnetLink = magnet.String()
	}

	return Torrent{
		ID:                      state.ID,
		HashString:              state.InfoHash.HexString(),
		Name:                    getName(state),
		Status:                  state.Status,
		AddedDate:               state.AddedDate,
		StartDate:               state.StartDate,
		ActivityDate:            getLastActivity(state),
		DownloadDir:             state.DownloadDir,
		Files:                   files,
		FileStats:               fileStats,
		Priorities:              priorities,
		Wanted:                  wanted,
		Trackers:                trackers,
		TrackerStats:            trackerStats,
		Peers:                   peers,
		TotalSize:               totalSize,
		LeftUntilDone:           totalSize - completed,
		PercentDone:             percentDone,
		SizeWhenDone:            totalSize,
		DownloadedEver:          completed,
		HaveValid:               completed,
		RateDownload:            state.DownloadRate,
		RateUpload:              state.UploadRate,
		UploadedEver:            state.Uploaded,
		UploadRatio:             calculateRatio(state.Uploaded, completed),
		SeedRatioLimit:          state.SeedRatioLimit,
		SeedIdleLimit:           state.SeedIdleLimit,
		HonorsSessionLimits:     state.HonorsSessionLimits,
		Labels:                  state.Labels,
		MagnetLink:              magnetLink,
		PieceCount:              0,     // Would need to calculate from piece length
		PieceSize:               0,     // Would get from metainfo
		IsPrivate:               false, // Would get from metainfo
		IsFinished:              state.Status == TorrentStatusSeeding,
		MetadataPercentComplete: getMetadataProgress(state),
		BandwidthPriority:       0, // Default priority
	}
}

// filterTorrentFields filters torrent fields based on requested fields
func (m *RPCMethods) filterTorrentFields(torrent Torrent, fields []string) Torrent {
	// Create a new torrent with only requested fields
	// This is a simplified implementation - in practice, you'd use reflection
	// or generate this code to handle all possible fields

	filtered := Torrent{}

	for _, field := range fields {
		switch field {
		case "id":
			filtered.ID = torrent.ID
		case "hashString":
			filtered.HashString = torrent.HashString
		case "name":
			filtered.Name = torrent.Name
		case "status":
			filtered.Status = torrent.Status
		case "addedDate":
			filtered.AddedDate = torrent.AddedDate
		case "startDate":
			filtered.StartDate = torrent.StartDate
		case "activityDate":
			filtered.ActivityDate = torrent.ActivityDate
		case "downloadDir":
			filtered.DownloadDir = torrent.DownloadDir
		case "files":
			filtered.Files = torrent.Files
		case "fileStats":
			filtered.FileStats = torrent.FileStats
		case "priorities":
			filtered.Priorities = torrent.Priorities
		case "wanted":
			filtered.Wanted = torrent.Wanted
		case "trackers":
			filtered.Trackers = torrent.Trackers
		case "trackerStats":
			filtered.TrackerStats = torrent.TrackerStats
		case "peers":
			filtered.Peers = torrent.Peers
		case "totalSize":
			filtered.TotalSize = torrent.TotalSize
		case "leftUntilDone":
			filtered.LeftUntilDone = torrent.LeftUntilDone
		case "percentDone":
			filtered.PercentDone = torrent.PercentDone
		case "sizeWhenDone":
			filtered.SizeWhenDone = torrent.SizeWhenDone
		case "downloadedEver":
			filtered.DownloadedEver = torrent.DownloadedEver
		case "haveValid":
			filtered.HaveValid = torrent.HaveValid
		case "rateDownload":
			filtered.RateDownload = torrent.RateDownload
		case "rateUpload":
			filtered.RateUpload = torrent.RateUpload
		case "uploadedEver":
			filtered.UploadedEver = torrent.UploadedEver
		case "uploadRatio":
			filtered.UploadRatio = torrent.UploadRatio
		case "seedRatioLimit":
			filtered.SeedRatioLimit = torrent.SeedRatioLimit
		case "seedIdleLimit":
			filtered.SeedIdleLimit = torrent.SeedIdleLimit
		case "honorsSessionLimits":
			filtered.HonorsSessionLimits = torrent.HonorsSessionLimits
		case "labels":
			filtered.Labels = torrent.Labels
		case "magnetLink":
			filtered.MagnetLink = torrent.MagnetLink
		case "pieceCount":
			filtered.PieceCount = torrent.PieceCount
		case "pieceSize":
			filtered.PieceSize = torrent.PieceSize
		case "isPrivate":
			filtered.IsPrivate = torrent.IsPrivate
		case "isFinished":
			filtered.IsFinished = torrent.IsFinished
		case "metadataPercentComplete":
			filtered.MetadataPercentComplete = torrent.MetadataPercentComplete
		case "bandwidthPriority":
			filtered.BandwidthPriority = torrent.BandwidthPriority
		}
	}

	return filtered
}

// Helper functions

func getName(state *TorrentState) string {
	if state.MetaInfo != nil {
		info, err := state.MetaInfo.Info()
		if err == nil {
			return info.Name
		}
	}
	return state.InfoHash.HexString()
}

func getLastActivity(state *TorrentState) time.Time {
	if state.Status == TorrentStatusDownloading || state.Status == TorrentStatusSeeding {
		return time.Now()
	}
	return state.StartDate
}

func calculateRatio(uploaded, downloaded int64) float64 {
	if downloaded == 0 {
		return 0
	}
	return float64(uploaded) / float64(downloaded)
}

func getMetadataProgress(state *TorrentState) float64 {
	if state.MetaInfo != nil {
		return 1.0 // We have complete metadata
	}
	if state.Status == TorrentStatusQueuedVerify {
		return 0.5 // Downloading metadata
	}
	return 0.0 // No metadata yet
}

// Method registry for JSON-RPC dispatch

// GetMethodHandler returns a handler function for the given method name
func (m *RPCMethods) GetMethodHandler(method string) (func(json.RawMessage) (interface{}, error), bool) {
	switch method {
	case "torrent-add":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentAddRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return m.TorrentAdd(req)
		}, true

	case "torrent-get":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentGetRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return m.TorrentGet(req)
		}, true

	case "torrent-start":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentStart(req)
		}, true

	case "torrent-start-now":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentStartNow(req)
		}, true

	case "torrent-stop":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentStop(req)
		}, true

	case "torrent-verify":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentVerify(req)
		}, true

	case "torrent-remove":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentRemove(req)
		}, true

	case "torrent-set":
		return func(params json.RawMessage) (interface{}, error) {
			var req TorrentActionRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.TorrentSet(req)
		}, true

	case "session-get":
		return func(params json.RawMessage) (interface{}, error) {
			return m.SessionGet()
		}, true

	case "session-set":
		return func(params json.RawMessage) (interface{}, error) {
			var req SessionSetRequest
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, &RPCError{Code: ErrCodeInvalidParams, Message: err.Error()}
			}
			return nil, m.SessionSet(req)
		}, true

	case "session-stats":
		return func(params json.RawMessage) (interface{}, error) {
			return m.SessionStats()
		}, true

	default:
		return nil, false
	}
}

// GetSupportedMethods returns a list of all supported RPC methods
func (m *RPCMethods) GetSupportedMethods() []string {
	return []string{
		"torrent-add",
		"torrent-get",
		"torrent-start",
		"torrent-start-now",
		"torrent-stop",
		"torrent-verify",
		"torrent-remove",
		"torrent-set",
		"session-get",
		"session-set",
		"session-stats",
	}
}
