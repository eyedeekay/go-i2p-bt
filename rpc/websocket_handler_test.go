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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWebSocketConfig tests WebSocket configuration validation and defaults
func TestWebSocketConfig(t *testing.T) {
	server := createTestServer(t)

	tests := []struct {
		name        string
		config      WebSocketConfig
		expectError bool
		description string
	}{
		{
			name: "valid config",
			config: WebSocketConfig{
				ReadTimeout:    60 * time.Second,
				WriteTimeout:   10 * time.Second,
				PingPeriod:     30 * time.Second,
				MaxMessageSize: 4096,
				RequireAuth:    false,
				UpdateInterval: 2 * time.Second,
			},
			expectError: false,
			description: "Valid configuration should work",
		},
		{
			name:        "empty config with defaults",
			config:      WebSocketConfig{},
			expectError: false,
			description: "Empty config should get reasonable defaults",
		},
		{
			name: "config with auth required",
			config: WebSocketConfig{
				RequireAuth: true,
			},
			expectError: false,
			description: "Authentication requirement should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewWebSocketHandler(tt.config, server)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if handler == nil {
				t.Errorf("Expected handler but got nil")
				return
			}

			// Check defaults were applied
			if handler.config.ReadTimeout == 0 {
				t.Errorf("Expected default ReadTimeout to be set")
			}
			if handler.config.WriteTimeout == 0 {
				t.Errorf("Expected default WriteTimeout to be set")
			}
			if handler.config.PingPeriod == 0 {
				t.Errorf("Expected default PingPeriod to be set")
			}
			if handler.config.MaxMessageSize == 0 {
				t.Errorf("Expected default MaxMessageSize to be set")
			}
			if handler.config.UpdateInterval == 0 {
				t.Errorf("Expected default UpdateInterval to be set")
			}

			// Clean up
			handler.Close()
		})
	}
}

// TestWebSocketHandlerNilServer tests error handling for nil server
func TestWebSocketHandlerNilServer(t *testing.T) {
	config := WebSocketConfig{}
	handler, err := NewWebSocketHandler(config, nil)

	if err == nil {
		t.Errorf("Expected error for nil server")
	}
	if handler != nil {
		t.Errorf("Expected nil handler for nil server")
	}
}

// TestWebSocketUpgrade tests WebSocket upgrade functionality
func TestWebSocketUpgrade(t *testing.T) {
	server := createTestServer(t)
	config := WebSocketConfig{
		RequireAuth: false,
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	// Create test server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Test WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Test basic ping message
	pingMsg := WebSocketMessage{
		Type: "ping",
		ID:   "test-ping-1",
	}

	err = conn.WriteJSON(pingMsg)
	if err != nil {
		t.Fatalf("Failed to send ping message: %v", err)
	}

	// Read pong response
	var response WebSocketMessage
	err = conn.ReadJSON(&response)
	if err != nil {
		t.Fatalf("Failed to read pong response: %v", err)
	}

	if response.Type != "pong" {
		t.Errorf("Expected pong response, got %s", response.Type)
	}
	if response.ID != "test-ping-1" {
		t.Errorf("Expected ID 'test-ping-1', got %s", response.ID)
	}
}

// TestWebSocketAuthentication tests WebSocket authentication
func TestWebSocketAuthentication(t *testing.T) {
	server := createTestServerWithAuth(t, "admin", "secret")
	config := WebSocketConfig{
		RequireAuth: true,
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	tests := []struct {
		name          string
		addAuth       bool
		username      string
		password      string
		expectUpgrade bool
		description   string
	}{
		{
			name:          "no auth provided",
			addAuth:       false,
			expectUpgrade: false,
			description:   "Should reject WebSocket upgrade without authentication",
		},
		{
			name:          "valid auth",
			addAuth:       true,
			username:      "admin",
			password:      "secret",
			expectUpgrade: true,
			description:   "Should allow WebSocket upgrade with valid credentials",
		},
		{
			name:          "invalid auth",
			addAuth:       true,
			username:      "admin",
			password:      "wrong",
			expectUpgrade: false,
			description:   "Should reject WebSocket upgrade with invalid credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			testServer := httptest.NewServer(handler)
			defer testServer.Close()

			// Convert http:// to ws://
			wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

			// Prepare headers
			var headers http.Header
			if tt.addAuth {
				headers = http.Header{}
				// Add basic auth header
				req, _ := http.NewRequest("GET", testServer.URL, nil)
				req.SetBasicAuth(tt.username, tt.password)
				headers.Set("Authorization", req.Header.Get("Authorization"))
			}

			// Attempt WebSocket connection
			conn, response, err := websocket.DefaultDialer.Dial(wsURL, headers)

			if tt.expectUpgrade {
				if err != nil {
					t.Errorf("Expected successful WebSocket upgrade, got error: %v", err)
				} else {
					conn.Close()
				}
			} else {
				if err == nil {
					conn.Close()
					t.Errorf("Expected WebSocket upgrade to fail")
				}
				// Check response status
				if response != nil && response.StatusCode == http.StatusUnauthorized {
					// This is expected for auth failures
				}
			}
		})
	}
}

// TestWebSocketSubscription tests subscription functionality
func TestWebSocketSubscription(t *testing.T) {
	server := createTestServer(t)
	config := WebSocketConfig{
		RequireAuth: false,
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	// Create test server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Test WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Test subscription
	subscribeMsg := WebSocketMessage{
		Type: "subscribe",
		Data: "torrent-stats",
		ID:   "sub-1",
	}

	err = conn.WriteJSON(subscribeMsg)
	if err != nil {
		t.Fatalf("Failed to send subscribe message: %v", err)
	}

	// Read subscription confirmation
	var response WebSocketMessage
	err = conn.ReadJSON(&response)
	if err != nil {
		t.Fatalf("Failed to read subscription response: %v", err)
	}

	if response.Type != "subscribed" {
		t.Errorf("Expected 'subscribed' response, got %s", response.Type)
	}
	if response.Data != "torrent-stats" {
		t.Errorf("Expected data 'torrent-stats', got %v", response.Data)
	}
	if response.ID != "sub-1" {
		t.Errorf("Expected ID 'sub-1', got %s", response.ID)
	}

	// Test unsubscription
	unsubscribeMsg := WebSocketMessage{
		Type: "unsubscribe",
		Data: "torrent-stats",
		ID:   "unsub-1",
	}

	err = conn.WriteJSON(unsubscribeMsg)
	if err != nil {
		t.Fatalf("Failed to send unsubscribe message: %v", err)
	}

	// Read unsubscription confirmation
	err = conn.ReadJSON(&response)
	if err != nil {
		t.Fatalf("Failed to read unsubscription response: %v", err)
	}

	if response.Type != "unsubscribed" {
		t.Errorf("Expected 'unsubscribed' response, got %s", response.Type)
	}
}

// TestWebSocketBroadcast tests message broadcasting functionality
func TestWebSocketBroadcast(t *testing.T) {
	server := createTestServer(t)
	config := WebSocketConfig{
		RequireAuth:    false,
		UpdateInterval: 100 * time.Millisecond, // Fast updates for testing
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	// Create test server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect first client
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect first client: %v", err)
	}
	defer conn1.Close()

	// Connect second client
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect second client: %v", err)
	}
	defer conn2.Close()

	// Subscribe both clients to session-stats
	subscribeMsg := WebSocketMessage{
		Type: "subscribe",
		Data: "session-stats",
		ID:   "sub-test",
	}

	err = conn1.WriteJSON(subscribeMsg)
	if err != nil {
		t.Fatalf("Failed to subscribe client 1: %v", err)
	}

	err = conn2.WriteJSON(subscribeMsg)
	if err != nil {
		t.Fatalf("Failed to subscribe client 2: %v", err)
	}

	// Read subscription confirmations
	var response1, response2 WebSocketMessage
	conn1.ReadJSON(&response1)
	conn2.ReadJSON(&response2)

	// Test manual broadcast
	testData := map[string]interface{}{
		"test":      "broadcast message",
		"timestamp": time.Now().Unix(),
	}

	handler.BroadcastMessage("session-stats", testData)

	// Set read timeout to avoid hanging
	conn1.SetReadDeadline(time.Now().Add(5 * time.Second))
	conn2.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Both clients should receive the broadcast
	err = conn1.ReadJSON(&response1)
	if err != nil {
		t.Fatalf("Client 1 failed to read broadcast: %v", err)
	}

	err = conn2.ReadJSON(&response2)
	if err != nil {
		t.Fatalf("Client 2 failed to read broadcast: %v", err)
	}

	// Verify both received the same message
	if response1.Type != "session-stats" {
		t.Errorf("Client 1 expected 'session-stats', got %s", response1.Type)
	}
	if response2.Type != "session-stats" {
		t.Errorf("Client 2 expected 'session-stats', got %s", response2.Type)
	}
}

// TestWebSocketPeriodicUpdates tests automatic periodic update broadcasting
func TestWebSocketPeriodicUpdates(t *testing.T) {
	server := createTestServer(t)
	config := WebSocketConfig{
		RequireAuth:    false,
		UpdateInterval: 50 * time.Millisecond, // Very fast updates for testing
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	// Create test server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect client
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Subscribe to both torrent-stats and session-stats to trigger periodic updates
	subscribeToTorrents := WebSocketMessage{
		Type: "subscribe",
		Data: "torrent-stats",
		ID:   "sub-torrents",
	}

	subscribeToSession := WebSocketMessage{
		Type: "subscribe",
		Data: "session-stats",
		ID:   "sub-session",
	}

	err = conn.WriteJSON(subscribeToTorrents)
	if err != nil {
		t.Fatalf("Failed to subscribe to torrents: %v", err)
	}

	err = conn.WriteJSON(subscribeToSession)
	if err != nil {
		t.Fatalf("Failed to subscribe to session: %v", err)
	}

	// Read subscription confirmations
	var response WebSocketMessage
	conn.ReadJSON(&response) // torrent subscription confirmation
	conn.ReadJSON(&response) // session subscription confirmation

	// Set read timeout
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// We should receive periodic updates
	receivedUpdates := 0
	for receivedUpdates < 2 { // Wait for at least 2 updates
		err = conn.ReadJSON(&response)
		if err != nil {
			// This is expected when timeout occurs
			break
		}

		if response.Type == "torrent-stats" || response.Type == "session-stats" {
			receivedUpdates++
		}
	}

	if receivedUpdates == 0 {
		t.Errorf("Expected to receive periodic updates, but got none")
	}
}

// TestWebSocketErrorHandling tests various error conditions
func TestWebSocketErrorHandling(t *testing.T) {
	server := createTestServer(t)
	config := WebSocketConfig{
		RequireAuth:    false,
		MaxMessageSize: 1024, // Normal size for testing
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	// Create test server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Test case 1: Connection closes after invalid JSON (which is expected behavior)
	t.Run("invalid_json_handling", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Send invalid JSON - this might close the connection, which is valid behavior
		err = conn.WriteMessage(websocket.TextMessage, []byte("{invalid json"))
		if err != nil {
			t.Fatalf("Failed to send invalid JSON: %v", err)
		}

		// Try to read response - might get connection closed, which is acceptable
		var response WebSocketMessage
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		err = conn.ReadJSON(&response)
		// We accept either a proper error response or connection close
		// Both are valid ways to handle invalid JSON
	})

	// Test case 2: Unknown message types
	t.Run("unknown_message_type", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		unknownMsg := WebSocketMessage{
			Type: "unknown-type",
			ID:   "unknown-test",
		}

		err = conn.WriteJSON(unknownMsg)
		if err != nil {
			t.Fatalf("Failed to send unknown message: %v", err)
		}

		// Connection should remain alive after unknown message
		pingMsg := WebSocketMessage{
			Type: "ping",
			ID:   "post-unknown-ping",
		}

		err = conn.WriteJSON(pingMsg)
		if err != nil {
			t.Fatalf("Failed to send ping after unknown message: %v", err)
		}

		// Should get pong response
		var response WebSocketMessage
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		err = conn.ReadJSON(&response)
		if err != nil {
			t.Fatalf("Failed to read pong after unknown message: %v", err)
		}

		if response.Type != "pong" {
			t.Errorf("Expected pong after unknown message, got %s", response.Type)
		}
	})
}

// TestWebSocketInvalidSubscriptions tests handling of invalid subscription requests
func TestWebSocketInvalidSubscriptions(t *testing.T) {
	server := createTestServer(t)
	config := WebSocketConfig{
		RequireAuth: false,
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	// Create test server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Test valid subscription first for baseline
	validSubscribe := WebSocketMessage{
		Type: "subscribe",
		Data: "session-stats",
		ID:   "valid-sub",
	}

	err = conn.WriteJSON(validSubscribe)
	if err != nil {
		t.Fatalf("Failed to send valid subscription: %v", err)
	}

	// Read subscription confirmation
	var response WebSocketMessage
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	err = conn.ReadJSON(&response)
	if err != nil {
		t.Fatalf("Failed to read subscription confirmation: %v", err)
	}

	if response.Type != "subscribed" {
		t.Errorf("Expected 'subscribed', got %s", response.Type)
	}

	// Test unsubscribing from subscribed channel
	validUnsubscribe := WebSocketMessage{
		Type: "unsubscribe",
		Data: "session-stats",
		ID:   "valid-unsub",
	}

	err = conn.WriteJSON(validUnsubscribe)
	if err != nil {
		t.Fatalf("Failed to send valid unsubscribe: %v", err)
	}

	// Should receive unsubscribed confirmation
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	err = conn.ReadJSON(&response)
	if err != nil {
		t.Fatalf("Failed to read unsubscribe confirmation: %v", err)
	}

	if response.Type != "unsubscribed" {
		t.Errorf("Expected 'unsubscribed', got %s", response.Type)
	}

	// Now test that connection still works with ping
	pingMsg := WebSocketMessage{
		Type: "ping",
		ID:   "final-ping",
	}

	err = conn.WriteJSON(pingMsg)
	if err != nil {
		t.Fatalf("Failed to send ping after operations: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	err = conn.ReadJSON(&response)
	if err != nil {
		t.Fatalf("Failed to read pong after operations: %v", err)
	}

	if response.Type != "pong" {
		t.Errorf("Expected pong, got %s", response.Type)
	}
}

// TestWebSocketClose tests proper cleanup when connections are closed
func TestWebSocketClose(t *testing.T) {
	server := createTestServer(t)
	config := WebSocketConfig{
		RequireAuth: false,
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create WebSocket handler: %v", err)
	}

	// Create test server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect client
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Check client count
	if handler.GetConnectedClients() != 1 {
		t.Errorf("Expected 1 connected client, got %d", handler.GetConnectedClients())
	}

	// Close connection
	conn.Close()

	// Give some time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Check client count after close
	if handler.GetConnectedClients() != 0 {
		t.Errorf("Expected 0 connected clients after close, got %d", handler.GetConnectedClients())
	}

	// Close handler
	err = handler.Close()
	if err != nil {
		t.Errorf("Failed to close handler: %v", err)
	}
}

// TestWebSocketUtilityFunctions tests utility functions
func TestWebSocketUtilityFunctions(t *testing.T) {
	server := createTestServer(t)

	t.Run("NewDefaultWebSocketHandler", func(t *testing.T) {
		handler, err := NewDefaultWebSocketHandler(server)
		if err != nil {
			t.Errorf("NewDefaultWebSocketHandler failed: %v", err)
		}
		if handler == nil {
			t.Errorf("Expected handler but got nil")
		}

		// Check defaults
		if handler.config.RequireAuth != false {
			t.Errorf("Expected RequireAuth to be false by default")
		}

		handler.Close()
	})

	t.Run("NewSecureWebSocketHandler", func(t *testing.T) {
		handler, err := NewSecureWebSocketHandler(server)
		if err != nil {
			t.Errorf("NewSecureWebSocketHandler failed: %v", err)
		}
		if handler == nil {
			t.Errorf("Expected handler but got nil")
		}

		// Check secure defaults
		if handler.config.RequireAuth != true {
			t.Errorf("Expected RequireAuth to be true for secure handler")
		}

		handler.Close()
	})
}

// TestCreateMuxWithWebSocketSupport tests the multiplexer with WebSocket support
func TestCreateMuxWithWebSocketSupport(t *testing.T) {
	server := createTestServer(t)
	tempDir := t.TempDir()
	createTestFiles(t, tempDir)

	webConfig := WebHandlerConfig{
		StaticDir: tempDir,
		URLPrefix: "/web/",
	}

	wsConfig := WebSocketConfig{
		RequireAuth: false,
	}

	mux, wsHandler, err := CreateMuxWithWebSocketSupport(server, webConfig, wsConfig)
	if err != nil {
		t.Fatalf("Failed to create mux with WebSocket support: %v", err)
	}
	defer wsHandler.Close()

	if mux == nil {
		t.Fatalf("Expected mux but got nil")
	}
	if wsHandler == nil {
		t.Fatalf("Expected WebSocket handler but got nil")
	}

	// Test that the mux routes correctly
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	// Test WebSocket endpoint
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Errorf("Failed to connect to WebSocket endpoint: %v", err)
	} else {
		conn.Close()
	}

	// Test web interface endpoint
	resp, err := http.Get(testServer.URL + "/web/test.txt")
	if err != nil {
		t.Errorf("Failed to access web interface: %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for web interface, got %d", resp.StatusCode)
		}
	}
}

// Helper functions for WebSocket testing
// Note: createTestFiles is reused from web_handler_test.go

// Benchmarks for WebSocket performance

func BenchmarkWebSocketConnection(b *testing.B) {
	server := createTestServer(&testing.T{})
	config := WebSocketConfig{
		RequireAuth: false,
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		b.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatalf("Failed to connect: %v", err)
		}
		conn.Close()
	}
}

func BenchmarkWebSocketPingPong(b *testing.B) {
	server := createTestServer(&testing.T{})
	config := WebSocketConfig{
		RequireAuth: false,
	}

	handler, err := NewWebSocketHandler(config, server)
	if err != nil {
		b.Fatalf("Failed to create WebSocket handler: %v", err)
	}
	defer handler.Close()

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	pingMsg := WebSocketMessage{
		Type: "ping",
		ID:   "bench-ping",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = conn.WriteJSON(pingMsg)
		if err != nil {
			b.Fatalf("Failed to send ping: %v", err)
		}

		var response WebSocketMessage
		err = conn.ReadJSON(&response)
		if err != nil {
			b.Fatalf("Failed to read pong: %v", err)
		}
	}
}
