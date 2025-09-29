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

package metainfo

import (
	"strings"
	"testing"
)

func TestMagnet_WebSeedSupport(t *testing.T) {
	testHash := Hash{0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12}

	t.Run("MagnetWithWebSeeds", func(t *testing.T) {
		magnet := Magnet{
			InfoHash:    testHash,
			DisplayName: "Test Torrent",
			Trackers:    []string{"http://tracker.example.com/announce"},
			WebSeeds:    []string{"http://webseed1.example.com/", "http://webseed2.example.com/"},
		}

		magnetStr := magnet.String()

		// Verify the magnet link contains WebSeed URLs (URL-encoded)
		if !strings.Contains(magnetStr, "ws=http%3A%2F%2Fwebseed1.example.com%2F") {
			t.Errorf("Expected magnet link to contain first WebSeed URL, got: %s", magnetStr)
		}
		if !strings.Contains(magnetStr, "ws=http%3A%2F%2Fwebseed2.example.com%2F") {
			t.Errorf("Expected magnet link to contain second WebSeed URL, got: %s", magnetStr)
		}

		// Verify the magnet link still contains other components
		if !strings.Contains(magnetStr, "xt=urn:btih:") {
			t.Errorf("Expected magnet link to contain info hash, got: %s", magnetStr)
		}
		if !strings.Contains(magnetStr, "dn=Test+Torrent") {
			t.Errorf("Expected magnet link to contain display name, got: %s", magnetStr)
		}
		if !strings.Contains(magnetStr, "tr=http%3A%2F%2Ftracker.example.com%2Fannounce") {
			t.Errorf("Expected magnet link to contain tracker, got: %s", magnetStr)
		}
	})

	t.Run("ParseMagnetWithWebSeeds", func(t *testing.T) {
		// Create a magnet link with WebSeeds
		magnetURI := "magnet:?xt=urn:btih:abcdef1234567890abcdef1234567890abcdef12&dn=Test+Torrent&tr=http://tracker.example.com/announce&ws=http://webseed1.example.com/&ws=http://webseed2.example.com/"

		magnet, err := ParseMagnetURI(magnetURI)
		if err != nil {
			t.Fatalf("Failed to parse magnet URI: %v", err)
		}

		// Verify WebSeeds were parsed correctly
		if len(magnet.WebSeeds) != 2 {
			t.Errorf("Expected 2 WebSeeds, got %d", len(magnet.WebSeeds))
		}
		if magnet.WebSeeds[0] != "http://webseed1.example.com/" {
			t.Errorf("Expected first WebSeed to be 'http://webseed1.example.com/', got '%s'", magnet.WebSeeds[0])
		}
		if magnet.WebSeeds[1] != "http://webseed2.example.com/" {
			t.Errorf("Expected second WebSeed to be 'http://webseed2.example.com/', got '%s'", magnet.WebSeeds[1])
		}

		// Verify other components were parsed correctly
		if magnet.DisplayName != "Test Torrent" {
			t.Errorf("Expected display name to be 'Test Torrent', got '%s'", magnet.DisplayName)
		}
		if len(magnet.Trackers) != 1 || magnet.Trackers[0] != "http://tracker.example.com/announce" {
			t.Errorf("Expected tracker to be parsed correctly, got %v", magnet.Trackers)
		}
	})

	t.Run("EmptyWebSeeds", func(t *testing.T) {
		magnet := Magnet{
			InfoHash:    testHash,
			DisplayName: "Test Torrent",
			WebSeeds:    []string{}, // Empty WebSeeds
		}

		magnetStr := magnet.String()

		// Verify the magnet link doesn't contain ws parameter when no WebSeeds
		if strings.Contains(magnetStr, "ws=") {
			t.Errorf("Expected magnet link to not contain ws parameter when no WebSeeds, got: %s", magnetStr)
		}
	})

	t.Run("RoundTripWithWebSeeds", func(t *testing.T) {
		original := Magnet{
			InfoHash:    testHash,
			DisplayName: "Test Torrent",
			Trackers:    []string{"http://tracker.example.com/announce"},
			WebSeeds:    []string{"http://webseed1.example.com/", "http://webseed2.example.com/"},
		}

		// Convert to string and back
		magnetStr := original.String()
		parsed, err := ParseMagnetURI(magnetStr)
		if err != nil {
			t.Fatalf("Failed to parse magnet URI: %v", err)
		}

		// Verify all fields are preserved
		if parsed.InfoHash != original.InfoHash {
			t.Errorf("InfoHash mismatch after round trip")
		}
		if parsed.DisplayName != original.DisplayName {
			t.Errorf("DisplayName mismatch after round trip")
		}
		if len(parsed.Trackers) != len(original.Trackers) {
			t.Errorf("Trackers length mismatch after round trip")
		}
		if len(parsed.WebSeeds) != len(original.WebSeeds) {
			t.Errorf("WebSeeds length mismatch after round trip")
		}
		for i, ws := range original.WebSeeds {
			if parsed.WebSeeds[i] != ws {
				t.Errorf("WebSeed %d mismatch after round trip: expected '%s', got '%s'", i, ws, parsed.WebSeeds[i])
			}
		}
	})
}

func TestMetaInfo_MagnetWithWebSeeds(t *testing.T) {
	t.Run("MetaInfoWithURLList", func(t *testing.T) {
		// Create MetaInfo with URLList (WebSeeds)
		mi := MetaInfo{
			Announce:     "http://tracker.example.com/announce",
			AnnounceList: [][]string{{"http://tracker.example.com/announce"}},
			URLList:      URLList{"http://webseed1.example.com/", "http://webseed2.example.com/"},
		}

		// Set InfoBytes for a test torrent
		mi.InfoBytes = []byte(`d4:name12:Test Torrente`) // Simple bencode

		magnet := mi.Magnet("", Hash{})

		// Verify WebSeeds are included in magnet
		if len(magnet.WebSeeds) != 2 {
			t.Errorf("Expected 2 WebSeeds in magnet, got %d", len(magnet.WebSeeds))
		}
		if magnet.WebSeeds[0] != "http://webseed1.example.com/" {
			t.Errorf("Expected first WebSeed to be 'http://webseed1.example.com/', got '%s'", magnet.WebSeeds[0])
		}
		if magnet.WebSeeds[1] != "http://webseed2.example.com/" {
			t.Errorf("Expected second WebSeed to be 'http://webseed2.example.com/', got '%s'", magnet.WebSeeds[1])
		}

		// Verify magnet string contains WebSeeds
		magnetStr := magnet.String()
		if !strings.Contains(magnetStr, "ws=http%3A%2F%2Fwebseed1.example.com%2F") {
			t.Errorf("Expected magnet string to contain first WebSeed, got: %s", magnetStr)
		}
		if !strings.Contains(magnetStr, "ws=http%3A%2F%2Fwebseed2.example.com%2F") {
			t.Errorf("Expected magnet string to contain second WebSeed, got: %s", magnetStr)
		}
	})

	t.Run("MetaInfoWithoutURLList", func(t *testing.T) {
		// Create MetaInfo without URLList
		mi := MetaInfo{
			Announce:     "http://tracker.example.com/announce",
			AnnounceList: [][]string{{"http://tracker.example.com/announce"}},
		}

		// Set InfoBytes for a test torrent
		mi.InfoBytes = []byte(`d4:name12:Test Torrente`)

		magnet := mi.Magnet("", Hash{})

		// Verify no WebSeeds are included
		if len(magnet.WebSeeds) != 0 {
			t.Errorf("Expected 0 WebSeeds in magnet, got %d", len(magnet.WebSeeds))
		}

		// Verify magnet string doesn't contain ws parameter
		magnetStr := magnet.String()
		if strings.Contains(magnetStr, "ws=") {
			t.Errorf("Expected magnet string to not contain ws parameter, got: %s", magnetStr)
		}
	})
}
