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
	"crypto/sha1"
	"encoding/binary"
	"net"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// GenerateAllowedFastSet generates some allowed fast set of the torrent file.
//
// Argument:
//
//	set: generated piece set, the length of which is the number to be generated.
//	sz: the number of pieces in torrent.
//	ip: the of the remote peer of the connection.
//	infohash: infohash of torrent.
//
// BEP 6
func GenerateAllowedFastSet(set []uint32, sz uint32, ip net.IP, infohash metainfo.Hash) {
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}

	var x []byte
	if len(ip) == 4 { // IPv4
		// BEP 6: x = 0xFFFFFF00 & ip (use only first 3 bytes, set 4th byte to 0)
		x = make([]byte, 24) // 4 bytes for IP + 20 bytes for infohash
		x[0] = ip[0]         // First byte of IP
		x[1] = ip[1]         // Second byte of IP
		x[2] = ip[2]         // Third byte of IP
		x[3] = 0             // Fourth byte set to 0 per BEP 6 (0xFFFFFF00 mask)
		copy(x[4:], infohash[:])
	} else {
		// IPv6 (no official BEP 6 support, but maintain existing behavior for compatibility)
		iplen := len(ip)
		x = make([]byte, 20+iplen)
		for i := 0; i < iplen; i++ {
			x[i] = ip[i] & 0xff
		}
		copy(x[iplen:], infohash[:])
	}

	for cur, k := 0, len(set); cur < k; {
		sum := sha1.Sum(x)                  // (3)
		x = sum[:]                          // (3)
		for i := 0; i < 5 && cur < k; i++ { // (4)
			j := i * 4                               // (5)
			y := binary.BigEndian.Uint32(x[j : j+4]) // (6)
			index := y % sz                          // (7)
			if !uint32Contains(set, index) {         // (8)
				set[cur] = index //                     (9)
				cur++
			}
		}
	}
}

func uint32Contains(ss []uint32, s uint32) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
