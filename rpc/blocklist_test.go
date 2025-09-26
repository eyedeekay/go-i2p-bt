package rpc

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewBlocklistManager(t *testing.T) {
	bm := NewBlocklistManager()

	if bm == nil {
		t.Fatal("NewBlocklistManager returned nil")
	}

	if bm.IsEnabled() {
		t.Error("New blocklist manager should be disabled by default")
	}

	if bm.GetURL() != "" {
		t.Error("New blocklist manager should have empty URL")
	}

	if bm.GetSize() != 0 {
		t.Error("New blocklist manager should have zero size")
	}
}

func TestBlocklistManager_EnableDisable(t *testing.T) {
	bm := NewBlocklistManager()

	// Test enabling
	bm.SetEnabled(true)
	if !bm.IsEnabled() {
		t.Error("SetEnabled(true) failed")
	}

	// Test disabling
	bm.SetEnabled(false)
	if bm.IsEnabled() {
		t.Error("SetEnabled(false) failed")
	}
}

func TestBlocklistManager_SetURL(t *testing.T) {
	bm := NewBlocklistManager()

	testURL := "http://example.com/blocklist.dat"

	err := bm.SetURL(testURL)
	if err != nil {
		t.Fatalf("SetURL failed: %v", err)
	}

	if bm.GetURL() != testURL {
		t.Errorf("Expected URL %s, got %s", testURL, bm.GetURL())
	}

	// Setting same URL should not error
	err = bm.SetURL(testURL)
	if err != nil {
		t.Errorf("Setting same URL should not error: %v", err)
	}
}

func TestBlocklistManager_IsBlocked_Disabled(t *testing.T) {
	bm := NewBlocklistManager()
	bm.SetEnabled(false)

	// Even with ranges loaded, disabled blocklist should not block
	bm.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.1"), end: net.ParseIP("192.168.1.255")},
	}

	if bm.IsBlocked("192.168.1.100") {
		t.Error("Disabled blocklist should not block any IPs")
	}
}

func TestBlocklistManager_IsBlocked_InvalidIP(t *testing.T) {
	bm := NewBlocklistManager()
	bm.SetEnabled(true)

	// Invalid IPs should be blocked
	if !bm.IsBlocked("invalid-ip") {
		t.Error("Invalid IP should be blocked")
	}

	if !bm.IsBlocked("") {
		t.Error("Empty IP should be blocked")
	}

	if !bm.IsBlocked("999.999.999.999") {
		t.Error("Out of range IP should be blocked")
	}
}

func TestBlocklistManager_IsBlocked_SingleIP(t *testing.T) {
	bm := NewBlocklistManager()
	bm.SetEnabled(true)

	// Add a single IP to blocklist
	bm.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.100"), end: net.ParseIP("192.168.1.100")},
	}

	// Test blocked IP
	if !bm.IsBlocked("192.168.1.100") {
		t.Error("IP 192.168.1.100 should be blocked")
	}

	// Test non-blocked IPs
	if bm.IsBlocked("192.168.1.99") {
		t.Error("IP 192.168.1.99 should not be blocked")
	}

	if bm.IsBlocked("192.168.1.101") {
		t.Error("IP 192.168.1.101 should not be blocked")
	}

	if bm.IsBlocked("10.0.0.1") {
		t.Error("IP 10.0.0.1 should not be blocked")
	}
}

func TestBlocklistManager_IsBlocked_IPRange(t *testing.T) {
	bm := NewBlocklistManager()
	bm.SetEnabled(true)

	// Add an IP range to blocklist
	bm.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.1"), end: net.ParseIP("192.168.1.255")},
	}

	// Test IPs within range
	testIPs := []string{
		"192.168.1.1",   // Start of range
		"192.168.1.100", // Middle of range
		"192.168.1.255", // End of range
	}

	for _, ip := range testIPs {
		if !bm.IsBlocked(ip) {
			t.Errorf("IP %s should be blocked (within range)", ip)
		}
	}

	// Test IPs outside range
	outsideIPs := []string{
		"192.168.0.255", // Just before range
		"192.168.2.1",   // Just after range
		"10.0.0.1",      // Different network
	}

	for _, ip := range outsideIPs {
		if bm.IsBlocked(ip) {
			t.Errorf("IP %s should not be blocked (outside range)", ip)
		}
	}
}

func TestBlocklistManager_IsBlocked_MultipleRanges(t *testing.T) {
	bm := NewBlocklistManager()
	bm.SetEnabled(true)

	// Add multiple ranges
	bm.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.1"), end: net.ParseIP("192.168.1.255")},
		{start: net.ParseIP("10.0.0.1"), end: net.ParseIP("10.0.0.100")},
		{start: net.ParseIP("172.16.0.1"), end: net.ParseIP("172.16.0.1")}, // Single IP
	}

	// Test IPs in different ranges
	blockedIPs := []string{
		"192.168.1.50", // First range
		"10.0.0.50",    // Second range
		"172.16.0.1",   // Third range (single IP)
	}

	for _, ip := range blockedIPs {
		if !bm.IsBlocked(ip) {
			t.Errorf("IP %s should be blocked", ip)
		}
	}

	// Test IPs not in any range
	allowedIPs := []string{
		"8.8.8.8",     // Public DNS
		"1.1.1.1",     // Cloudflare DNS
		"192.168.2.1", // Different private range
		"10.0.1.1",    // Outside second range
		"172.16.0.2",  // Next to single IP
	}

	for _, ip := range allowedIPs {
		if bm.IsBlocked(ip) {
			t.Errorf("IP %s should not be blocked", ip)
		}
	}
}

func TestBlocklistManager_parseBlocklist_PlainTextIPs(t *testing.T) {
	bm := NewBlocklistManager()

	input := `# Comment line
192.168.1.100
10.0.0.1

# Another comment
8.8.8.8`

	ranges, err := bm.parseBlocklist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseBlocklist failed: %v", err)
	}

	if len(ranges) != 3 {
		t.Errorf("Expected 3 ranges, got %d", len(ranges))
	}

	// Check that all are single IP ranges - ranges are sorted, so we need to find them
	expectedIPs := map[string]bool{"192.168.1.100": false, "10.0.0.1": false, "8.8.8.8": false}
	
	for _, r := range ranges {
		ipStr := r.start.String()
		if found, exists := expectedIPs[ipStr]; exists {
			if found {
				t.Errorf("Duplicate IP range found: %s", ipStr)
			}
			expectedIPs[ipStr] = true
			if !r.start.Equal(r.end) {
				t.Errorf("Expected single IP range for %s, but start != end", ipStr)
			}
		} else {
			t.Errorf("Unexpected IP range found: %s", ipStr)
		}
	}
	
	// Verify all expected IPs were found
	for ip, found := range expectedIPs {
		if !found {
			t.Errorf("Expected IP range not found: %s", ip)
		}
	}
}

func TestBlocklistManager_parseBlocklist_CIDR(t *testing.T) {
	bm := NewBlocklistManager()

	input := `192.168.1.0/24
10.0.0.0/8`

	ranges, err := bm.parseBlocklist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseBlocklist failed: %v", err)
	}

	if len(ranges) != 2 {
		t.Errorf("Expected 2 ranges, got %d", len(ranges))
	}

	// Find the ranges - they are sorted so we need to identify them by content
	found192 := false
	found10 := false
	
	for _, r := range ranges {
		startStr := r.start.String()
		if startStr == "192.168.1.0" {
			found192 = true
			if !r.end.Equal(net.ParseIP("192.168.1.255")) {
				t.Errorf("192.168.1.0/24 range end: expected 192.168.1.255, got %s", r.end)
			}
		} else if startStr == "10.0.0.0" {
			found10 = true
			if !r.end.Equal(net.ParseIP("10.255.255.255")) {
				t.Errorf("10.0.0.0/8 range end: expected 10.255.255.255, got %s", r.end)
			}
		}
	}
	
	if !found192 {
		t.Error("192.168.1.0/24 range not found")
	}
	if !found10 {
		t.Error("10.0.0.0/8 range not found")
	}
}

func TestBlocklistManager_parseBlocklist_DATFormat(t *testing.T) {
	bm := NewBlocklistManager()

	input := `Example Range:192.168.1.1-192.168.1.255
Bad Peer Network:10.0.0.1-10.0.0.100`

	ranges, err := bm.parseBlocklist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseBlocklist failed: %v", err)
	}

	if len(ranges) != 2 {
		t.Errorf("Expected 2 ranges, got %d", len(ranges))
	}

	// Find the ranges by their start IPs
	found192 := false
	found10 := false
	
	for _, r := range ranges {
		startStr := r.start.String()
		if startStr == "192.168.1.1" {
			found192 = true
			if !r.end.Equal(net.ParseIP("192.168.1.255")) {
				t.Errorf("192.168.1.1-255 range end: expected 192.168.1.255, got %s", r.end)
			}
		} else if startStr == "10.0.0.1" {
			found10 = true
			if !r.end.Equal(net.ParseIP("10.0.0.100")) {
				t.Errorf("10.0.0.1-100 range end: expected 10.0.0.100, got %s", r.end)
			}
		}
	}
	
	if !found192 {
		t.Error("192.168.1.1-255 range not found")
	}
	if !found10 {
		t.Error("10.0.0.1-100 range not found")
	}
}

func TestBlocklistManager_parseBlocklist_MixedFormats(t *testing.T) {
	bm := NewBlocklistManager()

	input := `# Mixed format blocklist
192.168.1.100
Range:10.0.0.1-10.0.0.255
172.16.0.0/16
8.8.8.8`

	ranges, err := bm.parseBlocklist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseBlocklist failed: %v", err)
	}

	if len(ranges) != 4 {
		t.Errorf("Expected 4 ranges, got %d", len(ranges))
	}
}

func TestBlocklistManager_Update_HTTPServer(t *testing.T) {
	// Create test HTTP server
	blocklist := `# Test blocklist
192.168.1.100
Bad Range:10.0.0.1-10.0.0.255`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		w.Header().Set("ETag", "\"test-etag\"")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(blocklist))
	}))
	defer server.Close()

	bm := NewBlocklistManager()
	bm.SetEnabled(true)

	err := bm.SetURL(server.URL)
	if err != nil {
		t.Fatalf("SetURL failed: %v", err)
	}

	err = bm.Update()
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Check that blocklist was loaded
	if bm.GetSize() != 2 {
		t.Errorf("Expected size 2, got %d", bm.GetSize())
	}

	// Test blocking
	if !bm.IsBlocked("192.168.1.100") {
		t.Error("192.168.1.100 should be blocked")
	}

	if !bm.IsBlocked("10.0.0.50") {
		t.Error("10.0.0.50 should be blocked (in range)")
	}

	if bm.IsBlocked("8.8.8.8") {
		t.Error("8.8.8.8 should not be blocked")
	}
}

func TestBlocklistManager_Update_NotModified(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// First request - return content
		if requestCount == 1 {
			w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
			w.Header().Set("ETag", "\"test-etag\"")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("192.168.1.100"))
			return
		}

		// Check for caching headers on subsequent requests
		if r.Header.Get("If-Modified-Since") == "" {
			t.Error("Expected If-Modified-Since header on subsequent request")
		}
		if r.Header.Get("If-None-Match") == "" {
			t.Error("Expected If-None-Match header on subsequent request")
		}

		// Return 304 Not Modified
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	bm := NewBlocklistManager()
	bm.SetEnabled(false) // Disable to prevent auto-update on SetURL
	bm.SetURL(server.URL)
	bm.SetEnabled(true) // Now enable it

	// First update
	err := bm.Update()
	if err != nil {
		t.Fatalf("First update failed: %v", err)
	}

	if bm.GetSize() != 1 {
		t.Errorf("Expected size 1 after first update, got %d", bm.GetSize())
	}

	// Second update - should get 304
	err = bm.Update()
	if err != nil {
		t.Fatalf("Second update failed: %v", err)
	}

	// Size should remain the same
	if bm.GetSize() != 1 {
		t.Errorf("Expected size 1 after second update, got %d", bm.GetSize())
	}

	if requestCount != 2 {
		t.Errorf("Expected 2 HTTP requests, got %d", requestCount)
	}
}

func TestBlocklistManager_Update_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	bm := NewBlocklistManager()
	bm.SetURL(server.URL)

	err := bm.Update()
	if err == nil {
		t.Error("Expected error for HTTP 500 response")
	}

	if !strings.Contains(err.Error(), "unexpected status code") {
		t.Errorf("Expected 'unexpected status code' error, got: %v", err)
	}
}

func TestBlocklistManager_Update_NoURL(t *testing.T) {
	bm := NewBlocklistManager()

	err := bm.Update()
	if err == nil {
		t.Error("Expected error when no URL is configured")
	}

	if !strings.Contains(err.Error(), "no blocklist URL configured") {
		t.Errorf("Expected 'no blocklist URL configured' error, got: %v", err)
	}
}

func TestBlocklistManager_cidrToRange(t *testing.T) {
	bm := NewBlocklistManager()

	// Test /24 network
	_, cidr, err := net.ParseCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatalf("Failed to parse CIDR: %v", err)
	}

	start, end := bm.cidrToRange(cidr)

	if !start.Equal(net.ParseIP("192.168.1.0")) {
		t.Errorf("Expected start 192.168.1.0, got %s", start)
	}

	if !end.Equal(net.ParseIP("192.168.1.255")) {
		t.Errorf("Expected end 192.168.1.255, got %s", end)
	}
}

func TestBlocklistManager_compareIPs(t *testing.T) {
	bm := NewBlocklistManager()

	tests := []struct {
		a, b     string
		expected int
	}{
		{"192.168.1.1", "192.168.1.1", 0},  // Equal
		{"192.168.1.1", "192.168.1.2", -1}, // A < B
		{"192.168.1.2", "192.168.1.1", 1},  // A > B
		{"10.0.0.1", "192.168.1.1", -1},    // Different networks
		{"255.255.255.255", "0.0.0.0", 1},  // Max vs Min
	}

	for _, test := range tests {
		result := bm.compareIPs(net.ParseIP(test.a), net.ParseIP(test.b))
		if result != test.expected {
			t.Errorf("compareIPs(%s, %s): expected %d, got %d",
				test.a, test.b, test.expected, result)
		}
	}
}

func TestBlocklistManager_Integration(t *testing.T) {
	// Test complete integration with various scenarios
	bm := NewBlocklistManager()

	// Start disabled
	if bm.IsBlocked("192.168.1.100") {
		t.Error("Should not block when disabled")
	}

	// Enable and add ranges
	bm.SetEnabled(true)
	bm.ipRanges = []ipRange{
		{start: net.ParseIP("192.168.1.1"), end: net.ParseIP("192.168.1.255")},
		{start: net.ParseIP("10.0.0.1"), end: net.ParseIP("10.0.0.1")}, // Single IP
	}
	bm.size = int64(len(bm.ipRanges))

	// Test various IPs
	testCases := []struct {
		ip      string
		blocked bool
		reason  string
	}{
		{"192.168.1.100", true, "within first range"},
		{"192.168.1.1", true, "start of first range"},
		{"192.168.1.255", true, "end of first range"},
		{"10.0.0.1", true, "single IP block"},
		{"10.0.0.2", false, "outside single IP block"},
		{"8.8.8.8", false, "not in any range"},
		{"invalid-ip", true, "invalid IP should be blocked"},
	}

	for _, tc := range testCases {
		blocked := bm.IsBlocked(tc.ip)
		if blocked != tc.blocked {
			t.Errorf("IP %s (%s): expected blocked=%t, got %t",
				tc.ip, tc.reason, tc.blocked, blocked)
		}
	}

	// Test disable
	bm.SetEnabled(false)
	if bm.IsBlocked("192.168.1.100") {
		t.Error("Should not block when disabled, even with ranges loaded")
	}
}
