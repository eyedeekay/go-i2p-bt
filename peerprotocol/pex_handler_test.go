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
	"net"
	"testing"
	"time"
)

// TestDefaultPEXHandler_HandleMessage tests basic PEX message handling
func TestDefaultPEXHandler_HandleMessage(t *testing.T) {
	handler := NewDefaultPEXHandler(60 * time.Second)

	// Create a test peer connection
	conn := &PeerConn{}

	// Test valid message with IPv4 peers
	msg := UtPexExtendedMsg{
		Added: []CompactPeer{
			{IP: CompactIP(net.ParseIP("192.168.1.1").To4()), Port: 6881},
			{IP: CompactIP(net.ParseIP("192.168.1.2").To4()), Port: 6882},
		},
		AddedF: []byte{0, 0},
	}

	err := handler.HandlePEXMessage(conn, msg)
	if err != nil {
		t.Fatalf("HandlePEXMessage failed: %v", err)
	}

	// Test invalid message with mismatched added/flags
	invalidMsg := UtPexExtendedMsg{
		Added:  []CompactPeer{{IP: CompactIP(net.ParseIP("192.168.1.1").To4()), Port: 6881}},
		AddedF: []byte{0, 0}, // Wrong count
	}

	err = handler.HandlePEXMessage(conn, invalidMsg)
	if err == nil {
		t.Fatal("Expected error for mismatched added/flags, got nil")
	}
}

// TestDefaultPEXHandler_HandleMessage_IPv6 tests PEX message handling with IPv6 peers
func TestDefaultPEXHandler_HandleMessage_IPv6(t *testing.T) {
	handler := NewDefaultPEXHandler(60 * time.Second)

	conn := &PeerConn{}

	// Test valid message with IPv6 peers
	msg := UtPexExtendedMsg{
		Added6: []CompactPeer{
			{IP: CompactIP(net.ParseIP("2001:db8::1")), Port: 6881},
			{IP: CompactIP(net.ParseIP("2001:db8::2")), Port: 6882},
		},
		Added6F: []byte{0, 0},
	}

	err := handler.HandlePEXMessage(conn, msg)
	if err != nil {
		t.Fatalf("HandlePEXMessage failed: %v", err)
	}

	// Test invalid message with mismatched added6/flags
	invalidMsg := UtPexExtendedMsg{
		Added6:  []CompactPeer{{IP: CompactIP(net.ParseIP("2001:db8::1")), Port: 6881}},
		Added6F: []byte{0, 0}, // Wrong count
	}

	err = handler.HandlePEXMessage(conn, invalidMsg)
	if err == nil {
		t.Fatal("Expected error for mismatched added6/flags, got nil")
	}
}

// TestDefaultPEXHandler_GenerateMessage tests PEX message generation
func TestDefaultPEXHandler_GenerateMessage(t *testing.T) {
	handler := NewDefaultPEXHandler(60 * time.Second)

	// Create a mock connection
	conn := &PeerConn{}
	conn.Conn = &mockNetConn{addr: &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 6881}}

	// No peers added yet - should return nil
	msg, err := handler.GeneratePEXMessage(conn)
	if err != nil {
		t.Fatalf("GeneratePEXMessage failed: %v", err)
	}
	if msg != nil {
		t.Fatal("Expected nil message when no peers added")
	}

	// Add some peers
	peerKey := conn.RemoteAddr().String()
	handler.AddPeer(peerKey, CompactPeer{IP: CompactIP(net.ParseIP("192.168.1.1").To4()), Port: 6881})
	handler.AddPeer(peerKey, CompactPeer{IP: CompactIP(net.ParseIP("192.168.1.2").To4()), Port: 6882})

	// Should now generate a message
	msg, err = handler.GeneratePEXMessage(conn)
	if err != nil {
		t.Fatalf("GeneratePEXMessage failed: %v", err)
	}
	if msg == nil {
		t.Fatal("Expected non-nil message after adding peers")
	}

	// Check message content
	if len(msg.Added) != 2 {
		t.Errorf("Expected 2 added peers, got %d", len(msg.Added))
	}
	if len(msg.AddedF) != 2 {
		t.Errorf("Expected 2 added flags, got %d", len(msg.AddedF))
	}

	// Calling again immediately should return nil (interval not elapsed)
	msg, err = handler.GeneratePEXMessage(conn)
	if err != nil {
		t.Fatalf("GeneratePEXMessage failed: %v", err)
	}
	if msg != nil {
		t.Fatal("Expected nil message when called too soon")
	}
}

// TestDefaultPEXHandler_GenerateMessage_IPv6 tests PEX message generation with IPv6 peers
func TestDefaultPEXHandler_GenerateMessage_IPv6(t *testing.T) {
	handler := NewDefaultPEXHandler(60 * time.Second)

	conn := &PeerConn{}
	conn.Conn = &mockNetConn{addr: &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 6881}}

	peerKey := conn.RemoteAddr().String()

	// Add IPv6 peers
	handler.AddPeer(peerKey, CompactPeer{IP: CompactIP(net.ParseIP("2001:db8::1")), Port: 6881})
	handler.AddPeer(peerKey, CompactPeer{IP: CompactIP(net.ParseIP("2001:db8::2")), Port: 6882})

	msg, err := handler.GeneratePEXMessage(conn)
	if err != nil {
		t.Fatalf("GeneratePEXMessage failed: %v", err)
	}
	if msg == nil {
		t.Fatal("Expected non-nil message after adding IPv6 peers")
	}

	// Check message content
	if len(msg.Added6) != 2 {
		t.Errorf("Expected 2 added6 peers, got %d", len(msg.Added6))
	}
	if len(msg.Added6F) != 2 {
		t.Errorf("Expected 2 added6 flags, got %d", len(msg.Added6F))
	}
}

// TestDefaultPEXHandler_DropPeer tests dropping peers
func TestDefaultPEXHandler_DropPeer(t *testing.T) {
	handler := NewDefaultPEXHandler(60 * time.Second)

	conn := &PeerConn{}
	conn.Conn = &mockNetConn{addr: &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 6881}}

	peerKey := conn.RemoteAddr().String()

	// Add a peer
	handler.AddPeer(peerKey, CompactPeer{IP: CompactIP(net.ParseIP("192.168.1.1").To4()), Port: 6881})

	// Drop a peer
	handler.DropPeer(peerKey, CompactPeer{IP: CompactIP(net.ParseIP("192.168.1.2").To4()), Port: 6882})

	// Generate message
	msg, err := handler.GeneratePEXMessage(conn)
	if err != nil {
		t.Fatalf("GeneratePEXMessage failed: %v", err)
	}
	if msg == nil {
		t.Fatal("Expected non-nil message after adding/dropping peers")
	}

	// Check message content
	if len(msg.Added) != 1 {
		t.Errorf("Expected 1 added peer, got %d", len(msg.Added))
	}
	if len(msg.Dropped) != 1 {
		t.Errorf("Expected 1 dropped peer, got %d", len(msg.Dropped))
	}
}

// TestHandlePEXPayload tests the helper function for decoding PEX payloads
func TestHandlePEXPayload(t *testing.T) {
	handler := NewDefaultPEXHandler(60 * time.Second)
	conn := &PeerConn{}

	// Create a valid PEX message
	originalMsg := UtPexExtendedMsg{
		Added: []CompactPeer{
			{IP: CompactIP(net.ParseIP("192.168.1.1").To4()), Port: 6881},
		},
		AddedF: []byte{0},
	}

	// Encode it
	payload, err := originalMsg.EncodeToBytes()
	if err != nil {
		t.Fatalf("Failed to encode message: %v", err)
	}

	// Decode and handle it
	err = HandlePEXPayload(conn, payload, handler)
	if err != nil {
		t.Fatalf("HandlePEXPayload failed: %v", err)
	}
}

// TestHandlePEXPayload_Invalid tests error handling for invalid payloads
func TestHandlePEXPayload_Invalid(t *testing.T) {
	handler := NewDefaultPEXHandler(60 * time.Second)
	conn := &PeerConn{}

	// Invalid bencode data
	invalidPayload := []byte("not bencode")

	err := HandlePEXPayload(conn, invalidPayload, handler)
	if err == nil {
		t.Fatal("Expected error for invalid payload, got nil")
	}
}

// mockNetConn implements net.Conn for testing
type mockNetConn struct {
	addr net.Addr
}

func (m *mockNetConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (m *mockNetConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (m *mockNetConn) Close() error                       { return nil }
func (m *mockNetConn) LocalAddr() net.Addr                { return m.addr }
func (m *mockNetConn) RemoteAddr() net.Addr               { return m.addr }
func (m *mockNetConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockNetConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockNetConn) SetWriteDeadline(t time.Time) error { return nil }
