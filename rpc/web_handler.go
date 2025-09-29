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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

// WebHandlerConfig configures the web interface handler
type WebHandlerConfig struct {
	// StaticDir is the directory containing static web files
	StaticDir string

	// URLPrefix is the URL path prefix for static files (default: "/web/")
	URLPrefix string

	// IndexFile is the default file to serve for directory requests (default: "index.html")
	IndexFile string

	// MaxAge sets Cache-Control max-age header in seconds (default: 3600 = 1 hour)
	MaxAge int

	// RequireAuth determines if authentication is required for web interface access
	RequireAuth bool

	// EnableDirectoryListing allows directory browsing (default: false for security)
	EnableDirectoryListing bool
}

// WebHandler provides HTTP handlers for serving static web content
// Uses Go's standard http.FileServer with security enhancements
type WebHandler struct {
	config     WebHandlerConfig
	fileServer http.Handler
	server     *Server // Reference to main RPC server for auth checks
}

// NewWebHandler creates a new web interface handler
// Returns error if configuration is invalid or static directory doesn't exist
func NewWebHandler(config WebHandlerConfig, server *Server) (*WebHandler, error) {
	if err := validateWebHandlerConfig(&config); err != nil {
		return nil, err
	}

	fileServer := createFileServer(config)

	handler := &WebHandler{
		config:     config,
		fileServer: fileServer,
		server:     server,
	}

	return handler, nil
}

// validateWebHandlerConfig validates and sets defaults for web handler configuration.
func validateWebHandlerConfig(config *WebHandlerConfig) error {
	if config.StaticDir == "" {
		return fmt.Errorf("StaticDir is required")
	}

	normalizeURLPrefix(config)
	setConfigDefaults(config)

	return nil
}

// normalizeURLPrefix ensures URLPrefix starts and ends with "/".
func normalizeURLPrefix(config *WebHandlerConfig) {
	if config.URLPrefix == "" {
		config.URLPrefix = "/web/"
	}

	if !strings.HasPrefix(config.URLPrefix, "/") {
		config.URLPrefix = "/" + config.URLPrefix
	}
	if !strings.HasSuffix(config.URLPrefix, "/") {
		config.URLPrefix = config.URLPrefix + "/"
	}
}

// setConfigDefaults sets default values for unspecified configuration options.
func setConfigDefaults(config *WebHandlerConfig) {
	if config.IndexFile == "" {
		config.IndexFile = "index.html"
	}

	if config.MaxAge == 0 {
		config.MaxAge = 3600 // 1 hour default cache
	}
}

// createFileServer creates the appropriate file server based on configuration.
func createFileServer(config WebHandlerConfig) http.Handler {
	if config.EnableDirectoryListing {
		return http.FileServer(http.Dir(config.StaticDir))
	}
	// Use custom file server that denies directory listing
	return http.FileServer(noDirectoryListingFS{http.Dir(config.StaticDir)})
}

// ServeHTTP handles static file requests with security headers and authentication
func (w *WebHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	// Handle file upload requests
	if r.Method == "POST" && r.URL.Path == "/upload" {
		w.handleFileUpload(rw, r)
		return
	}

	// Security: Only allow GET and HEAD methods for static files
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check authentication if required
	if w.config.RequireAuth && w.server != nil {
		if !w.server.checkAuth(r) {
			rw.Header().Set("WWW-Authenticate", `Basic realm="Transmission RPC"`)
			http.Error(rw, "Authentication required", http.StatusUnauthorized)
			return
		}
	}

	// Clean and validate the request path
	cleanPath := path.Clean(r.URL.Path)

	// Security: Prevent directory traversal attacks
	if strings.Contains(r.URL.Path, "..") || strings.Contains(cleanPath, "..") {
		http.Error(rw, "Invalid path", http.StatusBadRequest)
		return
	}

	// For root path, serve index file
	filePath := cleanPath
	if filePath == "" || filePath == "/" {
		filePath = "/" + w.config.IndexFile
	}

	// Set security headers before serving file
	w.setSecurityHeaders(rw)

	// Set cache headers for static assets
	w.setCacheHeaders(rw, filePath)

	// Create a new request with the cleaned path for the file server
	fileRequest := r.Clone(r.Context())
	fileRequest.URL.Path = filePath

	// Serve the file
	w.fileServer.ServeHTTP(rw, fileRequest)
}

// setSecurityHeaders adds security-focused HTTP headers
func (w *WebHandler) setSecurityHeaders(rw http.ResponseWriter) {
	headers := rw.Header()

	// Prevent XSS attacks
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")
	headers.Set("X-XSS-Protection", "1; mode=block")

	// Content Security Policy for web apps
	csp := "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"connect-src 'self'"
	headers.Set("Content-Security-Policy", csp)

	// Referrer policy
	headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// setCacheHeaders sets appropriate cache headers based on file type
func (w *WebHandler) setCacheHeaders(rw http.ResponseWriter, filePath string) {
	ext := strings.ToLower(filepath.Ext(filePath))

	// Different cache policies for different file types
	switch ext {
	case ".html", ".htm":
		// HTML files: short cache, check for updates
		rw.Header().Set("Cache-Control", "public, max-age=300") // 5 minutes
	case ".js", ".css":
		// Scripts and styles: longer cache with validation
		rw.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", w.config.MaxAge))
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico":
		// Images: long cache
		rw.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", w.config.MaxAge*24)) // 24x longer for images
	default:
		// Default cache policy
		rw.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", w.config.MaxAge))
	}
}

// noDirectoryListingFS wraps http.FileSystem to prevent directory listing
// This enhances security by not exposing directory contents
type noDirectoryListingFS struct {
	fs http.FileSystem
}

// Open implements http.FileSystem interface with directory listing disabled
func (nfs noDirectoryListingFS) Open(name string) (http.File, error) {
	f, err := nfs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	// Check if it's a directory
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	if stat.IsDir() {
		// Close the directory file and return a "not found" error
		// This prevents directory listing while still allowing index files
		f.Close()
		return nil, fmt.Errorf("directory listing disabled")
	}

	return f, nil
}

// CreateMuxWithWebHandler creates an HTTP multiplexer that handles both RPC and web requests
// This is the recommended way to integrate web interface with RPC server
func CreateMuxWithWebHandler(rpcServer *Server, webConfig WebHandlerConfig) (http.Handler, error) {
	webHandler, err := NewWebHandler(webConfig, rpcServer)
	if err != nil {
		return nil, fmt.Errorf("failed to create web handler: %w", err)
	}

	// Create multiplexer that routes requests appropriately
	mux := http.NewServeMux()

	// Handle RPC requests at /transmission/rpc
	mux.Handle("/transmission/rpc", rpcServer)

	// Handle file upload requests at /upload
	mux.Handle("/upload", webHandler)

	// Handle web interface requests at the configured prefix
	mux.Handle(webConfig.URLPrefix, http.StripPrefix(strings.TrimSuffix(webConfig.URLPrefix, "/"), webHandler))

	// Handle root redirect to web interface for convenience
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, webConfig.URLPrefix, http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	return mux, nil
}

// CreateMuxWithWebSocketSupport creates an HTTP multiplexer with integrated WebSocket support.
//
// This function provides a unified HTTP handler that serves both static web content
// and WebSocket connections for real-time updates. It's designed to simplify
// deployment by providing a single endpoint that handles all web interface needs.
//
// The resulting multiplexer routes requests as follows:
//   - /ws: WebSocket endpoint for real-time torrent and session updates
//   - All other paths: Static file serving from the configured directory
//
// Parameters:
//   - rpcServer: *Server instance for authentication and torrent data access
//   - webConfig: WebHandlerConfig for static file serving configuration
//   - wsConfig: WebSocketConfig for WebSocket behavior and security settings
//
// Returns:
//   - http.Handler: Configured multiplexer ready for HTTP server integration
//   - *WebSocketHandler: WebSocket handler instance for programmatic access
//   - error: Configuration or initialization error
//
// Example:
//
//	webConfig := WebHandlerConfig{
//		StaticDir: "./web",
//		URLPrefix: "/",
//	}
//	wsConfig := WebSocketConfig{
//		RequireAuth: true,
//		UpdateInterval: 2 * time.Second,
//	}
//	mux, wsHandler, err := CreateMuxWithWebSocketSupport(server, webConfig, wsConfig)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer wsHandler.Close()
//
//	log.Fatal(http.ListenAndServe(":8080", mux))
func CreateMuxWithWebSocketSupport(rpcServer *Server, webConfig WebHandlerConfig, wsConfig WebSocketConfig) (http.Handler, *WebSocketHandler, error) {
	webHandler, err := NewWebHandler(webConfig, rpcServer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create web handler: %w", err)
	}

	wsHandler, err := NewWebSocketHandler(wsConfig, rpcServer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create websocket handler: %w", err)
	}

	// Create multiplexer that routes requests appropriately
	mux := http.NewServeMux()

	// Handle RPC requests at /transmission/rpc
	mux.Handle("/transmission/rpc", rpcServer)

	// Handle WebSocket requests at /ws
	mux.Handle("/ws", wsHandler)

	// Handle file upload requests at /upload
	mux.Handle("/upload", webHandler)

	// Handle web interface requests at the configured prefix
	mux.Handle(webConfig.URLPrefix, http.StripPrefix(strings.TrimSuffix(webConfig.URLPrefix, "/"), webHandler))

	// Handle root redirect to web interface for convenience
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, webConfig.URLPrefix, http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	return mux, wsHandler, nil
}

// handleFileUpload processes .torrent file uploads and adds them via RPC.
//
// This method handles multipart/form-data POST requests for uploading .torrent files
// and automatically adds them to the BitTorrent client via the torrent-add RPC method.
//
// Request Format:
//   - Method: POST
//   - Content-Type: multipart/form-data
//   - Form fields:
//   - "torrent": .torrent file (required, max 10MB)
//   - "paused": boolean string ("true"/"false") to start torrent paused (optional)
//   - "download-dir": custom download directory (optional)
//
// Response Format (JSON):
//   - Success: {"success": true, "message": "...", "data": {...}}
//   - Error: {"success": false, "error": "..."}
//
// Security Features:
//   - File extension validation (.torrent only)
//   - File size limits (10MB maximum)
//   - Basic torrent format validation
//   - Optional HTTP Basic authentication
//   - CSRF protection via authentication requirement
//
// The method integrates with the existing torrent-add RPC functionality by:
//  1. Parsing the uploaded file
//  2. Converting to base64 encoding
//  3. Creating a TorrentAddRequest
//  4. Calling the TorrentAdd RPC method
//  5. Returning the result in JSON format
func (w *WebHandler) handleFileUpload(rw http.ResponseWriter, r *http.Request) {
	// Check authentication if required
	if w.config.RequireAuth && w.server != nil {
		if !w.server.checkAuth(r) {
			rw.Header().Set("WWW-Authenticate", `Basic realm="Transmission RPC"`)
			http.Error(rw, "Authentication required", http.StatusUnauthorized)
			return
		}
	}

	// Set security headers
	w.setSecurityHeaders(rw)

	// Parse multipart form with size limit (10MB max)
	const maxUploadSize = 10 << 20 // 10MB
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		w.sendJSONError(rw, "Failed to parse upload", http.StatusBadRequest)
		return
	}

	// Get the uploaded file
	file, header, err := r.FormFile("torrent")
	if err != nil {
		w.sendJSONError(rw, "No torrent file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".torrent") {
		w.sendJSONError(rw, "File must have .torrent extension", http.StatusBadRequest)
		return
	}

	// Read and validate file size
	if header.Size > maxUploadSize {
		w.sendJSONError(rw, "File too large (max 10MB)", http.StatusBadRequest)
		return
	}

	// Read file content
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		w.sendJSONError(rw, "Failed to read file", http.StatusInternalServerError)
		return
	}

	// Basic validation - check for torrent file signature
	if !w.isValidTorrentFile(fileBytes) {
		w.sendJSONError(rw, "Invalid torrent file format", http.StatusBadRequest)
		return
	}

	// Convert to base64 for RPC
	base64Data := base64.StdEncoding.EncodeToString(fileBytes)

	// Create torrent-add request
	addRequest := TorrentAddRequest{
		Metainfo:    base64Data,
		DownloadDir: r.FormValue("download-dir"), // Optional parameter from form
		Paused:      r.FormValue("paused") == "true",
	}

	// Add the torrent via RPC
	if w.server != nil && w.server.methods != nil {
		response, err := w.server.methods.TorrentAdd(addRequest)
		if err != nil {
			w.sendJSONError(rw, fmt.Sprintf("Failed to add torrent: %v", err), http.StatusInternalServerError)
			return
		}

		// Send success response
		w.sendJSONResponse(rw, map[string]interface{}{
			"success": true,
			"message": "Torrent uploaded successfully",
			"data":    response,
		})
	} else {
		w.sendJSONError(rw, "RPC server not available", http.StatusServiceUnavailable)
	}
}

// isValidTorrentFile performs basic validation of torrent file format.
//
// This method provides lightweight validation to ensure uploaded files
// are likely to be valid .torrent files before processing them further.
//
// Validation Checks:
//   - File must be at least 10 bytes long
//   - Must start with 'd' (bencode dictionary marker)
//   - Must contain either "announce" or "info" keys (standard torrent keys)
//
// Note: This is basic validation only. Full torrent parsing and validation
// is performed by the metainfo package when the torrent is actually added.
//
// Returns true if the file appears to be a valid torrent, false otherwise.
func (w *WebHandler) isValidTorrentFile(data []byte) bool {
	// Basic check: torrent files start with 'd' (bencode dictionary)
	// and contain key strings like 'announce' or 'info'
	if len(data) < 10 {
		return false
	}

	// Must start with 'd' (bencode dictionary)
	if data[0] != 'd' {
		return false
	}

	// Look for common torrent file keys
	content := string(data)
	hasAnnounce := strings.Contains(content, "announce")
	hasInfo := strings.Contains(content, "info")

	return hasAnnounce || hasInfo
}

// sendJSONResponse sends a JSON response with proper Content-Type headers.
//
// This helper method encodes the provided data as JSON and sends it with
// appropriate HTTP headers. If JSON encoding fails, it falls back to a
// plain text error response.
//
// Parameters:
//   - rw: HTTP response writer
//   - data: Any JSON-serializable data structure
func (w *WebHandler) sendJSONResponse(rw http.ResponseWriter, data interface{}) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(rw).Encode(data); err != nil {
		// Fallback to plain text if JSON encoding fails
		http.Error(rw, "Internal server error", http.StatusInternalServerError)
	}
}

// sendJSONError sends an error response in JSON format.
//
// This helper method creates a standardized error response with the format:
// {"success": false, "error": "error message"}
//
// Parameters:
//   - rw: HTTP response writer
//   - message: Error message to include in response
//   - statusCode: HTTP status code to return
func (w *WebHandler) sendJSONError(rw http.ResponseWriter, message string, statusCode int) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(statusCode)

	errorResponse := map[string]interface{}{
		"success": false,
		"error":   message,
	}

	if err := json.NewEncoder(rw).Encode(errorResponse); err != nil {
		// Fallback to plain text if JSON encoding fails
		http.Error(rw, message, statusCode)
	}
}

// Utility functions for common web handler setups

// NewDefaultWebHandler creates a web handler with sensible defaults
func NewDefaultWebHandler(staticDir string, server *Server) (*WebHandler, error) {
	config := WebHandlerConfig{
		StaticDir:              staticDir,
		URLPrefix:              "/web/",
		IndexFile:              "index.html",
		MaxAge:                 3600,
		RequireAuth:            false, // Allow public access to web interface
		EnableDirectoryListing: false, // Security: disable directory browsing
	}

	return NewWebHandler(config, server)
}

// NewSecureWebHandler creates a web handler with authentication required
func NewSecureWebHandler(staticDir string, server *Server) (*WebHandler, error) {
	config := WebHandlerConfig{
		StaticDir:              staticDir,
		URLPrefix:              "/web/",
		IndexFile:              "index.html",
		MaxAge:                 3600,
		RequireAuth:            true, // Require authentication
		EnableDirectoryListing: false,
	}

	return NewWebHandler(config, server)
}

// GetMimeType returns the MIME type for common web file extensions
// This can be used for setting Content-Type headers if needed
func GetMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	mimeTypes := map[string]string{
		".html": "text/html; charset=utf-8",
		".htm":  "text/html; charset=utf-8",
		".css":  "text/css; charset=utf-8",
		".js":   "application/javascript; charset=utf-8",
		".json": "application/json; charset=utf-8",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".svg":  "image/svg+xml",
		".ico":  "image/x-icon",
		".txt":  "text/plain; charset=utf-8",
	}

	if mimeType, exists := mimeTypes[ext]; exists {
		return mimeType
	}

	return "application/octet-stream"
}
