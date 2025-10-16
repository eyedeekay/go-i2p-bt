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
	"crypto/sha1"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// createTestInfo creates a test Info structure with valid piece hashes.
func createTestInfo(pieceCount int, pieceLength int64) metainfo.Info {
	pieces := make(metainfo.Hashes, pieceCount)

	// Generate valid hashes for each piece
	for i := 0; i < pieceCount; i++ {
		data := make([]byte, pieceLength)
		for j := range data {
			data[j] = byte(i) // Fill with piece index
		}
		hash := sha1.Sum(data)
		pieces[i] = metainfo.NewHash(hash[:])
	}

	return metainfo.Info{
		Name:        "test.bin",
		PieceLength: pieceLength,
		Length:      pieceLength * int64(pieceCount),
		Pieces:      pieces,
	}
}

// TestNewDownloadHandler tests DownloadHandler creation.
func TestNewDownloadHandler(t *testing.T) {
	info := createTestInfo(2, 16)
	urlList := metainfo.URLList{"http://example.com/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
	})

	if handler == nil {
		t.Fatal("NewDownloadHandler returned nil")
	}

	if handler.client == nil {
		t.Error("Expected default client to be created")
	}
}

// TestNewDownloadHandler_WithCustomClient tests handler with custom client.
func TestNewDownloadHandler_WithCustomClient(t *testing.T) {
	info := createTestInfo(2, 16)
	urlList := metainfo.URLList{"http://example.com/"}
	customClient := NewWebSeedClient()

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
		Client:  customClient,
	})

	if handler.client != customClient {
		t.Error("Expected custom client to be used")
	}
}

// TestDownloadPiece_Success tests successful piece download and verification.
func TestDownloadPiece_Success(t *testing.T) {
	pieceLength := int64(16)
	info := createTestInfo(2, pieceLength)

	// Generate piece 0 data
	pieceData := make([]byte, pieceLength)
	for i := range pieceData {
		pieceData[i] = 0 // Matches createTestInfo
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(pieceData)
	}))
	defer server.Close()

	urlList := metainfo.URLList{server.URL + "/"}

	completeCalled := false
	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
		OnComplete: func(pieceIndex int, data []byte) error {
			completeCalled = true
			if pieceIndex != 0 {
				t.Errorf("Expected piece index 0, got %d", pieceIndex)
			}
			if len(data) != int(pieceLength) {
				t.Errorf("Expected %d bytes, got %d", pieceLength, len(data))
			}
			return nil
		},
	})

	err := handler.DownloadPiece(0)
	if err != nil {
		t.Fatalf("DownloadPiece failed: %v", err)
	}

	if !completeCalled {
		t.Error("OnComplete callback was not called")
	}
}

// TestDownloadPiece_HashMismatch tests piece rejection on hash mismatch.
func TestDownloadPiece_HashMismatch(t *testing.T) {
	info := createTestInfo(2, 16)

	// Return wrong data (different from what createTestInfo expects)
	wrongData := make([]byte, 16)
	for i := range wrongData {
		wrongData[i] = 255 // Definitely wrong
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(wrongData)
	}))
	defer server.Close()

	urlList := metainfo.URLList{server.URL + "/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
	})

	err := handler.DownloadPiece(0)
	if err == nil {
		t.Fatal("Expected error due to hash mismatch")
	}
}

// TestDownloadPiece_MultipleURLs tests fallback to second URL on failure.
func TestDownloadPiece_MultipleURLs(t *testing.T) {
	pieceLength := int64(16)
	info := createTestInfo(2, pieceLength)

	pieceData := make([]byte, pieceLength)
	for i := range pieceData {
		pieceData[i] = 0
	}

	// First server fails
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server1.Close()

	// Second server succeeds
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(pieceData)
	}))
	defer server2.Close()

	urlList := metainfo.URLList{server1.URL + "/", server2.URL + "/"}

	completeCalled := false
	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
		OnComplete: func(pieceIndex int, data []byte) error {
			completeCalled = true
			return nil
		},
	})

	err := handler.DownloadPiece(0)
	if err != nil {
		t.Fatalf("DownloadPiece failed: %v", err)
	}

	if !completeCalled {
		t.Error("OnComplete callback was not called")
	}
}

// TestDownloadPiece_AllURLsFail tests error when all URLs fail.
func TestDownloadPiece_AllURLsFail(t *testing.T) {
	info := createTestInfo(2, 16)

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server2.Close()

	urlList := metainfo.URLList{server1.URL + "/", server2.URL + "/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
	})

	err := handler.DownloadPiece(0)
	if err == nil {
		t.Fatal("Expected error when all URLs fail")
	}
}

// TestDownloadPiece_OnCompleteError tests error handling from OnComplete callback.
func TestDownloadPiece_OnCompleteError(t *testing.T) {
	pieceLength := int64(16)
	info := createTestInfo(2, pieceLength)

	pieceData := make([]byte, pieceLength)
	for i := range pieceData {
		pieceData[i] = 0
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(pieceData)
	}))
	defer server.Close()

	urlList := metainfo.URLList{server.URL + "/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
		OnComplete: func(pieceIndex int, data []byte) error {
			return http.ErrAbortHandler // Return an error
		},
	})

	err := handler.DownloadPiece(0)
	if err == nil {
		t.Fatal("Expected error from OnComplete callback")
	}
}

// TestDownloadPiece_NoOnComplete tests download without OnComplete callback.
func TestDownloadPiece_NoOnComplete(t *testing.T) {
	pieceLength := int64(16)
	info := createTestInfo(2, pieceLength)

	pieceData := make([]byte, pieceLength)
	for i := range pieceData {
		pieceData[i] = 0
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(pieceData)
	}))
	defer server.Close()

	urlList := metainfo.URLList{server.URL + "/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
		// No OnComplete callback
	})

	err := handler.DownloadPiece(0)
	if err != nil {
		t.Fatalf("DownloadPiece failed: %v", err)
	}
}

// TestVerifyPiece tests piece hash verification.
func TestVerifyPiece(t *testing.T) {
	pieceLength := int64(16)
	info := createTestInfo(2, pieceLength)

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: metainfo.URLList{"http://example.com/"},
	})

	// Test valid piece
	validData := make([]byte, pieceLength)
	for i := range validData {
		validData[i] = 0 // Matches createTestInfo
	}
	if !handler.verifyPiece(0, validData) {
		t.Error("Expected valid piece to pass verification")
	}

	// Test invalid piece
	invalidData := make([]byte, pieceLength)
	for i := range invalidData {
		invalidData[i] = 255 // Wrong data
	}
	if handler.verifyPiece(0, invalidData) {
		t.Error("Expected invalid piece to fail verification")
	}
}

// TestGetFileName tests filename extraction for single-file torrents.
func TestGetFileName(t *testing.T) {
	// Single file torrent
	info := metainfo.Info{
		Name:        "testfile.bin",
		PieceLength: 16,
		Length:      32,
		Pieces:      make(metainfo.Hashes, 2),
	}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: metainfo.URLList{"http://example.com/"},
	})

	fileName := handler.getFileName()
	if fileName != "testfile.bin" {
		t.Errorf("Expected 'testfile.bin', got '%s'", fileName)
	}
}

// TestGetFileName_MultiFile tests filename for multi-file torrents.
func TestGetFileName_MultiFile(t *testing.T) {
	// Multi-file torrent
	info := metainfo.Info{
		Name:        "testdir",
		PieceLength: 16,
		Files: []metainfo.File{
			{Paths: []string{"file1.txt"}, Length: 16},
			{Paths: []string{"file2.txt"}, Length: 16},
		},
		Pieces: make(metainfo.Hashes, 2),
	}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: metainfo.URLList{"http://example.com/"},
	})

	fileName := handler.getFileName()
	if fileName != "" {
		t.Errorf("Expected empty string for multi-file, got '%s'", fileName)
	}
}

// TestDownloadHandler_LastPiece tests downloading the last (potentially smaller) piece.
func TestDownloadHandler_LastPiece(t *testing.T) {
	// Create info with last piece being smaller
	info := metainfo.Info{
		Name:        "test.bin",
		PieceLength: 16,
		Length:      25, // 2 pieces: 16 + 9
		Pieces:      make(metainfo.Hashes, 2),
	}

	// Generate proper hash for last piece (9 bytes)
	lastPieceData := make([]byte, 9)
	for i := range lastPieceData {
		lastPieceData[i] = 1
	}
	hash := sha1.Sum(lastPieceData)
	info.Pieces[1] = metainfo.NewHash(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(lastPieceData)
	}))
	defer server.Close()

	urlList := metainfo.URLList{server.URL + "/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
	})

	err := handler.DownloadPiece(1)
	if err != nil {
		t.Fatalf("DownloadPiece for last piece failed: %v", err)
	}
}

// TestDownloadPiece_URLConstruction tests URL construction with URLList.FullURL.
func TestDownloadPiece_URLConstruction(t *testing.T) {
	pieceLength := int64(16)
	info := createTestInfo(2, pieceLength)

	pieceData := make([]byte, pieceLength)
	for i := range pieceData {
		pieceData[i] = 0
	}

	var requestedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.WriteHeader(http.StatusPartialContent)
		w.Write(pieceData)
	}))
	defer server.Close()

	// Test URL with trailing slash
	urlList := metainfo.URLList{server.URL + "/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
	})

	err := handler.DownloadPiece(0)
	if err != nil {
		t.Fatalf("DownloadPiece failed: %v", err)
	}

	// Should append filename to URL
	expectedPath := "/test.bin"
	if requestedURL != expectedPath {
		t.Errorf("Expected URL path '%s', got '%s'", expectedPath, requestedURL)
	}
}

// TestDownloadHandler_EmptyURLList tests error with empty URL list.
func TestDownloadHandler_EmptyURLList(t *testing.T) {
	info := createTestInfo(2, 16)
	urlList := metainfo.URLList{} // Empty

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
	})

	err := handler.DownloadPiece(0)
	if err == nil {
		t.Fatal("Expected error with empty URL list")
	}
}

// BenchmarkDownloadPiece benchmarks piece download and verification.
func BenchmarkDownloadPiece(b *testing.B) {
	pieceLength := int64(16384) // 16KB
	info := createTestInfo(1, pieceLength)

	pieceData := make([]byte, pieceLength)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		w.Write(pieceData)
	}))
	defer server.Close()

	urlList := metainfo.URLList{server.URL + "/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := handler.DownloadPiece(0)
		if err != nil {
			b.Fatalf("DownloadPiece failed: %v", err)
		}
	}
}

// TestDownloadPiece_ConcurrentRequests tests handling of concurrent piece downloads.
func TestDownloadPiece_ConcurrentRequests(t *testing.T) {
	pieceLength := int64(16)
	pieceCount := 10
	info := createTestInfo(pieceCount, pieceLength)

	// Create piece data for each piece
	pieceDataMap := make(map[int][]byte)
	for i := 0; i < pieceCount; i++ {
		data := make([]byte, pieceLength)
		for j := range data {
			data[j] = byte(i)
		}
		pieceDataMap[i] = data
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse range to determine piece index
		w.WriteHeader(http.StatusPartialContent)
		// Return appropriate piece (simplified - just return piece 0 data)
		w.Write(pieceDataMap[0])
	}))
	defer server.Close()

	urlList := metainfo.URLList{server.URL + "/"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
	})

	// Download multiple pieces concurrently
	errChan := make(chan error, pieceCount)
	for i := 0; i < pieceCount; i++ {
		go func(pieceIndex int) {
			errChan <- handler.DownloadPiece(pieceIndex)
		}(i)
	}

	// Collect results
	for i := 0; i < pieceCount; i++ {
		err := <-errChan
		if err != nil {
			t.Logf("Piece %d failed (expected for simplified test): %v", i, err)
		}
	}
}

// Example demonstrates basic usage of DownloadHandler.
func ExampleDownloadHandler_DownloadPiece() {
	info := metainfo.Info{
		Name:        "file.bin",
		PieceLength: 16384,
		Length:      32768,
		Pieces:      make(metainfo.Hashes, 2),
	}

	urlList := metainfo.URLList{"http://example.com/file.bin"}

	handler := NewDownloadHandler(DownloadHandlerConfig{
		Info:    info,
		URLList: urlList,
		OnComplete: func(pieceIndex int, data []byte) error {
			// Handle completed piece
			return nil
		},
	})

	// Download piece 0
	err := handler.DownloadPiece(0)
	if err != nil {
		// Handle error
		return
	}
}
