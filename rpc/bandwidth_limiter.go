package rpc

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// BandwidthLimiter provides rate limiting for network operations
// using a token bucket algorithm for smooth bandwidth allocation
type BandwidthLimiter struct {
	// Configuration
	maxBytesPerSecond int64
	enabled           bool

	// Token bucket state
	tokens    int64
	maxTokens int64
	lastFill  time.Time

	// Synchronization
	mu       sync.Mutex
	fillRate time.Duration
}

// NewBandwidthLimiter creates a new bandwidth limiter
// maxBytesPerSecond: maximum bytes per second (0 = unlimited)
// enabled: whether the limiter is active
func NewBandwidthLimiter(maxBytesPerSecond int64, enabled bool) *BandwidthLimiter {
	now := time.Now()
	limiter := &BandwidthLimiter{
		maxBytesPerSecond: maxBytesPerSecond,
		enabled:           enabled,
		lastFill:          now,
		fillRate:          time.Millisecond * 10, // Fill tokens every 10ms for smooth rate limiting
	}

	// Initialize token bucket with full capacity
	if enabled && maxBytesPerSecond > 0 {
		limiter.maxTokens = maxBytesPerSecond
		limiter.tokens = maxBytesPerSecond // Start with full bucket
	}

	return limiter
}

// updateTokenBucket fills the token bucket based on elapsed time
// Must be called with mu locked
func (bl *BandwidthLimiter) updateTokenBucket() {
	if !bl.enabled || bl.maxBytesPerSecond <= 0 {
		bl.maxTokens = 0
		bl.tokens = 0
		return
	}

	now := time.Now()
	elapsed := now.Sub(bl.lastFill)

	// Calculate how many tokens to add based on elapsed time
	// Allow bursts up to 1 second worth of bandwidth
	bl.maxTokens = bl.maxBytesPerSecond
	tokensToAdd := int64(float64(bl.maxBytesPerSecond) * elapsed.Seconds())

	bl.tokens += tokensToAdd
	if bl.tokens > bl.maxTokens {
		bl.tokens = bl.maxTokens
	}

	bl.lastFill = now
}

// WaitForTokens blocks until the specified number of bytes can be transferred
// Returns immediately if the limiter is disabled or unlimited
func (bl *BandwidthLimiter) WaitForTokens(ctx context.Context, bytes int64) error {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	// If disabled or unlimited, allow immediately
	if !bl.enabled || bl.maxBytesPerSecond <= 0 {
		return nil
	}

	// Handle zero or negative byte requests
	if bytes <= 0 {
		return nil
	}

	// Update token bucket
	bl.updateTokenBucket()

	// If we have enough tokens, consume them and return
	if bl.tokens >= bytes {
		bl.tokens -= bytes
		return nil
	}

	// Calculate how long we need to wait for enough tokens
	tokensNeeded := bytes - bl.tokens
	waitTime := time.Duration(float64(tokensNeeded) / float64(bl.maxBytesPerSecond) * float64(time.Second))

	// Consume all available tokens now
	bl.tokens = 0

	// Wait for the required time, checking for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(waitTime):
		// After waiting, we should have enough tokens
		return nil
	}
}

// UpdateConfiguration updates the limiter's configuration at runtime
func (bl *BandwidthLimiter) UpdateConfiguration(maxBytesPerSecond int64, enabled bool) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	bl.maxBytesPerSecond = maxBytesPerSecond
	bl.enabled = enabled
	bl.lastFill = time.Now()

	// Reset token bucket with new configuration
	bl.updateTokenBucket()
}

// GetConfiguration returns the current limiter configuration
func (bl *BandwidthLimiter) GetConfiguration() (maxBytesPerSecond int64, enabled bool) {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	return bl.maxBytesPerSecond, bl.enabled
}

// GetStats returns current limiter statistics
func (bl *BandwidthLimiter) GetStats() (availableTokens, maxTokens int64) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	bl.updateTokenBucket()
	return bl.tokens, bl.maxTokens
}

// BandwidthManager manages both download and upload bandwidth limiting
type BandwidthManager struct {
	downloadLimiter *BandwidthLimiter
	uploadLimiter   *BandwidthLimiter
}

// NewBandwidthManager creates a new bandwidth manager with separate download/upload limiters
func NewBandwidthManager(config SessionConfiguration) *BandwidthManager {
	return &BandwidthManager{
		downloadLimiter: NewBandwidthLimiter(config.SpeedLimitDown, config.SpeedLimitDownEnabled),
		uploadLimiter:   NewBandwidthLimiter(config.SpeedLimitUp, config.SpeedLimitUpEnabled),
	}
}

// WaitForDownload blocks until the specified number of download bytes can be transferred
func (bm *BandwidthManager) WaitForDownload(ctx context.Context, bytes int64) error {
	return bm.downloadLimiter.WaitForTokens(ctx, bytes)
}

// WaitForUpload blocks until the specified number of upload bytes can be transferred
func (bm *BandwidthManager) WaitForUpload(ctx context.Context, bytes int64) error {
	return bm.uploadLimiter.WaitForTokens(ctx, bytes)
}

// UpdateConfiguration updates both limiters with new session configuration
func (bm *BandwidthManager) UpdateConfiguration(config SessionConfiguration) {
	bm.downloadLimiter.UpdateConfiguration(config.SpeedLimitDown, config.SpeedLimitDownEnabled)
	bm.uploadLimiter.UpdateConfiguration(config.SpeedLimitUp, config.SpeedLimitUpEnabled)
}

// GetStats returns statistics for both limiters
func (bm *BandwidthManager) GetStats() (downloadTokens, downloadMax, uploadTokens, uploadMax int64) {
	downloadTokens, downloadMax = bm.downloadLimiter.GetStats()
	uploadTokens, uploadMax = bm.uploadLimiter.GetStats()
	return
}

// String returns a human-readable representation of the bandwidth manager
func (bm *BandwidthManager) String() string {
	downLimit, downEnabled := bm.downloadLimiter.GetConfiguration()
	upLimit, upEnabled := bm.uploadLimiter.GetConfiguration()

	downStatus := "unlimited"
	if downEnabled && downLimit > 0 {
		downStatus = fmt.Sprintf("%d KB/s", downLimit/1024)
	}

	upStatus := "unlimited"
	if upEnabled && upLimit > 0 {
		upStatus = fmt.Sprintf("%d KB/s", upLimit/1024)
	}

	return fmt.Sprintf("BandwidthManager{down: %s, up: %s}", downStatus, upStatus)
}
