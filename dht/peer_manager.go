// Copyright 2020 xgfone, 2023 idk
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

package dht

import (
	"net"
	"sync"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	"github.com/go-i2p/go-i2p-bt/utils"
)

// PeerManager is used to manage the peers.
type PeerManager interface {
	// If ipv6 is true, only return ipv6 addresses. Or return ipv4 addresses.
	GetPeers(infohash metainfo.Hash, maxnum int, ipv6 bool) []metainfo.Address
}

type peer struct {
	ID metainfo.Hash
	//	IP    net.IP
	IP    net.Addr
	Port  uint16
	Token string
	Time  time.Time
}

type tokenPeerManager struct {
	lock  sync.RWMutex
	exit  chan struct{}
	peers map[metainfo.Hash]map[string]peer
}

func newTokenPeerManager() *tokenPeerManager {
	return &tokenPeerManager{
		exit:  make(chan struct{}),
		peers: make(map[metainfo.Hash]map[string]peer, 128),
	}
}

// Start starts the token-peer manager.
func (tpm *tokenPeerManager) Start(interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-tpm.exit:
			return
		case now := <-tick.C:
			tpm.lock.Lock()
			for id, peers := range tpm.peers {
				for addr, peer := range peers {
					if now.Sub(peer.Time) > interval {
						delete(peers, addr)
					}
				}

				if len(peers) == 0 {
					delete(tpm.peers, id)
				}
			}
			tpm.lock.Unlock()
		}
	}
}

func (tpm *tokenPeerManager) Set(id metainfo.Hash, addr net.Addr, token string) {
	addrkey := addr.String()
	tpm.lock.Lock()
	peers, ok := tpm.peers[id]
	if !ok {
		peers = make(map[string]peer, 4)
		tpm.peers[id] = peers
	}
	peers[addrkey] = peer{
		ID:    id,
		IP:    addr,
		Port:  uint16(utils.Port(addr)),
		Token: token,
		Time:  time.Now(),
	}
	tpm.lock.Unlock()
}

func (tpm *tokenPeerManager) Get(id metainfo.Hash, addr net.Addr) (token string) {
	addrkey := addr.String()
	tpm.lock.RLock()
	if peers, ok := tpm.peers[id]; ok {
		if peer, ok := peers[addrkey]; ok {
			token = peer.Token
		}
	}
	tpm.lock.RUnlock()
	return
}

func (tpm *tokenPeerManager) Stop() {
	select {
	case <-tpm.exit:
	default:
		close(tpm.exit)
	}
}

func (tpm *tokenPeerManager) GetPeers(infohash metainfo.Hash, maxnum int,
	ipv6 bool) (addrs []metainfo.Address) {
	addrs = make([]metainfo.Address, 0, maxnum)
	tpm.lock.RLock()
	if peers, ok := tpm.peers[infohash]; ok {
		for _, peer := range peers {
			if maxnum < 1 {
				break
			}
			//if ipv6 { // For IPv6
			//if isIPv6(peer.IP) {
			maxnum--
			addrs = append(addrs, metainfo.NewAddress(peer.IP, peer.Port))
			//}
			//} else if !isIPv6(peer.IP) { // For IPv4
			//	maxnum--
			//	addrs = append(addrs, metainfo.NewAddress(peer.IP, peer.Port))
			//}
		}
	}
	tpm.lock.RUnlock()
	return
}
