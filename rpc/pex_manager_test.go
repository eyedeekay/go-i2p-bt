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
	"net"
	"testing"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	"github.com/go-i2p/go-i2p-bt/peerprotocol"
)

func TestPEXManager(t *testing.T) {
	t.Run("CreatePEXManager", func(t *testing.T) {
		pex := NewPEXManager(true)
		if !pex.IsEnabled() {
			t.Error("PEX should be enabled")
		}

		stats := pex.GetStats()
		if stats["enabled"] != true {
			t.Error("PEX should be enabled in stats")
		}
	})

	t.Run("EnableDisablePEX", func(t *testing.T) {
		pex := NewPEXManager(false)
		if pex.IsEnabled() {
			t.Error("PEX should be disabled initially")
		}

		pex.SetEnabled(true)
		if !pex.IsEnabled() {
			t.Error("PEX should be enabled after SetEnabled(true)")
		}

		pex.SetEnabled(false)
		if pex.IsEnabled() {
			t.Error("PEX should be disabled after SetEnabled(false)")
		}
	})

	t.Run("AddPeer", func(t *testing.T) {
		pex := NewPEXManager(true)
		torrentID := "test-torrent"
		ip := net.ParseIP("8.8.8.8")
		port := uint16(6881)

		pex.AddPeer(torrentID, ip, port, 0)

		count := pex.GetPeerCount(torrentID)
		if count != 1 {
			t.Errorf("Expected 1 peer, got %d", count)
		}

		peers := pex.GetAllPeers(torrentID)
		if len(peers) != 1 {
			t.Errorf("Expected 1 peer in list, got %d", len(peers))
		}

		if !peers[0].IP.Equal(ip) {
			t.Errorf("Expected IP %s, got %s", ip.String(), peers[0].IP.String())
		}

		if peers[0].Port != port {
			t.Errorf("Expected port %d, got %d", port, peers[0].Port)
		}
	})

	t.Run("RemovePeer", func(t *testing.T) {
		pex := NewPEXManager(true)
		torrentID := "test-torrent"
		ip := net.ParseIP("8.8.8.8")
		port := uint16(6881)

		pex.AddPeer(torrentID, ip, port, 0)
		if pex.GetPeerCount(torrentID) != 1 {
			t.Error("Peer should be added")
		}

		pex.RemovePeer(torrentID, ip, port)
		if pex.GetPeerCount(torrentID) != 0 {
			t.Error("Peer should be removed")
		}
	})

	t.Run("PEXMessage", func(t *testing.T) {
		pex := NewPEXManager(true)
		torrentID := "test-torrent"
		ip1 := net.ParseIP("8.8.8.8")
		ip2 := net.ParseIP("1.1.1.1")
		port := uint16(6881)

		// Add some peers
		pex.AddPeer(torrentID, ip1, port, 0)
		pex.AddPeer(torrentID, ip2, port, 0)

		// Should send PEX for first time
		if !pex.ShouldSendPEX(torrentID) {
			t.Error("Should send PEX for first time")
		}

		// Get PEX message
		msg, err := pex.GetPEXMessage(torrentID)
		if err != nil {
			t.Fatalf("Failed to get PEX message: %v", err)
		}

		if msg == nil {
			t.Fatal("PEX message should not be nil")
		}

		// Should have added peers
		if len(msg.Added) != 2 {
			t.Errorf("Expected 2 added peers, got %d", len(msg.Added))
		}

		// After getting message, should not send again immediately
		if pex.ShouldSendPEX(torrentID) {
			t.Error("Should not send PEX immediately after getting message")
		}
	})

	t.Run("ProcessPEXMessage", func(t *testing.T) {
		pex := NewPEXManager(true)
		torrentID := "test-torrent"

		// Create incoming PEX message
		ipBytes := net.ParseIP("8.8.8.8").To4()  // Public IP
		ip2Bytes := net.ParseIP("1.1.1.1").To4() // Public IP

		msg := &peerprotocol.UtPexExtendedMsg{
			Added: []peerprotocol.CompactPeer{
				{
					IP:   peerprotocol.CompactIP(ipBytes),
					Port: 6881,
				},
				{
					IP:   peerprotocol.CompactIP(ip2Bytes),
					Port: 6882,
				},
			},
			AddedF: []byte{0, 0}, // No special flags
		}

		discoveredPeers := 0
		callback := func(tID string, ip net.IP, port uint16) {
			discoveredPeers++
		}

		pex.ProcessPEXMessage(torrentID, msg, callback)

		if discoveredPeers != 2 {
			t.Errorf("Expected 2 discovered peers, got %d", discoveredPeers)
		}

		if pex.GetPeerCount(torrentID) != 2 {
			t.Errorf("Expected 2 peers after processing, got %d", pex.GetPeerCount(torrentID))
		}
	})

	t.Run("CleanupTorrent", func(t *testing.T) {
		pex := NewPEXManager(true)
		torrentID := "test-torrent"
		ip := net.ParseIP("8.8.8.8")
		port := uint16(6881)

		pex.AddPeer(torrentID, ip, port, 0)
		if pex.GetPeerCount(torrentID) != 1 {
			t.Error("Peer should be added")
		}

		pex.CleanupTorrent(torrentID)
		if pex.GetPeerCount(torrentID) != 0 {
			t.Error("All peers should be removed after cleanup")
		}
	})

	t.Run("DisabledPEX", func(t *testing.T) {
		pex := NewPEXManager(false)
		torrentID := "test-torrent"
		ip := net.ParseIP("8.8.8.8")
		port := uint16(6881)

		// Should not add peers when disabled
		pex.AddPeer(torrentID, ip, port, 0)
		if pex.GetPeerCount(torrentID) != 0 {
			t.Error("Should not add peers when PEX is disabled")
		}

		// Should not send PEX when disabled
		if pex.ShouldSendPEX(torrentID) {
			t.Error("Should not send PEX when disabled")
		}

		// Should return nil for PEX message when disabled
		msg, err := pex.GetPEXMessage(torrentID)
		if err != nil {
			t.Errorf("Should not error when disabled: %v", err)
		}
		if msg != nil {
			t.Error("Should return nil message when disabled")
		}
	})
}

func TestTorrentManagerPEX(t *testing.T) {
	t.Run("PEXIntegration", func(t *testing.T) {
		manager := createTestTorrentManager(t)

		// Check initial PEX stats
		stats := manager.GetPEXStats()
		if stats["enabled"] != true {
			t.Error("PEX should be enabled by default")
		}

		// Create test torrent
		testHash := metainfo.Hash{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
		testTorrent := &TorrentState{
			ID:       1,
			InfoHash: testHash,
			Status:   TorrentStatusDownloading,
		}

		// Add torrent to manager
		manager.mu.Lock()
		manager.torrents[1] = testTorrent
		manager.mu.Unlock()

		// Add PEX peer
		ip := net.ParseIP("8.8.8.8")
		port := uint16(6881)
		manager.AddPEXPeer(1, ip, port, 0)

		// Check if should send PEX
		if !manager.ShouldSendPEX(1) {
			t.Error("Should send PEX for torrent with new peers")
		}

		// Get PEX message
		pexData, err := manager.GetPEXMessage(1)
		if err != nil {
			t.Fatalf("Failed to get PEX message: %v", err)
		}

		if pexData == nil {
			t.Error("PEX message should not be nil")
		}

		// Process PEX message (simulate receiving from another peer)
		err = manager.ProcessPEXMessage(1, pexData)
		if err != nil {
			t.Errorf("Failed to process PEX message: %v", err)
		}
	})

	t.Run("PEXDisabledByConfig", func(t *testing.T) {
		manager := createTestTorrentManager(t)

		// Disable PEX
		config := manager.GetSessionConfig()
		config.PEXEnabled = false
		err := manager.UpdateSessionConfig(config)
		if err != nil {
			t.Fatalf("Failed to update session config: %v", err)
		}

		// Check PEX is disabled
		stats := manager.GetPEXStats()
		if stats["enabled"] != false {
			t.Error("PEX should be disabled after config update")
		}

		// Should not send PEX when disabled
		if manager.ShouldSendPEX(1) {
			t.Error("Should not send PEX when disabled")
		}
	})
}

func TestPEXProtocol(t *testing.T) {
	t.Run("EncodeDecodePEXMessage", func(t *testing.T) {
		original := &peerprotocol.UtPexExtendedMsg{
			Added: []peerprotocol.CompactPeer{
				{
					IP:   peerprotocol.CompactIP(net.ParseIP("8.8.8.8").To4()),
					Port: 6881,
				},
			},
			AddedF: []byte{0},
		}

		// Encode
		data, err := original.EncodeToBytes()
		if err != nil {
			t.Fatalf("Failed to encode PEX message: %v", err)
		}

		// Decode
		decoded := &peerprotocol.UtPexExtendedMsg{}
		err = decoded.DecodeFromPayload(data)
		if err != nil {
			t.Fatalf("Failed to decode PEX message: %v", err)
		}

		// Verify
		if len(decoded.Added) != 1 {
			t.Errorf("Expected 1 added peer, got %d", len(decoded.Added))
		}

		if !net.IP(decoded.Added[0].IP).Equal(net.ParseIP("8.8.8.8")) {
			t.Errorf("IP mismatch after encode/decode")
		}

		if decoded.Added[0].Port != 6881 {
			t.Errorf("Port mismatch after encode/decode")
		}
	})

	t.Run("PEXMessageWithDropped", func(t *testing.T) {
		msg := &peerprotocol.UtPexExtendedMsg{
			Added: []peerprotocol.CompactPeer{
				{
					IP:   peerprotocol.CompactIP(net.ParseIP("8.8.8.8").To4()),
					Port: 6881,
				},
			},
			AddedF: []byte{0},
			Dropped: []peerprotocol.CompactPeer{
				{
					IP:   peerprotocol.CompactIP(net.ParseIP("1.1.1.1").To4()),
					Port: 6882,
				},
			},
		}

		// Should encode/decode correctly
		data, err := msg.EncodeToBytes()
		if err != nil {
			t.Fatalf("Failed to encode PEX message with dropped peers: %v", err)
		}

		decoded := &peerprotocol.UtPexExtendedMsg{}
		err = decoded.DecodeFromPayload(data)
		if err != nil {
			t.Fatalf("Failed to decode PEX message with dropped peers: %v", err)
		}

		if len(decoded.Added) != 1 {
			t.Errorf("Expected 1 added peer, got %d", len(decoded.Added))
		}

		if len(decoded.Dropped) != 1 {
			t.Errorf("Expected 1 dropped peer, got %d", len(decoded.Dropped))
		}
	})
}
