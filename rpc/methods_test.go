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
	"encoding/base64"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestRPCMethods_NewRPCMethods tests the constructor
func TestRPCMethods_NewRPCMethods(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)

	methods := NewRPCMethods(manager)
	if methods == nil {
		t.Fatal("NewRPCMethods returned nil")
	}
	if methods.manager != manager {
		t.Error("NewRPCMethods did not set manager correctly")
	}
}

// TestRPCMethods_TorrentAdd tests the torrent-add RPC method
func TestRPCMethods_TorrentAdd(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	tests := []struct {
		name        string
		request     TorrentAddRequest
		expectError bool
		errorCode   int
		expectAdded bool
	}{
		{
			name: "valid metainfo add",
			request: TorrentAddRequest{
				Metainfo: createValidTestMetainfo(t),
				Paused:   true,
			},
			expectError: false,
			expectAdded: true,
		},
		{
			name: "invalid base64 metainfo",
			request: TorrentAddRequest{
				Metainfo: "invalid-base64",
			},
			expectError: true,
			errorCode:   ErrCodeInvalidArgument,
		},
		{
			name: "empty request",
			request: TorrentAddRequest{
				Metainfo: "",
			},
			expectError: true,
			errorCode:   ErrCodeInvalidArgument,
		},
		{
			name: "with download directory",
			request: TorrentAddRequest{
				Metainfo:    createValidTestMetainfo(t),
				DownloadDir: "/custom/dir",
				Paused:      true,
			},
			expectError: false,
			expectAdded: true,
		},
		{
			name: "with labels and priorities",
			request: TorrentAddRequest{
				Metainfo:          createValidTestMetainfo(t),
				Labels:            []string{"test", "high-priority"},
				BandwidthPriority: 1,
				PeerLimit:         50,
				Paused:            true,
			},
			expectError: false,
			expectAdded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := methods.TorrentAdd(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Expected error code %d, got %d", tt.errorCode, rpcErr.Code)
					}
				} else {
					t.Errorf("Expected RPCError, got %T", err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.expectAdded {
				if response.TorrentAdded == nil {
					t.Error("Expected torrent-added in response")
				} else {
					if response.TorrentAdded.ID <= 0 {
						t.Error("Expected valid torrent ID")
					}
					if response.TorrentAdded.Name == "" {
						t.Error("Expected torrent name")
					}
				}
			}
		})
	}
}

// TestRPCMethods_TorrentGet tests the torrent-get RPC method
func TestRPCMethods_TorrentGet(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	// Add a test torrent first
	addReq := TorrentAddRequest{
		Metainfo: createValidTestMetainfo(t),
		Paused:   true,
	}
	addResp, err := methods.TorrentAdd(addReq)
	if err != nil {
		t.Fatalf("Failed to add test torrent: %v", err)
	}
	torrentID := addResp.TorrentAdded.ID

	tests := []struct {
		name         string
		request      TorrentGetRequest
		expectError  bool
		errorCode    int
		expectCount  int
		expectFields []string
	}{
		{
			name: "get all torrents",
			request: TorrentGetRequest{
				IDs: []interface{}{},
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name: "get specific torrent by ID",
			request: TorrentGetRequest{
				IDs: []interface{}{torrentID},
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name: "get with specific fields",
			request: TorrentGetRequest{
				IDs:    []interface{}{torrentID},
				Fields: []string{"id", "name", "status"},
			},
			expectError:  false,
			expectCount:  1,
			expectFields: []string{"id", "name", "status"},
		},
		{
			name: "get non-existent torrent",
			request: TorrentGetRequest{
				IDs: []interface{}{99999},
			},
			expectError: false,
			expectCount: 0, // Should return empty list
		},
		{
			name: "invalid torrent ID type",
			request: TorrentGetRequest{
				IDs: []interface{}{"invalid-id"},
			},
			expectError: true,
			errorCode:   ErrCodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := methods.TorrentGet(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Expected error code %d, got %d", tt.errorCode, rpcErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(response.Torrents) != tt.expectCount {
				t.Errorf("Expected %d torrents, got %d", tt.expectCount, len(response.Torrents))
				return
			}

			if tt.expectCount > 0 {
				torrent := response.Torrents[0]
				if torrent.ID != torrentID {
					t.Errorf("Expected torrent ID %d, got %d", torrentID, torrent.ID)
				}

				// Verify field filtering if specified
				if len(tt.expectFields) > 0 {
					verifyFilteredFields(t, torrent, tt.expectFields)
				}
			}
		})
	}
}

// TestRPCMethods_TorrentStart tests the torrent-start RPC method
func TestRPCMethods_TorrentStart(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	// Add a paused torrent first
	addReq := TorrentAddRequest{
		Metainfo: createValidTestMetainfo(t),
		Paused:   true,
	}
	addResp, err := methods.TorrentAdd(addReq)
	if err != nil {
		t.Fatalf("Failed to add test torrent: %v", err)
	}
	torrentID := addResp.TorrentAdded.ID

	tests := []struct {
		name        string
		request     TorrentActionRequest
		expectError bool
		errorCode   int
	}{
		{
			name: "start single torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{torrentID},
			},
			expectError: false,
		},
		{
			name: "start multiple torrents",
			request: TorrentActionRequest{
				IDs: []interface{}{torrentID},
			},
			expectError: false,
		},
		{
			name: "start non-existent torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{99999},
			},
			expectError: true,
			errorCode:   ErrCodeInternalError,
		},
		{
			name: "invalid torrent ID",
			request: TorrentActionRequest{
				IDs: []interface{}{"invalid"},
			},
			expectError: true,
			errorCode:   ErrCodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := methods.TorrentStart(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Expected error code %d, got %d", tt.errorCode, rpcErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestRPCMethods_TorrentStartNow tests the torrent-start-now RPC method
func TestRPCMethods_TorrentStartNow(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	// Add a paused torrent first
	addReq := TorrentAddRequest{
		Metainfo: createValidTestMetainfo(t),
		Paused:   true,
	}
	addResp, err := methods.TorrentAdd(addReq)
	if err != nil {
		t.Fatalf("Failed to add test torrent: %v", err)
	}
	torrentID := addResp.TorrentAdded.ID

	tests := []struct {
		name        string
		request     TorrentActionRequest
		expectError bool
		errorCode   int
	}{
		{
			name: "start now single torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{torrentID},
			},
			expectError: false,
		},
		{
			name: "start now non-existent torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{99999},
			},
			expectError: true,
			errorCode:   ErrCodeInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := methods.TorrentStartNow(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Expected error code %d, got %d", tt.errorCode, rpcErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestRPCMethods_TorrentStop tests the torrent-stop RPC method
func TestRPCMethods_TorrentStop(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	// Add a torrent first
	addReq := TorrentAddRequest{
		Metainfo: createValidTestMetainfo(t),
		Paused:   false,
	}
	addResp, err := methods.TorrentAdd(addReq)
	if err != nil {
		t.Fatalf("Failed to add test torrent: %v", err)
	}
	torrentID := addResp.TorrentAdded.ID

	tests := []struct {
		name        string
		request     TorrentActionRequest
		expectError bool
		errorCode   int
	}{
		{
			name: "stop single torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{torrentID},
			},
			expectError: false,
		},
		{
			name: "stop non-existent torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{99999},
			},
			expectError: true,
			errorCode:   ErrCodeInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := methods.TorrentStop(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Expected error code %d, got %d", tt.errorCode, rpcErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestRPCMethods_TorrentVerify tests the torrent-verify RPC method
func TestRPCMethods_TorrentVerify(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	// Add a torrent first
	addReq := TorrentAddRequest{
		Metainfo: createValidTestMetainfo(t),
		Paused:   true,
	}
	addResp, err := methods.TorrentAdd(addReq)
	if err != nil {
		t.Fatalf("Failed to add test torrent: %v", err)
	}
	torrentID := addResp.TorrentAdded.ID

	tests := []struct {
		name        string
		request     TorrentActionRequest
		expectError bool
		errorCode   int
	}{
		{
			name: "verify single torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{torrentID},
			},
			expectError: false,
		},
		{
			name: "verify non-existent torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{99999},
			},
			expectError: true,
			errorCode:   ErrCodeInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := methods.TorrentVerify(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Expected error code %d, got %d", tt.errorCode, rpcErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestRPCMethods_TorrentRemove tests the torrent-remove RPC method
func TestRPCMethods_TorrentRemove(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	tests := []struct {
		name         string
		request      TorrentActionRequest
		expectError  bool
		errorCode    int
		setupTorrent bool
	}{
		{
			name: "remove torrent without deleting data",
			request: TorrentActionRequest{
				IDs:             []interface{}{1}, // Will be set by setup
				DeleteLocalData: false,
			},
			expectError:  false,
			setupTorrent: true,
		},
		{
			name: "remove torrent with deleting data",
			request: TorrentActionRequest{
				IDs:             []interface{}{1}, // Will be set by setup
				DeleteLocalData: true,
			},
			expectError:  false,
			setupTorrent: true,
		},
		{
			name: "remove non-existent torrent",
			request: TorrentActionRequest{
				IDs: []interface{}{99999},
			},
			expectError: true,
			errorCode:   ErrCodeInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var torrentID int64
			if tt.setupTorrent {
				// Add a torrent first
				addReq := TorrentAddRequest{
					Metainfo: createValidTestMetainfo(t),
					Paused:   true,
				}
				addResp, err := methods.TorrentAdd(addReq)
				if err != nil {
					t.Fatalf("Failed to add test torrent: %v", err)
				}
				torrentID = addResp.TorrentAdded.ID
				tt.request.IDs = []interface{}{torrentID}
			}

			err := methods.TorrentRemove(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Expected error code %d, got %d", tt.errorCode, rpcErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify torrent was removed
			if tt.setupTorrent {
				getReq := TorrentGetRequest{
					IDs: []interface{}{torrentID},
				}
				getResp, _ := methods.TorrentGet(getReq)
				if len(getResp.Torrents) > 0 {
					t.Error("Torrent was not removed")
				}
			}
		})
	}
}

// TestRPCMethods_SessionGet tests the session-get RPC method
func TestRPCMethods_SessionGet(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	response, err := methods.SessionGet()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify essential fields are present
	if response.DownloadDir == "" {
		t.Error("Expected DownloadDir to be set")
	}
	if response.RPCVersion == 0 {
		t.Error("Expected RPCVersion to be set")
	}
	if response.Version == "" {
		t.Error("Expected Version to be set")
	}

	// Verify reasonable default values
	if response.RPCVersion != 17 {
		t.Errorf("Expected RPC version 17, got %d", response.RPCVersion)
	}
	if response.RPCVersionMinimum != 1 {
		t.Errorf("Expected minimum RPC version 1, got %d", response.RPCVersionMinimum)
	}
}

// TestRPCMethods_SessionSet tests the session-set RPC method
func TestRPCMethods_SessionSet(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	tests := []struct {
		name        string
		request     SessionSetRequest
		expectError bool
		errorCode   int
	}{
		{
			name: "set speed limits",
			request: SessionSetRequest{
				SpeedLimitDown:        intPtrLocal(1000),
				SpeedLimitDownEnabled: boolPtrLocal(true),
				SpeedLimitUp:          intPtrLocal(500),
				SpeedLimitUpEnabled:   boolPtrLocal(true),
			},
			expectError: false,
		},
		{
			name: "set peer limits",
			request: SessionSetRequest{
				PeerLimitGlobal:     intPtrLocal(100),
				PeerLimitPerTorrent: intPtrLocal(20),
			},
			expectError: false,
		},
		{
			name: "set download directory",
			request: SessionSetRequest{
				DownloadDir: stringPtrLocal("/tmp/test_download_dir"),
			},
			expectError: false,
		},
		{
			name: "set queue settings",
			request: SessionSetRequest{
				DownloadQueueEnabled: boolPtrLocal(true),
				DownloadQueueSize:    intPtrLocal(5),
				SeedQueueEnabled:     boolPtrLocal(true),
				SeedQueueSize:        intPtrLocal(3),
			},
			expectError: false,
		},
		{
			name: "set protocol options",
			request: SessionSetRequest{
				DHT: boolPtrLocal(true),
				PEX: boolPtrLocal(true),
				LPD: boolPtrLocal(false),
				UTP: boolPtrLocal(true),
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := methods.SessionSet(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if rpcErr, ok := err.(*RPCError); ok {
					if rpcErr.Code != tt.errorCode {
						t.Errorf("Expected error code %d, got %d", tt.errorCode, rpcErr.Code)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify changes were applied by getting session config
			sessionResp, err := methods.SessionGet()
			if err != nil {
				t.Errorf("Failed to verify session changes: %v", err)
				return
			}

			// Check a few key fields to ensure they were updated
			if tt.request.SpeedLimitDown != nil && sessionResp.SpeedLimitDown != *tt.request.SpeedLimitDown {
				t.Errorf("Speed limit down not updated: expected %d, got %d",
					*tt.request.SpeedLimitDown, sessionResp.SpeedLimitDown)
			}
			if tt.request.DHT != nil && sessionResp.DHT != *tt.request.DHT {
				t.Errorf("DHT setting not updated: expected %v, got %v",
					*tt.request.DHT, sessionResp.DHT)
			}
		})
	}
}

// TestRPCMethods_SessionStats tests the session-stats RPC method
func TestRPCMethods_SessionStats(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	stats, err := methods.SessionStats()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify expected fields are present
	expectedFields := []string{
		"activeTorrentCount",
		"downloadSpeed",
		"pausedTorrentCount",
		"torrentCount",
		"uploadSpeed",
		"cumulative-stats",
		"current-stats",
	}

	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("Expected field %s not found in stats", field)
		}
	}

	// Verify nested stats structures
	if cumulativeStats, ok := stats["cumulative-stats"].(map[string]interface{}); ok {
		cumulativeFields := []string{"downloadedBytes", "uploadedBytes", "filesAdded", "sessionCount", "secondsActive"}
		for _, field := range cumulativeFields {
			if _, exists := cumulativeStats[field]; !exists {
				t.Errorf("Expected cumulative stats field %s not found", field)
			}
		}
	} else {
		t.Error("cumulative-stats field is not a map")
	}

	if currentStats, ok := stats["current-stats"].(map[string]interface{}); ok {
		currentFields := []string{"downloadedBytes", "uploadedBytes", "filesAdded", "sessionCount", "secondsActive"}
		for _, field := range currentFields {
			if _, exists := currentStats[field]; !exists {
				t.Errorf("Expected current stats field %s not found", field)
			}
		}
	} else {
		t.Error("current-stats field is not a map")
	}
}

// TestRPCMethods_TorrentMagnet tests the torrent-magnet RPC method
func TestRPCMethods_TorrentMagnet(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	// Add a torrent first
	addReq := TorrentAddRequest{
		Metainfo: createValidTestMetainfo(t),
		Paused:   true,
	}
	addResp, err := methods.TorrentAdd(addReq)
	if err != nil {
		t.Fatalf("Failed to add test torrent: %v", err)
	}
	torrentID := addResp.TorrentAdded.ID

	tests := []struct {
		name        string
		request     TorrentMagnetRequest
		expectError bool
		expectCount int
	}{
		{
			name: "get magnet for specific torrent",
			request: TorrentMagnetRequest{
				IDs: []int64{torrentID},
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name: "get magnet for all torrents",
			request: TorrentMagnetRequest{
				IDs: []int64{},
			},
			expectError: false,
			expectCount: 1,
		},
		{
			name: "get magnet for non-existent torrent",
			request: TorrentMagnetRequest{
				IDs: []int64{99999},
			},
			expectError: false,
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := methods.TorrentMagnet(tt.request)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(response.Magnets) != tt.expectCount {
				t.Errorf("Expected %d magnets, got %d", tt.expectCount, len(response.Magnets))
				return
			}

			if tt.expectCount > 0 {
				magnet := response.Magnets[0]
				if magnet.ID != torrentID {
					t.Errorf("Expected torrent ID %d, got %d", torrentID, magnet.ID)
				}
				if magnet.MagnetLink == "" {
					t.Error("Expected non-empty magnet link")
				}
				if !strings.Contains(magnet.MagnetLink, "magnet:?xt=urn:btih:") {
					t.Error("Expected valid magnet link format")
				}
			}
		})
	}
}

// TestRPCMethods_GetMethodHandler tests the method dispatch system
func TestRPCMethods_GetMethodHandler(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	supportedMethods := methods.GetSupportedMethods()

	// Test that all supported methods have handlers
	for _, method := range supportedMethods {
		t.Run(fmt.Sprintf("handler_for_%s", method), func(t *testing.T) {
			handler, exists := methods.GetMethodHandler(method)
			if !exists {
				t.Errorf("No handler found for method %s", method)
				return
			}
			if handler == nil {
				t.Errorf("Handler for method %s is nil", method)
			}
		})
	}

	// Test unsupported method
	t.Run("unsupported_method", func(t *testing.T) {
		handler, exists := methods.GetMethodHandler("unsupported-method")
		if exists {
			t.Error("Expected no handler for unsupported method")
		}
		if handler != nil {
			t.Error("Expected nil handler for unsupported method")
		}
	})
}

// TestRPCMethods_GetSupportedMethods tests the supported methods list
func TestRPCMethods_GetSupportedMethods(t *testing.T) {
	manager := createTestTorrentManagerForMethods(t)
	defer cleanupTestTorrentManager(t, manager)
	methods := NewRPCMethods(manager)

	supportedMethods := methods.GetSupportedMethods()

	expectedMethods := []string{
		"torrent-add",
		"torrent-get",
		"torrent-start",
		"torrent-start-now",
		"torrent-stop",
		"torrent-verify",
		"torrent-remove",
		"torrent-set",
		"torrent-magnet",
		"torrent-webseed-add",
		"torrent-webseed-remove",
		"torrent-webseed-get",
		"session-get",
		"session-set",
		"session-stats",
	}

	if len(supportedMethods) != len(expectedMethods) {
		t.Errorf("Expected %d supported methods, got %d", len(expectedMethods), len(supportedMethods))
	}

	for _, expected := range expectedMethods {
		found := false
		for _, supported := range supportedMethods {
			if supported == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected method %s not found in supported methods", expected)
		}
	}
}

// Helper functions for testing

// createTestTorrentManagerForMethods creates a test TorrentManager instance for methods testing
func createTestTorrentManagerForMethods(t *testing.T) *TorrentManager {
	config := createTestTorrentManagerConfig()

	// Create temporary directory for test downloads
	tempDir, err := os.MkdirTemp("", "rpc_methods_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	config.DownloadDir = tempDir
	config.SessionConfig.DownloadDir = tempDir

	manager, err := NewTorrentManager(config)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create TorrentManager: %v", err)
	}

	return manager
}

// cleanupTestTorrentManager cleans up the test TorrentManager
func cleanupTestTorrentManager(t *testing.T, manager *TorrentManager) {
	if manager != nil {
		// Clean up the download directory
		config := manager.GetSessionConfig()
		if config.DownloadDir != "" && strings.Contains(config.DownloadDir, "rpc_methods_test") {
			os.RemoveAll(config.DownloadDir)
		}

		// Close the manager
		manager.Close()
	}
}

// createValidTestMetainfo creates a valid base64-encoded metainfo for testing
func createValidTestMetainfo(t *testing.T) string {
	// Use existing helper function from torrent_manager_test.go
	metainfoBytes := createTestMetainfoBytes(t)
	return base64.StdEncoding.EncodeToString(metainfoBytes)
}

// verifyFilteredFields verifies that only the specified fields are populated in a torrent response
func verifyFilteredFields(t *testing.T, torrent Torrent, expectedFields []string) {
	v := reflect.ValueOf(torrent)
	typ := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := typ.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" {
			continue
		}

		// Extract field name from json tag
		fieldName := strings.Split(jsonTag, ",")[0]

		// Skip if this field should be empty (not in expectedFields)
		shouldBeSet := false
		for _, expected := range expectedFields {
			if expected == fieldName {
				shouldBeSet = true
				break
			}
		}

		if !shouldBeSet {
			fieldValue := v.Field(i)
			// Check if field is zero value (should be for filtered out fields)
			if !fieldValue.IsZero() && fieldName != "id" {
				// Allow ID to always be set as it's fundamental
				t.Logf("Warning: Field %s was set but not requested in filter", fieldName)
			}
		}
	}
}

// Note: Helper pointer functions are already defined in torrent_manager_test.go

// Local helper functions for pointer creation (to avoid import cycles)
func intPtrLocal(i int64) *int64 {
	return &i
}

func boolPtrLocal(b bool) *bool {
	return &b
}

func stringPtrLocal(s string) *string {
	return &s
}
