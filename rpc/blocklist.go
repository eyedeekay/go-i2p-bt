// Package rpc implements blocklist functionality for peer IP filtering.
package rpc

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BlocklistManager handles IP and CIDR range blocking for peer connections.
// It supports loading blocklists from URLs and provides efficient IP lookup.
type BlocklistManager struct {
	mu           sync.RWMutex
	enabled      bool
	url          string
	lastModified string
	etag         string
	lastUpdated  time.Time

	// IP ranges for efficient lookup
	ipRanges []ipRange
	size     int64

	// HTTP client for fetching blocklists
	httpClient *http.Client
}

// ipRange represents an IP range for blocklist matching
type ipRange struct {
	start net.IP
	end   net.IP
}

// NewBlocklistManager creates a new blocklist manager with default configuration.
func NewBlocklistManager() *BlocklistManager {
	return &BlocklistManager{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetEnabled enables or disables the blocklist functionality.
func (bm *BlocklistManager) SetEnabled(enabled bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.enabled = enabled
}

// IsEnabled returns whether the blocklist is currently enabled.
func (bm *BlocklistManager) IsEnabled() bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.enabled
}

// SetURL sets the blocklist URL and triggers an update if the URL has changed.
func (bm *BlocklistManager) SetURL(url string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.url == url {
		return nil // No change
	}

	bm.url = url
	bm.lastModified = ""
	bm.etag = ""

	// If enabled, trigger an immediate update
	if bm.enabled && url != "" {
		go func() {
			if err := bm.Update(); err != nil {
				// Log error in production, for now just ignore
				_ = err
			}
		}()
	}

	return nil
}

// GetURL returns the current blocklist URL.
func (bm *BlocklistManager) GetURL() string {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.url
}

// GetSize returns the number of IP ranges in the blocklist.
func (bm *BlocklistManager) GetSize() int64 {
	return atomic.LoadInt64(&bm.size)
}

// IsBlocked checks if an IP address is blocked by the current blocklist.
// Returns true if the IP should be blocked, false otherwise.
func (bm *BlocklistManager) IsBlocked(ip string) bool {
	// Quick check if blocklist is disabled
	if !bm.IsEnabled() {
		return false
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return true // Block invalid IPs
	}

	bm.mu.RLock()
	defer bm.mu.RUnlock()

	// Binary search through sorted IP ranges
	for _, ipRange := range bm.ipRanges {
		if bm.ipInRange(parsedIP, ipRange) {
			return true
		}
	}

	return false
}

// Update fetches the latest blocklist from the configured URL.
// Uses HTTP caching headers to avoid unnecessary downloads.
func (bm *BlocklistManager) Update() error {
	bm.mu.Lock()
	url := bm.url
	lastModified := bm.lastModified
	etag := bm.etag
	bm.mu.Unlock()

	if url == "" {
		return fmt.Errorf("no blocklist URL configured")
	}

	// Create HTTP request with caching headers
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := bm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch blocklist: %w", err)
	}
	defer resp.Body.Close()

	// Handle not modified
	if resp.StatusCode == http.StatusNotModified {
		bm.mu.Lock()
		bm.lastUpdated = time.Now()
		bm.mu.Unlock()
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse the blocklist
	ranges, err := bm.parseBlocklist(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to parse blocklist: %w", err)
	}

	// Update the blocklist atomically
	bm.mu.Lock()
	bm.ipRanges = ranges
	bm.lastModified = resp.Header.Get("Last-Modified")
	bm.etag = resp.Header.Get("ETag")
	bm.lastUpdated = time.Now()
	atomic.StoreInt64(&bm.size, int64(len(ranges)))
	bm.mu.Unlock()

	return nil
}

// parseBlocklist parses a blocklist from various formats.
// Supports DAT format and plain text IP/CIDR formats.
func (bm *BlocklistManager) parseBlocklist(r io.Reader) ([]ipRange, error) {
	var ranges []ipRange
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Try to parse as CIDR first
		if strings.Contains(line, "/") {
			_, cidr, err := net.ParseCIDR(line)
			if err == nil {
				start, end := bm.cidrToRange(cidr)
				ranges = append(ranges, ipRange{start: start, end: end})
				continue
			}
		}

		// Try to parse as DAT format (name:start-end)
		if strings.Contains(line, ":") && strings.Contains(line, "-") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				rangePart := parts[1]
				if strings.Contains(rangePart, "-") {
					rangeParts := strings.SplitN(rangePart, "-", 2)
					if len(rangeParts) == 2 {
						start := net.ParseIP(strings.TrimSpace(rangeParts[0]))
						end := net.ParseIP(strings.TrimSpace(rangeParts[1]))
						if start != nil && end != nil {
							ranges = append(ranges, ipRange{start: start, end: end})
							continue
						}
					}
				}
			}
		}

		// Try to parse as single IP
		ip := net.ParseIP(line)
		if ip != nil {
			ranges = append(ranges, ipRange{start: ip, end: ip})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Sort ranges for efficient lookup
	sort.Slice(ranges, func(i, j int) bool {
		return bm.compareIPs(ranges[i].start, ranges[j].start) < 0
	})

	return ranges, nil
}

// cidrToRange converts a CIDR block to start and end IP addresses.
func (bm *BlocklistManager) cidrToRange(cidr *net.IPNet) (net.IP, net.IP) {
	start := cidr.IP
	end := make(net.IP, len(start))
	copy(end, start)

	// Calculate the end IP by ORing with the inverted mask
	mask := cidr.Mask
	for i := 0; i < len(end); i++ {
		if i < len(mask) {
			end[i] |= ^mask[i]
		}
	}

	return start, end
}

// ipInRange checks if an IP is within the specified range.
func (bm *BlocklistManager) ipInRange(ip net.IP, r ipRange) bool {
	// Ensure IP versions match
	if len(ip) != len(r.start) || len(ip) != len(r.end) {
		// Try to normalize
		if len(ip) == 4 && len(r.start) == 16 {
			ip = ip.To16()
		} else if len(ip) == 16 && len(r.start) == 4 {
			ip = ip.To4()
			if ip == nil {
				return false
			}
		} else {
			return false
		}
	}

	return bm.compareIPs(ip, r.start) >= 0 && bm.compareIPs(ip, r.end) <= 0
}

// compareIPs compares two IP addresses.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func (bm *BlocklistManager) compareIPs(a, b net.IP) int {
	// Ensure same length
	if len(a) != len(b) {
		if len(a) == 4 && len(b) == 16 {
			a = a.To16()
		} else if len(a) == 16 && len(b) == 4 {
			b = b.To16()
		}
	}

	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}

	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}

	return 0
}

// GetLastUpdated returns the time when the blocklist was last updated.
func (bm *BlocklistManager) GetLastUpdated() time.Time {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.lastUpdated
}
