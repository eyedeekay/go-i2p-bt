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

package peerprotocol

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/bencode"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// TestPEXIntegration_FullFlow demonstrates a complete PEX exchange between two peers
func TestPEXIntegration_FullFlow(t *testing.T) {
	// Setup: Create two mock peer connections
	peer1 := createMockPeerConn(t, "192.168.1.1:6881")
	peer2 := createMockPeerConn(t, "192.168.1.2:6882")

	// Both peers advertise PEX support in extended handshake
	peer1.ExtendedHandshakeMsg.M = map[string]uint8{
		"ut_metadata": 1,
		"i2p_pex":     2,
	}
	peer2.ExtendedHandshakeMsg.M = map[string]uint8{
		"ut_metadata": 1,
		"i2p_pex":     2,
	}

	// Create PEX handlers for both peers
	handler1 := NewDefaultPEXHandler(60 * time.Second)
	handler2 := NewDefaultPEXHandler(60 * time.Second)

	// Simulate peer1 discovering new peers and wanting to share them with peer2
	peer1Key := peer2.RemoteAddr().String() // Key for peer2 from peer1's perspective
	handler1.AddPeer(peer1Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("192.168.1.10").To4()),
		Port: 6890,
	})
	handler1.AddPeer(peer1Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("192.168.1.11").To4()),
		Port: 6891,
	})

	// Generate PEX message from peer1
	pexMsg, err := handler1.GeneratePEXMessage(peer2)
	if err != nil {
		t.Fatalf("Failed to generate PEX message: %v", err)
	}
	if pexMsg == nil {
		t.Fatal("Expected PEX message, got nil")
	}

	// Verify message content
	if len(pexMsg.Added) != 2 {
		t.Errorf("Expected 2 added peers, got %d", len(pexMsg.Added))
	}
	if len(pexMsg.AddedF) != 2 {
		t.Errorf("Expected 2 added flags, got %d", len(pexMsg.AddedF))
	}

	// Encode the message as it would be sent over the wire
	payload, err := bencode.EncodeBytes(pexMsg)
	if err != nil {
		t.Fatalf("Failed to encode PEX message: %v", err)
	}

	// Simulate peer2 receiving the PEX message
	err = HandlePEXPayload(peer1, payload, handler2)
	if err != nil {
		t.Fatalf("Failed to handle PEX payload: %v", err)
	}

	// Verify the received message can be decoded correctly
	var receivedMsg UtPexExtendedMsg
	err = bencode.DecodeBytes(payload, &receivedMsg)
	if err != nil {
		t.Fatalf("Failed to decode received message: %v", err)
	}

	// Check that received peers match sent peers
	if len(receivedMsg.Added) != 2 {
		t.Errorf("Received message has %d peers, expected 2", len(receivedMsg.Added))
	}

	// Verify IP addresses
	if receivedMsg.Added[0].IP.String() != "192.168.1.10" {
		t.Errorf("First peer IP is %s, expected 192.168.1.10", receivedMsg.Added[0].IP.String())
	}
	if receivedMsg.Added[1].IP.String() != "192.168.1.11" {
		t.Errorf("Second peer IP is %s, expected 192.168.1.11", receivedMsg.Added[1].IP.String())
	}

	// Verify ports
	if receivedMsg.Added[0].Port != 6890 {
		t.Errorf("First peer port is %d, expected 6890", receivedMsg.Added[0].Port)
	}
	if receivedMsg.Added[1].Port != 6891 {
		t.Errorf("Second peer port is %d, expected 6891", receivedMsg.Added[1].Port)
	}
}

// TestPEXIntegration_BidirectionalExchange tests PEX exchange in both directions
func TestPEXIntegration_BidirectionalExchange(t *testing.T) {
	peer1 := createMockPeerConn(t, "192.168.1.1:6881")
	peer2 := createMockPeerConn(t, "192.168.1.2:6882")

	// Both peers advertise PEX support
	peer1.ExtendedHandshakeMsg.M = map[string]uint8{"i2p_pex": 2}
	peer2.ExtendedHandshakeMsg.M = map[string]uint8{"i2p_pex": 2}

	handler1 := NewDefaultPEXHandler(60 * time.Second)
	handler2 := NewDefaultPEXHandler(60 * time.Second)

	// Peer1 has some peers to share
	peer1Key := peer2.RemoteAddr().String()
	handler1.AddPeer(peer1Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("10.0.0.1").To4()),
		Port: 6881,
	})

	// Peer2 has different peers to share
	peer2Key := peer1.RemoteAddr().String()
	handler2.AddPeer(peer2Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("10.0.0.2").To4()),
		Port: 6882,
	})

	// Exchange 1: peer1 -> peer2
	msg1, err := handler1.GeneratePEXMessage(peer2)
	if err != nil || msg1 == nil {
		t.Fatalf("Failed to generate message from peer1: %v", err)
	}

	payload1, _ := bencode.EncodeBytes(msg1)
	err = HandlePEXPayload(peer1, payload1, handler2)
	if err != nil {
		t.Fatalf("Peer2 failed to handle message from peer1: %v", err)
	}

	// Exchange 2: peer2 -> peer1
	msg2, err := handler2.GeneratePEXMessage(peer1)
	if err != nil || msg2 == nil {
		t.Fatalf("Failed to generate message from peer2: %v", err)
	}

	payload2, _ := bencode.EncodeBytes(msg2)
	err = HandlePEXPayload(peer2, payload2, handler1)
	if err != nil {
		t.Fatalf("Peer1 failed to handle message from peer2: %v", err)
	}

	// Both exchanges should have succeeded
	t.Log("Bidirectional PEX exchange completed successfully")
}

// TestPEXIntegration_DroppedPeers tests PEX with dropped peers
func TestPEXIntegration_DroppedPeers(t *testing.T) {
	peer1 := createMockPeerConn(t, "192.168.1.1:6881")
	peer2 := createMockPeerConn(t, "192.168.1.2:6882")

	peer1.ExtendedHandshakeMsg.M = map[string]uint8{"i2p_pex": 2}
	peer2.ExtendedHandshakeMsg.M = map[string]uint8{"i2p_pex": 2}

	handler1 := NewDefaultPEXHandler(60 * time.Second)
	handler2 := NewDefaultPEXHandler(60 * time.Second)

	peer1Key := peer2.RemoteAddr().String()

	// Peer1 adds some peers
	handler1.AddPeer(peer1Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("192.168.1.10").To4()),
		Port: 6890,
	})

	// Then drops some peers
	handler1.DropPeer(peer1Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("192.168.1.99").To4()),
		Port: 6899,
	})

	// Generate and send message
	msg, err := handler1.GeneratePEXMessage(peer2)
	if err != nil || msg == nil {
		t.Fatalf("Failed to generate PEX message: %v", err)
	}

	// Check both added and dropped peers are included
	if len(msg.Added) != 1 {
		t.Errorf("Expected 1 added peer, got %d", len(msg.Added))
	}
	if len(msg.Dropped) != 1 {
		t.Errorf("Expected 1 dropped peer, got %d", len(msg.Dropped))
	}

	// Verify the message can be processed
	payload, _ := bencode.EncodeBytes(msg)
	err = HandlePEXPayload(peer1, payload, handler2)
	if err != nil {
		t.Fatalf("Failed to handle PEX message with dropped peers: %v", err)
	}
}

// TestPEXIntegration_IPv6Support tests PEX with IPv6 addresses
func TestPEXIntegration_IPv6Support(t *testing.T) {
	peer1 := createMockPeerConn(t, "192.168.1.1:6881")
	peer2 := createMockPeerConn(t, "192.168.1.2:6882")

	peer1.ExtendedHandshakeMsg.M = map[string]uint8{"i2p_pex": 2}
	peer2.ExtendedHandshakeMsg.M = map[string]uint8{"i2p_pex": 2}

	handler1 := NewDefaultPEXHandler(60 * time.Second)
	handler2 := NewDefaultPEXHandler(60 * time.Second)

	peer1Key := peer2.RemoteAddr().String()

	// Add IPv6 peers
	handler1.AddPeer(peer1Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("2001:db8::1")),
		Port: 6881,
	})
	handler1.AddPeer(peer1Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("2001:db8::2")),
		Port: 6882,
	})

	// Generate message
	msg, err := handler1.GeneratePEXMessage(peer2)
	if err != nil || msg == nil {
		t.Fatalf("Failed to generate PEX message: %v", err)
	}

	// Verify IPv6 peers are in Added6
	if len(msg.Added6) != 2 {
		t.Errorf("Expected 2 IPv6 peers, got %d", len(msg.Added6))
	}
	if len(msg.Added6F) != 2 {
		t.Errorf("Expected 2 IPv6 flags, got %d", len(msg.Added6F))
	}

	// Verify empty IPv4 fields
	if len(msg.Added) != 0 {
		t.Errorf("Expected 0 IPv4 peers, got %d", len(msg.Added))
	}

	// Process message
	payload, _ := bencode.EncodeBytes(msg)
	err = HandlePEXPayload(peer1, payload, handler2)
	if err != nil {
		t.Fatalf("Failed to handle IPv6 PEX message: %v", err)
	}
}

// TestPEXIntegration_NoSupport tests behavior when peer doesn't support PEX
func TestPEXIntegration_NoSupport(t *testing.T) {
	peer1 := createMockPeerConn(t, "192.168.1.1:6881")
	peer2 := createMockPeerConn(t, "192.168.1.2:6882")

	// Peer1 supports PEX
	peer1.ExtendedHandshakeMsg.M = map[string]uint8{"i2p_pex": 2}

	// Peer2 does NOT support PEX (no i2p_pex in M map)
	peer2.ExtendedHandshakeMsg.M = map[string]uint8{"ut_metadata": 1}

	handler1 := NewDefaultPEXHandler(60 * time.Second)

	peer1Key := peer2.RemoteAddr().String()
	handler1.AddPeer(peer1Key, CompactPeer{
		IP:   CompactIP(net.ParseIP("192.168.1.10").To4()),
		Port: 6890,
	})

	// Try to send PEX message - should fail gracefully
	err := SendPEXMessage(peer2, handler1)
	if err != nil {
		t.Fatalf("SendPEXMessage should not error when peer doesn't support PEX: %v", err)
	}

	// The function should return nil (no message sent) when peer doesn't support PEX
	t.Log("Correctly handled peer without PEX support")
}

// createMockPeerConn creates a mock PeerConn for testing
func createMockPeerConn(t *testing.T, addr string) *PeerConn {
	t.Helper()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("Invalid address %s: %v", addr, err)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		t.Fatalf("Invalid IP in address: %s", host)
	}

	// Parse port
	var port int
	if _, err := bytes.NewBufferString(portStr).ReadByte(); err == nil {
		for _, c := range portStr {
			if c < '0' || c > '9' {
				t.Fatalf("Invalid port: %s", portStr)
			}
			port = port*10 + int(c-'0')
		}
	}

	conn := &PeerConn{
		Conn:                 &mockNetConn{addr: &net.TCPAddr{IP: ip, Port: port}},
		InfoHash:             metainfo.NewRandomHash(),
		ID:                   metainfo.NewRandomHash(),
		ExtendedHandshakeMsg: ExtendedHandshakeMsg{M: make(map[string]uint8)},
	}
	conn.ExtBits.Set(ExtensionBitExtended)

	return conn
}
