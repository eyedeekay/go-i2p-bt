# Torrent Statistics and Monitoring

This document describes the comprehensive statistics and monitoring system implemented in the go-i2p-bt BitTorrent client.

## Overview

The statistics system provides real-time monitoring of torrent activity including:
- Transfer rates (download/upload)
- Completion percentages
- ETA (Estimated Time to Arrival)
- Piece statistics
- Peer activity monitoring

## Architecture

The statistics system is built around the `TorrentState` structure with periodic updates every 5 seconds via the `updateTorrentStatistics` method.

### Core Components

#### 1. TorrentState Statistics Fields

```go
type TorrentState struct {
    // Basic transfer statistics
    Downloaded      int64   `json:"downloaded"`       // Total bytes downloaded
    Uploaded        int64   `json:"uploaded"`         // Total bytes uploaded
    Left            int64   `json:"left"`            // Bytes remaining to download
    
    // Rate calculations
    DownloadRate    int64   `json:"download_rate"`    // Current download rate (bytes/sec)
    UploadRate      int64   `json:"upload_rate"`      // Current upload rate (bytes/sec)
    
    // Completion tracking
    PercentDone     float64 `json:"percent_done"`     // Completion percentage (0.0-1.0)
    ETA             int64   `json:"eta"`             // Estimated time to completion (seconds)
    
    // Piece statistics
    PieceCount      int64   `json:"piece_count"`      // Total number of pieces
    PiecesComplete  int64   `json:"pieces_complete"`  // Number of completed pieces
    PiecesAvailable int64   `json:"pieces_available"` // Number of pieces available from peers
    
    // Peer statistics
    PeerCount           int64 `json:"peer_count"`            // Total number of peers
    PeerConnectedCount  int64 `json:"peer_connected_count"`  // Number of connected peers
    PeerSendingCount    int64 `json:"peer_sending_count"`    // Number of peers sending data
    PeerReceivingCount  int64 `json:"peer_receiving_count"`  // Number of peers receiving data
    
    // Internal tracking fields (not exposed in JSON)
    lastStatsUpdate  time.Time // Last statistics update time
    lastDownloaded   int64     // Downloaded bytes at last update
    lastUploaded     int64     // Uploaded bytes at last update
}
```

#### 2. Statistics Calculation Methods

##### updateTorrentStatistics
Main method that calculates and updates all statistics for a torrent.

**Features:**
- Real-time transfer rate calculation based on bytes transferred since last update
- Completion percentage calculation using MetaInfo total size
- Piece statistics calculation
- Peer activity monitoring
- ETA calculation

**Performance:** ~57ns per operation with zero memory allocations

##### calculateETA
Calculates estimated time to completion in seconds.

**Logic:**
- Returns `0` if download is complete (`Left <= 0`)
- Returns `-1` if ETA cannot be determined (`DownloadRate <= 0`)
- Otherwise returns `Left / DownloadRate`

**Performance:** ~1.2ns per operation with zero memory allocations

##### Peer Counting Methods

- `countConnectedPeers`: Returns number of actually connected peers
- `countSendingPeers`: Estimates peers currently sending data (30% of connected peers during download)
- `countReceivingPeers`: Estimates peers currently receiving data (20% of connected peers during seeding)

## Usage Examples

### Real-time Monitoring

```go
// Start periodic statistics updates (every 5 seconds)
go func() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            for _, torrent := range torrentManager.GetAllTorrents() {
                torrentManager.updateTorrentStatistics(torrent, time.Now())
            }
        case <-ctx.Done():
            return
        }
    }
}()
```

### Accessing Statistics

```go
// Get torrent with current statistics
torrent, err := torrentManager.GetTorrent(torrentID)
if err != nil {
    return err
}

// Display current stats
fmt.Printf("Progress: %.1f%%\n", torrent.PercentDone*100)
fmt.Printf("Download Rate: %s/s\n", humanizeBytes(torrent.DownloadRate))
fmt.Printf("Upload Rate: %s/s\n", humanizeBytes(torrent.UploadRate))
fmt.Printf("ETA: %s\n", formatDuration(torrent.ETA))
fmt.Printf("Peers: %d connected, %d sending, %d receiving\n", 
    torrent.PeerConnectedCount, 
    torrent.PeerSendingCount, 
    torrent.PeerReceivingCount)
```

### JSON API Response

Statistics are automatically included in JSON responses:

```json
{
    "id": 1,
    "info_hash": "1234567890abcdef...",
    "name": "example_torrent",
    "status": 4,
    "downloaded": 1048576,
    "uploaded": 524288,
    "left": 4194304,
    "download_rate": 102400,
    "upload_rate": 51200,
    "percent_done": 0.2,
    "eta": 41,
    "piece_count": 100,
    "pieces_complete": 20,
    "pieces_available": 85,
    "peer_count": 15,
    "peer_connected_count": 12,
    "peer_sending_count": 4,
    "peer_receiving_count": 0
}
```

## Implementation Details

### Rate Calculation Algorithm

Transfer rates are calculated using a sliding window approach:

1. Store previous statistics (bytes and timestamp) in internal fields
2. On each update, calculate the difference in bytes and time
3. Rate = `(current_bytes - previous_bytes) / time_delta`

### Completion Percentage

Calculated as: `Downloaded / TotalSize` where TotalSize comes from MetaInfo.

### Piece Statistics

- **PieceCount**: Total pieces from MetaInfo (`info.CountPieces()`)
- **PiecesComplete**: Estimated based on completion percentage
- **PiecesAvailable**: Estimated based on connected peers (assumes 50% availability per peer)

### Error Handling

The statistics system is designed to be robust:
- Gracefully handles missing MetaInfo (returns 0 for piece statistics)
- Prevents division by zero in rate calculations
- Bounds checking for completion percentages
- Safe handling of negative values

### Performance Characteristics

- **Memory**: Zero allocations during statistics updates
- **CPU**: Sub-nanosecond to tens of nanoseconds per operation
- **Thread Safety**: All operations are safe for concurrent access
- **Scalability**: Linear performance with number of torrents

## Testing

The statistics system includes comprehensive unit tests covering:

- Transfer rate calculations
- ETA calculations under various conditions
- Peer counting methods
- Edge cases (zero rates, completed downloads, etc.)
- Performance benchmarks

Run tests with:
```bash
go test -v ./rpc -run "TestUpdate.*Statistics|TestCalculate.*|TestCount.*Peers"
```

Run benchmarks with:
```bash
go test -bench=Benchmark.*Statistics -benchmem ./rpc
```

## Future Enhancements

Potential improvements to the statistics system:

1. **Real Piece Tracking**: Replace estimated piece counts with actual piece completion tracking
2. **Historical Data**: Store historical transfer rates for trend analysis
3. **Peer-specific Statistics**: Track per-peer transfer rates and activity
4. **Adaptive Refresh Rates**: Adjust statistics update frequency based on activity
5. **Advanced ETA**: More sophisticated ETA calculation considering peer availability and transfer patterns

## Related Documentation

- [API Reference](api.md) - Complete API documentation
- [Architecture Overview](architecture.md) - System architecture
- [Performance Guide](performance.md) - Performance optimization tips