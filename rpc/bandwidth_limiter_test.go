package rpc

import (
	"context"
	"testing"
	"time"
)

func TestBandwidthLimiter(t *testing.T) {
	t.Run("Unlimited bandwidth", func(t *testing.T) {
		limiter := NewBandwidthLimiter(0, true)
		ctx := context.Background()

		// Should allow immediate transfer regardless of size
		start := time.Now()
		err := limiter.WaitForTokens(ctx, 1024*1024) // 1MB
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if elapsed > 10*time.Millisecond {
			t.Errorf("Expected immediate transfer, took %v", elapsed)
		}
	})

	t.Run("Disabled limiter", func(t *testing.T) {
		limiter := NewBandwidthLimiter(1024, false)
		ctx := context.Background()

		// Should allow immediate transfer when disabled
		start := time.Now()
		err := limiter.WaitForTokens(ctx, 2048) // More than limit
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if elapsed > 10*time.Millisecond {
			t.Errorf("Expected immediate transfer when disabled, took %v", elapsed)
		}
	})

	t.Run("Small transfer within limit", func(t *testing.T) {
		limiter := NewBandwidthLimiter(1024, true) // 1KB/s
		ctx := context.Background()

		// Should allow small transfer immediately
		start := time.Now()
		err := limiter.WaitForTokens(ctx, 512) // 0.5KB
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if elapsed > 50*time.Millisecond {
			t.Errorf("Expected fast transfer for small amount, took %v", elapsed)
		}
	})

	t.Run("Large transfer requiring wait", func(t *testing.T) {
		limiter := NewBandwidthLimiter(100, true) // 100 bytes/s
		ctx := context.Background()

		// Should wait for large transfer
		start := time.Now()
		err := limiter.WaitForTokens(ctx, 200) // 2 seconds worth
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		// Should wait approximately 1 second (we had 100 tokens, need 200)
		if elapsed < 800*time.Millisecond || elapsed > 1500*time.Millisecond {
			t.Errorf("Expected ~1s wait, got %v", elapsed)
		}
	})

	t.Run("Context cancellation", func(t *testing.T) {
		limiter := NewBandwidthLimiter(10, true) // Very slow: 10 bytes/s
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Should be cancelled before completion
		start := time.Now()
		err := limiter.WaitForTokens(ctx, 100) // Would take 9+ seconds
		elapsed := time.Since(start)

		if err != context.DeadlineExceeded {
			t.Errorf("Expected context.DeadlineExceeded, got %v", err)
		}
		if elapsed > 200*time.Millisecond {
			t.Errorf("Expected quick cancellation, took %v", elapsed)
		}
	})

	t.Run("Zero byte request", func(t *testing.T) {
		limiter := NewBandwidthLimiter(100, true)
		ctx := context.Background()

		// Should handle zero bytes gracefully
		err := limiter.WaitForTokens(ctx, 0)
		if err != nil {
			t.Errorf("Expected no error for zero bytes, got %v", err)
		}
	})

	t.Run("Negative byte request", func(t *testing.T) {
		limiter := NewBandwidthLimiter(100, true)
		ctx := context.Background()

		// Should handle negative bytes gracefully
		err := limiter.WaitForTokens(ctx, -10)
		if err != nil {
			t.Errorf("Expected no error for negative bytes, got %v", err)
		}
	})
}

func TestBandwidthLimiterConfiguration(t *testing.T) {
	t.Run("Update configuration", func(t *testing.T) {
		limiter := NewBandwidthLimiter(1000, true)

		// Update configuration
		limiter.UpdateConfiguration(2000, false)

		maxBytes, enabled := limiter.GetConfiguration()
		if maxBytes != 2000 {
			t.Errorf("Expected maxBytesPerSecond=2000, got %d", maxBytes)
		}
		if enabled {
			t.Errorf("Expected enabled=false, got %t", enabled)
		}
	})

	t.Run("Get stats", func(t *testing.T) {
		limiter := NewBandwidthLimiter(1000, true)

		// Get initial stats
		tokens, maxTokens := limiter.GetStats()
		if maxTokens != 1000 {
			t.Errorf("Expected maxTokens=1000, got %d", maxTokens)
		}
		if tokens < 0 || tokens > maxTokens {
			t.Errorf("Expected tokens in range [0, %d], got %d", maxTokens, tokens)
		}
	})
}

func TestBandwidthManager(t *testing.T) {
	t.Run("Create bandwidth manager", func(t *testing.T) {
		config := SessionConfiguration{
			SpeedLimitDown:        1024,
			SpeedLimitDownEnabled: true,
			SpeedLimitUp:          512,
			SpeedLimitUpEnabled:   false,
		}

		manager := NewBandwidthManager(config)
		if manager == nil {
			t.Fatal("Expected non-nil bandwidth manager")
		}

		// Test initial configuration
		downTokens, downMax, upTokens, upMax := manager.GetStats()
		if downMax != 1024 {
			t.Errorf("Expected download max=1024, got %d", downMax)
		}
		if upMax != 0 { // Disabled should result in 0 max tokens
			t.Errorf("Expected upload max=0 (disabled), got %d", upMax)
		}
		if downTokens < 0 || downTokens > downMax {
			t.Errorf("Expected download tokens in range [0, %d], got %d", downMax, downTokens)
		}
		if upTokens != 0 { // Disabled should have no tokens
			t.Errorf("Expected upload tokens=0 (disabled), got %d", upTokens)
		}
	})

	t.Run("Download and upload operations", func(t *testing.T) {
		config := SessionConfiguration{
			SpeedLimitDown:        1000,
			SpeedLimitDownEnabled: true,
			SpeedLimitUp:          500,
			SpeedLimitUpEnabled:   true,
		}

		manager := NewBandwidthManager(config)
		ctx := context.Background()

		// Test small download
		start := time.Now()
		err := manager.WaitForDownload(ctx, 100)
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("Expected no error for download, got %v", err)
		}
		if elapsed > 50*time.Millisecond {
			t.Errorf("Expected fast download, took %v", elapsed)
		}

		// Test small upload
		start = time.Now()
		err = manager.WaitForUpload(ctx, 50)
		elapsed = time.Since(start)

		if err != nil {
			t.Errorf("Expected no error for upload, got %v", err)
		}
		if elapsed > 50*time.Millisecond {
			t.Errorf("Expected fast upload, took %v", elapsed)
		}
	})

	t.Run("Update configuration", func(t *testing.T) {
		config := SessionConfiguration{
			SpeedLimitDown:        1000,
			SpeedLimitDownEnabled: true,
			SpeedLimitUp:          500,
			SpeedLimitUpEnabled:   true,
		}

		manager := NewBandwidthManager(config)

		// Update configuration
		newConfig := SessionConfiguration{
			SpeedLimitDown:        2000,
			SpeedLimitDownEnabled: false,
			SpeedLimitUp:          1000,
			SpeedLimitUpEnabled:   true,
		}

		manager.UpdateConfiguration(newConfig)

		// Verify new configuration
		downTokens, downMax, upTokens, upMax := manager.GetStats()
		if downMax != 0 { // Disabled should result in 0 max tokens
			t.Errorf("Expected download max=0 (disabled), got %d", downMax)
		}
		if upMax != 1000 {
			t.Errorf("Expected upload max=1000, got %d", upMax)
		}
		if downTokens != 0 { // Disabled should have no tokens
			t.Errorf("Expected download tokens=0 (disabled), got %d", downTokens)
		}
		if upTokens < 0 || upTokens > upMax {
			t.Errorf("Expected upload tokens in range [0, %d], got %d", upMax, upTokens)
		}
	})

	t.Run("String representation", func(t *testing.T) {
		config := SessionConfiguration{
			SpeedLimitDown:        1024,
			SpeedLimitDownEnabled: true,
			SpeedLimitUp:          0,
			SpeedLimitUpEnabled:   false,
		}

		manager := NewBandwidthManager(config)
		str := manager.String()

		if str == "" {
			t.Error("Expected non-empty string representation")
		}

		// Should mention both download and upload status
		if len(str) < 20 {
			t.Errorf("Expected detailed string representation, got: %s", str)
		}
	})
}

func TestBandwidthLimiterConcurrency(t *testing.T) {
	t.Run("Concurrent requests", func(t *testing.T) {
		limiter := NewBandwidthLimiter(500, true) // 500 bytes/s
		ctx := context.Background()

		// Start multiple concurrent requests that exceed initial capacity
		const numRequests = 5
		const bytesPerRequest = 200 // Total: 1000 bytes, capacity: 500 bytes

		startTime := time.Now()
		results := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func() {
				results <- limiter.WaitForTokens(ctx, bytesPerRequest)
			}()
		}

		// Wait for all requests to complete
		for i := 0; i < numRequests; i++ {
			err := <-results
			if err != nil {
				t.Errorf("Request %d failed: %v", i, err)
			}
		}

		elapsed := time.Since(startTime)

		// All requests together need 1000 bytes, we have 500/s capacity with 500 initial
		// Should need at least 1 second to generate the remaining 500 bytes
		if elapsed < 500*time.Millisecond {
			t.Errorf("Expected significant delay for concurrent requests exceeding capacity, completed in %v", elapsed)
		}
		if elapsed > 2*time.Second {
			t.Errorf("Expected reasonable completion time, took %v", elapsed)
		}
	})
}

func BenchmarkBandwidthLimiter(b *testing.B) {
	limiter := NewBandwidthLimiter(1024*1024, true) // 1MB/s - should be fast enough for benchmarking
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.WaitForTokens(ctx, 100) // Small request
	}
}

func BenchmarkBandwidthManager(b *testing.B) {
	config := SessionConfiguration{
		SpeedLimitDown:        1024 * 1024,
		SpeedLimitDownEnabled: true,
		SpeedLimitUp:          1024 * 1024,
		SpeedLimitUpEnabled:   true,
	}

	manager := NewBandwidthManager(config)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			manager.WaitForDownload(ctx, 100)
		} else {
			manager.WaitForUpload(ctx, 100)
		}
	}
}
