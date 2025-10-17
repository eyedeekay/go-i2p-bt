// Copyright 2020 go-i2p, 2023 idk
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

package webseed

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// WebSeedClient provides HTTP/FTP downloading capabilities for BEP 19.
// It supports range requests to download specific byte ranges from web seeds.
type WebSeedClient struct {
	client  *http.Client
	timeout time.Duration
	retries int
}

// WebSeedConfig configures the WebSeedClient behavior.
type WebSeedConfig struct {
	// Timeout for individual HTTP requests. Default: 30 seconds.
	Timeout time.Duration

	// MaxRetries for failed requests. Default: 3.
	MaxRetries int

	// HTTPClient allows custom HTTP client. Default: http.DefaultClient.
	HTTPClient *http.Client
}

// NewWebSeedClient creates a new WebSeedClient with the given configuration.
func NewWebSeedClient(cfg ...WebSeedConfig) *WebSeedClient {
	var c WebSeedConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}

	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{
			Timeout: c.Timeout,
		}
	}

	return &WebSeedClient{
		client:  c.HTTPClient,
		timeout: c.Timeout,
		retries: c.MaxRetries,
	}
}

// DownloadPiece downloads a complete piece from a web seed URL.
// Returns the piece data or an error.
func (w *WebSeedClient) DownloadPiece(url string, info metainfo.Info, pieceIndex int) ([]byte, error) {
	piece := info.Piece(pieceIndex)
	return w.DownloadRange(url, piece.Offset(), piece.Length())
}

// DownloadBlock downloads a block (part of a piece) from a web seed URL.
// Returns the block data or an error.
func (w *WebSeedClient) DownloadBlock(url string, offset, length int64) ([]byte, error) {
	return w.DownloadRange(url, offset, length)
}

// DownloadRange downloads a specific byte range from a web seed URL.
// Implements HTTP range requests as specified in BEP 19.
// Returns the data or an error after retries are exhausted.
func (w *WebSeedClient) DownloadRange(url string, offset, length int64) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= w.retries; attempt++ {
		data, err := w.tryDownloadRange(url, offset, length)
		if err == nil {
			return data, nil
		}

		lastErr = err

		// Don't retry on client errors (4xx)
		if httpErr, ok := err.(*HTTPError); ok && httpErr.Code >= 400 && httpErr.Code < 500 {
			return nil, lastErr
		}

		// Exponential backoff between retries
		if attempt < w.retries {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}
	}

	return nil, fmt.Errorf("download failed after %d retries: %w", w.retries, lastErr)
}

// tryDownloadRange attempts a single range request download.
func (w *WebSeedClient) tryDownloadRange(url string, offset, length int64) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set Range header for partial content (BEP 19)
	rangeHeader := fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)
	req.Header.Set("Range", rangeHeader)

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// BEP 19: Accept both 200 (full content) and 206 (partial content)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, &HTTPError{
			Code:    resp.StatusCode,
			Message: resp.Status,
			URL:     url,
		}
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Verify received length matches requested
	if int64(len(data)) != length {
		return nil, fmt.Errorf("length mismatch: expected %d, got %d", length, len(data))
	}

	return data, nil
}

// HTTPError represents an HTTP-specific error with status code.
type HTTPError struct {
	Code    int    // HTTP status code
	Message string // Status message
	URL     string // URL that failed
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d for %s: %s", e.Code, e.URL, e.Message)
}
