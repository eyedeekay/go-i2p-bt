// Copyright 2025 go-i2ppackage rpc

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

// Package rpc implements a Transmission RPC compatible HTTP server
// for the go-i2p-bt library.
package rpc

import (
	"encoding/json"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// Standard JSON-RPC 2.0 structures

// Request represents a JSON-RPC 2.0 request
type Request struct {
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	Version string      `json:"jsonrpc"`
}

// Response represents a JSON-RPC 2.0 response
type Response struct {
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	Version string      `json:"jsonrpc"`
}

// RPCError represents a JSON-RPC 2.0 error
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error implements the error interface
func (e *RPCError) Error() string {
	return e.Message
}

// Standard Transmission RPC error codes
const (
	ErrCodeInvalidRequest   = -32600
	ErrCodeMethodNotFound   = -32601
	ErrCodeInvalidParams    = -32602
	ErrCodeInternalError    = -32603
	ErrCodeParseError       = -32700
	ErrCodeDuplicateTorrent = 1
	ErrCodeInvalidArgument  = 2
	ErrCodeInvalidTorrent   = 3
	ErrCodeSessionShutdown  = 4
)

// Transmission RPC Torrent Status values
const (
	TorrentStatusStopped      = 0 // Torrent is stopped
	TorrentStatusQueuedVerify = 1 // Queued to check files
	TorrentStatusVerifying    = 2 // Checking files
	TorrentStatusQueuedDown   = 3 // Queued to download
	TorrentStatusDownloading  = 4 // Downloading
	TorrentStatusQueuedSeed   = 5 // Queued to seed
	TorrentStatusSeeding      = 6 // Seeding
)

// TorrentAddRequest represents the arguments for torrent-add method
type TorrentAddRequest struct {
	Cookies           string   `json:"cookies,omitempty"`
	DownloadDir       string   `json:"download-dir,omitempty"`
	Filename          string   `json:"filename,omitempty"`
	Labels            []string `json:"labels,omitempty"`
	Metainfo          string   `json:"metainfo,omitempty"` // base64 encoded
	Paused            bool     `json:"paused,omitempty"`
	PeerLimit         int64    `json:"peer-limit,omitempty"`
	BandwidthPriority int64    `json:"bandwidthPriority,omitempty"`
	FilesWanted       []int64  `json:"files-wanted,omitempty"`
	FilesUnwanted     []int64  `json:"files-unwanted,omitempty"`
	PriorityHigh      []int64  `json:"priority-high,omitempty"`
	PriorityLow       []int64  `json:"priority-low,omitempty"`
	PriorityNormal    []int64  `json:"priority-normal,omitempty"`
}

// TorrentAddResponse represents the response for torrent-add method
type TorrentAddResponse struct {
	TorrentAdded     *Torrent `json:"torrent-added,omitempty"`
	TorrentDuplicate *Torrent `json:"torrent-duplicate,omitempty"`
}

// TorrentGetRequest represents the arguments for torrent-get method
type TorrentGetRequest struct {
	Fields []string      `json:"fields"`
	IDs    []interface{} `json:"ids,omitempty"` // Can be numbers, hash strings, or "recently-active"
	Format string        `json:"format,omitempty"`
}

// TorrentGetResponse represents the response for torrent-get method
type TorrentGetResponse struct {
	Torrents []Torrent `json:"torrents"`
	Removed  []int64   `json:"removed,omitempty"`
}

// TorrentActionRequest represents the arguments for torrent action methods
// (torrent-start, torrent-stop, torrent-remove, etc.)
type TorrentActionRequest struct {
	IDs                 []interface{} `json:"ids,omitempty"`
	DeleteLocalData     bool          `json:"delete-local-data,omitempty"`
	MoveToTrash         bool          `json:"move-to-trash,omitempty"`
	ForceReannounce     bool          `json:"force-reannounce,omitempty"`
	QueuePosition       int64         `json:"queue-position,omitempty"`
	BandwidthPriority   int64         `json:"bandwidthPriority,omitempty"`
	FilesWanted         []int64       `json:"files-wanted,omitempty"`
	FilesUnwanted       []int64       `json:"files-unwanted,omitempty"`
	PriorityHigh        []int64       `json:"priority-high,omitempty"`
	PriorityLow         []int64       `json:"priority-low,omitempty"`
	PriorityNormal      []int64       `json:"priority-normal,omitempty"`
	Location            string        `json:"location,omitempty"`
	Move                bool          `json:"move,omitempty"`
	HonorsSessionLimits bool          `json:"honorsSessionLimits,omitempty"`
	Labels              []string      `json:"labels,omitempty"`
	PeerLimit           int64         `json:"peer-limit,omitempty"`
	SeedIdleLimit       int64         `json:"seedIdleLimit,omitempty"`
	SeedIdleMode        int64         `json:"seedIdleMode,omitempty"`
	SeedRatioLimit      float64       `json:"seedRatioLimit,omitempty"`
	SeedRatioMode       int64         `json:"seedRatioMode,omitempty"`
	TrackerAdd          []string      `json:"trackerAdd,omitempty"`
	TrackerRemove       []int64       `json:"trackerRemove,omitempty"`
	TrackerReplace      []interface{} `json:"trackerReplace,omitempty"`
}

// SessionGetResponse represents the response for session-get method
type SessionGetResponse struct {
	AltSpeedDown              int64   `json:"alt-speed-down"`
	AltSpeedEnabled           bool    `json:"alt-speed-enabled"`
	AltSpeedTimeBegin         int64   `json:"alt-speed-time-begin"`
	AltSpeedTimeDay           int64   `json:"alt-speed-time-day"`
	AltSpeedTimeEnabled       bool    `json:"alt-speed-time-enabled"`
	AltSpeedTimeEnd           int64   `json:"alt-speed-time-end"`
	AltSpeedUp                int64   `json:"alt-speed-up"`
	BlocklistEnabled          bool    `json:"blocklist-enabled"`
	BlocklistSize             int64   `json:"blocklist-size"`
	BlocklistURL              string  `json:"blocklist-url"`
	CacheSizeMB               int64   `json:"cache-size-mb"`
	ConfigDir                 string  `json:"config-dir"`
	DHT                       bool    `json:"dht-enabled"`
	DownloadDir               string  `json:"download-dir"`
	DownloadDirFreeSpace      int64   `json:"download-dir-free-space"`
	DownloadQueueEnabled      bool    `json:"download-queue-enabled"`
	DownloadQueueSize         int64   `json:"download-queue-size"`
	Encryption                string  `json:"encryption"`
	IdleSeedingLimit          int64   `json:"idle-seeding-limit"`
	IdleSeedingLimitEnabled   bool    `json:"idle-seeding-limit-enabled"`
	IncompleteDir             string  `json:"incomplete-dir"`
	IncompleteDirEnabled      bool    `json:"incomplete-dir-enabled"`
	LPD                       bool    `json:"lpd-enabled"`
	PeerLimitGlobal           int64   `json:"peer-limit-global"`
	PeerLimitPerTorrent       int64   `json:"peer-limit-per-torrent"`
	PeerPort                  int64   `json:"peer-port"`
	PeerPortRandomOnStart     bool    `json:"peer-port-random-on-start"`
	PEX                       bool    `json:"pex-enabled"`
	PortForwardingEnabled     bool    `json:"port-forwarding-enabled"`
	QueueStalledEnabled       bool    `json:"queue-stalled-enabled"`
	QueueStalledMinutes       int64   `json:"queue-stalled-minutes"`
	RenamePartialFiles        bool    `json:"rename-partial-files"`
	RPCVersion                int64   `json:"rpc-version"`
	RPCVersionMinimum         int64   `json:"rpc-version-minimum"`
	ScriptTorrentDoneEnabled  bool    `json:"script-torrent-done-enabled"`
	ScriptTorrentDoneFilename string  `json:"script-torrent-done-filename"`
	SeedQueueEnabled          bool    `json:"seed-queue-enabled"`
	SeedQueueSize             int64   `json:"seed-queue-size"`
	SeedRatioLimit            float64 `json:"seedRatioLimit"`
	SeedRatioLimited          bool    `json:"seedRatioLimited"`
	SpeedLimitDown            int64   `json:"speed-limit-down"`
	SpeedLimitDownEnabled     bool    `json:"speed-limit-down-enabled"`
	SpeedLimitUp              int64   `json:"speed-limit-up"`
	SpeedLimitUpEnabled       bool    `json:"speed-limit-up-enabled"`
	StartAddedTorrents        bool    `json:"start-added-torrents"`
	TrashOriginalTorrentFiles bool    `json:"trash-original-torrent-files"`
	UTP                       bool    `json:"utp-enabled"`
	Version                   string  `json:"version"`
}

// SessionSetRequest represents the arguments for session-set method
type SessionSetRequest struct {
	AltSpeedDown              *int64   `json:"alt-speed-down,omitempty"`
	AltSpeedEnabled           *bool    `json:"alt-speed-enabled,omitempty"`
	AltSpeedTimeBegin         *int64   `json:"alt-speed-time-begin,omitempty"`
	AltSpeedTimeDay           *int64   `json:"alt-speed-time-day,omitempty"`
	AltSpeedTimeEnabled       *bool    `json:"alt-speed-time-enabled,omitempty"`
	AltSpeedTimeEnd           *int64   `json:"alt-speed-time-end,omitempty"`
	AltSpeedUp                *int64   `json:"alt-speed-up,omitempty"`
	BlocklistEnabled          *bool    `json:"blocklist-enabled,omitempty"`
	BlocklistURL              *string  `json:"blocklist-url,omitempty"`
	CacheSizeMB               *int64   `json:"cache-size-mb,omitempty"`
	DHT                       *bool    `json:"dht-enabled,omitempty"`
	DownloadDir               *string  `json:"download-dir,omitempty"`
	DownloadQueueEnabled      *bool    `json:"download-queue-enabled,omitempty"`
	DownloadQueueSize         *int64   `json:"download-queue-size,omitempty"`
	Encryption                *string  `json:"encryption,omitempty"`
	IdleSeedingLimit          *int64   `json:"idle-seeding-limit,omitempty"`
	IdleSeedingLimitEnabled   *bool    `json:"idle-seeding-limit-enabled,omitempty"`
	IncompleteDir             *string  `json:"incomplete-dir,omitempty"`
	IncompleteDirEnabled      *bool    `json:"incomplete-dir-enabled,omitempty"`
	LPD                       *bool    `json:"lpd-enabled,omitempty"`
	PeerLimitGlobal           *int64   `json:"peer-limit-global,omitempty"`
	PeerLimitPerTorrent       *int64   `json:"peer-limit-per-torrent,omitempty"`
	PeerPort                  *int64   `json:"peer-port,omitempty"`
	PeerPortRandomOnStart     *bool    `json:"peer-port-random-on-start,omitempty"`
	PEX                       *bool    `json:"pex-enabled,omitempty"`
	PortForwardingEnabled     *bool    `json:"port-forwarding-enabled,omitempty"`
	QueueStalledEnabled       *bool    `json:"queue-stalled-enabled,omitempty"`
	QueueStalledMinutes       *int64   `json:"queue-stalled-minutes,omitempty"`
	RenamePartialFiles        *bool    `json:"rename-partial-files,omitempty"`
	ScriptTorrentDoneEnabled  *bool    `json:"script-torrent-done-enabled,omitempty"`
	ScriptTorrentDoneFilename *string  `json:"script-torrent-done-filename,omitempty"`
	SeedQueueEnabled          *bool    `json:"seed-queue-enabled,omitempty"`
	SeedQueueSize             *int64   `json:"seed-queue-size,omitempty"`
	SeedRatioLimit            *float64 `json:"seedRatioLimit,omitempty"`
	SeedRatioLimited          *bool    `json:"seedRatioLimited,omitempty"`
	SpeedLimitDown            *int64   `json:"speed-limit-down,omitempty"`
	SpeedLimitDownEnabled     *bool    `json:"speed-limit-down-enabled,omitempty"`
	SpeedLimitUp              *int64   `json:"speed-limit-up,omitempty"`
	SpeedLimitUpEnabled       *bool    `json:"speed-limit-up-enabled,omitempty"`
	StartAddedTorrents        *bool    `json:"start-added-torrents,omitempty"`
	TrashOriginalTorrentFiles *bool    `json:"trash-original-torrent-files,omitempty"`
	UTP                       *bool    `json:"utp-enabled,omitempty"`
}

// Torrent represents a torrent object with all possible fields
type Torrent struct {
	// Torrent identification
	ID         int64  `json:"id"`
	HashString string `json:"hashString"`
	Name       string `json:"name"`

	// Status and activity
	Status                  int64   `json:"status"`
	Error                   int64   `json:"error"`
	ErrorString             string  `json:"errorString"`
	QueuePosition           int64   `json:"queuePosition"`
	IsFinished              bool    `json:"isFinished"`
	IsPrivate               bool    `json:"isPrivate"`
	IsStalled               bool    `json:"isStalled"`
	LeftUntilDone           int64   `json:"leftUntilDone"`
	MetadataPercentComplete float64 `json:"metadataPercentComplete"`
	PercentDone             float64 `json:"percentDone"`
	RecheckProgress         float64 `json:"recheckProgress"`
	SizeWhenDone            int64   `json:"sizeWhenDone"`

	// Transfer statistics
	AddedDate           time.Time `json:"addedDate"`
	ActivityDate        time.Time `json:"activityDate"`
	CorruptEver         int64     `json:"corruptEver"`
	DesiredAvailable    int64     `json:"desiredAvailable"`
	DownloadedEver      int64     `json:"downloadedEver"`
	DownloadLimit       int64     `json:"downloadLimit"`
	DownloadLimited     bool      `json:"downloadLimited"`
	ETA                 int64     `json:"eta"`
	ETAIdle             int64     `json:"etaIdle"`
	HaveUnchecked       int64     `json:"haveUnchecked"`
	HaveValid           int64     `json:"haveValid"`
	HonorsSessionLimits bool      `json:"honorsSessionLimits"`
	ManualAnnounceTime  int64     `json:"manualAnnounceTime"`
	MaxConnectedPeers   int64     `json:"maxConnectedPeers"`
	PeerLimit           int64     `json:"peer-limit"`
	PeersConnected      int64     `json:"peersConnected"`
	PeersFrom           PeersFrom `json:"peersFrom"`
	PeersGettingFromUs  int64     `json:"peersGettingFromUs"`
	PeersSendingToUs    int64     `json:"peersSendingToUs"`
	PieceCount          int64     `json:"pieceCount"`
	PieceSize           int64     `json:"pieceSize"`
	RateDownload        int64     `json:"rateDownload"`
	RateUpload          int64     `json:"rateUpload"`
	SeedIdleLimit       int64     `json:"seedIdleLimit"`
	SeedIdleMode        int64     `json:"seedIdleMode"`
	SeedRatioLimit      float64   `json:"seedRatioLimit"`
	SeedRatioMode       int64     `json:"seedRatioMode"`
	StartDate           time.Time `json:"startDate"`
	TotalSize           int64     `json:"totalSize"`
	UploadedEver        int64     `json:"uploadedEver"`
	UploadLimit         int64     `json:"uploadLimit"`
	UploadLimited       bool      `json:"uploadLimited"`
	UploadRatio         float64   `json:"uploadRatio"`

	// File and directory information
	DownloadDir string     `json:"downloadDir"`
	Files       []File     `json:"files"`
	FileStats   []FileStat `json:"fileStats"`
	Priorities  []int64    `json:"priorities"`
	Wanted      []bool     `json:"wanted"`

	// Tracker information
	Trackers     []Tracker     `json:"trackers"`
	TrackerStats []TrackerStat `json:"trackerStats"`

	// Peer information
	Peers []Peer `json:"peers"`

	// Pieces information
	Pieces         string   `json:"pieces"`
	PiecesComplete []bool   `json:"pieces_complete"`
	Webseeds       []string `json:"webseeds"`

	// Labels and other metadata
	Labels      []string               `json:"labels"`
	Creator     string                 `json:"creator"`
	DateCreated time.Time              `json:"dateCreated"`
	Comment     string                 `json:"comment"`
	UserFields  map[string]interface{} `json:"userFields,omitempty"`

	// Magnet link
	MagnetLink string `json:"magnetLink"`

	// Bandwidth priority
	BandwidthPriority int64 `json:"bandwidthPriority"`
}

// File represents a file within a torrent
type File struct {
	BytesCompleted int64  `json:"bytesCompleted"`
	Length         int64  `json:"length"`
	Name           string `json:"name"`
}

// FileStat represents file statistics
type FileStat struct {
	BytesCompleted int64 `json:"bytesCompleted"`
	Wanted         bool  `json:"wanted"`
	Priority       int64 `json:"priority"`
}

// Tracker represents a tracker
type Tracker struct {
	Announce string `json:"announce"`
	ID       int64  `json:"id"`
	Scrape   string `json:"scrape"`
	Tier     int64  `json:"tier"`
}

// TrackerStat represents tracker statistics
type TrackerStat struct {
	Announce              string `json:"announce"`
	AnnounceState         int64  `json:"announceState"`
	DownloadCount         int64  `json:"downloadCount"`
	HasAnnounced          bool   `json:"hasAnnounced"`
	HasScraped            bool   `json:"hasScraped"`
	Host                  string `json:"host"`
	ID                    int64  `json:"id"`
	IsBackup              bool   `json:"isBackup"`
	LastAnnouncePeerCount int64  `json:"lastAnnouncePeerCount"`
	LastAnnounceResult    string `json:"lastAnnounceResult"`
	LastAnnounceStartTime int64  `json:"lastAnnounceStartTime"`
	LastAnnounceSucceeded bool   `json:"lastAnnounceSucceeded"`
	LastAnnounceTime      int64  `json:"lastAnnounceTime"`
	LastAnnounceTimedOut  bool   `json:"lastAnnounceTimedOut"`
	LastScrapeResult      string `json:"lastScrapeResult"`
	LastScrapeStartTime   int64  `json:"lastScrapeStartTime"`
	LastScrapeSucceeded   bool   `json:"lastScrapeSucceeded"`
	LastScrapeTime        int64  `json:"lastScrapeTime"`
	LastScrapeTimedOut    bool   `json:"lastScrapeTimedOut"`
	LeecherCount          int64  `json:"leecherCount"`
	NextAnnounceTime      int64  `json:"nextAnnounceTime"`
	NextScrapeTime        int64  `json:"nextScrapeTime"`
	Scrape                string `json:"scrape"`
	ScrapeState           int64  `json:"scrapeState"`
	SeederCount           int64  `json:"seederCount"`
	Tier                  int64  `json:"tier"`
}

// Peer represents a peer connection
type Peer struct {
	Address            string  `json:"address"`
	ClientName         string  `json:"clientName"`
	ClientIsChoked     bool    `json:"clientIsChoked"`
	ClientIsInterested bool    `json:"clientIsInterested"`
	FlagStr            string  `json:"flagStr"`
	IsDownloadingFrom  bool    `json:"isDownloadingFrom"`
	IsEncrypted        bool    `json:"isEncrypted"`
	IsIncoming         bool    `json:"isIncoming"`
	IsUploadingTo      bool    `json:"isUploadingTo"`
	IsUTP              bool    `json:"isUTP"`
	PeerIsChoked       bool    `json:"peerIsChoked"`
	PeerIsInterested   bool    `json:"peerIsInterested"`
	Port               int64   `json:"port"`
	Progress           float64 `json:"progress"`
	RateToClient       int64   `json:"rateToClient"`
	RateToPeer         int64   `json:"rateToPeer"`
}

// PeersFrom represents peer sources
type PeersFrom struct {
	FromCache    int64 `json:"fromCache"`
	FromDHT      int64 `json:"fromDht"`
	FromIncoming int64 `json:"fromIncoming"`
	FromLPD      int64 `json:"fromLpd"`
	FromLTEP     int64 `json:"fromLtep"`
	FromPEX      int64 `json:"fromPex"`
	FromTracker  int64 `json:"fromTracker"`
}

// Internal structures for managing torrent state

// TorrentState represents the internal state of a torrent
type TorrentState struct {
	ID          int64              `json:"id"`
	InfoHash    metainfo.Hash      `json:"info_hash"`
	MetaInfo    *metainfo.MetaInfo `json:"meta_info,omitempty"`
	Status      int64              `json:"status"`
	Error       error              `json:"error,omitempty"`
	DownloadDir string             `json:"download_dir"`
	AddedDate   time.Time          `json:"added_date"`
	StartDate   time.Time          `json:"start_date,omitempty"`
	Labels      []string           `json:"labels"`

	// Download/Upload statistics
	Downloaded int64 `json:"downloaded"`
	Uploaded   int64 `json:"uploaded"`
	Left       int64 `json:"left"`

	// Progress and completion statistics
	PercentDone     float64 `json:"percent_done"`     // Completion percentage (0.0 to 1.0)
	PieceCount      int64   `json:"piece_count"`      // Total number of pieces
	PiecesComplete  int64   `json:"pieces_complete"`  // Number of completed pieces
	PiecesAvailable int64   `json:"pieces_available"` // Number of pieces available from peers

	// Transfer rates and timing
	DownloadRate int64 `json:"download_rate"` // Bytes per second
	UploadRate   int64 `json:"upload_rate"`   // Bytes per second
	ETA          int64 `json:"eta"`           // Estimated time to completion in seconds (-1 if unknown)

	// Peer statistics
	PeerCount          int64 `json:"peer_count"`           // Number of connected peers
	PeerConnectedCount int64 `json:"peer_connected_count"` // Number of peers we're connected to
	PeerSendingCount   int64 `json:"peer_sending_count"`   // Number of peers sending data to us
	PeerReceivingCount int64 `json:"peer_receiving_count"` // Number of peers receiving data from us

	// Activity tracking for transfer rate calculation
	lastStatsUpdate time.Time // Internal field for rate calculation
	lastDownloaded  int64     // Internal field for rate calculation
	lastUploaded    int64     // Internal field for rate calculation
	wasComplete     bool      // Internal field to track completion state changes for script hooks

	// File information
	Files      []FileInfo `json:"files"`
	Priorities []int64    `json:"priorities"`
	Wanted     []bool     `json:"wanted"`

	// Peer and tracker information
	TrackerList []string   `json:"tracker_list"`
	Peers       []PeerInfo `json:"peers"`

	// Seeding configuration
	SeedRatioLimit      float64 `json:"seed_ratio_limit"`
	SeedIdleLimit       int64   `json:"seed_idle_limit"`
	HonorsSessionLimits bool    `json:"honors_session_limits"`
}

// FileInfo represents internal file information
type FileInfo struct {
	Name           string `json:"name"`
	Length         int64  `json:"length"`
	BytesCompleted int64  `json:"bytes_completed"`
	Priority       int64  `json:"priority"`
	Wanted         bool   `json:"wanted"`
}

// PeerInfo represents internal peer information
type PeerInfo struct {
	ID         string `json:"id"`
	Address    string `json:"address"`
	Port       int64  `json:"port"`
	Uploaded   int64  `json:"uploaded"`
	Downloaded int64  `json:"downloaded"`
	Direction  string `json:"direction"` // "incoming" or "outgoing"
}

// SessionConfiguration represents the global session configuration
type SessionConfiguration struct {
	// Basic settings
	DownloadDir          string `json:"download_dir"`
	IncompleteDir        string `json:"incomplete_dir"`
	IncompleteDirEnabled bool   `json:"incomplete_dir_enabled"`
	PeerPort             int64  `json:"peer_port"`
	PeerLimitGlobal      int64  `json:"peer_limit_global"`
	PeerLimitPerTorrent  int64  `json:"peer_limit_per_torrent"`

	// Protocol settings
	DHTEnabled bool   `json:"dht_enabled"`
	PEXEnabled bool   `json:"pex_enabled"`
	LPDEnabled bool   `json:"lpd_enabled"`
	UTPEnabled bool   `json:"utp_enabled"`
	Encryption string `json:"encryption"`

	// Speed limits
	SpeedLimitDown        int64 `json:"speed_limit_down"`
	SpeedLimitDownEnabled bool  `json:"speed_limit_down_enabled"`
	SpeedLimitUp          int64 `json:"speed_limit_up"`
	SpeedLimitUpEnabled   bool  `json:"speed_limit_up_enabled"`

	// Alt speed limits
	AltSpeedDown    int64 `json:"alt_speed_down"`
	AltSpeedUp      int64 `json:"alt_speed_up"`
	AltSpeedEnabled bool  `json:"alt_speed_enabled"`

	// Queue settings
	DownloadQueueEnabled bool  `json:"download_queue_enabled"`
	DownloadQueueSize    int64 `json:"download_queue_size"`
	SeedQueueEnabled     bool  `json:"seed_queue_enabled"`
	SeedQueueSize        int64 `json:"seed_queue_size"`

	// Seeding limits
	SeedRatioLimit          float64 `json:"seed_ratio_limit"`
	SeedRatioLimited        bool    `json:"seed_ratio_limited"`
	IdleSeedingLimit        int64   `json:"idle_seeding_limit"`
	IdleSeedingLimitEnabled bool    `json:"idle_seeding_limit_enabled"`

	// Misc settings
	StartAddedTorrents bool   `json:"start_added_torrents"`
	CacheSizeMB        int64  `json:"cache_size_mb"`
	Version            string `json:"version"`

	// Blocklist settings
	BlocklistEnabled bool   `json:"blocklist_enabled"`
	BlocklistURL     string `json:"blocklist_url"`
	BlocklistSize    int64  `json:"blocklist_size"`

	// Script hook settings
	ScriptTorrentDoneEnabled  bool   `json:"script_torrent_done_enabled"`
	ScriptTorrentDoneFilename string `json:"script_torrent_done_filename"`
}

// Helper methods for JSON marshaling with custom time format

func (t *Torrent) MarshalJSON() ([]byte, error) {
	type Alias Torrent
	return json.Marshal(&struct {
		AddedDate    int64 `json:"addedDate"`
		ActivityDate int64 `json:"activityDate"`
		StartDate    int64 `json:"startDate"`
		DateCreated  int64 `json:"dateCreated"`
		*Alias
	}{
		AddedDate:    t.AddedDate.Unix(),
		ActivityDate: t.ActivityDate.Unix(),
		StartDate:    t.StartDate.Unix(),
		DateCreated:  t.DateCreated.Unix(),
		Alias:        (*Alias)(t),
	})
}

func (t *Torrent) UnmarshalJSON(data []byte) error {
	type Alias Torrent
	aux := &struct {
		AddedDate    int64 `json:"addedDate"`
		ActivityDate int64 `json:"activityDate"`
		StartDate    int64 `json:"startDate"`
		DateCreated  int64 `json:"dateCreated"`
		*Alias
	}{
		Alias: (*Alias)(t),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	t.AddedDate = time.Unix(aux.AddedDate, 0)
	t.ActivityDate = time.Unix(aux.ActivityDate, 0)
	t.StartDate = time.Unix(aux.StartDate, 0)
	t.DateCreated = time.Unix(aux.DateCreated, 0)
	return nil
}
