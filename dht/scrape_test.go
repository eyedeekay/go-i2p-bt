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

package dht

import (
	"testing"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// TestPeerManager_GetSampleInfoHashes tests the GetSampleInfoHashes method
func TestPeerManager_GetSampleInfoHashes(t *testing.T) {
	pm := newTestPeerManager()

	// Test with empty peer manager
	samples, num := pm.GetSampleInfoHashes(metainfo.NewRandomHash(), 10)
	if samples != nil {
		t.Error("Expected nil samples for empty peer manager")
	}
	if num != 0 {
		t.Errorf("Expected 0 num for empty peer manager, got %d", num)
	}

	// Add some test peers
	hash1 := metainfo.NewRandomHash()
	hash2 := metainfo.NewRandomHash()
	hash3 := metainfo.NewRandomHash()

	pm.AddPeer(hash1, metainfo.NewAddress(nil, 6881))
	pm.AddPeer(hash2, metainfo.NewAddress(nil, 6882))
	pm.AddPeer(hash3, metainfo.NewAddress(nil, 6883))

	// Test with maxnum larger than available
	samples, num = pm.GetSampleInfoHashes(metainfo.NewRandomHash(), 10)
	if len(samples) != 3 {
		t.Errorf("Expected 3 samples, got %d", len(samples))
	}
	if num != 3 {
		t.Errorf("Expected num=3, got %d", num)
	}

	// Verify all hashes are present
	hashMap := make(map[metainfo.Hash]bool)
	for _, h := range samples {
		hashMap[h] = true
	}
	if !hashMap[hash1] || !hashMap[hash2] || !hashMap[hash3] {
		t.Error("Not all infohashes returned in samples")
	}

	// Test with maxnum smaller than available
	samples, num = pm.GetSampleInfoHashes(metainfo.NewRandomHash(), 2)
	if len(samples) != 2 {
		t.Errorf("Expected 2 samples, got %d", len(samples))
	}
	if num != 3 {
		t.Errorf("Expected num=3, got %d", num)
	}
}

// TestTokenPeerManager_GetSampleInfoHashes tests the token peer manager implementation
func TestTokenPeerManager_GetSampleInfoHashes(t *testing.T) {
	tpm := newTokenPeerManager()

	// Test with empty manager
	samples, num := tpm.GetSampleInfoHashes(metainfo.NewRandomHash(), 10)
	if samples != nil {
		t.Error("Expected nil samples for empty manager")
	}
	if num != 0 {
		t.Errorf("Expected 0 num for empty manager, got %d", num)
	}

	// We can't directly add peers to tokenPeerManager without network operations,
	// but we can test the method exists and works with empty data
	tpm.Stop()
}

// TestGetSampleInfoHashes_Boundary tests boundary conditions
func TestGetSampleInfoHashes_Boundary(t *testing.T) {
	pm := newTestPeerManager()

	// Add exactly maxnum peers
	maxnum := 5
	hashes := make([]metainfo.Hash, maxnum)
	for i := 0; i < maxnum; i++ {
		hashes[i] = metainfo.NewRandomHash()
		pm.AddPeer(hashes[i], metainfo.NewAddress(nil, uint16(6881+i)))
	}

	// Request exactly maxnum
	samples, num := pm.GetSampleInfoHashes(metainfo.NewRandomHash(), maxnum)
	if len(samples) != maxnum {
		t.Errorf("Expected %d samples, got %d", maxnum, len(samples))
	}
	if num != maxnum {
		t.Errorf("Expected num=%d, got %d", maxnum, num)
	}

	// Request maxnum + 1
	samples, num = pm.GetSampleInfoHashes(metainfo.NewRandomHash(), maxnum+1)
	if len(samples) != maxnum {
		t.Errorf("Expected %d samples, got %d", maxnum, len(samples))
	}
	if num != maxnum {
		t.Errorf("Expected num=%d, got %d", maxnum, num)
	}

	// Request 0 - should return empty slice
	samples, num = pm.GetSampleInfoHashes(metainfo.NewRandomHash(), 0)
	if len(samples) != 0 {
		t.Errorf("Expected 0 samples when maxnum=0, got %d", len(samples))
	}
	if num != maxnum {
		t.Errorf("Expected num=%d, got %d", maxnum, num)
	}
}

// TestGetSampleInfoHashes_Determinism tests that same input gives consistent results
func TestGetSampleInfoHashes_Determinism(t *testing.T) {
	pm := newTestPeerManager()

	// Add test peers
	for i := 0; i < 5; i++ {
		hash := metainfo.NewRandomHash()
		pm.AddPeer(hash, metainfo.NewAddress(nil, uint16(6881+i)))
	}

	target := metainfo.NewRandomHash()

	// Call multiple times with same target
	samples1, num1 := pm.GetSampleInfoHashes(target, 3)
	samples2, num2 := pm.GetSampleInfoHashes(target, 3)

	if num1 != num2 {
		t.Errorf("Expected same num, got %d and %d", num1, num2)
	}

	if len(samples1) != len(samples2) {
		t.Errorf("Expected same sample count, got %d and %d", len(samples1), len(samples2))
	}
}
