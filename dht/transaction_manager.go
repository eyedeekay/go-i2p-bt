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

package dht

import (
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-i2p/go-i2p-bt/krpc"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

type transaction struct {
	ID    string
	Query string
	Arg   krpc.QueryArg
	Addr  net.Addr
	Time  time.Time
	Depth int

	Visited    metainfo.Hashes
	Callback   func(Result)
	OnError    func(t *transaction, code int, reason string)
	OnTimeout  func(t *transaction)
	OnResponse func(t *transaction, radd net.Addr, msg krpc.Message)
}

func (t *transaction) Done(r Result) {
	if t.Callback != nil {
		r.Addr = t.Addr
		t.Callback(r)
	}
}

func noopResponse(*transaction, net.Addr, krpc.Message) {}
func newTransaction(s *Server, a net.Addr, q string, qa krpc.QueryArg,
	callback ...func(Result),
) *transaction {
	var cb func(Result)
	if len(callback) > 0 {
		cb = callback[0]
	}

	return &transaction{
		Addr:       a,
		Query:      q,
		Arg:        qa,
		Callback:   cb,
		OnError:    s.onError,
		OnTimeout:  s.onTimeout,
		OnResponse: noopResponse,
		Time:       time.Now(),
	}
}

type transactionkey struct {
	id   string
	addr string
}

type transactionManager struct {
	lock  sync.Mutex
	exit  chan struct{}
	trans map[transactionkey]*transaction
	tid   uint32
}

func newTransactionManager() *transactionManager {
	return &transactionManager{
		exit:  make(chan struct{}),
		trans: make(map[transactionkey]*transaction, 128),
	}
}

// Start starts the transaction manager.
func (tm *transactionManager) Start(s *Server, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-tm.exit:
			return
		case now := <-tick.C:
			tm.cleanupExpiredTransactions(now, interval)
		}
	}
}

// cleanupExpiredTransactions removes expired transactions and triggers their timeout handlers.
func (tm *transactionManager) cleanupExpiredTransactions(now time.Time, interval time.Duration) {
	tm.lock.Lock()
	defer tm.lock.Unlock()
	for k, t := range tm.trans {
		if tm.isTransactionExpired(t, now, interval) {
			delete(tm.trans, k)
			tm.handleTransactionTimeout(t)
		}
	}
}

// isTransactionExpired checks if a transaction has expired based on the interval.
func (tm *transactionManager) isTransactionExpired(t *transaction, now time.Time, interval time.Duration) bool {
	return now.Sub(t.Time) > interval
}

// handleTransactionTimeout calls the OnTimeout handler for a transaction.
func (tm *transactionManager) handleTransactionTimeout(t *transaction) {
	if t.OnTimeout != nil {
		t.OnTimeout(t)
	}
}

// Stop stops the transaction manager.
func (tm *transactionManager) Stop() {
	select {
	case <-tm.exit:
	default:
		close(tm.exit)
	}
}

// GetTransactionID returns a new transaction id.
func (tm *transactionManager) GetTransactionID() string {
	return strconv.FormatUint(uint64(atomic.AddUint32(&tm.tid, 1)), 36)
}

// AddTransaction adds the new transaction.
func (tm *transactionManager) AddTransaction(t *transaction) {
	key := transactionkey{id: t.ID, addr: t.Addr.String()}
	tm.lock.Lock()
	tm.trans[key] = t
	tm.lock.Unlock()
}

// DeleteTransaction deletes the transaction.
func (tm *transactionManager) DeleteTransaction(t *transaction) {
	key := transactionkey{id: t.ID, addr: t.Addr.String()}
	tm.lock.Lock()
	delete(tm.trans, key)
	tm.lock.Unlock()
}

// PopTransaction deletes and returns the transaction by the transaction id
// and the peer address.
//
// Return nil if there is no the transaction.
func (tm *transactionManager) PopTransaction(tid string, addr net.Addr) (t *transaction) {
	key := transactionkey{id: tid, addr: addr.String()}
	tm.lock.Lock()
	if t = tm.trans[key]; t != nil {
		delete(tm.trans, key)
	}
	tm.lock.Unlock()
	return
}
