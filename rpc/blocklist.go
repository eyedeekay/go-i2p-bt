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
	url, lastModified, etag := bm.getCacheInfo()

	if url == "" {
		return fmt.Errorf("no blocklist URL configured")
	}

	resp, err := bm.fetchBlocklistWithCache(url, lastModified, etag)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return bm.processBlocklistResponse(resp)
}

// getCacheInfo retrieves the current URL and caching information thread-safely
func (bm *BlocklistManager) getCacheInfo() (url, lastModified, etag string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.url, bm.lastModified, bm.etag
}

// fetchBlocklistWithCache creates and executes an HTTP request with appropriate caching headers
func (bm *BlocklistManager) fetchBlocklistWithCache(url, lastModified, etag string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := bm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blocklist: %w", err)
	}

	return resp, nil
}

// processBlocklistResponse handles the HTTP response and updates the blocklist if necessary
func (bm *BlocklistManager) processBlocklistResponse(resp *http.Response) error {
	// Handle not modified
	if resp.StatusCode == http.StatusNotModified {
		bm.updateLastModifiedTime()
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

	bm.updateBlocklistData(ranges, resp.Header)
	return nil
}

// updateLastModifiedTime updates only the last updated timestamp when content is not modified
func (bm *BlocklistManager) updateLastModifiedTime() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.lastUpdated = time.Now()
}

// updateBlocklistData atomically updates the blocklist with new data and metadata
func (bm *BlocklistManager) updateBlocklistData(ranges []ipRange, headers http.Header) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.ipRanges = ranges
	bm.lastModified = headers.Get("Last-Modified")
	bm.etag = headers.Get("ETag")
	bm.lastUpdated = time.Now()
	atomic.StoreInt64(&bm.size, int64(len(ranges)))
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

		// Try parsing different formats in order of likelihood
		if ipRange, parsed := bm.parseCIDRLine(line); parsed {
			ranges = append(ranges, ipRange)
			continue
		}

		if ipRange, parsed := bm.parseDATFormatLine(line); parsed {
			ranges = append(ranges, ipRange)
			continue
		}

		if ipRange, parsed := bm.parseSingleIPLine(line); parsed {
			ranges = append(ranges, ipRange)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	bm.sortIPRanges(ranges)
	return ranges, nil
}

// parseCIDRLine attempts to parse a line as a CIDR block.
// Returns the parsed IP range and true if successful, zero value and false otherwise.
func (bm *BlocklistManager) parseCIDRLine(line string) (ipRange, bool) {
	if !strings.Contains(line, "/") {
		return ipRange{}, false
	}

	_, cidr, err := net.ParseCIDR(line)
	if err != nil {
		return ipRange{}, false
	}

	start, end := bm.cidrToRange(cidr)
	return ipRange{start: start, end: end}, true
}

// parseDATFormatLine attempts to parse a line in DAT format (name:start-end).
// Returns the parsed IP range and true if successful, zero value and false otherwise.
func (bm *BlocklistManager) parseDATFormatLine(line string) (ipRange, bool) {
	if !strings.Contains(line, ":") || !strings.Contains(line, "-") {
		return ipRange{}, false
	}

	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return ipRange{}, false
	}

	rangePart := parts[1]
	if !strings.Contains(rangePart, "-") {
		return ipRange{}, false
	}

	rangeParts := strings.SplitN(rangePart, "-", 2)
	if len(rangeParts) != 2 {
		return ipRange{}, false
	}

	start := net.ParseIP(strings.TrimSpace(rangeParts[0]))
	end := net.ParseIP(strings.TrimSpace(rangeParts[1]))
	if start == nil || end == nil {
		return ipRange{}, false
	}

	return ipRange{start: start, end: end}, true
}

// parseSingleIPLine attempts to parse a line as a single IP address.
// Returns the parsed IP range (start == end) and true if successful, zero value and false otherwise.
func (bm *BlocklistManager) parseSingleIPLine(line string) (ipRange, bool) {
	ip := net.ParseIP(line)
	if ip == nil {
		return ipRange{}, false
	}
	return ipRange{start: ip, end: ip}, true
}

// sortIPRanges sorts the provided IP ranges for efficient binary search lookup.
// Modifies the slice in place using the BlocklistManager's IP comparison function.
func (bm *BlocklistManager) sortIPRanges(ranges []ipRange) {
	sort.Slice(ranges, func(i, j int) bool {
		return bm.compareIPs(ranges[i].start, ranges[j].start) < 0
	})
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

// normalizeIPAddresses ensures both IP addresses have the same format for comparison.
func (bm *BlocklistManager) normalizeIPAddresses(a, b net.IP) (net.IP, net.IP) {
	if len(a) != len(b) {
		if len(a) == 4 && len(b) == 16 {
			a = a.To16()
		} else if len(a) == 16 && len(b) == 4 {
			b = b.To16()
		}
	}
	return a, b
}

// compareIPBytes performs byte-by-byte comparison of normalized IP addresses.
func (bm *BlocklistManager) compareIPBytes(a, b net.IP) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// compareIPLengths compares IP address lengths when byte comparison is equal.
func (bm *BlocklistManager) compareIPLengths(a, b net.IP) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// compareIPs compares two IP addresses.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func (bm *BlocklistManager) compareIPs(a, b net.IP) int {
	// Normalize IP addresses to same format
	normalizedA, normalizedB := bm.normalizeIPAddresses(a, b)

	// Compare bytes
	if result := bm.compareIPBytes(normalizedA, normalizedB); result != 0 {
		return result
	}

	// Compare lengths if bytes are equal
	return bm.compareIPLengths(normalizedA, normalizedB)
}

// GetLastUpdated returns the time when the blocklist was last updated.
func (bm *BlocklistManager) GetLastUpdated() time.Time {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.lastUpdated
}
