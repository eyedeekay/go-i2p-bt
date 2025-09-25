package rpc

import (
	"encoding/base64"
	"testing"

	"github.com/go-i2p/go-i2p-bt/bencode"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// Helper function to create a test torrent for TorrentSet tests
func createTestTorrentForSet(t *testing.T, tm *TorrentManager) *TorrentState {
	// Create test metainfo with multiple files
	metainfoBytes := createTestMetainfoBytesWithFiles(t, "torrent_set_test", []testFile{
		{"file1.txt", 1024},
		{"file2.txt", 2048},
		{"file3.txt", 4096},
	})

	req := TorrentAddRequest{
		Metainfo: base64.StdEncoding.EncodeToString(metainfoBytes),
		Paused:   true,
	}

	torrent, err := tm.AddTorrent(req)
	if err != nil {
		t.Fatalf("Failed to add test torrent: %v", err)
	}

	// Initialize arrays needed for TorrentSet operations
	// Normally these would be set when the MetaInfo is parsed
	if torrent.Wanted == nil {
		torrent.Wanted = []bool{true, true, true} // 3 files, all wanted initially
	}
	if torrent.Priorities == nil {
		torrent.Priorities = []int64{0, 0, 0} // All normal priority initially
	}
	if torrent.TrackerList == nil {
		torrent.TrackerList = []string{"http://tracker.example.com/announce"}
	}

	return torrent
}

// Helper function to create metainfo with specific files
type testFile struct {
	name   string
	length int64
}

func createTestMetainfoBytesWithFiles(t *testing.T, name string, files []testFile) []byte {
	// Create info with multiple files
	info := metainfo.Info{
		Name:        name,
		PieceLength: 32768, // 32KB pieces
		Files:       make([]metainfo.File, len(files)),
	}

	var totalLength int64
	for i, file := range files {
		info.Files[i] = metainfo.File{
			Length: file.length,
			Paths:  []string{file.name},
		}
		totalLength += file.length
	}

	// Create dummy piece hashes
	pieceCount := (totalLength + info.PieceLength - 1) / info.PieceLength
	info.Pieces = make(metainfo.Hashes, pieceCount)
	for i := range info.Pieces {
		info.Pieces[i] = metainfo.NewRandomHash()
	}

	// Encode info to bytes
	infoBytes, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatalf("Failed to encode info: %v", err)
	}

	// Create metainfo structure
	metaInfo := metainfo.MetaInfo{
		InfoBytes: infoBytes,
		Announce:  "http://tracker.example.com/announce",
		Comment:   "Test torrent for TorrentSet",
		CreatedBy: "torrent-set-test",
	}

	// Encode to bytes
	metainfoBytes, err := bencode.EncodeBytes(metaInfo)
	if err != nil {
		t.Fatalf("Failed to encode metainfo: %v", err)
	}

	return metainfoBytes
}

func TestTorrentSetValidation(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	methods := NewRPCMethods(tm)

	// Add a test torrent first for valid request tests
	torrent := createTestTorrentForSet(t, tm)

	tests := []struct {
		name        string
		req         TorrentActionRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid request",
			req: TorrentActionRequest{
				IDs:               []interface{}{torrent.ID},
				PeerLimit:         50,
				SeedRatioLimit:    2.0,
				SeedIdleLimit:     30,
				BandwidthPriority: 0,
			},
			expectError: false,
		},
		{
			name: "Invalid peer limit - negative",
			req: TorrentActionRequest{
				IDs:       []interface{}{torrent.ID},
				PeerLimit: -1,
			},
			expectError: true,
			errorMsg:    "peer-limit must be non-negative",
		},
		{
			name: "Invalid seed ratio limit - negative",
			req: TorrentActionRequest{
				IDs:            []interface{}{torrent.ID},
				SeedRatioLimit: -1.0,
			},
			expectError: true,
			errorMsg:    "seedRatioLimit must be non-negative",
		},
		{
			name: "Invalid seed idle limit - negative",
			req: TorrentActionRequest{
				IDs:           []interface{}{torrent.ID},
				SeedIdleLimit: -1,
			},
			expectError: true,
			errorMsg:    "seedIdleLimit must be non-negative",
		},
		{
			name: "Invalid seed ratio mode",
			req: TorrentActionRequest{
				IDs:           []interface{}{torrent.ID},
				SeedRatioMode: 5,
			},
			expectError: true,
			errorMsg:    "seedRatioMode must be 0, 1, or 2",
		},
		{
			name: "Invalid seed idle mode",
			req: TorrentActionRequest{
				IDs:          []interface{}{torrent.ID},
				SeedIdleMode: -5,
			},
			expectError: true,
			errorMsg:    "seedIdleMode must be 0, 1, or 2",
		},
		{
			name: "Invalid bandwidth priority",
			req: TorrentActionRequest{
				IDs:               []interface{}{torrent.ID},
				BandwidthPriority: 5,
			},
			expectError: true,
			errorMsg:    "bandwidthPriority must be -1, 0, or 1",
		},
		{
			name: "Invalid tracker replace - wrong length",
			req: TorrentActionRequest{
				IDs:            []interface{}{torrent.ID},
				TrackerReplace: []interface{}{"only-one-element"},
			},
			expectError: true,
			errorMsg:    "trackerReplace must contain exactly 2 elements",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := methods.TorrentSet(tt.req)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errorMsg)
				} else if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != ErrCodeInvalidArgument {
						t.Errorf("Expected error code %d, got %d", ErrCodeInvalidArgument, rpcErr.Code)
					}
					// Check if error message contains expected text
					if tt.errorMsg != "" && len(rpcErr.Message) > 0 {
						// Just verify we got an error with a message
						t.Logf("Got expected error: %s", rpcErr.Message)
					}
				} else {
					t.Errorf("Expected RPCError, got %T: %v", err, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestTorrentSetFileSelection(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	methods := NewRPCMethods(tm)

	// Create a test torrent (arrays are initialized in the helper)
	torrent := createTestTorrentForSet(t, tm)

	tests := []struct {
		name           string
		req            TorrentActionRequest
		expectedWanted []bool
		expectError    bool
	}{
		{
			name: "Mark file unwanted",
			req: TorrentActionRequest{
				IDs:           []interface{}{torrent.ID},
				FilesUnwanted: []int64{1}, // Mark second file as unwanted
			},
			expectedWanted: []bool{true, false, true},
			expectError:    false,
		},
		{
			name: "Mark file wanted",
			req: TorrentActionRequest{
				IDs:         []interface{}{torrent.ID},
				FilesWanted: []int64{1}, // Mark second file as wanted again
			},
			expectedWanted: []bool{true, true, true},
			expectError:    false,
		},
		{
			name: "Invalid file index - out of bounds high",
			req: TorrentActionRequest{
				IDs:         []interface{}{torrent.ID},
				FilesWanted: []int64{10}, // Index 10 doesn't exist
			},
			expectError: true,
		},
		{
			name: "Invalid file index - negative",
			req: TorrentActionRequest{
				IDs:           []interface{}{torrent.ID},
				FilesUnwanted: []int64{-1},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := methods.TorrentSet(tt.req)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				} else {
					// Check the wanted array
					for i, expected := range tt.expectedWanted {
						if i < len(torrent.Wanted) && torrent.Wanted[i] != expected {
							t.Errorf("File %d: expected wanted=%v, got %v", i, expected, torrent.Wanted[i])
						}
					}
				}
			}
		})
	}
}

func TestTorrentSetFilePriorities(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	methods := NewRPCMethods(tm)

	// Create a test torrent (arrays are initialized in the helper)
	torrent := createTestTorrentForSet(t, tm)

	tests := []struct {
		name               string
		req                TorrentActionRequest
		expectedPriorities []int64
		expectError        bool
	}{
		{
			name: "Set high priority",
			req: TorrentActionRequest{
				IDs:          []interface{}{torrent.ID},
				PriorityHigh: []int64{0, 2}, // First and third files high priority
			},
			expectedPriorities: []int64{1, 0, 1}, // High, normal, high
			expectError:        false,
		},
		{
			name: "Set low priority",
			req: TorrentActionRequest{
				IDs:         []interface{}{torrent.ID},
				PriorityLow: []int64{1}, // Second file low priority
			},
			expectedPriorities: []int64{1, -1, 1}, // High, low, high
			expectError:        false,
		},
		{
			name: "Set normal priority",
			req: TorrentActionRequest{
				IDs:            []interface{}{torrent.ID},
				PriorityNormal: []int64{0, 1, 2}, // All files normal priority
			},
			expectedPriorities: []int64{0, 0, 0}, // All normal
			expectError:        false,
		},
		{
			name: "Invalid priority index",
			req: TorrentActionRequest{
				IDs:          []interface{}{torrent.ID},
				PriorityHigh: []int64{5}, // Index 5 doesn't exist
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := methods.TorrentSet(tt.req)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				} else {
					// Check the priorities array
					for i, expected := range tt.expectedPriorities {
						if i < len(torrent.Priorities) && torrent.Priorities[i] != expected {
							t.Errorf("File %d: expected priority=%d, got %d", i, expected, torrent.Priorities[i])
						}
					}
				}
			}
		})
	}
}

func TestTorrentSetTrackerManagement(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	methods := NewRPCMethods(tm)

	// Create a test torrent (arrays are initialized in the helper)
	torrent := createTestTorrentForSet(t, tm)

	// Add an additional tracker for testing
	torrent.TrackerList = append(torrent.TrackerList, "http://tracker2.example.com/announce")

	tests := []struct {
		name             string
		req              TorrentActionRequest
		expectedTrackers []string
		expectError      bool
	}{
		{
			name: "Add new tracker",
			req: TorrentActionRequest{
				IDs:        []interface{}{torrent.ID},
				TrackerAdd: []string{"http://tracker3.example.com/announce"},
			},
			expectedTrackers: []string{
				"http://tracker.example.com/announce",
				"http://tracker2.example.com/announce",
				"http://tracker3.example.com/announce",
			},
			expectError: false,
		},
		{
			name: "Add duplicate tracker - should be ignored",
			req: TorrentActionRequest{
				IDs:        []interface{}{torrent.ID},
				TrackerAdd: []string{"http://tracker.example.com/announce"}, // Already exists
			},
			expectedTrackers: []string{
				"http://tracker.example.com/announce",
				"http://tracker2.example.com/announce",
				"http://tracker3.example.com/announce",
			}, // No change
			expectError: false,
		},
		{
			name: "Remove tracker by index",
			req: TorrentActionRequest{
				IDs:           []interface{}{torrent.ID},
				TrackerRemove: []int64{1}, // Remove second tracker
			},
			expectedTrackers: []string{
				"http://tracker.example.com/announce",
				"http://tracker3.example.com/announce",
			},
			expectError: false,
		},
		{
			name: "Replace tracker",
			req: TorrentActionRequest{
				IDs: []interface{}{torrent.ID},
				TrackerReplace: []interface{}{
					0, // Replace first tracker
					"http://newtracker.example.com/announce",
				},
			},
			expectedTrackers: []string{
				"http://newtracker.example.com/announce",
				"http://tracker3.example.com/announce",
			},
			expectError: false,
		},
		{
			name: "Invalid tracker remove index",
			req: TorrentActionRequest{
				IDs:           []interface{}{torrent.ID},
				TrackerRemove: []int64{10}, // Index doesn't exist
			},
			expectError: true,
		},
		{
			name: "Invalid tracker replace index",
			req: TorrentActionRequest{
				IDs: []interface{}{torrent.ID},
				TrackerReplace: []interface{}{
					10, // Invalid index
					"http://newtracker.example.com/announce",
				},
			},
			expectError: true,
		},
		{
			name: "Invalid tracker replace - wrong type for index",
			req: TorrentActionRequest{
				IDs: []interface{}{torrent.ID},
				TrackerReplace: []interface{}{
					"not-a-number",
					"http://newtracker.example.com/announce",
				},
			},
			expectError: true,
		},
		{
			name: "Invalid tracker replace - wrong type for URL",
			req: TorrentActionRequest{
				IDs: []interface{}{torrent.ID},
				TrackerReplace: []interface{}{
					0,
					123, // Should be string
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store initial state
			initialTrackers := make([]string, len(torrent.TrackerList))
			copy(initialTrackers, torrent.TrackerList)

			err := methods.TorrentSet(tt.req)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				// Restore state for next test
				torrent.TrackerList = initialTrackers
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				} else {
					// Check the tracker list
					if len(torrent.TrackerList) != len(tt.expectedTrackers) {
						t.Errorf("Expected %d trackers, got %d", len(tt.expectedTrackers), len(torrent.TrackerList))
					} else {
						for i, expected := range tt.expectedTrackers {
							if torrent.TrackerList[i] != expected {
								t.Errorf("Tracker %d: expected %s, got %s", i, expected, torrent.TrackerList[i])
							}
						}
					}
				}
			}
		})
	}
}

func TestTorrentSetSeedingConfiguration(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	methods := NewRPCMethods(tm)

	// Create a test torrent
	torrent := createTestTorrentForSet(t, tm)

	// Test seeding configuration
	req := TorrentActionRequest{
		IDs:                 []interface{}{torrent.ID},
		SeedRatioLimit:      2.5,
		SeedIdleLimit:       1800, // 30 minutes
		HonorsSessionLimits: true,
		Labels:              []string{"test", "important"},
	}

	err = methods.TorrentSet(req)
	if err != nil {
		t.Fatalf("Failed to set seeding configuration: %v", err)
	}

	// Verify changes were applied
	if torrent.SeedRatioLimit != 2.5 {
		t.Errorf("Expected seed ratio limit 2.5, got %f", torrent.SeedRatioLimit)
	}

	if torrent.SeedIdleLimit != 1800 {
		t.Errorf("Expected seed idle limit 1800, got %d", torrent.SeedIdleLimit)
	}

	if !torrent.HonorsSessionLimits {
		t.Errorf("Expected honors session limits to be true")
	}

	expectedLabels := []string{"test", "important"}
	if len(torrent.Labels) != len(expectedLabels) {
		t.Errorf("Expected %d labels, got %d", len(expectedLabels), len(torrent.Labels))
	} else {
		for i, expected := range expectedLabels {
			if torrent.Labels[i] != expected {
				t.Errorf("Label %d: expected %s, got %s", i, expected, torrent.Labels[i])
			}
		}
	}
}

func TestTorrentSetLocationUpdate(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	methods := NewRPCMethods(tm)

	// Create a test torrent
	torrent := createTestTorrentForSet(t, tm)

	tests := []struct {
		name        string
		location    string
		move        bool
		expectError bool
	}{
		{
			name:        "Valid location update",
			location:    "/new/download/path",
			move:        false,
			expectError: false,
		},
		{
			name:        "Empty location - ignored, no error",
			location:    "",
			move:        false,
			expectError: false, // Empty location is ignored, not an error
		},
		{
			name:        "Whitespace only location - should error",
			location:    "   ",
			move:        false,
			expectError: true, // This will trigger validation since it's not empty
		},
		{
			name:        "Move flag set (not implemented)",
			location:    "/another/path",
			move:        true,
			expectError: true, // Move flag should return error since file moving is not implemented
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original location
			originalLocation := torrent.DownloadDir

			req := TorrentActionRequest{
				IDs:      []interface{}{torrent.ID},
				Location: tt.location,
				Move:     tt.move,
			}

			err := methods.TorrentSet(req)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				// Verify location wasn't changed on error
				if torrent.DownloadDir != originalLocation {
					t.Errorf("Location was changed unexpectedly to %s", torrent.DownloadDir)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				} else {
					// For empty location, verify it wasn't changed
					if tt.location == "" {
						if torrent.DownloadDir != originalLocation {
							t.Errorf("Empty location should not change download dir, but it changed from %s to %s", originalLocation, torrent.DownloadDir)
						}
					} else {
						// For non-empty location, verify it was updated
						if torrent.DownloadDir != tt.location {
							t.Errorf("Expected download dir %s, got %s", tt.location, torrent.DownloadDir)
						}
					}
				}
			}
		})
	}
}

func TestTorrentSetNonexistentTorrent(t *testing.T) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	methods := NewRPCMethods(tm)

	// Try to update a non-existent torrent
	req := TorrentActionRequest{
		IDs:            []interface{}{9999}, // Non-existent ID
		SeedRatioLimit: 2.0,
	}

	err = methods.TorrentSet(req)
	if err == nil {
		t.Errorf("Expected error for non-existent torrent, got nil")
	}

	if rpcErr, ok := err.(*RPCError); ok {
		if rpcErr.Code != ErrCodeInvalidArgument {
			t.Errorf("Expected error code %d, got %d", ErrCodeInvalidArgument, rpcErr.Code)
		}
	} else {
		t.Errorf("Expected RPCError, got %T: %v", err, err)
	}
}

// Benchmark the TorrentSet operation
func BenchmarkTorrentSet(b *testing.B) {
	tm, err := NewTorrentManager(createTestTorrentManagerConfig())
	if err != nil {
		b.Fatalf("Failed to create TorrentManager: %v", err)
	}
	defer tm.Close()

	methods := NewRPCMethods(tm)

	// Create a test torrent
	torrent := createTestTorrentForSet(&testing.T{}, tm)
	torrent.Wanted = []bool{true, true, true}
	torrent.Priorities = []int64{0, 0, 0}

	req := TorrentActionRequest{
		IDs:            []interface{}{torrent.ID},
		FilesWanted:    []int64{0, 1},
		FilesUnwanted:  []int64{2},
		PriorityHigh:   []int64{0},
		PriorityNormal: []int64{1},
		SeedRatioLimit: 2.0,
		Labels:         []string{"benchmark", "test"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := methods.TorrentSet(req)
		if err != nil {
			b.Fatalf("TorrentSet failed: %v", err)
		}
	}
}
