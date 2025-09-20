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

package tracker

import (
	"context"
	"net"
	"net/url"
	"sync"

	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// Force a peer address to be used
var PeerAddress net.Addr

// GetPeersResult represents the result of getting the peers from the tracker.
type GetPeersResult struct {
	Error   error // nil stands for success. Or, for failure.
	Tracker string
	Resp    AnnounceResponse
}

// normalizeTrackerURLs ensures all tracker URLs have the proper /announce path.
func normalizeTrackerURLs(trackers []string) {
	for i, t := range trackers {
		if u, err := url.Parse(t); err == nil && u.Path == "" {
			u.Path = "/announce"
			trackers[i] = u.String()
		}
	}
}

// calculateWorkerCount determines the optimal number of workers for tracker requests.
func calculateWorkerCount(trackerCount int) int {
	wlen := trackerCount
	if wlen > 10 {
		wlen = 10
	}
	return wlen
}

// createRequestChannel creates and populates a channel with tracker URLs.
func createRequestChannel(trackers []string, workerCount int) chan string {
	reqs := make(chan string, workerCount)
	go func() {
		for i := 0; i < len(trackers); i++ {
			reqs <- trackers[i]
		}
	}()
	return reqs
}

// spawnWorkerRoutines creates worker goroutines to process tracker requests concurrently.
func spawnWorkerRoutines(ctx context.Context, workerCount int, reqs chan string,
	wg *sync.WaitGroup, id, infohash metainfo.Hash, lock *sync.Mutex, results *[]GetPeersResult) {
	for i := 0; i < workerCount; i++ {
		go func() {
			for tracker := range reqs {
				resp, err := getPeers(ctx, wg, tracker, id, infohash)
				lock.Lock()
				*results = append(*results, GetPeersResult{
					Tracker: tracker,
					Error:   err,
					Resp:    resp,
				})
				lock.Unlock()
			}
		}()
	}
}

// GetPeers gets the peers from the trackers.
//
// Notice: the returned chan will be closed when all the requests end.
func GetPeers(ctx context.Context, id, infohash metainfo.Hash, trackers []string) []GetPeersResult {
	if len(trackers) == 0 {
		return nil
	}

	normalizeTrackerURLs(trackers)

	trackerCount := len(trackers)
	workerCount := calculateWorkerCount(trackerCount)

	reqs := createRequestChannel(trackers, workerCount)

	wg := new(sync.WaitGroup)
	wg.Add(trackerCount)

	var lock sync.Mutex
	results := make([]GetPeersResult, 0, trackerCount)

	spawnWorkerRoutines(ctx, workerCount, reqs, wg, id, infohash, &lock, &results)

	wg.Wait()
	close(reqs)

	return results
}

func getPeers(ctx context.Context, wg *sync.WaitGroup, tracker string,
	nodeID, infoHash metainfo.Hash) (resp AnnounceResponse, err error) {
	defer wg.Done()

	client, err := NewClient(tracker, ClientConfig{ID: nodeID})
	if err == nil {
		resp, err = client.Announce(ctx, AnnounceRequest{InfoHash: infoHash, IP: PeerAddress})
	}
	return
}
