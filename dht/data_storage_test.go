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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// generateEd25519KeyPair generates a test Ed25519 key pair.
func generateEd25519KeyPair() (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return pub, priv
}

// TestStorageItem_ComputeTarget_Immutable tests target computation for immutable data.
func TestStorageItem_ComputeTarget_Immutable(t *testing.T) {
	value := []byte("hello world")
	item := &StorageItem{
		Value:     value,
		IsMutable: false,
	}

	target := item.ComputeTarget()

	// For immutable data: target = SHA-256(value)
	expected := sha256.Sum256(value)
	if target != metainfo.NewHash(expected[:]) {
		t.Error("Target hash mismatch for immutable data")
	}
}

// TestStorageItem_ComputeTarget_Mutable tests target computation for mutable data.
func TestStorageItem_ComputeTarget_Mutable(t *testing.T) {
	pub, _ := generateEd25519KeyPair()
	salt := []byte("test-salt")

	item := &StorageItem{
		Value:     []byte("data"),
		IsMutable: true,
		PublicKey: pub,
		Salt:      salt,
	}

	target := item.ComputeTarget()

	// For mutable data: target = SHA-256(public_key + salt)
	h := sha256.New()
	h.Write(pub)
	h.Write(salt)
	expected := h.Sum(nil)

	if target != metainfo.NewHash(expected) {
		t.Error("Target hash mismatch for mutable data")
	}
}

// TestStorageItem_ComputeTarget_MutableNoSalt tests mutable data without salt.
func TestStorageItem_ComputeTarget_MutableNoSalt(t *testing.T) {
	pub, _ := generateEd25519KeyPair()

	item := &StorageItem{
		Value:     []byte("data"),
		IsMutable: true,
		PublicKey: pub,
	}

	target := item.ComputeTarget()

	// For mutable data without salt: target = SHA-256(public_key)
	expected := sha256.Sum256(pub)
	if target != metainfo.NewHash(expected[:]) {
		t.Error("Target hash mismatch for mutable data without salt")
	}
}

// TestStorageItem_VerifySignature_Valid tests signature verification with valid signature.
func TestStorageItem_VerifySignature_Valid(t *testing.T) {
	pub, priv := generateEd25519KeyPair()
	value := []byte("test data")
	seq := int64(1)

	item := &StorageItem{
		Value:     value,
		IsMutable: true,
		PublicKey: pub,
		Seq:       seq,
	}

	// Sign the message
	msg := item.CreateSignatureMessage()
	item.Signature = ed25519.Sign(priv, msg)

	if !item.VerifySignature() {
		t.Error("Valid signature verification failed")
	}
}

// TestStorageItem_VerifySignature_Invalid tests signature verification with invalid signature.
func TestStorageItem_VerifySignature_Invalid(t *testing.T) {
	pub, _ := generateEd25519KeyPair()
	_, priv2 := generateEd25519KeyPair() // Different key

	value := []byte("test data")
	item := &StorageItem{
		Value:     value,
		IsMutable: true,
		PublicKey: pub,
		Seq:       1,
	}

	// Sign with wrong private key
	msg := item.CreateSignatureMessage()
	item.Signature = ed25519.Sign(priv2, msg)

	if item.VerifySignature() {
		t.Error("Invalid signature verification should fail")
	}
}

// TestStorageItem_VerifySignature_Immutable tests that immutable data doesn't require signatures.
func TestStorageItem_VerifySignature_Immutable(t *testing.T) {
	item := &StorageItem{
		Value:     []byte("immutable data"),
		IsMutable: false,
	}

	if !item.VerifySignature() {
		t.Error("Immutable data should always verify successfully")
	}
}

// TestStorageItem_CreateSignatureMessage tests signature message creation.
func TestStorageItem_CreateSignatureMessage(t *testing.T) {
	item := &StorageItem{
		Value: []byte("value"),
		Seq:   42,
		Salt:  []byte("salt"),
	}

	msg := item.CreateSignatureMessage()
	if msg == nil {
		t.Fatal("CreateSignatureMessage returned nil")
	}

	// Message should be bencode of {seq, salt, v}
	if len(msg) == 0 {
		t.Error("Signature message is empty")
	}
}

// TestStorageItem_CreateSignatureMessage_NoSalt tests signature message without salt.
func TestStorageItem_CreateSignatureMessage_NoSalt(t *testing.T) {
	item := &StorageItem{
		Value: []byte("value"),
		Seq:   42,
	}

	msg := item.CreateSignatureMessage()
	if msg == nil {
		t.Fatal("CreateSignatureMessage returned nil")
	}

	// Should still work without salt
	if len(msg) == 0 {
		t.Error("Signature message is empty")
	}
}

// TestStorageItem_IsExpired tests expiration checking.
func TestStorageItem_IsExpired(t *testing.T) {
	// Fresh item
	item := &StorageItem{
		Value:    []byte("data"),
		StoredAt: time.Now(),
	}

	if item.IsExpired() {
		t.Error("Fresh item should not be expired")
	}

	// Expired item
	item.StoredAt = time.Now().Add(-3 * time.Hour)
	if !item.IsExpired() {
		t.Error("Old item should be expired")
	}
}

// TestDataStore_Put_Immutable tests storing immutable data.
func TestDataStore_Put_Immutable(t *testing.T) {
	ds := NewDataStore()
	value := []byte("immutable test data")

	item := &StorageItem{
		Value:     value,
		IsMutable: false,
	}

	err := ds.Put(item)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify stored
	target := item.ComputeTarget()
	retrieved := ds.Get(target)
	if retrieved == nil {
		t.Fatal("Failed to retrieve stored item")
	}

	if string(retrieved.Value) != string(value) {
		t.Errorf("Retrieved value mismatch: got %q, want %q", retrieved.Value, value)
	}
}

// TestDataStore_Put_Mutable tests storing mutable data with signature.
func TestDataStore_Put_Mutable(t *testing.T) {
	ds := NewDataStore()
	pub, priv := generateEd25519KeyPair()
	value := []byte("mutable test data")

	item := &StorageItem{
		Value:     value,
		IsMutable: true,
		PublicKey: pub,
		Seq:       1,
	}

	// Sign the item
	msg := item.CreateSignatureMessage()
	item.Signature = ed25519.Sign(priv, msg)

	err := ds.Put(item)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify stored
	target := item.ComputeTarget()
	retrieved := ds.Get(target)
	if retrieved == nil {
		t.Fatal("Failed to retrieve stored item")
	}

	if retrieved.Seq != 1 {
		t.Errorf("Sequence number mismatch: got %d, want 1", retrieved.Seq)
	}
}

// TestDataStore_Put_ValueTooLarge tests rejection of oversized values.
func TestDataStore_Put_ValueTooLarge(t *testing.T) {
	ds := NewDataStore()
	value := make([]byte, MaxValueSize+1)

	item := &StorageItem{
		Value:     value,
		IsMutable: false,
	}

	err := ds.Put(item)
	if err == nil {
		t.Fatal("Expected error for oversized value")
	}
}

// TestDataStore_Put_SaltTooLarge tests rejection of oversized salt.
func TestDataStore_Put_SaltTooLarge(t *testing.T) {
	ds := NewDataStore()
	pub, priv := generateEd25519KeyPair()
	salt := make([]byte, MaxSaltSize+1)

	item := &StorageItem{
		Value:     []byte("data"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       1,
		Salt:      salt,
	}

	msg := item.CreateSignatureMessage()
	item.Signature = ed25519.Sign(priv, msg)

	err := ds.Put(item)
	if err == nil {
		t.Fatal("Expected error for oversized salt")
	}
}

// TestDataStore_Put_InvalidSignature tests rejection of invalid signatures.
func TestDataStore_Put_InvalidSignature(t *testing.T) {
	ds := NewDataStore()
	pub, _ := generateEd25519KeyPair()

	item := &StorageItem{
		Value:     []byte("data"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       1,
		Signature: make([]byte, 64), // Invalid signature
	}

	err := ds.Put(item)
	if err == nil {
		t.Fatal("Expected error for invalid signature")
	}
}

// TestDataStore_Put_SequenceUpdate tests updating mutable data with higher sequence.
func TestDataStore_Put_SequenceUpdate(t *testing.T) {
	ds := NewDataStore()
	pub, priv := generateEd25519KeyPair()

	// Store initial version
	item1 := &StorageItem{
		Value:     []byte("version 1"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       1,
	}
	msg1 := item1.CreateSignatureMessage()
	item1.Signature = ed25519.Sign(priv, msg1)

	if err := ds.Put(item1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Update with higher sequence
	item2 := &StorageItem{
		Value:     []byte("version 2"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       2,
		Target:    item1.ComputeTarget(), // Same target
	}
	msg2 := item2.CreateSignatureMessage()
	item2.Signature = ed25519.Sign(priv, msg2)

	if err := ds.Put(item2); err != nil {
		t.Fatalf("Put update failed: %v", err)
	}

	// Verify updated value
	retrieved := ds.Get(item1.ComputeTarget())
	if string(retrieved.Value) != "version 2" {
		t.Errorf("Value not updated: got %q", retrieved.Value)
	}
	if retrieved.Seq != 2 {
		t.Errorf("Sequence not updated: got %d", retrieved.Seq)
	}
}

// TestDataStore_Put_SequenceNotMonotonic tests rejection of lower sequence numbers.
func TestDataStore_Put_SequenceNotMonotonic(t *testing.T) {
	ds := NewDataStore()
	pub, priv := generateEd25519KeyPair()

	// Store version with seq=2
	item1 := &StorageItem{
		Value:     []byte("version 2"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       2,
	}
	msg1 := item1.CreateSignatureMessage()
	item1.Signature = ed25519.Sign(priv, msg1)
	ds.Put(item1)

	// Try to store with seq=1 (lower)
	item2 := &StorageItem{
		Value:     []byte("version 1"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       1,
		Target:    item1.ComputeTarget(),
	}
	msg2 := item2.CreateSignatureMessage()
	item2.Signature = ed25519.Sign(priv, msg2)

	err := ds.Put(item2)
	if err == nil {
		t.Fatal("Expected error for non-monotonic sequence")
	}
}

// TestDataStore_Get_NonExistent tests getting non-existent item.
func TestDataStore_Get_NonExistent(t *testing.T) {
	ds := NewDataStore()
	target := metainfo.NewRandomHash()

	item := ds.Get(target)
	if item != nil {
		t.Error("Expected nil for non-existent item")
	}
}

// TestDataStore_Get_Expired tests that expired items return nil.
func TestDataStore_Get_Expired(t *testing.T) {
	ds := NewDataStore()
	value := []byte("expired data")

	item := &StorageItem{
		Value:     value,
		IsMutable: false,
		StoredAt:  time.Now().Add(-3 * time.Hour), // Expired
	}
	item.Target = item.ComputeTarget()

	// Manually insert expired item
	ds.items[item.Target] = item

	// Should return nil for expired item
	retrieved := ds.Get(item.Target)
	if retrieved != nil {
		t.Error("Expired item should return nil")
	}
}

// TestDataStore_CleanupExpired tests expired item cleanup.
func TestDataStore_CleanupExpired(t *testing.T) {
	ds := NewDataStore()

	// Add fresh item
	fresh := &StorageItem{
		Value:    []byte("fresh"),
		StoredAt: time.Now(),
	}
	fresh.Target = fresh.ComputeTarget()
	ds.items[fresh.Target] = fresh

	// Add expired item
	expired := &StorageItem{
		Value:    []byte("expired"),
		StoredAt: time.Now().Add(-3 * time.Hour),
	}
	expired.Target = expired.ComputeTarget()
	ds.items[expired.Target] = expired

	// Cleanup
	removed := ds.CleanupExpired()
	if removed != 1 {
		t.Errorf("Expected 1 removed item, got %d", removed)
	}

	// Verify fresh item remains
	if ds.Get(fresh.Target) == nil {
		t.Error("Fresh item was removed")
	}

	// Verify expired item gone
	if _, ok := ds.items[expired.Target]; ok {
		t.Error("Expired item still exists")
	}
}

// TestDataStore_Count tests item counting.
func TestDataStore_Count(t *testing.T) {
	ds := NewDataStore()

	if ds.Count() != 0 {
		t.Error("Empty store should have count 0")
	}

	// Add items
	for i := 0; i < 5; i++ {
		item := &StorageItem{
			Value:     []byte{byte(i)},
			IsMutable: false,
		}
		ds.Put(item)
	}

	if ds.Count() != 5 {
		t.Errorf("Expected count 5, got %d", ds.Count())
	}
}

// TestDataStore_Concurrent tests concurrent access.
func TestDataStore_Concurrent(t *testing.T) {
	ds := NewDataStore()
	done := make(chan bool)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(n int) {
			item := &StorageItem{
				Value:     []byte{byte(n)},
				IsMutable: false,
			}
			ds.Put(item)
			done <- true
		}(i)
	}

	// Wait for all writes
	for i := 0; i < 10; i++ {
		<-done
	}

	if ds.Count() != 10 {
		t.Errorf("Expected 10 items, got %d", ds.Count())
	}
}

// TestStorageItem_CAS tests compare-and-swap functionality.
func TestStorageItem_CAS(t *testing.T) {
	ds := NewDataStore()
	pub, priv := generateEd25519KeyPair()

	// Store initial version
	item1 := &StorageItem{
		Value:     []byte("version 1"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       1,
	}
	msg1 := item1.CreateSignatureMessage()
	item1.Signature = ed25519.Sign(priv, msg1)
	ds.Put(item1)

	// Update with CAS matching current seq
	item2 := &StorageItem{
		Value:     []byte("version 2"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       2,
		CAS:       1, // Expect current seq to be 1
		Target:    item1.ComputeTarget(),
	}
	msg2 := item2.CreateSignatureMessage()
	item2.Signature = ed25519.Sign(priv, msg2)

	err := ds.Put(item2)
	if err != nil {
		t.Fatalf("CAS update failed: %v", err)
	}

	// Verify updated
	retrieved := ds.Get(item1.ComputeTarget())
	if retrieved.Seq != 2 {
		t.Errorf("Expected seq 2, got %d", retrieved.Seq)
	}
}

// TestStorageItem_CASMismatch tests CAS failure when sequence doesn't match.
func TestStorageItem_CASMismatch(t *testing.T) {
	ds := NewDataStore()
	pub, priv := generateEd25519KeyPair()

	// Store initial version with seq=5
	item1 := &StorageItem{
		Value:     []byte("version 1"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       5,
	}
	msg1 := item1.CreateSignatureMessage()
	item1.Signature = ed25519.Sign(priv, msg1)
	ds.Put(item1)

	// Try to update with CAS=3 (doesn't match current seq=5)
	item2 := &StorageItem{
		Value:     []byte("version 2"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       6,
		CAS:       3, // Wrong!
		Target:    item1.ComputeTarget(),
	}
	msg2 := item2.CreateSignatureMessage()
	item2.Signature = ed25519.Sign(priv, msg2)

	err := ds.Put(item2)
	if err == nil {
		t.Fatal("Expected CAS mismatch error")
	}

	// Verify original value unchanged
	retrieved := ds.Get(item1.ComputeTarget())
	if retrieved.Seq != 5 {
		t.Errorf("Seq should still be 5, got %d", retrieved.Seq)
	}
}

// BenchmarkDataStore_Put benchmarks storage performance.
func BenchmarkDataStore_Put(b *testing.B) {
	ds := NewDataStore()
	value := []byte("benchmark data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		item := &StorageItem{
			Value:     value,
			IsMutable: false,
		}
		ds.Put(item)
	}
}

// BenchmarkDataStore_Get benchmarks retrieval performance.
func BenchmarkDataStore_Get(b *testing.B) {
	ds := NewDataStore()
	value := []byte("benchmark data")

	item := &StorageItem{
		Value:     value,
		IsMutable: false,
	}
	ds.Put(item)
	target := item.ComputeTarget()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ds.Get(target)
	}
}

// BenchmarkStorageItem_VerifySignature benchmarks signature verification.
func BenchmarkStorageItem_VerifySignature(b *testing.B) {
	pub, priv := generateEd25519KeyPair()
	item := &StorageItem{
		Value:     []byte("benchmark data"),
		IsMutable: true,
		PublicKey: pub,
		Seq:       1,
	}

	msg := item.CreateSignatureMessage()
	item.Signature = ed25519.Sign(priv, msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		item.VerifySignature()
	}
}
