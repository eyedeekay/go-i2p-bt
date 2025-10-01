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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// WebSeedType represents the type of web seed
type WebSeedType string

const (
	WebSeedTypeHTTPFTP  WebSeedType = "url-list" // BEP-19 HTTP/FTP seeding
	WebSeedTypeGetRight WebSeedType = "url-data" // GetRight style
)

// WebSeed represents a web seed source
type WebSeed struct {
	URL        string      `json:"url"`
	Type       WebSeedType `json:"type"`
	Active     bool        `json:"active"`
	Failed     bool        `json:"failed"`
	FailCount  int         `json:"fail_count"`
	LastError  string      `json:"last_error,omitempty"`
	LastTried  time.Time   `json:"last_tried"`
	Downloaded int64       `json:"downloaded"`
	Speed      int64       `json:"speed"` // bytes per second
}

// WebSeedManager manages web seed sources for torrents
type WebSeedManager struct {
	mu        sync.RWMutex
	enabled   bool
	webSeeds  map[string][]*WebSeed // torrentID -> webseeds
	client    *http.Client
	userAgent string
}

// NewWebSeedManager creates a new WebSeed manager
func NewWebSeedManager(enabled bool) *WebSeedManager {
	return &WebSeedManager{
		enabled:  enabled,
		webSeeds: make(map[string][]*WebSeed),
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    30 * time.Second,
				DisableCompression: false,
			},
		},
		userAgent: "go-i2p-bt/1.0",
	}
}

// IsEnabled returns whether WebSeed is enabled
func (ws *WebSeedManager) IsEnabled() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.enabled
}

// SetEnabled enables or disables WebSeed
func (ws *WebSeedManager) SetEnabled(enabled bool) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.enabled = enabled
}

// AddWebSeed adds a web seed for a torrent
func (ws *WebSeedManager) AddWebSeed(torrentID, webseedURL string, seedType WebSeedType) error {
	if !ws.IsEnabled() {
		return nil
	}

	// Validate URL
	parsedURL, err := url.Parse(webseedURL)
	if err != nil {
		return fmt.Errorf("invalid webseed URL: %v", err)
	}

	// Check that URL has a valid scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" && parsedURL.Scheme != "ftp" {
		return fmt.Errorf("invalid webseed URL scheme: must be http, https, or ftp")
	}

	// Check that URL has a host
	if parsedURL.Host == "" {
		return fmt.Errorf("invalid webseed URL: missing host")
	}

	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.webSeeds[torrentID] == nil {
		ws.webSeeds[torrentID] = make([]*WebSeed, 0)
	}

	// Check if webseed already exists
	for _, existing := range ws.webSeeds[torrentID] {
		if existing.URL == webseedURL {
			return nil // Already exists
		}
	}

	webseed := &WebSeed{
		URL:       webseedURL,
		Type:      seedType,
		Active:    true,
		Failed:    false,
		FailCount: 0,
		LastTried: time.Time{},
	}

	ws.webSeeds[torrentID] = append(ws.webSeeds[torrentID], webseed)
	return nil
}

// RemoveWebSeed removes a web seed
func (ws *WebSeedManager) RemoveWebSeed(torrentID, webseedURL string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	seeds := ws.webSeeds[torrentID]
	if seeds == nil {
		return
	}

	for i, webseed := range seeds {
		if webseed.URL == webseedURL {
			// Remove the webseed
			ws.webSeeds[torrentID] = append(seeds[:i], seeds[i+1:]...)
			break
		}
	}

	// Clean up empty slice
	if len(ws.webSeeds[torrentID]) == 0 {
		delete(ws.webSeeds, torrentID)
	}
}

// GetWebSeeds returns all web seeds for a torrent
func (ws *WebSeedManager) GetWebSeeds(torrentID string) []*WebSeed {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	seeds := ws.webSeeds[torrentID]
	if seeds == nil {
		return nil
	}

	// Return a copy to avoid concurrent access issues
	result := make([]*WebSeed, len(seeds))
	for i, seed := range seeds {
		seedCopy := *seed
		result[i] = &seedCopy
	}

	return result
}

// GetActiveWebSeeds returns only active (non-failed) web seeds
func (ws *WebSeedManager) GetActiveWebSeeds(torrentID string) []*WebSeed {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	seeds := ws.webSeeds[torrentID]
	if seeds == nil {
		return nil
	}

	var active []*WebSeed
	for _, seed := range seeds {
		if seed.Active && !seed.Failed {
			seedCopy := *seed
			active = append(active, &seedCopy)
		}
	}

	return active
}

// DownloadPieceData downloads a piece from web seeds
func (ws *WebSeedManager) DownloadPieceData(ctx context.Context, torrentID string, pieceIndex int, pieceLength int64, fileName string) ([]byte, error) {
	if !ws.IsEnabled() {
		return nil, fmt.Errorf("webseeds disabled")
	}

	activeSeeds := ws.GetActiveWebSeeds(torrentID)
	if len(activeSeeds) == 0 {
		return nil, fmt.Errorf("no active webseeds available")
	}

	// Try each webseed in order
	for _, seed := range activeSeeds {
		data, err := ws.downloadFromSeed(ctx, torrentID, seed, pieceIndex, pieceLength, fileName)
		if err != nil {
			ws.markSeedFailed(torrentID, seed.URL, err)
			continue
		}

		ws.markSeedSuccess(torrentID, seed.URL, int64(len(data)))
		return data, nil
	}

	return nil, fmt.Errorf("all webseeds failed")
}

// downloadFromSeed downloads data from a specific webseed
func (ws *WebSeedManager) downloadFromSeed(ctx context.Context, torrentID string, seed *WebSeed, pieceIndex int, pieceLength int64, fileName string) ([]byte, error) {
	requestURL, err := ws.buildRequestURL(seed, fileName)
	if err != nil {
		return nil, err
	}

	req, err := ws.createHTTPRequest(ctx, requestURL, pieceIndex, pieceLength)
	if err != nil {
		return nil, err
	}

	ws.recordAttemptTime(torrentID, seed.URL)

	data, err := ws.executeRequest(req, pieceLength)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// buildRequestURL constructs the appropriate request URL based on webseed type
func (ws *WebSeedManager) buildRequestURL(seed *WebSeed, fileName string) (string, error) {
	var requestURL string

	switch seed.Type {
	case WebSeedTypeHTTPFTP:
		// BEP-19: URL-list style
		// URL points to the file, we request a byte range
		requestURL = seed.URL
		if !strings.HasSuffix(requestURL, "/") && fileName != "" {
			requestURL = requestURL + "/" + fileName
		}
	case WebSeedTypeGetRight:
		// GetRight style URL-data
		// URL includes file path
		requestURL = seed.URL
	default:
		return "", fmt.Errorf("unsupported webseed type: %s", seed.Type)
	}

	return requestURL, nil
}

// createHTTPRequest creates and configures an HTTP request for piece download
func (ws *WebSeedManager) createHTTPRequest(ctx context.Context, requestURL string, pieceIndex int, pieceLength int64) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("User-Agent", ws.userAgent)

	// Calculate byte range for the piece
	startByte := int64(pieceIndex) * pieceLength
	endByte := startByte + pieceLength - 1

	// Set Range header for partial content
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", startByte, endByte))

	return req, nil
}

// recordAttemptTime updates the last attempted time for a webseed
func (ws *WebSeedManager) recordAttemptTime(torrentID, seedURL string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if seeds := ws.webSeeds[torrentID]; seeds != nil {
		for _, s := range seeds {
			if s.URL == seedURL {
				s.LastTried = time.Now()
				break
			}
		}
	}
}

// executeRequest performs the HTTP request and validates the response
func (ws *WebSeedManager) executeRequest(req *http.Request, expectedLength int64) ([]byte, error) {
	resp, err := ws.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read the response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Verify the length
	if int64(len(data)) != expectedLength {
		return nil, fmt.Errorf("received %d bytes, expected %d", len(data), expectedLength)
	}

	return data, nil
}

// markSeedFailed marks a webseed as failed
func (ws *WebSeedManager) markSeedFailed(torrentID, seedURL string, err error) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	seeds := ws.webSeeds[torrentID]
	for _, seed := range seeds {
		if seed.URL == seedURL {
			seed.FailCount++
			seed.LastError = err.Error()
			seed.LastTried = time.Now()

			// Mark as failed after 3 consecutive failures
			if seed.FailCount >= 3 {
				seed.Failed = true
				seed.Active = false
			}
			break
		}
	}
}

// markSeedSuccess marks a webseed as successful
func (ws *WebSeedManager) markSeedSuccess(torrentID, seedURL string, bytes int64) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	seeds := ws.webSeeds[torrentID]
	for _, seed := range seeds {
		if seed.URL == seedURL {
			seed.FailCount = 0
			seed.LastError = ""
			seed.Failed = false
			seed.Active = true
			seed.Downloaded += bytes
			seed.LastTried = time.Now()

			// Simple speed calculation (bytes per second over last operation)
			// In a real implementation, this would be more sophisticated
			seed.Speed = bytes // Simplified for now
			break
		}
	}
}

// RetryFailedSeeds re-enables failed webseeds after a cooldown period
func (ws *WebSeedManager) RetryFailedSeeds(torrentID string) int {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	seeds := ws.webSeeds[torrentID]
	if seeds == nil {
		return 0
	}

	retried := 0
	now := time.Now()
	cooldownPeriod := 5 * time.Minute

	for _, seed := range seeds {
		if seed.Failed && now.Sub(seed.LastTried) > cooldownPeriod {
			seed.Failed = false
			seed.Active = true
			seed.FailCount = 0
			seed.LastError = ""
			retried++
		}
	}

	return retried
}

// CleanupTorrent removes all webseeds for a torrent
func (ws *WebSeedManager) CleanupTorrent(torrentID string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.webSeeds, torrentID)
}

// GetStats returns webseed statistics
func (ws *WebSeedManager) GetStats() map[string]interface{} {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	totalSeeds := 0
	activeSeeds := 0
	failedSeeds := 0
	totalDownloaded := int64(0)

	for _, seeds := range ws.webSeeds {
		for _, seed := range seeds {
			totalSeeds++
			if seed.Active && !seed.Failed {
				activeSeeds++
			}
			if seed.Failed {
				failedSeeds++
			}
			totalDownloaded += seed.Downloaded
		}
	}

	return map[string]interface{}{
		"enabled":          ws.enabled,
		"total_webseeds":   totalSeeds,
		"active_webseeds":  activeSeeds,
		"failed_webseeds":  failedSeeds,
		"total_downloaded": totalDownloaded,
	}
}
