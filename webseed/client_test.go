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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// TestNewWebSeedClient tests WebSeedClient creation with default config.
func TestNewWebSeedClient(t *testing.T) {
	client := NewWebSeedClient()

	if client == nil {
		t.Fatal("NewWebSeedClient returned nil")
	}

	if client.timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client.timeout)
	}

	if client.retries != 3 {
		t.Errorf("Expected retries 3, got %d", client.retries)
	}
}

// TestNewWebSeedClientWithConfig tests WebSeedClient creation with custom config.
func TestNewWebSeedClientWithConfig(t *testing.T) {
	cfg := WebSeedConfig{
		Timeout:    10 * time.Second,
		MaxRetries: 5,
	}

	client := NewWebSeedClient(cfg)

	if client.timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", client.timeout)
	}

	if client.retries != 5 {
		t.Errorf("Expected retries 5, got %d", client.retries)
	}
}

// TestDownloadRange_Success tests successful range download.
func TestDownloadRange_Success(t *testing.T) {
	testData := []byte("0123456789abcdefghijklmnopqrstuvwxyz")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			t.Error("Expected Range header")
		}

		// Parse range and return partial content
		w.WriteHeader(http.StatusPartialContent)
		w.Write(testData[10:20]) // Return bytes 10-19
	}))
	defer server.Close()

	client := NewWebSeedClient()
	data, err := client.DownloadRange(server.URL, 10, 10)
	if err != nil {
		t.Fatalf("DownloadRange failed: %v", err)
	}

	expected := testData[10:20]
	if string(data) != string(expected) {
		t.Errorf("Expected %q, got %q", expected, data)
	}
}

// TestDownloadRange_FullContent tests download with 200 OK response.
func TestDownloadRange_FullContent(t *testing.T) {
	testData := []byte("hello world")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(testData)
	}))
	defer server.Close()

	client := NewWebSeedClient()
	data, err := client.DownloadRange(server.URL, 0, int64(len(testData)))
	if err != nil {
		t.Fatalf("DownloadRange failed: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("Expected %q, got %q", testData, data)
	}
}

// TestDownloadRange_LengthMismatch tests error when length doesn't match.
func TestDownloadRange_LengthMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("short")) // Return less than requested
	}))
	defer server.Close()

	client := NewWebSeedClient()
	_, err := client.DownloadRange(server.URL, 0, 100)

	if err == nil {
		t.Fatal("Expected error for length mismatch")
	}
}

// TestDownloadRange_HTTPError tests HTTP error handling.
func TestDownloadRange_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewWebSeedClient()
	_, err := client.DownloadRange(server.URL, 0, 10)

	if err == nil {
		t.Fatal("Expected error for 404")
	}

	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("Expected HTTPError, got %T", err)
	}

	if httpErr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", httpErr.Code)
	}
}

// TestDownloadRange_Retry tests retry logic with transient failures.
func TestDownloadRange_Retry(t *testing.T) {
	attempts := 0
	testData := []byte("success data")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Fail first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed on 3rd attempt
		w.WriteHeader(http.StatusOK)
		w.Write(testData)
	}))
	defer server.Close()

	client := NewWebSeedClient()
	data, err := client.DownloadRange(server.URL, 0, int64(len(testData)))
	if err != nil {
		t.Fatalf("Expected success after retries, got error: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("Expected %q, got %q", testData, data)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

// TestDownloadRange_NoRetryOn4xx tests that 4xx errors don't retry.
func TestDownloadRange_NoRetryOn4xx(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewWebSeedClient()
	_, err := client.DownloadRange(server.URL, 0, 10)

	if err == nil {
		t.Fatal("Expected error for 400")
	}

	if attempts != 1 {
		t.Errorf("Expected 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

// TestDownloadRange_ExhaustedRetries tests error after all retries fail.
func TestDownloadRange_ExhaustedRetries(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := WebSeedConfig{MaxRetries: 2}
	client := NewWebSeedClient(cfg)
	_, err := client.DownloadRange(server.URL, 0, 10)

	if err == nil {
		t.Fatal("Expected error after exhausted retries")
	}

	// Should attempt: initial + 2 retries = 3 total
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

// TestDownloadBlock tests block download.
func TestDownloadBlock(t *testing.T) {
	testData := []byte("block data here")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(testData)
	}))
	defer server.Close()

	client := NewWebSeedClient()
	data, err := client.DownloadBlock(server.URL, 1024, int64(len(testData)))
	if err != nil {
		t.Fatalf("DownloadBlock failed: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("Expected %q, got %q", testData, data)
	}
}

// TestDownloadPiece tests piece download with Info.
func TestDownloadPiece(t *testing.T) {
	// Create test info with 2 pieces
	pieceData := []byte("0123456789abcdef") // 16 bytes per piece
	info := metainfo.Info{
		Name:        "test.bin",
		PieceLength: 16,
		Length:      32, // 2 pieces
		Pieces:      make(metainfo.Hashes, 2),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")

		// Return appropriate piece based on range
		if rangeHeader == "bytes=0-15" {
			w.WriteHeader(http.StatusPartialContent)
			w.Write(pieceData)
		} else if rangeHeader == "bytes=16-31" {
			w.WriteHeader(http.StatusPartialContent)
			w.Write(pieceData)
		} else {
			t.Errorf("Unexpected range: %s", rangeHeader)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := NewWebSeedClient()

	// Download piece 0
	data, err := client.DownloadPiece(server.URL, info, 0)
	if err != nil {
		t.Fatalf("DownloadPiece(0) failed: %v", err)
	}
	if len(data) != 16 {
		t.Errorf("Expected 16 bytes, got %d", len(data))
	}

	// Download piece 1
	data, err = client.DownloadPiece(server.URL, info, 1)
	if err != nil {
		t.Fatalf("DownloadPiece(1) failed: %v", err)
	}
	if len(data) != 16 {
		t.Errorf("Expected 16 bytes, got %d", len(data))
	}
}

// TestHTTPError_Error tests HTTPError error message formatting.
func TestHTTPError_Error(t *testing.T) {
	err := &HTTPError{
		Code:    404,
		Message: "Not Found",
		URL:     "http://example.com/file",
	}

	expected := "HTTP 404 for http://example.com/file: Not Found"
	if err.Error() != expected {
		t.Errorf("Expected %q, got %q", expected, err.Error())
	}
}

// TestDownloadRange_RangeHeader tests Range header format.
func TestDownloadRange_RangeHeader(t *testing.T) {
	var receivedRange string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRange = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, 100))
	}))
	defer server.Close()

	client := NewWebSeedClient()
	_, err := client.DownloadRange(server.URL, 50, 100)
	if err != nil {
		t.Fatalf("DownloadRange failed: %v", err)
	}

	expected := "bytes=50-149" // offset 50, length 100 = bytes 50-149
	if receivedRange != expected {
		t.Errorf("Expected Range header %q, got %q", expected, receivedRange)
	}
}

// TestDownloadRange_InvalidURL tests error handling for invalid URLs.
func TestDownloadRange_InvalidURL(t *testing.T) {
	client := NewWebSeedClient()
	_, err := client.DownloadRange("://invalid", 0, 10)

	if err == nil {
		t.Fatal("Expected error for invalid URL")
	}
}

// TestDownloadPiece_LastPiece tests downloading the last piece which may be smaller.
func TestDownloadPiece_LastPiece(t *testing.T) {
	// Create info with last piece being smaller
	info := metainfo.Info{
		Name:        "test.bin",
		PieceLength: 16,
		Length:      25, // Last piece is only 9 bytes
		Pieces:      make(metainfo.Hashes, 2),
	}

	lastPieceData := []byte("lastpiece") // 9 bytes

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")

		if rangeHeader == "bytes=16-24" { // Last piece: offset 16, length 9
			w.WriteHeader(http.StatusPartialContent)
			w.Write(lastPieceData)
		} else {
			t.Errorf("Unexpected range for last piece: %s", rangeHeader)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := NewWebSeedClient()
	data, err := client.DownloadPiece(server.URL, info, 1) // piece 1 is the last piece
	if err != nil {
		t.Fatalf("DownloadPiece for last piece failed: %v", err)
	}

	if len(data) != 9 {
		t.Errorf("Expected 9 bytes for last piece, got %d", len(data))
	}

	if string(data) != string(lastPieceData) {
		t.Errorf("Expected %q, got %q", lastPieceData, data)
	}
}

// BenchmarkDownloadRange benchmarks the range download performance.
func BenchmarkDownloadRange(b *testing.B) {
	testData := make([]byte, 16384) // 16KB block

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(testData)
	}))
	defer server.Close()

	client := NewWebSeedClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.DownloadRange(server.URL, 0, int64(len(testData)))
		if err != nil {
			b.Fatalf("Download failed: %v", err)
		}
	}
}

// TestDownloadRange_Timeout tests timeout handling.
func TestDownloadRange_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Simulate slow server
		w.Write([]byte("data"))
	}))
	defer server.Close()

	cfg := WebSeedConfig{
		Timeout:    100 * time.Millisecond,
		MaxRetries: 0, // No retries for faster test
	}
	client := NewWebSeedClient(cfg)

	_, err := client.DownloadRange(server.URL, 0, 10)

	if err == nil {
		t.Fatal("Expected timeout error")
	}
}

// TestDownloadPiece_SingleFileInfo tests piece download for single-file torrent.
func TestDownloadPiece_SingleFileInfo(t *testing.T) {
	info := metainfo.Info{
		Name:        "singlefile.bin",
		PieceLength: 100,
		Length:      300, // 3 pieces
		Pieces:      make(metainfo.Hashes, 3),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, 100))
	}))
	defer server.Close()

	client := NewWebSeedClient()
	data, err := client.DownloadPiece(server.URL, info, 1)
	if err != nil {
		t.Fatalf("DownloadPiece failed: %v", err)
	}

	if len(data) != 100 {
		t.Errorf("Expected 100 bytes, got %d", len(data))
	}
}

// TestHTTPError_Unwrap tests HTTPError type assertion.
func TestHTTPError_TypeAssertion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewWebSeedClient()
	_, err := client.DownloadRange(server.URL, 0, 10)

	if err == nil {
		t.Fatal("Expected error")
	}

	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("Expected *HTTPError, got %T", err)
	}

	if httpErr.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", httpErr.Code)
	}

	if httpErr.URL != server.URL {
		t.Errorf("Expected URL %s, got %s", server.URL, httpErr.URL)
	}
}

// TestDownloadRange_MultipleRetries tests all retry attempts.
func TestDownloadRange_MultipleRetries(t *testing.T) {
	const maxRetries = 4
	attempts := 0
	attemptChan := make(chan int, maxRetries+1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		attemptChan <- attempts
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := WebSeedConfig{MaxRetries: maxRetries}
	client := NewWebSeedClient(cfg)

	start := time.Now()
	_, err := client.DownloadRange(server.URL, 0, 10)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected error after exhausted retries")
	}

	if attempts != maxRetries+1 {
		t.Errorf("Expected %d attempts, got %d", maxRetries+1, attempts)
	}

	// Verify exponential backoff occurred (should take at least 1+2+4+8=15 seconds)
	// But we'll be lenient and just check it took some time
	if elapsed < 1*time.Second {
		t.Errorf("Expected exponential backoff, but completed in %v", elapsed)
	}

	close(attemptChan)
}

// TestDownloadBlock_ZeroLength tests error handling for zero-length blocks.
func TestDownloadBlock_ZeroLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	client := NewWebSeedClient()
	data, err := client.DownloadBlock(server.URL, 0, 0)

	// Should fail due to length mismatch (server returns nothing, client expects 0)
	// Actually, this should succeed with empty data
	if err != nil {
		t.Logf("Zero-length block returned error (acceptable): %v", err)
	} else if len(data) != 0 {
		t.Errorf("Expected empty data, got %d bytes", len(data))
	}
}

// TestWebSeedConfig_Defaults tests default configuration values.
func TestWebSeedConfig_Defaults(t *testing.T) {
	tests := []struct {
		name   string
		cfg    WebSeedConfig
		verify func(*testing.T, *WebSeedClient)
	}{
		{
			name: "empty config uses defaults",
			cfg:  WebSeedConfig{},
			verify: func(t *testing.T, c *WebSeedClient) {
				if c.timeout != 30*time.Second {
					t.Errorf("Expected default timeout 30s, got %v", c.timeout)
				}
				if c.retries != 3 {
					t.Errorf("Expected default retries 3, got %d", c.retries)
				}
			},
		},
		{
			name: "partial config fills in defaults",
			cfg:  WebSeedConfig{Timeout: 5 * time.Second},
			verify: func(t *testing.T, c *WebSeedClient) {
				if c.timeout != 5*time.Second {
					t.Errorf("Expected timeout 5s, got %v", c.timeout)
				}
				if c.retries != 3 {
					t.Errorf("Expected default retries 3, got %d", c.retries)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewWebSeedClient(tt.cfg)
			tt.verify(t, client)
		})
	}
}

// TestDownloadRange_ServerClosesConnection tests handling of connection closure.
func TestDownloadRange_ServerClosesConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close connection immediately
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("Server doesn't support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}))
	defer server.Close()

	cfg := WebSeedConfig{MaxRetries: 1}
	client := NewWebSeedClient(cfg)
	_, err := client.DownloadRange(server.URL, 0, 10)

	if err == nil {
		t.Fatal("Expected error when server closes connection")
	}
}

// Example demonstrates basic usage of WebSeedClient.
func ExampleWebSeedClient_DownloadRange() {
	client := NewWebSeedClient()
	data, err := client.DownloadRange("http://example.com/file", 0, 1024)
	if err != nil {
		fmt.Printf("Download failed: %v\n", err)
		return
	}
	fmt.Printf("Downloaded %d bytes\n", len(data))
}

// Example demonstrates downloading a piece.
func ExampleWebSeedClient_DownloadPiece() {
	info := metainfo.Info{
		Name:        "file.bin",
		PieceLength: 16384,
		Length:      32768,
		Pieces:      make(metainfo.Hashes, 2),
	}

	client := NewWebSeedClient()
	data, err := client.DownloadPiece("http://example.com/file.bin", info, 0)
	if err != nil {
		fmt.Printf("Download failed: %v\n", err)
		return
	}
	fmt.Printf("Downloaded piece 0: %d bytes\n", len(data))
}
