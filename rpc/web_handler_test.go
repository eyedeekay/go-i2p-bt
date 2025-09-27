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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWebHandlerConfig tests configuration validation and defaults
func TestWebHandlerConfig(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Create test server for auth testing
	server := createTestServer(t)

	tests := []struct {
		name        string
		config      WebHandlerConfig
		expectError bool
		description string
	}{
		{
			name: "valid config",
			config: WebHandlerConfig{
				StaticDir:              tempDir,
				URLPrefix:              "/web/",
				IndexFile:              "index.html",
				MaxAge:                 3600,
				RequireAuth:            false,
				EnableDirectoryListing: false,
			},
			expectError: false,
			description: "Valid configuration should work",
		},
		{
			name: "missing static dir",
			config: WebHandlerConfig{
				URLPrefix: "/web/",
			},
			expectError: true,
			description: "Missing StaticDir should cause error",
		},
		{
			name: "empty config with defaults",
			config: WebHandlerConfig{
				StaticDir: tempDir,
			},
			expectError: false,
			description: "Empty config should get reasonable defaults",
		},
		{
			name: "custom prefix without slashes",
			config: WebHandlerConfig{
				StaticDir: tempDir,
				URLPrefix: "custom",
			},
			expectError: false,
			description: "URL prefix should be normalized with slashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewWebHandler(tt.config, server)

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
			if handler.config.URLPrefix == "custom" {
				// Should be normalized to "/custom/"
				if handler.config.URLPrefix != "/custom/" {
					t.Errorf("Expected URL prefix to be normalized to '/custom/', got '%s'", handler.config.URLPrefix)
				}
			}

			if handler.config.IndexFile == "" {
				t.Errorf("Expected default index file to be set")
			}
		})
	}
}

// TestWebHandlerServeHTTP tests the main HTTP serving functionality
func TestWebHandlerServeHTTP(t *testing.T) {
	// Create temporary directory with test files
	tempDir := t.TempDir()
	createTestFiles(t, tempDir)

	// Create test server
	server := createTestServer(t)

	// Create web handler
	config := WebHandlerConfig{
		StaticDir:              tempDir,
		URLPrefix:              "/web/",
		IndexFile:              "index.html",
		MaxAge:                 3600,
		RequireAuth:            false,
		EnableDirectoryListing: false,
	}

	handler, err := NewWebHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create web handler: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedBody   string
		checkHeaders   func(t *testing.T, headers http.Header)
		description    string
	}{
		{
			name:           "redirect root to directory",
			method:         "GET",
			path:           "/",
			expectedStatus: http.StatusMovedPermanently,
			expectedBody:   "",
			checkHeaders: func(t *testing.T, headers http.Header) {
				location := headers.Get("Location")
				if location == "" {
					t.Errorf("Expected Location header for redirect")
				}
			},
			description: "Should redirect root directory requests",
		},
		{
			name:           "serve specific file",
			method:         "GET",
			path:           "/test.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "Test Content",
			checkHeaders: func(t *testing.T, headers http.Header) {
				checkSecurityHeaders(t, headers)
			},
			description: "Should serve specific files",
		},
		{
			name:           "serve CSS file",
			method:         "GET",
			path:           "/style.css",
			expectedStatus: http.StatusOK,
			expectedBody:   "body { margin: 0; }",
			checkHeaders: func(t *testing.T, headers http.Header) {
				checkSecurityHeaders(t, headers)
				checkCacheHeaders(t, headers, ".css")
			},
			description: "Should serve CSS files with appropriate headers",
		},
		{
			name:           "directory traversal attempt",
			method:         "GET",
			path:           "/test/../../../etc/passwd",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid path",
			checkHeaders:   nil,
			description:    "Should block directory traversal attacks",
		},
		{
			name:           "non-existent file",
			method:         "GET",
			path:           "/nonexistent.html",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "",
			checkHeaders:   nil,
			description:    "Should return 404 for non-existent files",
		},
		{
			name:           "method not allowed",
			method:         "POST",
			path:           "/index.html",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "Method not allowed",
			checkHeaders:   nil,
			description:    "Should only allow GET and HEAD methods",
		},
		{
			name:           "HEAD request",
			method:         "HEAD",
			path:           "/test.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "",
			checkHeaders: func(t *testing.T, headers http.Header) {
				checkSecurityHeaders(t, headers)
			},
			description: "Should handle HEAD requests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedBody != "" {
				bodyStr := strings.TrimSpace(w.Body.String())
				if !strings.Contains(bodyStr, tt.expectedBody) {
					t.Errorf("Expected body to contain '%s', got '%s'", tt.expectedBody, bodyStr)
				}
			}

			if tt.checkHeaders != nil {
				tt.checkHeaders(t, w.Header())
			}
		})
	}
}

// TestWebHandlerAuthentication tests authentication functionality
func TestWebHandlerAuthentication(t *testing.T) {
	tempDir := t.TempDir()
	createTestFiles(t, tempDir)

	// Create server with authentication
	server := createTestServerWithAuth(t, "admin", "secret")

	// Create web handler requiring auth
	config := WebHandlerConfig{
		StaticDir:   tempDir,
		URLPrefix:   "/web/",
		RequireAuth: true,
	}

	handler, err := NewWebHandler(config, server)
	if err != nil {
		t.Fatalf("Failed to create web handler: %v", err)
	}

	tests := []struct {
		name           string
		addAuth        bool
		username       string
		password       string
		expectedStatus int
		description    string
	}{
		{
			name:           "no auth provided",
			addAuth:        false,
			expectedStatus: http.StatusUnauthorized,
			description:    "Should require authentication when configured",
		},
		{
			name:           "valid auth",
			addAuth:        true,
			username:       "admin",
			password:       "secret",
			expectedStatus: http.StatusOK,
			description:    "Should allow access with valid credentials",
		},
		{
			name:           "invalid auth",
			addAuth:        true,
			username:       "admin",
			password:       "wrong",
			expectedStatus: http.StatusUnauthorized,
			description:    "Should deny access with invalid credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test.txt", nil)

			if tt.addAuth {
				req.SetBasicAuth(tt.username, tt.password)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedStatus == http.StatusUnauthorized {
				authHeader := w.Header().Get("WWW-Authenticate")
				if authHeader == "" {
					t.Errorf("Expected WWW-Authenticate header for 401 response")
				}
			}
		})
	}
}

// TestCreateMuxWithWebHandler tests the multiplexer creation
func TestCreateMuxWithWebHandler(t *testing.T) {
	tempDir := t.TempDir()
	createTestFiles(t, tempDir)

	server := createTestServer(t)

	config := WebHandlerConfig{
		StaticDir: tempDir,
		URLPrefix: "/web/",
	}

	mux, err := CreateMuxWithWebHandler(server, config)
	if err != nil {
		t.Fatalf("Failed to create mux: %v", err)
	}

	if mux == nil {
		t.Fatalf("Expected mux but got nil")
	}

	// Test different routes
	tests := []struct {
		name           string
		path           string
		expectedStatus int
		description    string
	}{
		{
			name:           "root redirect",
			path:           "/",
			expectedStatus: http.StatusFound,
			description:    "Should redirect root to web interface",
		},
		{
			name:           "web interface redirect",
			path:           "/web/",
			expectedStatus: http.StatusMovedPermanently,
			description:    "Should handle web interface directory requests",
		},
		{
			name:           "RPC endpoint",
			path:           "/transmission/rpc",
			expectedStatus: http.StatusMethodNotAllowed, // Expects POST
			description:    "Should handle RPC requests",
		},
		{
			name:           "unknown path",
			path:           "/unknown",
			expectedStatus: http.StatusNotFound,
			description:    "Should return 404 for unknown paths",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestNoDirectoryListingFS tests directory listing prevention
func TestNoDirectoryListingFS(t *testing.T) {
	tempDir := t.TempDir()
	createTestFiles(t, tempDir)

	// Create subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	err := os.Mkdir(subDir, 0o755)
	if err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	fs := noDirectoryListingFS{http.Dir(tempDir)}

	// Test file access (should work)
	file, err := fs.Open("/test.txt")
	if err != nil {
		t.Errorf("Expected file to be accessible, got error: %v", err)
	} else {
		file.Close()
	}

	// Test directory access (should fail)
	_, err = fs.Open("/subdir")
	if err == nil {
		t.Errorf("Expected directory access to be denied")
	}
}

// TestGetMimeType tests MIME type detection
func TestGetMimeType(t *testing.T) {
	tests := []struct {
		filename     string
		expectedMime string
		description  string
	}{
		{"index.html", "text/html; charset=utf-8", "HTML files"},
		{"style.css", "text/css; charset=utf-8", "CSS files"},
		{"app.js", "application/javascript; charset=utf-8", "JavaScript files"},
		{"data.json", "application/json; charset=utf-8", "JSON files"},
		{"image.png", "image/png", "PNG images"},
		{"photo.jpg", "image/jpeg", "JPEG images"},
		{"icon.svg", "image/svg+xml", "SVG images"},
		{"unknown.xyz", "application/octet-stream", "Unknown extensions"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			mimeType := GetMimeType(tt.filename)
			if mimeType != tt.expectedMime {
				t.Errorf("Expected MIME type '%s' for '%s', got '%s'", tt.expectedMime, tt.filename, mimeType)
			}
		})
	}
}

// TestUtilityFunctions tests the utility functions
func TestUtilityFunctions(t *testing.T) {
	tempDir := t.TempDir()
	createTestFiles(t, tempDir)

	server := createTestServer(t)

	t.Run("NewDefaultWebHandler", func(t *testing.T) {
		handler, err := NewDefaultWebHandler(tempDir, server)
		if err != nil {
			t.Errorf("NewDefaultWebHandler failed: %v", err)
		}
		if handler == nil {
			t.Errorf("Expected handler but got nil")
		}

		// Check defaults
		if handler.config.URLPrefix != "/web/" {
			t.Errorf("Expected default URL prefix '/web/', got '%s'", handler.config.URLPrefix)
		}
		if handler.config.RequireAuth != false {
			t.Errorf("Expected RequireAuth to be false by default")
		}
	})

	t.Run("NewSecureWebHandler", func(t *testing.T) {
		handler, err := NewSecureWebHandler(tempDir, server)
		if err != nil {
			t.Errorf("NewSecureWebHandler failed: %v", err)
		}
		if handler == nil {
			t.Errorf("Expected handler but got nil")
		}

		// Check secure defaults
		if handler.config.RequireAuth != true {
			t.Errorf("Expected RequireAuth to be true for secure handler")
		}
	})

	t.Run("CreateMuxWithWebHandler", func(t *testing.T) {
		config := WebHandlerConfig{
			StaticDir: tempDir,
			URLPrefix: "/web/",
		}

		mux, err := CreateMuxWithWebHandler(server, config)
		if err != nil {
			t.Errorf("CreateMuxWithWebHandler failed: %v", err)
		}
		if mux == nil {
			t.Errorf("Expected mux but got nil")
		}

		// Test that the mux routes correctly
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		// Should redirect to web interface
		if w.Code != http.StatusFound {
			t.Errorf("Expected redirect status 302, got %d", w.Code)
		}
	})
}

// Helper functions for testing

func createTestServer(t *testing.T) *Server {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			DownloadDir: t.TempDir(),
			Version:     "test",
		},
	}

	manager, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create torrent manager: %v", err)
	}

	serverConfig := ServerConfig{
		TorrentManager: manager,
	}

	server, err := NewServer(serverConfig)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	return server
}

func createTestServerWithAuth(t *testing.T, username, password string) *Server {
	config := TorrentManagerConfig{
		DownloadDir: t.TempDir(),
		SessionConfig: SessionConfiguration{
			DownloadDir: t.TempDir(),
			Version:     "test",
		},
	}

	manager, err := NewTorrentManager(config)
	if err != nil {
		t.Fatalf("Failed to create torrent manager: %v", err)
	}

	serverConfig := ServerConfig{
		TorrentManager: manager,
		Username:       username,
		Password:       password,
	}

	server, err := NewServer(serverConfig)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	return server
}

func createTestFiles(t *testing.T, dir string) {
	files := map[string]string{
		"index.html": "<html><body>Test Index</body></html>",
		"test.txt":   "Test Content",
		"style.css":  "body { margin: 0; }",
		"app.js":     "console.log('test');",
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		err := os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}
}

func checkSecurityHeaders(t *testing.T, headers http.Header) {
	expectedHeaders := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"X-XSS-Protection":        "1; mode=block",
		"Content-Security-Policy": "", // Just check it exists
		"Referrer-Policy":         "strict-origin-when-cross-origin",
	}

	for header, expectedValue := range expectedHeaders {
		value := headers.Get(header)
		if value == "" {
			t.Errorf("Expected security header '%s' to be set", header)
		} else if expectedValue != "" && value != expectedValue {
			t.Errorf("Expected header '%s' to be '%s', got '%s'", header, expectedValue, value)
		}
	}
}

func checkCacheHeaders(t *testing.T, headers http.Header, fileExt string) {
	cacheControl := headers.Get("Cache-Control")
	if cacheControl == "" {
		t.Errorf("Expected Cache-Control header to be set")
		return
	}

	// Different cache policies for different file types
	switch fileExt {
	case ".html", ".htm":
		if !strings.Contains(cacheControl, "max-age=300") {
			t.Errorf("Expected HTML files to have 5-minute cache, got: %s", cacheControl)
		}
	case ".css", ".js":
		if !strings.Contains(cacheControl, "max-age=3600") {
			t.Errorf("Expected CSS/JS files to have 1-hour cache, got: %s", cacheControl)
		}
	}
}

// Benchmarks for performance testing

func BenchmarkWebHandlerServeFile(b *testing.B) {
	tempDir := b.TempDir()
	createTestFiles(&testing.T{}, tempDir)

	server := createTestServer(&testing.T{})
	config := WebHandlerConfig{
		StaticDir: tempDir,
		URLPrefix: "/web/",
	}

	handler, err := NewWebHandler(config, server)
	if err != nil {
		b.Fatalf("Failed to create web handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/test.txt", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkMimeTypeDetection(b *testing.B) {
	filenames := []string{
		"index.html", "style.css", "app.js", "data.json",
		"image.png", "photo.jpg", "icon.svg", "file.txt",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, filename := range filenames {
			GetMimeType(filename)
		}
	}
}
