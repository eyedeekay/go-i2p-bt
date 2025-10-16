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
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/go-i2p/go-i2p-bt/bencode"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

const (
	// MaxValueSize is the maximum size allowed for stored values (1000 bytes per BEP 44)
	MaxValueSize = 1000

	// MaxSaltSize is the maximum size allowed for salt (64 bytes per BEP 44)
	MaxSaltSize = 64

	// DataExpirationTime is how long stored data remains valid (2 hours per BEP 44)
	DataExpirationTime = 2 * time.Hour
)

// StorageItem represents a stored item in the DHT (BEP 44).
// Items can be either mutable or immutable.
type StorageItem struct {
	// Value is the data being stored (max 1000 bytes)
	Value []byte

	// IsMutable indicates whether this is mutable or immutable data
	IsMutable bool

	// --- Mutable data fields (only used if IsMutable == true) ---

	// PublicKey is the Ed25519 public key (32 bytes) for mutable data
	PublicKey []byte

	// Signature is the Ed25519 signature (64 bytes) for mutable data
	Signature []byte

	// Seq is the sequence number for mutable data (monotonically increasing)
	Seq int64

	// Salt is an optional salt for mutable data (max 64 bytes)
	Salt []byte

	// CAS is the compare-and-swap previous sequence number (optional)
	CAS int64

	// --- Metadata ---

	// StoredAt is when the item was stored
	StoredAt time.Time

	// Target is the SHA-256 hash used as the storage key
	Target metainfo.Hash
}

// IsExpired checks if the stored item has expired.
func (item *StorageItem) IsExpired() bool {
	return time.Since(item.StoredAt) > DataExpirationTime
}

// VerifySignature verifies the Ed25519 signature for mutable data.
// Returns true if the signature is valid, false otherwise.
func (item *StorageItem) VerifySignature() bool {
	if !item.IsMutable {
		return true // Immutable data doesn't require signatures
	}

	if len(item.PublicKey) != ed25519.PublicKeySize {
		return false
	}

	if len(item.Signature) != ed25519.SignatureSize {
		return false
	}

	// Create the message to verify: bencode(seq + salt + v)
	msg := item.CreateSignatureMessage()

	return ed25519.Verify(item.PublicKey, msg, item.Signature)
}

// CreateSignatureMessage creates the message that should be signed for mutable data.
// Format: bencode({seq, salt, v}) where salt is optional.
func (item *StorageItem) CreateSignatureMessage() []byte {
	// Create the dictionary to sign
	dict := make(map[string]interface{})
	dict["seq"] = item.Seq
	dict["v"] = item.Value

	if len(item.Salt) > 0 {
		dict["salt"] = item.Salt
	}

	// Bencode it
	buf := bytes.NewBuffer(nil)
	if err := bencode.NewEncoder(buf).Encode(dict); err != nil {
		return nil
	}

	return buf.Bytes()
}

// ComputeTarget computes the storage target hash for this item.
// For immutable data: SHA-256(value)
// For mutable data: SHA-256(public_key + salt) where salt is optional
func (item *StorageItem) ComputeTarget() metainfo.Hash {
	if !item.IsMutable {
		// Immutable: target = SHA-256(value)
		hash := sha256.Sum256(item.Value)
		return metainfo.NewHash(hash[:])
	}

	// Mutable: target = SHA-256(public_key + salt)
	h := sha256.New()
	h.Write(item.PublicKey)
	if len(item.Salt) > 0 {
		h.Write(item.Salt)
	}
	return metainfo.NewHash(h.Sum(nil))
}

// DataStore manages storage of BEP 44 data items.
type DataStore struct {
	mu    sync.RWMutex
	items map[metainfo.Hash]*StorageItem
}

// NewDataStore creates a new DataStore.
func NewDataStore() *DataStore {
	return &DataStore{
		items: make(map[metainfo.Hash]*StorageItem),
	}
}

// Put stores an item in the data store.
// Returns an error if validation fails.
func (ds *DataStore) Put(item *StorageItem) error {
	// Validate value size
	if len(item.Value) > MaxValueSize {
		return fmt.Errorf("value too large: %d > %d", len(item.Value), MaxValueSize)
	}

	// Validate salt size
	if len(item.Salt) > MaxSaltSize {
		return fmt.Errorf("salt too large: %d > %d", len(item.Salt), MaxSaltSize)
	}

	// Verify signature for mutable data
	if item.IsMutable {
		if !item.VerifySignature() {
			return fmt.Errorf("invalid signature")
		}
	}

	// Compute target if not set
	if item.Target.IsZero() {
		item.Target = item.ComputeTarget()
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Check for existing item (for mutable data CAS support)
	if existing, ok := ds.items[item.Target]; ok {
		if existing.IsMutable && item.IsMutable {
			// If CAS is specified, verify it matches current sequence
			if item.CAS != 0 && item.CAS != existing.Seq {
				return fmt.Errorf("CAS mismatch: expected %d, got %d", item.CAS, existing.Seq)
			}

			// For mutable data, check sequence number must increase
			if item.Seq <= existing.Seq {
				return fmt.Errorf("sequence number must be greater than current: %d <= %d", item.Seq, existing.Seq)
			}
		}
	}

	// Set storage timestamp
	item.StoredAt = time.Now()

	// Store the item
	ds.items[item.Target] = item
	return nil
}

// Get retrieves an item from the data store.
// Returns nil if the item doesn't exist or has expired.
func (ds *DataStore) Get(target metainfo.Hash) *StorageItem {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	item, ok := ds.items[target]
	if !ok {
		return nil
	}

	// Check expiration
	if item.IsExpired() {
		return nil
	}

	return item
}

// CleanupExpired removes all expired items from the store.
// Returns the number of items removed.
func (ds *DataStore) CleanupExpired() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	removed := 0
	for target, item := range ds.items {
		if item.IsExpired() {
			delete(ds.items, target)
			removed++
		}
	}

	return removed
}

// Count returns the number of items currently stored.
func (ds *DataStore) Count() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.items)
}
