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

package peerprotocol

import (
	"bytes"
	"log"
	"testing"

	"github.com/go-i2p/sam3"
)

func TestCompactIP(t *testing.T) {
	ipv4 := CompactIP([]byte{1, 2, 3, 4})
	b, err := ipv4.MarshalBencode()
	if err != nil {
		t.Fatal(err)
	}

	log.Println("IPv4 Test", ipv4.String(), len(ipv4))

	var ip CompactIP
	if err = ip.UnmarshalBencode(b); err != nil {
		t.Error(err, ip)
	} else if ip.String() != "1.2.3.4" {
		t.Error(ip.String(), ",", ip)
	}
}

func TestCompactIP6(t *testing.T) {
	ipv6 := CompactIP([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	b, err := ipv6.MarshalBencode()
	if err != nil {
		t.Fatal(err)
	}

	log.Println("IPv6 Test", ipv6.String(), len(ipv6))

	var ip CompactIP
	if err = ip.UnmarshalBencode(b); err != nil {
		t.Error(err)
	} else if ip.String() != "102:304:506:708:90a:b0c:d0e:f10" {
		t.Error(ip.String(), ",", ip)
	}
}

func TestCompactI2P(t *testing.T) {
	sam, err := sam3.NewSAM("127.0.0.1:7656")
	if err != nil {
		t.Fatal(err)
	}
	defer sam.Close()
	i2pkeys, err := sam.NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	dh := i2pkeys.Address.DestHash()
	i2p := CompactIP(dh[:])
	b, err := i2p.MarshalBencode()
	if err != nil {
		t.Fatal(err)
	}

	log.Println("I2P Test", i2p.String(), len(b))

	var ip CompactIP
	if err = ip.UnmarshalBencode(b); err != nil {
		t.Error(err)
	} else if ip.String() != dh.String() {
		t.Error(ip, dh)
	}
}

func TestUtMetadataExtendedMsg(t *testing.T) {
	buf := new(bytes.Buffer)
	data := []byte{0x31, 0x32, 0x33, 0x34, 0x35}
	m1 := UtMetadataExtendedMsg{MsgType: 1, Piece: 2, TotalSize: 1024, Data: data}
	if err := m1.EncodeToPayload(buf); err != nil {
		t.Fatal(err)
	}

	msg := Message{Type: MTypeExtended, ExtendedPayload: buf.Bytes()}
	m2, err := msg.UtMetadataExtendedMsg()
	if err != nil {
		t.Fatal(err)
	} else if m2.MsgType != 1 || m2.Piece != 2 || m2.TotalSize != 1024 {
		t.Error(m2)
	} else if !bytes.Equal(m2.Data, data) {
		t.Fail()
	}
}

// TestUtPexExtendedMsg_IPv4 tests PEX message encoding/decoding with IPv4 addresses
func TestUtPexExtendedMsg_IPv4(t *testing.T) {
	// Create a PEX message with IPv4 peers
	originalMsg := UtPexExtendedMsg{
		Added: []CompactPeer{
			{IP: CompactIP([]byte{192, 168, 1, 1}), Port: 6881},
			{IP: CompactIP([]byte{10, 0, 0, 1}), Port: 6882},
		},
		AddedF: []byte{0x00, 0x02}, // Second peer prefers encryption
		Dropped: []CompactPeer{
			{IP: CompactIP([]byte{172, 16, 0, 1}), Port: 6883},
		},
	}

	// Encode to bytes
	encoded, err := originalMsg.EncodeToBytes()
	if err != nil {
		t.Fatalf("Failed to encode PEX message: %v", err)
	}

	// Decode back
	var decodedMsg UtPexExtendedMsg
	if err := decodedMsg.DecodeFromPayload(encoded); err != nil {
		t.Fatalf("Failed to decode PEX message: %v", err)
	}

	// Verify added peers
	if len(decodedMsg.Added) != 2 {
		t.Errorf("Expected 2 added peers, got %d", len(decodedMsg.Added))
	}
	if decodedMsg.Added[0].Port != 6881 {
		t.Errorf("Expected port 6881, got %d", decodedMsg.Added[0].Port)
	}
	if decodedMsg.Added[1].Port != 6882 {
		t.Errorf("Expected port 6882, got %d", decodedMsg.Added[1].Port)
	}

	// Verify flags
	if len(decodedMsg.AddedF) != 2 {
		t.Errorf("Expected 2 flags, got %d", len(decodedMsg.AddedF))
	}
	if decodedMsg.AddedF[1] != 0x02 {
		t.Errorf("Expected flag 0x02, got 0x%02x", decodedMsg.AddedF[1])
	}

	// Verify dropped peers
	if len(decodedMsg.Dropped) != 1 {
		t.Errorf("Expected 1 dropped peer, got %d", len(decodedMsg.Dropped))
	}
	if decodedMsg.Dropped[0].Port != 6883 {
		t.Errorf("Expected port 6883, got %d", decodedMsg.Dropped[0].Port)
	}
}

// TestUtPexExtendedMsg_IPv6 tests PEX message encoding/decoding with IPv6 addresses
func TestUtPexExtendedMsg_IPv6(t *testing.T) {
	// Create IPv6 addresses
	ipv6_1 := []byte{
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
	}
	ipv6_2 := []byte{
		0xfe, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
	}

	originalMsg := UtPexExtendedMsg{
		Added6: []CompactPeer{
			{IP: CompactIP(ipv6_1), Port: 6881},
			{IP: CompactIP(ipv6_2), Port: 6882},
		},
		Added6F: []byte{0x00, 0x01}, // Second peer is a seed
		Dropped6: []CompactPeer{
			{IP: CompactIP(ipv6_1), Port: 6883},
		},
	}

	// Encode and decode
	encoded, err := originalMsg.EncodeToBytes()
	if err != nil {
		t.Fatalf("Failed to encode IPv6 PEX message: %v", err)
	}

	var decodedMsg UtPexExtendedMsg
	if err := decodedMsg.DecodeFromPayload(encoded); err != nil {
		t.Fatalf("Failed to decode IPv6 PEX message: %v", err)
	}

	// Verify IPv6 peers
	if len(decodedMsg.Added6) != 2 {
		t.Errorf("Expected 2 IPv6 added peers, got %d", len(decodedMsg.Added6))
	}
	if len(decodedMsg.Added6F) != 2 {
		t.Errorf("Expected 2 IPv6 flags, got %d", len(decodedMsg.Added6F))
	}
	if len(decodedMsg.Dropped6) != 1 {
		t.Errorf("Expected 1 IPv6 dropped peer, got %d", len(decodedMsg.Dropped6))
	}
}

// TestUtPexExtendedMsg_Empty tests encoding/decoding of empty PEX messages
func TestUtPexExtendedMsg_Empty(t *testing.T) {
	originalMsg := UtPexExtendedMsg{}

	encoded, err := originalMsg.EncodeToBytes()
	if err != nil {
		t.Fatalf("Failed to encode empty PEX message: %v", err)
	}

	var decodedMsg UtPexExtendedMsg
	if err := decodedMsg.DecodeFromPayload(encoded); err != nil {
		t.Fatalf("Failed to decode empty PEX message: %v", err)
	}

	// Verify all fields are empty
	if len(decodedMsg.Added) != 0 || len(decodedMsg.Added6) != 0 ||
		len(decodedMsg.Dropped) != 0 || len(decodedMsg.Dropped6) != 0 {
		t.Error("Decoded empty message should have no peers")
	}
}

// TestUtPexExtendedMsg_Mixed tests PEX with both IPv4 and IPv6 peers
func TestUtPexExtendedMsg_Mixed(t *testing.T) {
	ipv6 := []byte{
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
	}

	originalMsg := UtPexExtendedMsg{
		Added: []CompactPeer{
			{IP: CompactIP([]byte{192, 168, 1, 1}), Port: 6881},
		},
		AddedF: []byte{0x00},
		Added6: []CompactPeer{
			{IP: CompactIP(ipv6), Port: 6882},
		},
		Added6F: []byte{0x01},
		Dropped: []CompactPeer{
			{IP: CompactIP([]byte{10, 0, 0, 1}), Port: 6883},
		},
		Dropped6: []CompactPeer{
			{IP: CompactIP(ipv6), Port: 6884},
		},
	}

	// Encode and decode
	encoded, err := originalMsg.EncodeToBytes()
	if err != nil {
		t.Fatalf("Failed to encode mixed PEX message: %v", err)
	}

	var decodedMsg UtPexExtendedMsg
	if err := decodedMsg.DecodeFromPayload(encoded); err != nil {
		t.Fatalf("Failed to decode mixed PEX message: %v", err)
	}

	// Verify both IPv4 and IPv6 peers are present
	if len(decodedMsg.Added) != 1 {
		t.Errorf("Expected 1 IPv4 added peer, got %d", len(decodedMsg.Added))
	}
	if len(decodedMsg.Added6) != 1 {
		t.Errorf("Expected 1 IPv6 added peer, got %d", len(decodedMsg.Added6))
	}
	if len(decodedMsg.Dropped) != 1 {
		t.Errorf("Expected 1 IPv4 dropped peer, got %d", len(decodedMsg.Dropped))
	}
	if len(decodedMsg.Dropped6) != 1 {
		t.Errorf("Expected 1 IPv6 dropped peer, got %d", len(decodedMsg.Dropped6))
	}
}

// TestUtPexExtendedMsg_EncodeToPayload tests the buffer-based encoding
func TestUtPexExtendedMsg_EncodeToPayload(t *testing.T) {
	msg := UtPexExtendedMsg{
		Added: []CompactPeer{
			{IP: CompactIP([]byte{192, 168, 1, 1}), Port: 6881},
		},
		AddedF: []byte{0x00},
	}

	buf := new(bytes.Buffer)
	if err := msg.EncodeToPayload(buf); err != nil {
		t.Fatalf("EncodeToPayload failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Encoded payload should not be empty")
	}

	// Verify we can decode what was encoded
	var decodedMsg UtPexExtendedMsg
	if err := decodedMsg.DecodeFromPayload(buf.Bytes()); err != nil {
		t.Fatalf("Failed to decode payload: %v", err)
	}

	if len(decodedMsg.Added) != 1 {
		t.Errorf("Expected 1 added peer after decode, got %d", len(decodedMsg.Added))
	}
}
