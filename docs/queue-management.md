# Queue Management Documentation

## Overview

The go-i2p-bt library includes a comprehensive torrent queue management system that provides Transmission RPC-compatible queue functionality. This system manages download and seed queues to control how many torrents are active simultaneously, following Transmission's queue behavior.

## Features

### Core Queue Management
- **Download Queue**: Controls active downloading torrents
- **Seed Queue**: Controls active seeding torrents  
- **Priority-based Sorting**: Higher priority torrents activate first
- **Manual Position Override**: Allows explicit queue position setting
- **Force Activation**: Bypass queue limits for immediate torrent start

### Performance Characteristics
- **AddToQueue**: ~305µs per operation
- **GetQueuePosition**: ~24.7ns per operation
- **ProcessQueues**: ~87.5ns per processing cycle
- **Concurrent Safe**: Full thread safety with mutex protection

### Transmission RPC Compatibility
- Full support for `queue-position` parameter in `torrent-set`
- Compatible queue status constants (`TorrentStatusQueued*`)
- Automatic queue type selection based on completion status
- Support for `torrent-start-now` bypassing queue limits

## Architecture

### QueueManager

The `QueueManager` is the core component responsible for queue operations:

```go
type QueueManager struct {
    // Queue storage and ordering
    downloadQueue map[int64]*QueueItem
    seedQueue     map[int64]*QueueItem
    downloadOrder []int64
    seedOrder     []int64
    
    // Active torrent tracking
    activeDownloads map[int64]bool
    activeSeeds     map[int64]bool
    
    // Configuration and callbacks
    config QueueConfig
    onTorrentActivated   func(torrentID int64, queueType QueueType)
    onTorrentDeactivated func(torrentID int64, queueType QueueType)
}
```

### QueueItem

Each queued torrent is represented by a `QueueItem`:

```go
type QueueItem struct {
    TorrentID     int64     // Unique torrent identifier
    QueuePosition int64     // Position in queue (0-based)
    AddedTime     time.Time // When added to queue
    Priority      int64     // Priority level (higher = more important)
}
```

### Configuration

Queue behavior is controlled via `QueueConfig`:

```go
type QueueConfig struct {
    MaxActiveDownloads   int64         // Maximum active downloads (0 = unlimited)
    MaxActiveSeeds       int64         // Maximum active seeds (0 = unlimited)
    ProcessInterval      time.Duration // Queue processing interval
    DownloadQueueEnabled bool          // Enable download queue
    SeedQueueEnabled     bool          // Enable seed queue
}
```

## Usage Examples

### Basic Queue Setup

```go
// Configure queue limits
config := QueueConfig{
    MaxActiveDownloads:   3,
    MaxActiveSeeds:       2,
    ProcessInterval:      5 * time.Second,
    DownloadQueueEnabled: true,
    SeedQueueEnabled:     true,
}

// Callback functions for torrent state changes
onActivated := func(torrentID int64, queueType QueueType) {
    fmt.Printf("Torrent %d activated for %v\n", torrentID, queueType)
}

onDeactivated := func(torrentID int64, queueType QueueType) {
    fmt.Printf("Torrent %d deactivated from %v\n", torrentID, queueType)
}

// Create queue manager
qm := NewQueueManager(config, onActivated, onDeactivated)
defer qm.Close()
```

### Adding Torrents to Queue

```go
// Add torrent to download queue with priority
err := qm.AddToQueue(1, DownloadQueue, 10) // High priority
if err != nil {
    log.Printf("Failed to add torrent to queue: %v", err)
}

// Add torrent to seed queue  
err = qm.AddToQueue(2, SeedQueue, 5) // Medium priority
if err != nil {
    log.Printf("Failed to add torrent to queue: %v", err)
}
```

### Queue Position Management

```go
// Get current queue position
position := qm.GetQueuePosition(1)
fmt.Printf("Torrent 1 is at position %d\n", position)

// Set specific queue position
err := qm.SetQueuePosition(1, 0) // Move to front
if err != nil {
    log.Printf("Failed to set queue position: %v", err)
}
```

### Force Activation

```go
// Start torrent immediately, bypassing queue limits
qm.ForceActivate(3, DownloadQueue)

// Check if torrent is active
if qm.IsActive(3) {
    fmt.Println("Torrent 3 is now active")
}
```

### Queue Statistics

```go
stats := qm.GetStats()
fmt.Printf("Download Queue: %d torrents, %d active\n", 
    stats.DownloadQueueLength, stats.ActiveDownloads)
fmt.Printf("Seed Queue: %d torrents, %d active\n", 
    stats.SeedQueueLength, stats.ActiveSeeds)
fmt.Printf("Total processed: %d torrents, %d activated\n", 
    stats.TotalQueued, stats.TotalActivated)
```

## Integration with TorrentManager

The queue system is fully integrated with `TorrentManager`:

### Automatic Queue Selection

```go
// Starting a torrent automatically chooses the appropriate queue
err := tm.StartTorrent(torrentID)
// - Incomplete torrents → Download queue
// - Complete torrents → Seed queue
```

### Transmission RPC Integration

```go
// Queue position can be set via torrent-set RPC method
req := TorrentActionRequest{
    IDs:           []interface{}{1, 2, 3},
    QueuePosition: 0, // Move to front of queue
}

err := rpcMethods.TorrentSet(req)
```

### State Transitions

The queue system automatically manages torrent status transitions:

- `TorrentStatusQueuedDown` → `TorrentStatusDownloading` (when activated)
- `TorrentStatusQueuedSeed` → `TorrentStatusSeeding` (when activated)
- Active status → Queued status (when deactivated)

## Queue Processing Logic

### Priority-Based Sorting

Torrents in queues are sorted by:
1. **Priority** (higher values first)
2. **Added Time** (earlier times first for same priority)

### Activation Algorithm

```go
// Pseudo-code for queue processing
for activeCount < maxActive && queueLength > 0 {
    torrent := queue.Front() // Highest priority torrent
    queue.Remove(torrent)
    activeSet.Add(torrent)
    callbackOnActivated(torrent)
}
```

### Manual Position Override

When `SetQueuePosition` is called:
1. Remove torrent from current position
2. Insert at specified position
3. Shift other torrents accordingly
4. Update all position numbers

## Error Handling

The queue system includes comprehensive error handling:

```go
var (
    ErrTorrentNotInQueue = &RPCError{
        Code:    ErrCodeInvalidTorrent,
        Message: "torrent not found in queue",
    }
)
```

Common error scenarios:
- Setting position for non-existent torrent
- Invalid queue configuration
- Concurrent modification during processing

## Best Practices

### Configuration
- Set reasonable queue limits based on system resources
- Use shorter processing intervals for responsive queue management
- Enable both download and seed queues for full functionality

### Performance
- Queue operations are very fast (nanosecond to microsecond range)
- Concurrent operations are safe and efficient
- Background processing has minimal CPU overhead

### Integration
- Use queue callbacks for custom torrent management logic
- Leverage automatic queue type selection for simplicity
- Implement proper cleanup with `qm.Close()` when shutting down

## Testing

The queue system includes comprehensive tests covering:
- Basic queue operations (add, remove, position)
- Priority-based sorting and activation
- Concurrent operations safety
- Error conditions and edge cases
- Performance benchmarks

Run tests with:
```bash
go test ./rpc -v -run TestQueueManager
go test ./rpc -bench BenchmarkQueueManager
```

## Future Enhancements

Potential improvements for the queue system:
- Queue persistence across application restarts
- Advanced scheduling (time-based activation)
- Bandwidth-aware queue management
- Queue templates and presets
- Historical queue analytics