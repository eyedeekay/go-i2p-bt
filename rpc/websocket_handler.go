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

// Package rpc provides WebSocket real-time communication support for the BitTorrent client.
//
// The WebSocket implementation enables real-time updates for torrent statistics, session data,
// and system events. It supports secure authentication, subscription-based message delivery,
// and seamless integration with the existing RPC server infrastructure.
//
// # WebSocket API Overview
//
// The WebSocket endpoint (/ws) accepts the following message types:
//
//   - ping: Health check messages that receive pong responses
//   - subscribe: Subscribe to real-time updates (torrent-stats, session-stats)
//   - unsubscribe: Unsubscribe from update channels
//
// # Security Considerations
//
// WebSocket connections can be configured to require HTTP Basic authentication,
// matching the security model of the RPC server. All connections are validated
// and managed with proper cleanup to prevent resource leaks.
//
// # Usage Example
//
//	// Create WebSocket handler with authentication
//	config := WebSocketConfig{
//		RequireAuth:    true,
//		UpdateInterval: 2 * time.Second,
//	}
//	handler, err := NewWebSocketHandler(config, rpcServer)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer handler.Close()
//
//	// Integrate with HTTP multiplexer
//	mux := http.NewServeMux()
//	mux.Handle("/ws", handler)
//	mux.Handle("/", webHandler)
//
// # Client Integration
//
// JavaScript clients can connect and subscribe to updates:
//
//	const ws = new WebSocket('ws://localhost:9091/ws');
//	ws.send(JSON.stringify({
//		type: 'subscribe',
//		data: 'torrent-stats',
//		id: 'my-subscription'
//	}));
package rpc

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketConfig configures the WebSocket handler behavior and security settings.
//
// This struct provides comprehensive configuration options for WebSocket connections
// including timeouts, authentication requirements, update intervals, and message limits.
// Default values are automatically applied for unset fields to ensure proper operation.
type WebSocketConfig struct {
	// ReadTimeout is the maximum time to wait for a WebSocket read operation
	ReadTimeout time.Duration

	// WriteTimeout is the maximum time to wait for a WebSocket write operation
	WriteTimeout time.Duration

	// PingPeriod is the interval between ping messages to keep connections alive
	PingPeriod time.Duration

	// MaxMessageSize is the maximum size of WebSocket messages in bytes
	MaxMessageSize int64

	// RequireAuth determines if authentication is required for WebSocket connections
	RequireAuth bool

	// UpdateInterval is how often to broadcast torrent status updates
	UpdateInterval time.Duration
}

// WebSocketMessage represents a message sent or received over WebSocket connections.
//
// The message format supports different types of operations including ping/pong for health checks,
// subscribe/unsubscribe for real-time updates, and data delivery for torrent and session statistics.
// Each message includes an optional ID for request-response correlation.
//
// Example message types:
//   - ping: Health check request (receives pong response)
//   - subscribe: Start receiving updates for specified data type
//   - unsubscribe: Stop receiving updates for specified data type
//   - torrent-stats: Real-time torrent statistics data
//   - session-stats: Real-time session statistics data
type WebSocketMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
	ID   string      `json:"id,omitempty"`
}

// WebSocketHandler manages WebSocket connections for real-time torrent and session updates.
//
// This handler provides a secure, scalable WebSocket endpoint that integrates with the existing
// RPC server infrastructure. It supports authentication, subscription management, automatic
// cleanup, and graceful shutdown procedures.
//
// Key features:
//   - HTTP Basic authentication integration (optional)
//   - Real-time broadcasting of torrent and session statistics
//   - Subscription-based message delivery
//   - Automatic connection cleanup and resource management
//   - Configurable timeouts and message limits
//   - Thread-safe client management
//
// The handler maintains active WebSocket connections, manages client subscriptions,
// and periodically broadcasts updates to subscribed clients. All operations are
// thread-safe and designed for concurrent access.
type WebSocketHandler struct {
	config   WebSocketConfig
	server   *Server
	upgrader websocket.Upgrader

	// Connection management
	clientsMu sync.RWMutex
	clients   map[*websocket.Conn]*WebSocketClient

	// Broadcasting
	broadcast chan WebSocketMessage
	shutdown  chan struct{}
}

// WebSocketClient represents a connected WebSocket client
type WebSocketClient struct {
	conn            *websocket.Conn
	authenticated   bool
	subscriptions   map[string]bool // Track what the client is subscribed to
	subscriptionsMu sync.RWMutex    // Protects subscriptions map from race conditions
	send            chan WebSocketMessage
}

// NewWebSocketHandler creates a new WebSocket handler with the specified configuration.
//
// This function initializes a WebSocket handler that integrates with the provided RPC server.
// The configuration parameter controls authentication requirements, timeouts, update intervals,
// and other operational parameters. Default values are applied for unset configuration fields.
//
// Parameters:
//   - config: WebSocketConfig containing handler configuration options
//   - server: *Server instance for RPC integration and authentication
//
// Returns:
//   - *WebSocketHandler: Configured handler ready for HTTP multiplexer integration
//   - error: Configuration or initialization error
//
// Example:
//
//	config := WebSocketConfig{
//		RequireAuth:    true,
//		UpdateInterval: 2 * time.Second,
//		MaxMessageSize: 4096,
//	}
//	handler, err := NewWebSocketHandler(config, rpcServer)
//	if err != nil {
//		log.Fatal("Failed to create WebSocket handler:", err)
//	}
//	defer handler.Close()
func NewWebSocketHandler(config WebSocketConfig, server *Server) (*WebSocketHandler, error) {
	if server == nil {
		return nil, fmt.Errorf("server is required")
	}

	// Set defaults for configuration
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 60 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.PingPeriod == 0 {
		config.PingPeriod = 30 * time.Second
	}
	if config.MaxMessageSize == 0 {
		config.MaxMessageSize = 4096 // 4KB max message size
	}
	if config.UpdateInterval == 0 {
		config.UpdateInterval = 2 * time.Second // Update every 2 seconds
	}

	handler := &WebSocketHandler{
		config: config,
		server: server,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins for now - can be made configurable later
				return true
			},
		},
		clients:   make(map[*websocket.Conn]*WebSocketClient),
		broadcast: make(chan WebSocketMessage, 256),
		shutdown:  make(chan struct{}),
	}

	// Start the broadcasting goroutine
	go handler.handleBroadcast()

	// Start periodic updates
	go handler.periodicUpdates()

	return handler, nil
}

// ServeHTTP handles WebSocket upgrade requests and manages the connection lifecycle.
//
// This method implements the http.Handler interface and is responsible for:
//   - Validating authentication credentials (if required)
//   - Upgrading HTTP connections to WebSocket protocol
//   - Managing client connections and cleanup
//   - Starting client message processing goroutines
//
// The method performs security checks, upgrades the connection, and delegates
// ongoing communication to the handleClient method in a separate goroutine.
// Authentication is validated using the same credentials as the RPC server.
func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check authentication if required
	if h.config.RequireAuth && h.server != nil {
		if !h.server.checkAuth(r) {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}
	}

	// Upgrade the HTTP connection to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Create client and register it
	client := &WebSocketClient{
		conn:          conn,
		authenticated: !h.config.RequireAuth, // If no auth required, consider authenticated
		subscriptions: make(map[string]bool),
		send:          make(chan WebSocketMessage, 256),
	}

	h.clientsMu.Lock()
	h.clients[conn] = client
	h.clientsMu.Unlock()

	// Handle the client connection
	go h.handleClient(client)
	go h.writeToClient(client)
}

// handleClient processes messages from a WebSocket client
func (h *WebSocketHandler) handleClient(client *WebSocketClient) {
	defer func() {
		h.removeClient(client.conn)
		client.conn.Close()
	}()

	// Configure connection limits
	client.conn.SetReadLimit(h.config.MaxMessageSize)
	client.conn.SetReadDeadline(time.Now().Add(h.config.ReadTimeout))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(h.config.ReadTimeout))
		return nil
	})

	for {
		var msg WebSocketMessage
		err := client.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Process the message based on its type
		h.processClientMessage(client, msg)
	}
}

// processClientMessage handles incoming messages from clients
func (h *WebSocketHandler) processClientMessage(client *WebSocketClient, msg WebSocketMessage) {
	switch msg.Type {
	case "subscribe":
		h.handleSubscribeMessage(client, msg)
	case "unsubscribe":
		h.handleUnsubscribeMessage(client, msg)
	case "ping":
		h.handlePingMessage(client, msg)
	default:
		h.handleUnknownMessage(msg)
	}
}

// handleSubscribeMessage processes subscription requests from WebSocket clients.
// It validates the subscription data, updates the client's subscription state,
// and sends a confirmation response back to the client.
func (h *WebSocketHandler) handleSubscribeMessage(client *WebSocketClient, msg WebSocketMessage) {
	subscription, ok := msg.Data.(string)
	if !ok {
		return
	}

	client.subscriptionsMu.Lock()
	client.subscriptions[subscription] = true
	client.subscriptionsMu.Unlock()

	h.sendSubscriptionResponse(client, "subscribed", subscription, msg.ID)
}

// handleUnsubscribeMessage processes unsubscription requests from WebSocket clients.
// It validates the subscription data, removes it from the client's subscription state,
// and sends a confirmation response back to the client.
func (h *WebSocketHandler) handleUnsubscribeMessage(client *WebSocketClient, msg WebSocketMessage) {
	subscription, ok := msg.Data.(string)
	if !ok {
		return
	}

	client.subscriptionsMu.Lock()
	delete(client.subscriptions, subscription)
	client.subscriptionsMu.Unlock()

	h.sendSubscriptionResponse(client, "unsubscribed", subscription, msg.ID)
}

// handlePingMessage processes ping requests from WebSocket clients.
// It immediately responds with a pong message containing the original message ID.
func (h *WebSocketHandler) handlePingMessage(client *WebSocketClient, msg WebSocketMessage) {
	response := WebSocketMessage{
		Type: "pong",
		ID:   msg.ID,
	}
	h.sendResponseToClient(client, response)
}

// handleUnknownMessage processes unknown message types by logging them.
// This provides debugging information for unsupported message types.
func (h *WebSocketHandler) handleUnknownMessage(msg WebSocketMessage) {
	log.Printf("Unknown WebSocket message type: %s", msg.Type)
}

// sendSubscriptionResponse sends a subscription-related response to the WebSocket client.
// It creates and sends a response message with the specified type, data, and message ID.
func (h *WebSocketHandler) sendSubscriptionResponse(client *WebSocketClient, responseType, subscription string, msgID string) {
	response := WebSocketMessage{
		Type: responseType,
		Data: subscription,
		ID:   msgID,
	}
	h.sendResponseToClient(client, response)
}

// sendResponseToClient attempts to send a response message to the WebSocket client.
// It uses a non-blocking send to avoid hanging if the client's send buffer is full.
func (h *WebSocketHandler) sendResponseToClient(client *WebSocketClient, response WebSocketMessage) {
	select {
	case client.send <- response:
	default:
		// Client send buffer is full, skip
	}
}

// writeToClient handles sending messages to a WebSocket client
func (h *WebSocketHandler) writeToClient(client *WebSocketClient) {
	ticker := time.NewTicker(h.config.PingPeriod)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			client.conn.SetWriteDeadline(time.Now().Add(h.config.WriteTimeout))
			if !ok {
				// Channel was closed
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := client.conn.WriteJSON(message); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}

		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(h.config.WriteTimeout))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// checkClientSubscription determines if a client is subscribed to the given message type.
// System messages are always delivered regardless of subscription status.
func (h *WebSocketHandler) checkClientSubscription(client *WebSocketClient, messageType string) bool {
	client.subscriptionsMu.RLock()
	isSubscribed := client.subscriptions[messageType] || messageType == "system"
	client.subscriptionsMu.RUnlock()
	return isSubscribed
}

// sendMessageToClient attempts to send a message to the specified client.
// Returns true if the message was sent successfully, false if the client should be cleaned up.
func (h *WebSocketHandler) sendMessageToClient(client *WebSocketClient, message WebSocketMessage) bool {
	select {
	case client.send <- message:
		return true
	default:
		// Client send buffer is full, client needs cleanup
		return false
	}
}

// cleanupFailedClient safely removes a client that failed to receive messages.
// This function closes the client's send channel and removes it from the clients map.
func (h *WebSocketHandler) cleanupFailedClient(client *WebSocketClient) {
	close(client.send)
	delete(h.clients, client.conn)
}

// handleBroadcast processes broadcast messages and sends them to subscribed clients
func (h *WebSocketHandler) handleBroadcast() {
	for {
		select {
		case message := <-h.broadcast:
			h.processBroadcastMessage(message)
		case <-h.shutdown:
			return
		}
	}
}

// processBroadcastMessage distributes a broadcast message to all subscribed clients
func (h *WebSocketHandler) processBroadcastMessage(message WebSocketMessage) {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()

	for _, client := range h.clients {
		h.handleClientMessage(client, message)
	}
}

// handleClientMessage processes a message for a specific client, checking subscription and handling delivery
func (h *WebSocketHandler) handleClientMessage(client *WebSocketClient, message WebSocketMessage) {
	if h.checkClientSubscription(client, message.Type) {
		if !h.sendMessageToClient(client, message) {
			h.cleanupFailedClient(client)
		}
	}
}

// periodicUpdates sends regular updates to connected clients
func (h *WebSocketHandler) periodicUpdates() {
	ticker := time.NewTicker(h.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Get torrent stats and broadcast to subscribed clients
			h.broadcastTorrentStats()
			h.broadcastSessionStats()

		case <-h.shutdown:
			return
		}
	}
}

// broadcastTorrentStats sends torrent status updates to subscribed clients
func (h *WebSocketHandler) broadcastTorrentStats() {
	if h.server == nil || h.server.config.TorrentManager == nil {
		return
	}

	// Get all torrents from the manager
	torrents := h.server.config.TorrentManager.GetAllTorrents()

	// Convert to a format suitable for JSON
	torrentData := make([]map[string]interface{}, 0, len(torrents))
	for _, torrent := range torrents {
		// Get torrent name from MetaInfo if available
		var name string
		if torrent.MetaInfo != nil {
			if info, err := torrent.MetaInfo.Info(); err == nil {
				name = info.Name
			} else {
				name = fmt.Sprintf("Torrent %d", torrent.ID)
			}
		} else {
			name = fmt.Sprintf("Torrent %d", torrent.ID)
		}

		torrentData = append(torrentData, map[string]interface{}{
			"id":             torrent.ID,
			"name":           name,
			"status":         torrent.Status,
			"percentDone":    torrent.PercentDone,
			"rateDownload":   torrent.DownloadRate,
			"rateUpload":     torrent.UploadRate,
			"eta":            torrent.ETA,
			"peersConnected": torrent.PeerConnectedCount,
			"downloaded":     torrent.Downloaded,
			"uploaded":       torrent.Uploaded,
			"left":           torrent.Left,
		})
	}

	msg := WebSocketMessage{
		Type: "torrent-stats",
		Data: torrentData,
	}

	select {
	case h.broadcast <- msg:
	default:
		// Broadcast buffer is full, skip this update
	}
}

// broadcastSessionStats sends session statistics to subscribed clients
func (h *WebSocketHandler) broadcastSessionStats() {
	if h.server == nil {
		return
	}

	stats := h.server.GetStats()

	msg := WebSocketMessage{
		Type: "session-stats",
		Data: stats,
	}

	select {
	case h.broadcast <- msg:
	default:
		// Broadcast buffer is full, skip this update
	}
}

// removeClient safely removes a client from the handler
func (h *WebSocketHandler) removeClient(conn *websocket.Conn) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	if client, exists := h.clients[conn]; exists {
		// Close the channel safely (check if it's already closed)
		select {
		case <-client.send:
			// Channel is already closed
		default:
			close(client.send)
		}
		delete(h.clients, conn)
	}
}

// Close shuts down the WebSocket handler gracefully
func (h *WebSocketHandler) Close() error {
	close(h.shutdown)

	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	// Close all client connections
	for conn, client := range h.clients {
		close(client.send)
		conn.Close()
	}

	return nil
}

// GetConnectedClients returns the number of connected WebSocket clients
func (h *WebSocketHandler) GetConnectedClients() int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clients)
}

// BroadcastMessage sends a message to all connected clients subscribed to the message type
func (h *WebSocketHandler) BroadcastMessage(msgType string, data interface{}) {
	msg := WebSocketMessage{
		Type: msgType,
		Data: data,
	}

	select {
	case h.broadcast <- msg:
	default:
		// Broadcast buffer is full, message dropped
	}
}

// Utility functions for creating WebSocket handlers

// NewDefaultWebSocketHandler creates a WebSocket handler with sensible defaults for development.
//
// This convenience function creates a WebSocket handler with default configuration
// suitable for development and testing environments. Authentication is disabled
// and update intervals are set to reasonable values.
//
// Default configuration:
//   - RequireAuth: false (no authentication required)
//   - UpdateInterval: 5 seconds
//   - ReadTimeout: 60 seconds
//   - WriteTimeout: 10 seconds
//   - PingPeriod: 54 seconds
//   - MaxMessageSize: 512 bytes
//
// For production environments, use NewSecureWebSocketHandler or configure
// authentication manually with NewWebSocketHandler.
func NewDefaultWebSocketHandler(server *Server) (*WebSocketHandler, error) {
	config := WebSocketConfig{
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   10 * time.Second,
		PingPeriod:     30 * time.Second,
		MaxMessageSize: 4096,
		RequireAuth:    false, // Allow public WebSocket access by default
		UpdateInterval: 2 * time.Second,
	}

	return NewWebSocketHandler(config, server)
}

// NewSecureWebSocketHandler creates a WebSocket handler with authentication enabled.
//
// This convenience function creates a WebSocket handler with security-focused defaults
// suitable for production environments. HTTP Basic authentication is required and
// matches the credentials configured for the RPC server.
//
// Secure configuration:
//   - RequireAuth: true (HTTP Basic authentication required)
//   - UpdateInterval: 5 seconds
//   - ReadTimeout: 60 seconds
//   - WriteTimeout: 10 seconds
//   - PingPeriod: 54 seconds
//   - MaxMessageSize: 512 bytes
//
// Clients must provide valid credentials in the WebSocket handshake using
// HTTP Basic authentication headers.
func NewSecureWebSocketHandler(server *Server) (*WebSocketHandler, error) {
	config := WebSocketConfig{
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   10 * time.Second,
		PingPeriod:     30 * time.Second,
		MaxMessageSize: 4096,
		RequireAuth:    true, // Require authentication
		UpdateInterval: 2 * time.Second,
	}

	return NewWebSocketHandler(config, server)
}
