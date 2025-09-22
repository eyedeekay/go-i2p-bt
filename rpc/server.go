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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ServerConfig configures the RPC server
type ServerConfig struct {
	// TorrentManager for handling torrent operations
	TorrentManager *TorrentManager

	// Authentication settings
	Username string
	Password string

	// Session token settings
	SessionTokenHeader string // Default: "X-Transmission-Session-Id"

	// CORS settings
	AllowOrigin      string
	AllowCredentials bool

	// Logging
	ErrorLog  func(format string, args ...interface{})
	AccessLog func(format string, args ...interface{})

	// HTTP timeouts
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// Request size limits
	MaxRequestSize int64
}

// Server implements a Transmission RPC compatible HTTP server
type Server struct {
	config     ServerConfig
	methods    *RPCMethods
	sessionKey string

	// Session management
	sessionMu   sync.RWMutex
	sessionID   string
	sessionTime time.Time

	// Statistics
	requestCount int64
	errorCount   int64
}

// NewServer creates a new RPC server with the given configuration
func NewServer(config ServerConfig) (*Server, error) {
	if config.TorrentManager == nil {
		return nil, fmt.Errorf("TorrentManager is required")
	}

	if config.ErrorLog == nil {
		config.ErrorLog = log.Printf
	}

	if config.AccessLog == nil {
		config.AccessLog = func(format string, args ...interface{}) {
			// Default: no access logging
		}
	}

	if config.SessionTokenHeader == "" {
		config.SessionTokenHeader = "X-Transmission-Session-Id"
	}

	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30 * time.Second
	}

	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}

	if config.MaxRequestSize == 0 {
		config.MaxRequestSize = 10 * 1024 * 1024 // 10MB
	}

	server := &Server{
		config:      config,
		methods:     NewRPCMethods(config.TorrentManager),
		sessionID:   generateSessionID(),
		sessionTime: time.Now(),
	}

	return server, nil
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Log the request
	s.config.AccessLog("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)

	// Apply CORS headers
	s.applyCORSHeaders(w, r)

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Validate HTTP request requirements
	if !s.validateHTTPRequest(w, r) {
		return
	}

	// Parse the JSON-RPC request
	rpcReq, ok := s.parseRPCRequest(w, r)
	if !ok {
		return
	}

	// Validate authentication and session tokens
	if !s.validateRPCAuthentication(w, r, rpcReq) {
		return
	}

	// Process the RPC request
	s.processRPCRequest(w, r, rpcReq)
}

// validateHTTPRequest validates the HTTP request method, content type, and size.
// Returns false if validation fails and an error response has been sent.
func (s *Server) validateHTTPRequest(w http.ResponseWriter, r *http.Request) bool {
	// Only allow POST requests for RPC
	if r.Method != "POST" {
		s.sendError(w, http.StatusMethodNotAllowed, ErrCodeInvalidRequest, "Only POST method allowed")
		return false
	}

	// Check content type
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		s.sendError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "Content-Type must be application/json")
		return false
	}

	// Check request size
	if r.ContentLength > s.config.MaxRequestSize {
		s.sendError(w, http.StatusRequestEntityTooLarge, ErrCodeInvalidRequest, "Request too large")
		return false
	}

	return true
}

// parseRPCRequest reads and parses the JSON-RPC request from the HTTP request body.
// Returns the parsed request and true on success, or nil and false if parsing fails.
func (s *Server) parseRPCRequest(w http.ResponseWriter, r *http.Request) (*Request, bool) {
	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, s.config.MaxRequestSize))
	if err != nil {
		s.sendError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "Failed to read request body")
		return nil, false
	}

	// Parse JSON-RPC request
	var rpcReq Request
	if err := json.Unmarshal(body, &rpcReq); err != nil {
		s.sendError(w, http.StatusBadRequest, ErrCodeParseError, "Invalid JSON")
		return nil, false
	}

	// Validate JSON-RPC version
	if rpcReq.Version != "2.0" {
		s.sendError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "Invalid JSON-RPC version")
		return nil, false
	}

	return &rpcReq, true
}

// validateRPCAuthentication checks HTTP authentication and session tokens for the RPC request.
// Returns false if validation fails and an error response has been sent.
func (s *Server) validateRPCAuthentication(w http.ResponseWriter, r *http.Request, rpcReq *Request) bool {
	// Check authentication
	if !s.checkAuth(r) {
		s.sendError(w, http.StatusUnauthorized, ErrCodeInternalError, "Authentication required")
		return false
	}

	// Check session token for state-changing operations
	if s.isStateChangingMethod(rpcReq.Method) {
		if !s.checkSessionToken(r) {
			// Send session token error
			w.Header().Set(s.config.SessionTokenHeader, s.sessionID)
			s.sendError(w, http.StatusConflict, ErrCodeInternalError, "Session token required")
			return false
		}
	}

	return true
}

// processRPCRequest handles the actual RPC method execution
func (s *Server) processRPCRequest(w http.ResponseWriter, r *http.Request, rpcReq *Request) {
	// Create response
	response := Response{
		ID:      rpcReq.ID,
		Version: "2.0",
	}

	// Get method handler
	handler, exists := s.methods.GetMethodHandler(rpcReq.Method)
	if !exists {
		response.Error = &RPCError{
			Code:    ErrCodeMethodNotFound,
			Message: fmt.Sprintf("Method '%s' not found", rpcReq.Method),
		}
		s.sendJSONResponse(w, http.StatusOK, response)
		return
	}

	// Execute method
	var params json.RawMessage
	if rpcReq.Params != nil {
		paramsBytes, err := json.Marshal(rpcReq.Params)
		if err != nil {
			response.Error = &RPCError{
				Code:    ErrCodeInvalidParams,
				Message: "Invalid parameters",
			}
			s.sendJSONResponse(w, http.StatusOK, response)
			return
		}
		params = paramsBytes
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := s.executeWithTimeout(ctx, handler, params)
	if err != nil {
		if rpcErr, ok := err.(*RPCError); ok {
			response.Error = rpcErr
		} else {
			response.Error = &RPCError{
				Code:    ErrCodeInternalError,
				Message: err.Error(),
			}
		}
	} else {
		response.Result = result
	}

	// Send response
	s.sendJSONResponse(w, http.StatusOK, response)
}

// executeWithTimeout executes a method handler with timeout
func (s *Server) executeWithTimeout(ctx context.Context, handler func(json.RawMessage) (interface{}, error), params json.RawMessage) (interface{}, error) {
	type result struct {
		data interface{}
		err  error
	}

	resultChan := make(chan result, 1)

	go func() {
		data, err := handler(params)
		resultChan <- result{data: data, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, &RPCError{
			Code:    ErrCodeInternalError,
			Message: "Request timeout",
		}
	case res := <-resultChan:
		return res.data, res.err
	}
}

// checkAuth validates HTTP basic authentication
func (s *Server) checkAuth(r *http.Request) bool {
	if s.config.Username == "" && s.config.Password == "" {
		return true // No authentication required
	}

	username, password, ok := r.BasicAuth()
	if !ok {
		return false
	}

	return username == s.config.Username && password == s.config.Password
}

// checkSessionToken validates the session token
func (s *Server) checkSessionToken(r *http.Request) bool {
	token := r.Header.Get(s.config.SessionTokenHeader)

	s.sessionMu.RLock()
	valid := token == s.sessionID
	s.sessionMu.RUnlock()

	return valid
}

// isStateChangingMethod returns true if the method changes server state
func (s *Server) isStateChangingMethod(method string) bool {
	stateChangingMethods := map[string]bool{
		"torrent-add":       true,
		"torrent-start":     true,
		"torrent-start-now": true,
		"torrent-stop":      true,
		"torrent-verify":    true,
		"torrent-remove":    true,
		"torrent-set":       true,
		"session-set":       true,
	}

	return stateChangingMethods[method]
}

// applyCORSHeaders adds CORS headers to the response
func (s *Server) applyCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := s.config.AllowOrigin
	if origin == "" {
		origin = "*"
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, "+s.config.SessionTokenHeader)

	if s.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

// sendError sends an HTTP error response
func (s *Server) sendError(w http.ResponseWriter, httpStatus, rpcCode int, message string) {
	response := Response{
		Version: "2.0",
		Error: &RPCError{
			Code:    rpcCode,
			Message: message,
		},
	}

	s.sendJSONResponse(w, httpStatus, response)
}

// sendJSONResponse sends a JSON response
func (s *Server) sendJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.config.ErrorLog("Failed to encode JSON response: %v", err)
	}
}

// RefreshSessionID generates a new session ID
func (s *Server) RefreshSessionID() {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	s.sessionID = generateSessionID()
	s.sessionTime = time.Now()
}

// GetSessionID returns the current session ID
func (s *Server) GetSessionID() string {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()

	return s.sessionID
}

// GetStats returns server statistics
func (s *Server) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"requests": s.requestCount,
		"errors":   s.errorCount,
		"uptime":   time.Since(s.sessionTime).Seconds(),
		"session":  s.GetSessionID(),
	}
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	// Close the torrent manager
	if s.config.TorrentManager != nil {
		return s.config.TorrentManager.Close()
	}
	return nil
}

// Helper functions

// generateSessionID creates a new session identifier using cryptographically secure random bytes
func generateSessionID() string {
	// Generate 16 bytes of random data for session ID
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to time-based ID if crypto/rand fails (highly unlikely)
		// This maintains functionality even in edge cases
		return fmt.Sprintf("fallback-session-%d", time.Now().UnixNano())
	}

	// Return hex-encoded random session ID
	return fmt.Sprintf("session-%s", hex.EncodeToString(bytes))
}

// Middleware support

// WithLogging wraps a handler with request logging
func WithLogging(handler http.Handler, logger func(format string, args ...interface{})) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		handler.ServeHTTP(ww, r)

		duration := time.Since(start)
		logger("%s %s %s %d %v", r.Method, r.URL.Path, r.RemoteAddr, ww.statusCode, duration)
	})
}

// WithRecovery wraps a handler with panic recovery
func WithRecovery(handler http.Handler, logger func(format string, args ...interface{})) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger("Panic in RPC handler: %v", err)

				response := Response{
					Version: "2.0",
					Error: &RPCError{
						Code:    ErrCodeInternalError,
						Message: "Internal server error",
					},
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(response)
			}
		}()

		handler.ServeHTTP(w, r)
	})
}

// WithRateLimit wraps a handler with rate limiting
func WithRateLimit(handler http.Handler, requestsPerSecond int) http.Handler {
	// Simple rate limiting implementation
	var (
		lastReset time.Time
		requests  int
		mu        sync.Mutex
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		now := time.Now()
		if now.Sub(lastReset) >= time.Second {
			requests = 0
			lastReset = now
		}

		if requests >= requestsPerSecond {
			mu.Unlock()
			response := Response{
				Version: "2.0",
				Error: &RPCError{
					Code:    ErrCodeInternalError,
					Message: "Rate limit exceeded",
				},
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(response)
			return
		}

		requests++
		mu.Unlock()

		handler.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Utility functions for creating and configuring servers

// NewSimpleServer creates a server with minimal configuration
func NewSimpleServer(downloadDir string) (*Server, error) {
	config := TorrentManagerConfig{
		DownloadDir: downloadDir,
		SessionConfig: SessionConfiguration{
			DownloadDir: downloadDir,
			Version:     "go-i2p-bt-rpc/1.0.0",
		},
	}

	manager, err := NewTorrentManager(config)
	if err != nil {
		return nil, err
	}

	serverConfig := ServerConfig{
		TorrentManager: manager,
	}

	return NewServer(serverConfig)
}

// NewServerWithAuth creates a server with authentication
func NewServerWithAuth(downloadDir, username, password string) (*Server, error) {
	config := TorrentManagerConfig{
		DownloadDir: downloadDir,
		SessionConfig: SessionConfiguration{
			DownloadDir: downloadDir,
			Version:     "go-i2p-bt-rpc/1.0.0",
		},
	}

	manager, err := NewTorrentManager(config)
	if err != nil {
		return nil, err
	}

	serverConfig := ServerConfig{
		TorrentManager: manager,
		Username:       username,
		Password:       password,
	}

	return NewServer(serverConfig)
}

// StartServer is a convenience function to start an HTTP server
func StartServer(server *Server, addr string) error {
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      WithRecovery(WithLogging(server, log.Printf), log.Printf),
		ReadTimeout:  server.config.ReadTimeout,
		WriteTimeout: server.config.WriteTimeout,
	}

	log.Printf("Starting Transmission RPC server on %s", addr)
	return httpServer.ListenAndServe()
}
