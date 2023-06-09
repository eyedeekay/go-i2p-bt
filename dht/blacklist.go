// Copyright 2020 xgfone
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
	"sync"
	"time"

	"github.com/xgfone/go-bt/krpc"
)

// Blacklist is used to manage the ip blacklist.
//
// Notice: The implementation should clear the address existed for long time.
type Blacklist interface {
	// In reports whether the address, ip and port, is in the blacklist.
	In(krpc.Addr) bool

	// If port is equal to 0, it should ignore port and only use ip when matching.
	Add(krpc.Addr)

	// If port is equal to 0, it should delete the address by only the ip.
	Del(krpc.Addr)

	// Close is used to notice the implementation to release the underlying resource.
	Close()
}

type noopBlacklist struct{}

func (nbl noopBlacklist) In(krpc.Addr) bool { return false }
func (nbl noopBlacklist) Add(krpc.Addr)     {}
func (nbl noopBlacklist) Del(krpc.Addr)     {}
func (nbl noopBlacklist) Close()            {}

// NewNoopBlacklist returns a no-op Blacklist.
func NewNoopBlacklist() Blacklist { return noopBlacklist{} }

// DebugBlacklist returns a new Blacklist to log the information as debug.
func DebugBlacklist(bl Blacklist, logf func(string, ...interface{})) Blacklist {
	return logBlacklist{Blacklist: bl, logf: logf}
}

type logBlacklist struct {
	Blacklist
	logf func(string, ...interface{})
}

func (l logBlacklist) Add(addr krpc.Addr) {
	l.logf("add the addr '%s' into the blacklist", addr.String())
	l.Blacklist.Add(addr)
}

func (l logBlacklist) Del(addr krpc.Addr) {
	l.logf("delete the addr '%s' from the blacklist", addr.String())
	l.Blacklist.Del(addr)
}

/// >>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>

// NewMemoryBlacklist returns a blacklst implementation based on memory.
//
// if maxnum is equal to 0, no limit.
func NewMemoryBlacklist(maxnum int, duration time.Duration) Blacklist {
	bl := &blacklist{
		num:  maxnum,
		ips:  make(map[string]*wrappedPort, 128),
		exit: make(chan struct{}),
	}
	go bl.loop(duration)
	return bl
}

type wrappedPort struct {
	Time   time.Time
	Enable bool
	Ports  map[uint16]struct{}
}

type blacklist struct {
	exit chan struct{}
	lock sync.RWMutex
	ips  map[string]*wrappedPort
	num  int
}

func (bl *blacklist) loop(interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-bl.exit:
			return
		case now := <-tick.C:
			bl.lock.Lock()
			for ip, wp := range bl.ips {
				if now.Sub(wp.Time) > interval {
					delete(bl.ips, ip)
				}
			}
			bl.lock.Unlock()
		}
	}
}

func (bl *blacklist) Close() {
	select {
	case <-bl.exit:
	default:
		close(bl.exit)
	}
}

// In reports whether the address, ip and port, is in the blacklist.
func (bl *blacklist) In(addr krpc.Addr) (yes bool) {
	bl.lock.RLock()
	if wp, ok := bl.ips[addr.IP.String()]; ok {
		if wp.Enable {
			_, yes = wp.Ports[addr.Port]
		} else {
			yes = true
		}
	}
	bl.lock.RUnlock()
	return
}

func (bl *blacklist) Add(addr krpc.Addr) {
	ip := addr.IP.String()
	bl.lock.Lock()
	wp, ok := bl.ips[ip]
	if !ok {
		if bl.num > 0 && len(bl.ips) >= bl.num {
			bl.lock.Unlock()
			return
		}

		wp = &wrappedPort{Enable: true}
		bl.ips[ip] = wp
	}

	if addr.Port < 1 {
		wp.Enable = false
		wp.Ports = nil
	} else if wp.Ports == nil {
		wp.Ports = map[uint16]struct{}{addr.Port: {}}
	} else {
		wp.Ports[addr.Port] = struct{}{}
	}

	wp.Time = time.Now()
	bl.lock.Unlock()
}

func (bl *blacklist) Del(addr krpc.Addr) {
	ip := addr.IP.String()
	bl.lock.Lock()
	if wp, ok := bl.ips[ip]; ok {
		if addr.Port < 1 {
			delete(bl.ips, ip)
		} else if wp.Enable {
			switch len(wp.Ports) {
			case 0, 1:
				delete(bl.ips, ip)
			default:
				delete(wp.Ports, addr.Port)
				wp.Time = time.Now()
			}
		}
	}
	bl.lock.Unlock()
}
