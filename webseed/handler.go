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
	"fmt"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// DownloadHandler manages downloading from web seed URLs.
// It downloads pieces from HTTP/FTP servers and verifies their integrity.
type DownloadHandler struct {
	client     *WebSeedClient
	info       metainfo.Info
	urlList    metainfo.URLList
	onComplete func(pieceIndex int, data []byte) error
}

// DownloadHandlerConfig configures the DownloadHandler.
type DownloadHandlerConfig struct {
	// Info contains the torrent metadata.
	Info metainfo.Info

	// URLList contains the web seed URLs from the torrent.
	URLList metainfo.URLList

	// OnComplete is called when a piece is successfully downloaded and verified.
	OnComplete func(pieceIndex int, data []byte) error

	// Client is the WebSeedClient to use. If nil, a default client is created.
	Client *WebSeedClient
}

// NewDownloadHandler creates a new DownloadHandler.
func NewDownloadHandler(cfg DownloadHandlerConfig) *DownloadHandler {
	client := cfg.Client
	if client == nil {
		client = NewWebSeedClient()
	}

	return &DownloadHandler{
		client:     client,
		info:       cfg.Info,
		urlList:    cfg.URLList,
		onComplete: cfg.OnComplete,
	}
}

// DownloadPiece downloads a specific piece from web seeds.
// It tries all available URLs until one succeeds.
// Returns an error if all URLs fail.
func (h *DownloadHandler) DownloadPiece(pieceIndex int) error {
	// Try each URL in the list
	for urlIndex := range h.urlList {
		url := h.urlList.FullURL(urlIndex, h.getFileName())

		data, err := h.client.DownloadPiece(url, h.info, pieceIndex)
		if err != nil {
			continue // Try next URL
		}

		// Verify piece hash
		if !h.verifyPiece(pieceIndex, data) {
			continue // Try next URL
		}

		// Call completion callback
		if h.onComplete != nil {
			if err := h.onComplete(pieceIndex, data); err != nil {
				return fmt.Errorf("onComplete callback: %w", err)
			}
		}

		return nil
	}

	return fmt.Errorf("failed to download piece %d from all URLs", pieceIndex)
}

// verifyPiece checks if the downloaded piece matches its expected hash.
func (h *DownloadHandler) verifyPiece(pieceIndex int, data []byte) bool {
	expectedHash := h.info.Piece(pieceIndex).Hash()
	actualHash := sha1.Sum(data)
	return expectedHash == metainfo.NewHash(actualHash[:])
}

// getFileName returns the file name for single-file torrents.
// For multi-file torrents, this would need to be enhanced.
func (h *DownloadHandler) getFileName() string {
	if h.info.IsDir() {
		// For multi-file torrents, BEP 19 requires special handling
		// This is a simplified implementation
		return ""
	}
	return h.info.Name
}
