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
	"os"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	"github.com/go-i2p/go-i2p-bt/rpc"
)

// createTestTorrent creates a test torrent state for testing
func createTestTorrent(id int64) *rpc.TorrentState {
	infoHash := metainfo.NewRandomHash()
	return &rpc.TorrentState{
		ID:                 id,
		InfoHash:           infoHash,
		Status:             rpc.TorrentStatusDownloading,
		DownloadDir:        "/downloads",
		AddedDate:          time.Now(),
		StartDate:          time.Now(),
		Labels:             []string{"test", "example"},
		Downloaded:         1024 * 1024, // 1MB
		Uploaded:           512 * 1024,  // 512KB
		Left:               2048 * 1024, // 2MB
		PercentDone:        0.33,
		PieceCount:         100,
		PiecesComplete:     33,
		PiecesAvailable:    66,
		DownloadRate:       102400, // 100 KB/s
		UploadRate:         51200,  // 50 KB/s
		ETA:                60,     // 60 seconds
		PeerCount:          5,
		PeerConnectedCount: 3,
		PeerSendingCount:   2,
		PeerReceivingCount: 1,
		Files: []rpc.FileInfo{
			{Name: "file1.txt", Length: 1024, BytesCompleted: 512, Priority: 1, Wanted: true},
			{Name: "file2.bin", Length: 2048, BytesCompleted: 1024, Priority: 0, Wanted: false},
		},
		Priorities:          []int64{1, 0, 1},
		Wanted:              []bool{true, false, true},
		TrackerList:         []string{"http://tracker1.example.com", "http://tracker2.example.com"},
		SeedRatioLimit:      2.0,
		SeedIdleLimit:       1800, // 30 minutes
		HonorsSessionLimits: true,
	}
}

// createTestSessionConfig creates a test session configuration
func createTestSessionConfig() *rpc.SessionConfiguration {
	return &rpc.SessionConfiguration{
		DownloadDir:          "/downloads",
		IncompleteDir:        "/incomplete",
		IncompleteDirEnabled: true,
		PeerPort:             6881,
		PeerLimitGlobal:      200,
		PeerLimitPerTorrent:  50,
		DHTEnabled:           true,
		PEXEnabled:           true,
		LPDEnabled:           false,
		UTPEnabled:           true,
		WebSeedsEnabled:      true,
		Encryption:           "preferred",
		SpeedLimitDown:       1024000, // 1MB/s
	}
}

// TestFilePersistence tests the file-based persistence implementation
func TestFilePersistence(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "file_persistence_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create persistence instance
	fp, err := NewFilePersistence(tempDir)
	if err != nil {
		t.Fatalf("Failed to create file persistence: %v", err)
	}
	defer fp.Close()

	ctx := context.Background()

	// Test session configuration persistence
	t.Run("SessionConfig", func(t *testing.T) {
		config := createTestSessionConfig()

		// Save session config
		if err := fp.SaveSessionConfig(ctx, config); err != nil {
			t.Fatalf("Failed to save session config: %v", err)
		}

		// Load session config
		loadedConfig, err := fp.LoadSessionConfig(ctx)
		if err != nil {
			t.Fatalf("Failed to load session config: %v", err)
		}

		// Compare critical fields
		if loadedConfig.DownloadDir != config.DownloadDir {
			t.Errorf("DownloadDir mismatch: got %s, want %s", loadedConfig.DownloadDir, config.DownloadDir)
		}
		if loadedConfig.PeerLimitGlobal != config.PeerLimitGlobal {
			t.Errorf("PeerLimitGlobal mismatch: got %d, want %d", loadedConfig.PeerLimitGlobal, config.PeerLimitGlobal)
		}
	})

	// Test single torrent persistence
	t.Run("SingleTorrent", func(t *testing.T) {
		torrent := createTestTorrent(1)

		// Save torrent
		if err := fp.SaveTorrent(ctx, torrent); err != nil {
			t.Fatalf("Failed to save torrent: %v", err)
		}

		// Load torrent
		loadedTorrent, err := fp.LoadTorrent(ctx, torrent.InfoHash)
		if err != nil {
			t.Fatalf("Failed to load torrent: %v", err)
		}

		// Compare critical fields
		if loadedTorrent.InfoHash != torrent.InfoHash {
			t.Errorf("InfoHash mismatch: got %s, want %s", loadedTorrent.InfoHash, torrent.InfoHash)
		}
		if loadedTorrent.Status != torrent.Status {
			t.Errorf("Status mismatch: got %d, want %d", loadedTorrent.Status, torrent.Status)
		}
		if loadedTorrent.Downloaded != torrent.Downloaded {
			t.Errorf("Downloaded mismatch: got %d, want %d", loadedTorrent.Downloaded, torrent.Downloaded)
		}
	})

	// Test multiple torrent persistence
	t.Run("MultipleTorrents", func(t *testing.T) {
		torrents := []*rpc.TorrentState{
			createTestTorrent(1),
			createTestTorrent(2),
			createTestTorrent(3),
		}

		// Save all torrents
		if err := fp.SaveAllTorrents(ctx, torrents); err != nil {
			t.Fatalf("Failed to save all torrents: %v", err)
		}

		// Load all torrents
		loadedTorrents, err := fp.LoadAllTorrents(ctx)
		if err != nil {
			t.Fatalf("Failed to load all torrents: %v", err)
		}

		if len(loadedTorrents) != len(torrents) {
			t.Errorf("Torrent count mismatch: got %d, want %d", len(loadedTorrents), len(torrents))
		}

		// Create map for easy lookup
		torrentMap := make(map[metainfo.Hash]*rpc.TorrentState)
		for _, torrent := range loadedTorrents {
			torrentMap[torrent.InfoHash] = torrent
		}

		// Verify each original torrent was loaded
		for _, original := range torrents {
			loaded, exists := torrentMap[original.InfoHash]
			if !exists {
				t.Errorf("Torrent not found: %s", original.InfoHash)
				continue
			}
			if loaded.Status != original.Status {
				t.Errorf("Status mismatch for %s: got %d, want %d", original.InfoHash, loaded.Status, original.Status)
			}
		}
	})

	// Test torrent deletion
	t.Run("DeleteTorrent", func(t *testing.T) {
		torrent := createTestTorrent(99)

		// Save torrent
		if err := fp.SaveTorrent(ctx, torrent); err != nil {
			t.Fatalf("Failed to save torrent: %v", err)
		}

		// Verify it exists
		_, err := fp.LoadTorrent(ctx, torrent.InfoHash)
		if err != nil {
			t.Fatalf("Torrent should exist: %v", err)
		}

		// Delete torrent
		if err := fp.DeleteTorrent(ctx, torrent.InfoHash); err != nil {
			t.Fatalf("Failed to delete torrent: %v", err)
		}

		// Verify it's gone
		_, err = fp.LoadTorrent(ctx, torrent.InfoHash)
		if err == nil {
			t.Error("Torrent should not exist after deletion")
		}
	})

	// Test error cases
	t.Run("ErrorCases", func(t *testing.T) {
		// Test nil torrent
		if err := fp.SaveTorrent(ctx, nil); err == nil {
			t.Error("Should fail with nil torrent")
		}

		// Test nil session config
		if err := fp.SaveSessionConfig(ctx, nil); err == nil {
			t.Error("Should fail with nil session config")
		}

		// Test non-existent torrent
		randomHash := metainfo.NewRandomHash()
		_, err := fp.LoadTorrent(ctx, randomHash)
		if err == nil {
			t.Error("Should fail loading non-existent torrent")
		}
	})
}

// TestMemoryPersistence tests the memory-based persistence implementation
func TestMemoryPersistence(t *testing.T) {
	ctx := context.Background()

	// Test memory persistence without backup
	t.Run("WithoutBackup", func(t *testing.T) {
		mp := NewMemoryPersistence(nil, 0)
		defer mp.Close()

		// Test basic operations
		torrent := createTestTorrent(1)

		// Save and load torrent
		if err := mp.SaveTorrent(ctx, torrent); err != nil {
			t.Fatalf("Failed to save torrent: %v", err)
		}

		loadedTorrent, err := mp.LoadTorrent(ctx, torrent.InfoHash)
		if err != nil {
			t.Fatalf("Failed to load torrent: %v", err)
		}

		if loadedTorrent.InfoHash != torrent.InfoHash {
			t.Errorf("InfoHash mismatch: got %s, want %s", loadedTorrent.InfoHash, torrent.InfoHash)
		}

		// Test statistics
		stats := mp.GetStatistics()
		if stats["torrent_count"].(int) != 1 {
			t.Errorf("Expected 1 torrent, got %v", stats["torrent_count"])
		}
		if stats["write_operations"].(int64) != 1 {
			t.Errorf("Expected 1 write operation, got %v", stats["write_operations"])
		}
	})

	// Test memory persistence with file backup
	t.Run("WithBackup", func(t *testing.T) {
		// Create backup persistence
		tempDir, err := os.MkdirTemp("", "memory_backup_test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		backup, err := NewFilePersistence(tempDir)
		if err != nil {
			t.Fatalf("Failed to create backup persistence: %v", err)
		}
		defer backup.Close()

		// Create memory persistence with short snapshot interval
		mp := NewMemoryPersistence(backup, 100*time.Millisecond)
		defer mp.Close()

		// Save some data
		torrent := createTestTorrent(1)
		if err := mp.SaveTorrent(ctx, torrent); err != nil {
			t.Fatalf("Failed to save torrent: %v", err)
		}

		// Force snapshot
		if err := mp.ForceSnapshot(ctx); err != nil {
			t.Fatalf("Failed to force snapshot: %v", err)
		}

		// Verify backup has the data
		_, err = backup.LoadTorrent(ctx, torrent.InfoHash)
		if err != nil {
			t.Fatalf("Backup should contain torrent: %v", err)
		}

		// Test restore from backup
		mp2 := NewMemoryPersistence(backup, 0)
		defer mp2.Close()

		if err := mp2.LoadFromBackup(ctx); err != nil {
			t.Fatalf("Failed to load from backup: %v", err)
		}

		_, err = mp2.LoadTorrent(ctx, torrent.InfoHash)
		if err != nil {
			t.Fatalf("Restored memory persistence should contain torrent: %v", err)
		}
	})
}

// TestPersistenceHook tests the automatic persistence hook
func TestPersistenceHook(t *testing.T) {
	// Create temporary persistence
	tempDir, err := os.MkdirTemp("", "hook_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	fp, err := NewFilePersistence(tempDir)
	if err != nil {
		t.Fatalf("Failed to create file persistence: %v", err)
	}
	defer fp.Close()

	// Create persistence hook
	hook := NewPersistenceHook(fp)

	// Test torrent added event
	t.Run("TorrentAdded", func(t *testing.T) {
		torrent := createTestTorrent(1)
		ctx := &rpc.HookContext{
			Event:     rpc.HookEventTorrentAdded,
			Torrent:   torrent,
			Context:   context.Background(),
			Timestamp: time.Now(),
		}

		if err := hook.HandleTorrentEvent(ctx); err != nil {
			t.Fatalf("Hook failed: %v", err)
		}

		// Verify torrent was saved
		_, err := fp.LoadTorrent(context.Background(), torrent.InfoHash)
		if err != nil {
			t.Fatalf("Torrent should be saved: %v", err)
		}
	})

	// Test torrent removed event
	t.Run("TorrentRemoved", func(t *testing.T) {
		torrent := createTestTorrent(2)

		// First save it
		if err := fp.SaveTorrent(context.Background(), torrent); err != nil {
			t.Fatalf("Failed to save torrent: %v", err)
		}

		// Then trigger remove event
		ctx := &rpc.HookContext{
			Event:     rpc.HookEventTorrentRemoved,
			Torrent:   torrent,
			Context:   context.Background(),
			Timestamp: time.Now(),
		}

		if err := hook.HandleTorrentEvent(ctx); err != nil {
			t.Fatalf("Hook failed: %v", err)
		}

		// Verify torrent was deleted
		_, err := fp.LoadTorrent(context.Background(), torrent.InfoHash)
		if err == nil {
			t.Error("Torrent should be deleted")
		}
	})
}

// Benchmark tests for performance comparison
func BenchmarkFilePersistence_SaveTorrent(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "bench_file")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	fp, err := NewFilePersistence(tempDir)
	if err != nil {
		b.Fatalf("Failed to create file persistence: %v", err)
	}
	defer fp.Close()

	torrent := createTestTorrent(1)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		torrent.ID = int64(i) // Change ID to avoid conflicts
		torrent.InfoHash = metainfo.NewRandomHash()
		fp.SaveTorrent(ctx, torrent)
	}
}

func BenchmarkMemoryPersistence_SaveTorrent(b *testing.B) {
	mp := NewMemoryPersistence(nil, 0)
	defer mp.Close()

	torrent := createTestTorrent(1)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		torrent.ID = int64(i) // Change ID to avoid conflicts
		torrent.InfoHash = metainfo.NewRandomHash()
		mp.SaveTorrent(ctx, torrent)
	}
}
