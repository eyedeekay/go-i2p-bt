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
	"fmt"
	"time"

	"github.com/go-i2p/go-i2p-bt/bencode"
)

// PEXHandler defines the interface for handling PEX messages.
// Implementations should process incoming PEX messages and generate outgoing ones.
type PEXHandler interface {
	// HandlePEXMessage processes an incoming PEX message from a peer.
	// It should extract peer information and integrate it with the peer discovery system.
	HandlePEXMessage(conn *PeerConn, msg UtPexExtendedMsg) error

	// GeneratePEXMessage creates a PEX message to send to a peer.
	// It should return recently added/dropped peers and their flags.
	// Returns nil if no PEX message should be sent at this time.
	GeneratePEXMessage(conn *PeerConn) (*UtPexExtendedMsg, error)
}

// DefaultPEXHandler provides a basic PEX message handler implementation.
// It stores received peers and generates PEX messages based on accumulated peer changes.
type DefaultPEXHandler struct {
	// pexInterval is the minimum time between sending PEX messages to a peer
	pexInterval time.Duration

	// lastPEXSent tracks the last time we sent a PEX message to each peer
	lastPEXSent map[string]time.Time

	// addedPeers tracks peers that have been added since last PEX message
	addedPeers map[string][]CompactPeer

	// droppedPeers tracks peers that have been dropped since last PEX message
	droppedPeers map[string][]CompactPeer
}

// NewDefaultPEXHandler creates a new DefaultPEXHandler with the specified interval.
// The interval determines how often PEX messages are sent to each peer.
func NewDefaultPEXHandler(interval time.Duration) *DefaultPEXHandler {
	return &DefaultPEXHandler{
		pexInterval:  interval,
		lastPEXSent:  make(map[string]time.Time),
		addedPeers:   make(map[string][]CompactPeer),
		droppedPeers: make(map[string][]CompactPeer),
	}
}

// HandlePEXMessage processes an incoming PEX message.
// This basic implementation logs the received peers but doesn't integrate them
// into a peer discovery system (that should be done by the application layer).
func (h *DefaultPEXHandler) HandlePEXMessage(conn *PeerConn, msg UtPexExtendedMsg) error {
	// Validate the message
	if len(msg.Added) != len(msg.AddedF) {
		return fmt.Errorf("pex: added peers count (%d) doesn't match flags count (%d)",
			len(msg.Added), len(msg.AddedF))
	}

	if len(msg.Added6) != len(msg.Added6F) {
		return fmt.Errorf("pex: added6 peers count (%d) doesn't match flags count (%d)",
			len(msg.Added6), len(msg.Added6F))
	}

	// In a real implementation, you would:
	// 1. Validate each peer address
	// 2. Check against blocklist
	// 3. Add to peer manager/discovery system
	// 4. Respect peer flags (seed, encryption preference, etc.)

	// For this basic implementation, we just validate that we received valid data
	return nil
}

// GeneratePEXMessage creates a PEX message to send to a peer.
// This basic implementation always returns nil, meaning no PEX messages are sent.
// Applications should override this to provide actual peer exchange functionality.
func (h *DefaultPEXHandler) GeneratePEXMessage(conn *PeerConn) (*UtPexExtendedMsg, error) {
	peerKey := conn.RemoteAddr().String()

	// Check if enough time has passed since last PEX message
	if lastSent, ok := h.lastPEXSent[peerKey]; ok {
		if time.Since(lastSent) < h.pexInterval {
			return nil, nil // Too soon to send another PEX message
		}
	}

	// Get accumulated peer changes for this peer
	added, ok1 := h.addedPeers[peerKey]
	dropped, ok2 := h.droppedPeers[peerKey]

	// If no changes, don't send a message
	if !ok1 && !ok2 {
		return nil, nil
	}

	// Create the PEX message
	msg := &UtPexExtendedMsg{
		Dropped: dropped,
	}

	// Separate IPv4 and IPv6 added peers
	for _, peer := range added {
		if len(peer.IP) == 4 {
			msg.Added = append(msg.Added, peer)
			msg.AddedF = append(msg.AddedF, 0) // Default flags
		} else if len(peer.IP) == 16 {
			msg.Added6 = append(msg.Added6, peer)
			msg.Added6F = append(msg.Added6F, 0) // Default flags
		}
	}

	// Clear the accumulated changes
	delete(h.addedPeers, peerKey)
	delete(h.droppedPeers, peerKey)

	// Update last sent time
	h.lastPEXSent[peerKey] = time.Now()

	return msg, nil
}

// AddPeer records a peer that should be included in the next PEX message.
// This allows the application to notify the handler of new peers.
func (h *DefaultPEXHandler) AddPeer(peerKey string, peer CompactPeer) {
	h.addedPeers[peerKey] = append(h.addedPeers[peerKey], peer)
}

// DropPeer records a peer that should be marked as dropped in the next PEX message.
// This allows the application to notify the handler when peers disconnect.
func (h *DefaultPEXHandler) DropPeer(peerKey string, peer CompactPeer) {
	h.droppedPeers[peerKey] = append(h.droppedPeers[peerKey], peer)
}

// SendPEXMessage sends a PEX message to the specified peer connection.
// It generates the message using the handler and encodes it properly.
// Returns nil if the peer doesn't support PEX or no message needs to be sent.
func SendPEXMessage(conn *PeerConn, handler PEXHandler) error {
	// Check if peer supports PEX (I2P uses "i2p_pex" extension name)
	pexID, ok := conn.ExtendedHandshakeMsg.M[ExtendedMessageNamePex]
	if !ok || pexID == 0 {
		return nil // Peer doesn't support PEX
	}

	// Generate the PEX message
	pexMsg, err := handler.GeneratePEXMessage(conn)
	if err != nil {
		return fmt.Errorf("failed to generate PEX message: %w", err)
	}

	// If no message to send, return early
	if pexMsg == nil {
		return nil
	}

	// Encode the PEX message
	payload, err := bencode.EncodeBytes(pexMsg)
	if err != nil {
		return fmt.Errorf("failed to encode PEX message: %w", err)
	}

	// Create and send the extended message
	msg := Message{
		Type:            MTypeExtended,
		ExtendedID:      pexID,
		ExtendedPayload: payload,
	}

	return conn.WriteMsg(msg)
}

// HandlePEXPayload decodes and processes a PEX message payload.
// This is a helper function that can be called from a Bep10Handler.OnPayload implementation.
func HandlePEXPayload(conn *PeerConn, payload []byte, handler PEXHandler) error {
	// Decode the PEX message
	var pexMsg UtPexExtendedMsg
	if err := bencode.DecodeBytes(payload, &pexMsg); err != nil {
		return fmt.Errorf("failed to decode PEX message: %w", err)
	}

	// Process the message through the handler
	return handler.HandlePEXMessage(conn, pexMsg)
}
