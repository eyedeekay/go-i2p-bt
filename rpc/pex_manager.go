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
	"sync"
	"time"

	"github.com/go-i2p/go-i2p-bt/peerprotocol"
)

// PEXManager manages Peer Exchange (PEX) operations for torrents
// It implements BEP 11 for peer discovery through peer exchange
type PEXManager struct {
	enabled    bool
	mu         sync.RWMutex
	peers      map[string]map[string]*PEXPeer // torrentID -> peerID -> PEXPeer
	lastUpdate map[string]time.Time           // torrentID -> last update time
	interval   time.Duration                  // PEX message interval
}

// PEXPeer represents a peer in the PEX system
type PEXPeer struct {
	IP       net.IP
	Port     uint16
	Added    time.Time
	LastSeen time.Time
	Flags    byte // Peer flags (seed, prefer encryption, etc.)
}

// NewPEXManager creates a new PEX manager
func NewPEXManager(enabled bool) *PEXManager {
	return &PEXManager{
		enabled:    enabled,
		peers:      make(map[string]map[string]*PEXPeer),
		lastUpdate: make(map[string]time.Time),
		interval:   60 * time.Second, // Send PEX messages every 60 seconds
	}
}

// SetEnabled enables or disables PEX functionality
func (pex *PEXManager) SetEnabled(enabled bool) {
	pex.mu.Lock()
	defer pex.mu.Unlock()
	pex.enabled = enabled
	if !enabled {
		// Clear all peer data when disabled
		pex.peers = make(map[string]map[string]*PEXPeer)
		pex.lastUpdate = make(map[string]time.Time)
	}
}

// IsEnabled returns whether PEX is currently enabled
func (pex *PEXManager) IsEnabled() bool {
	pex.mu.RLock()
	defer pex.mu.RUnlock()
	return pex.enabled
}

// AddPeer adds a new peer discovered through PEX
func (pex *PEXManager) AddPeer(torrentID string, ip net.IP, port uint16, flags byte) {
	if !pex.IsEnabled() {
		return
	}

	peerID := pex.makePeerID(ip, port)
	pex.mu.Lock()
	defer pex.mu.Unlock()

	if pex.peers[torrentID] == nil {
		pex.peers[torrentID] = make(map[string]*PEXPeer)
	}

	if existingPeer, exists := pex.peers[torrentID][peerID]; exists {
		// Update existing peer
		existingPeer.LastSeen = time.Now()
		existingPeer.Flags = flags
	} else {
		// Add new peer
		pex.peers[torrentID][peerID] = &PEXPeer{
			IP:       ip,
			Port:     port,
			Added:    time.Now(),
			LastSeen: time.Now(),
			Flags:    flags,
		}
	}
}

// RemovePeer removes a peer from PEX tracking
func (pex *PEXManager) RemovePeer(torrentID string, ip net.IP, port uint16) {
	if !pex.IsEnabled() {
		return
	}

	peerID := pex.makePeerID(ip, port)
	pex.mu.Lock()
	defer pex.mu.Unlock()

	if pex.peers[torrentID] != nil {
		delete(pex.peers[torrentID], peerID)
		if len(pex.peers[torrentID]) == 0 {
			delete(pex.peers, torrentID)
		}
	}
}

// GetPEXMessage generates a PEX message for a specific torrent
func (pex *PEXManager) GetPEXMessage(torrentID string) (*peerprotocol.UtPexExtendedMsg, error) {
	if !pex.IsEnabled() {
		return nil, nil
	}

	pex.mu.Lock()
	defer pex.mu.Unlock()

	lastUpdate, exists := pex.lastUpdate[torrentID]
	if !exists {
		lastUpdate = time.Time{} // Beginning of time for first message
	}

	now := time.Now()
	msg := &peerprotocol.UtPexExtendedMsg{}

	// Get peers for this torrent
	torrentPeers := pex.peers[torrentID]
	if torrentPeers == nil {
		pex.lastUpdate[torrentID] = now
		return msg, nil // Empty message
	}

	// Find added peers since last update
	for _, peer := range torrentPeers {
		if peer.Added.After(lastUpdate) {
			compactPeer := peerprotocol.CompactPeer{
				IP:   peerprotocol.CompactIP(peer.IP),
				Port: peer.Port,
			}

			if isIPv6(peer.IP) {
				msg.Added6 = append(msg.Added6, compactPeer)
				msg.Added6F = append(msg.Added6F, peer.Flags)
			} else {
				msg.Added = append(msg.Added, compactPeer)
				msg.AddedF = append(msg.AddedF, peer.Flags)
			}
		}
	}

	// Clean up expired peers (dropped)
	cutoff := now.Add(-5 * time.Minute) // Consider peers stale after 5 minutes
	for peerID, peer := range torrentPeers {
		if peer.LastSeen.Before(cutoff) {
			compactPeer := peerprotocol.CompactPeer{
				IP:   peerprotocol.CompactIP(peer.IP),
				Port: peer.Port,
			}

			if isIPv6(peer.IP) {
				msg.Dropped6 = append(msg.Dropped6, compactPeer)
			} else {
				msg.Dropped = append(msg.Dropped, compactPeer)
			}

			// Remove the expired peer
			delete(torrentPeers, peerID)
		}
	}

	pex.lastUpdate[torrentID] = now
	return msg, nil
}

// ProcessPEXMessage processes an incoming PEX message from a peer
func (pex *PEXManager) ProcessPEXMessage(torrentID string, msg *peerprotocol.UtPexExtendedMsg, callback func(string, net.IP, uint16)) {
	if !pex.IsEnabled() {
		return
	}

	pex.processAddedIPv4Peers(torrentID, msg.Added, msg.AddedF, callback)
	pex.processAddedIPv6Peers(torrentID, msg.Added6, msg.Added6F, callback)
	pex.processDroppedPeers(torrentID, msg.Dropped, msg.Dropped6)
}

// processAddedIPv4Peers processes added IPv4 peers from a PEX message
func (pex *PEXManager) processAddedIPv4Peers(torrentID string, added []peerprotocol.CompactPeer, flags []byte, callback func(string, net.IP, uint16)) {
	for i, peer := range added {
		ip := net.IP([]byte(peer.IP)) // Convert CompactIP to net.IP
		if pex.processValidPeer(torrentID, ip, peer.Port, flags, i, callback) {
			continue
		}
	}
}

// processAddedIPv6Peers processes added IPv6 peers from a PEX message
func (pex *PEXManager) processAddedIPv6Peers(torrentID string, added6 []peerprotocol.CompactPeer, flags6 []byte, callback func(string, net.IP, uint16)) {
	for i, peer := range added6 {
		ip := net.IP([]byte(peer.IP)) // Convert CompactIP to net.IP
		if pex.processValidPeer(torrentID, ip, peer.Port, flags6, i, callback) {
			continue
		}
	}
}

// processDroppedPeers processes dropped peers from a PEX message
func (pex *PEXManager) processDroppedPeers(torrentID string, dropped, dropped6 []peerprotocol.CompactPeer) {
	for _, peer := range dropped {
		ip := net.IP([]byte(peer.IP)) // Convert CompactIP to net.IP
		if ip != nil {
			pex.RemovePeer(torrentID, ip, peer.Port)
		}
	}

	for _, peer := range dropped6 {
		ip := net.IP([]byte(peer.IP)) // Convert CompactIP to net.IP
		if ip != nil {
			pex.RemovePeer(torrentID, ip, peer.Port)
		}
	}
}

// processValidPeer validates and processes a single peer entry
func (pex *PEXManager) processValidPeer(torrentID string, ip net.IP, port uint16, flags []byte, index int, callback func(string, net.IP, uint16)) bool {
	if ip == nil || !isValidIP(ip) {
		return false
	}

	var peerFlags byte
	if index < len(flags) {
		peerFlags = flags[index]
	}

	pex.AddPeer(torrentID, ip, port, peerFlags)
	if callback != nil {
		callback(torrentID, ip, port)
	}
	return true
}

// GetPeerCount returns the number of peers tracked for a torrent
func (pex *PEXManager) GetPeerCount(torrentID string) int {
	pex.mu.RLock()
	defer pex.mu.RUnlock()

	if torrentPeers := pex.peers[torrentID]; torrentPeers != nil {
		return len(torrentPeers)
	}
	return 0
}

// GetAllPeers returns all peers for a specific torrent
func (pex *PEXManager) GetAllPeers(torrentID string) []PEXPeer {
	pex.mu.RLock()
	defer pex.mu.RUnlock()

	torrentPeers := pex.peers[torrentID]
	if torrentPeers == nil {
		return nil
	}

	peers := make([]PEXPeer, 0, len(torrentPeers))
	for _, peer := range torrentPeers {
		peers = append(peers, *peer)
	}
	return peers
}

// ShouldSendPEX determines if it's time to send a PEX message for a torrent
func (pex *PEXManager) ShouldSendPEX(torrentID string) bool {
	if !pex.IsEnabled() {
		return false
	}

	pex.mu.RLock()
	defer pex.mu.RUnlock()

	lastUpdate, exists := pex.lastUpdate[torrentID]
	if !exists {
		return true // First time, should send
	}

	return time.Since(lastUpdate) >= pex.interval
}

// CleanupTorrent removes all PEX data for a torrent
func (pex *PEXManager) CleanupTorrent(torrentID string) {
	pex.mu.Lock()
	defer pex.mu.Unlock()

	delete(pex.peers, torrentID)
	delete(pex.lastUpdate, torrentID)
}

// GetStats returns PEX statistics
func (pex *PEXManager) GetStats() map[string]interface{} {
	pex.mu.RLock()
	defer pex.mu.RUnlock()

	totalPeers := 0
	for _, torrentPeers := range pex.peers {
		totalPeers += len(torrentPeers)
	}

	return map[string]interface{}{
		"enabled":          pex.enabled,
		"total_peers":      totalPeers,
		"tracked_torrents": len(pex.peers),
		"interval_seconds": int(pex.interval.Seconds()),
	}
}

// Helper functions

func (pex *PEXManager) makePeerID(ip net.IP, port uint16) string {
	return ip.String() + ":" + string(rune(port))
}

func isIPv6(ip net.IP) bool {
	return ip.To4() == nil && ip.To16() != nil
}

func isValidIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Don't exchange private/local addresses
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsMulticast()
}

// StartPEXTicker starts a background goroutine to periodically generate PEX messages
func (pex *PEXManager) StartPEXTicker(callback func(string, *peerprotocol.UtPexExtendedMsg)) {
	if !pex.IsEnabled() {
		return
	}

	go func() {
		ticker := time.NewTicker(pex.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				pex.processPEXTick(callback)
			}
		}
	}()
}

func (pex *PEXManager) processPEXTick(callback func(string, *peerprotocol.UtPexExtendedMsg)) {
	pex.mu.RLock()
	torrents := make([]string, 0, len(pex.peers))
	for torrentID := range pex.peers {
		if pex.ShouldSendPEX(torrentID) {
			torrents = append(torrents, torrentID)
		}
	}
	pex.mu.RUnlock()

	for _, torrentID := range torrents {
		if msg, err := pex.GetPEXMessage(torrentID); err == nil && msg != nil {
			// Only send if there are peers to exchange
			if len(msg.Added) > 0 || len(msg.Added6) > 0 || len(msg.Dropped) > 0 || len(msg.Dropped6) > 0 {
				callback(torrentID, msg)
			}
		}
	}
}
