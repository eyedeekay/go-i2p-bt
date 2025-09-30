# Persistence Examples for go-i2p-bt

This directory contains example implementations of different persistence strategies for torrent state management in applications using the go-i2p-bt library.

## Overview

The go-i2p-bt library intentionally does not include opinionated persistence mechanisms. Instead, it provides hooks and extensibility points that allow downstream applications to implement their own state management strategies based on their specific requirements.

## Included Implementations

### 1. File-based Persistence (`file.go`)

A simple JSON file-based persistence implementation suitable for small to medium deployments.

**Features:**
- Individual torrent state files in JSON format
- Atomic writes using temporary files
- Concurrent access safety with mutex protection
- MetaInfo preservation for complete state restoration
- Automatic directory structure creation

**Best for:**
- Development and testing environments
- Small deployments (< 1000 torrents)
- Simple setups where database overhead is not desired
- Applications that need human-readable state files

**Usage:**
```go
persistence, err := persistence.NewFilePersistence("/data/torrents")
if err != nil {
    log.Fatal(err)
}
defer persistence.Close()
```

### 2. Memory-based Persistence (`memory.go`)

High-performance in-memory persistence with optional snapshotting to backup storage.

**Features:**
- Instant read/write operations
- Optional periodic snapshotting to backup persistence
- Deep copying to prevent external modifications
- Memory usage monitoring and statistics
- Graceful backup restoration

**Best for:**
- High-performance scenarios where disk I/O should be minimized
- Temporary sessions where persistence is not critical
- Applications with their own snapshotting mechanisms
- Testing and development with quick iteration cycles

**Usage:**
```go
// Without backup
memPersistence := persistence.NewMemoryPersistence(nil, 0)

// With file backup every 5 minutes
backupPersistence, _ := persistence.NewFilePersistence("/backup")
memPersistence := persistence.NewMemoryPersistence(backupPersistence, 5*time.Minute)
```

### 3. Database Persistence (`database.go`)

SQLite-based persistence for production deployments requiring ACID guarantees.

**Features:**
- ACID transactions for data consistency
- SQL queries for reporting and analytics
- Connection pooling for concurrent access
- Prepared statements for performance
- Optional metrics tracking with retention policies
- Automatic schema migration

**Best for:**
- Production deployments
- Applications requiring complex queries
- Multi-process deployments
- Systems needing backup/restore capabilities
- Applications with compliance requirements

**Setup:**
```bash
# Add SQLite dependency to your go.mod
go get github.com/mattn/go-sqlite3
```

**Usage:**
```go
// Uncomment the SQLite import in database.go first
persistence, err := persistence.NewDatabasePersistence("/data/torrents.db", true)
if err != nil {
    log.Fatal(err)
}
defer persistence.Close()
```

## Interface Design

All implementations follow the `TorrentPersistence` interface:

```go
type TorrentPersistence interface {
    SaveTorrent(ctx context.Context, torrent *rpc.TorrentState) error
    LoadTorrent(ctx context.Context, infoHash metainfo.Hash) (*rpc.TorrentState, error)
    SaveAllTorrents(ctx context.Context, torrents []*rpc.TorrentState) error
    LoadAllTorrents(ctx context.Context) ([]*rpc.TorrentState, error)
    DeleteTorrent(ctx context.Context, infoHash metainfo.Hash) error
    SaveSessionConfig(ctx context.Context, config *rpc.SessionConfiguration) error
    LoadSessionConfig(ctx context.Context) (*rpc.SessionConfiguration, error)
    Close() error
}
```

## Automatic Persistence with Hooks

The `PersistenceHook` provides automatic state persistence by integrating with the library's hook system:

```go
// Create persistence layer
persistence, err := persistence.NewFilePersistence("/data")
if err != nil {
    log.Fatal(err)
}

// Create hook manager
hookManager := rpc.NewHookManager()

// Register automatic persistence hook
persistenceHook := persistence.NewPersistenceHook(persistence)
hookManager.RegisterHook(&rpc.Hook{
    ID:       "persistence",
    Callback: persistenceHook.HandleTorrentEvent,
    Events: []rpc.HookEvent{
        rpc.HookEventTorrentAdded,
        rpc.HookEventTorrentStarted,
        rpc.HookEventTorrentStopped,
        rpc.HookEventTorrentCompleted,
        rpc.HookEventTorrentRemoved,
    },
    Priority: 100, // High priority
})

// Use the hook manager in torrent manager configuration
managerConfig := rpc.TorrentManagerConfig{
    DownloadDir: "/downloads",
    HookManager: hookManager,
}
```

## Complete Example

See `../usage/example_usage.go` for a complete example showing:
- Configuration management
- Graceful startup and shutdown
- Multiple persistence strategies
- Performance monitoring
- Error handling and recovery

## Testing

Run the included tests to verify functionality:

```bash
cd examples/persistence
go test -v
go test -bench=.
```

## Performance Characteristics

Based on benchmark tests:

| Implementation | Save Operation | Load Operation | Memory Usage | Concurrent Access |
|---------------|----------------|----------------|--------------|-------------------|
| File          | ~1-5ms         | ~0.5-2ms       | Low          | Mutex-protected   |
| Memory        | ~10-50ns       | ~10-50ns       | High         | Mutex-protected   |
| Database      | ~0.1-1ms       | ~0.1-0.5ms     | Medium       | Connection pool   |

*Performance varies based on torrent count, system I/O, and hardware specifications.*

## Production Considerations

### Security
- File permissions: Ensure proper directory permissions (755 for directories, 644 for files)
- Database security: Use appropriate SQLite pragma settings for production
- Backup encryption: Consider encrypting backup files for sensitive data

### Monitoring
- Implement health checks for persistence operations
- Monitor disk space usage for file and database persistence
- Track performance metrics for optimization

### Backup and Recovery
- Implement regular backup strategies for critical data
- Test restore procedures regularly
- Consider using database WAL mode for better concurrency

### Scaling
- File persistence: Consider sharding across multiple directories for large datasets
- Memory persistence: Monitor memory usage and implement memory limits
- Database persistence: Use database connection pooling and consider read replicas

## Extending the Examples

To create custom persistence implementations:

1. Implement the `TorrentPersistence` interface
2. Add appropriate error handling and logging
3. Consider implementing `MetricsPersistence` for historical data
4. Add comprehensive tests following the patterns in `persistence_test.go`
5. Document performance characteristics and best practices

## Dependencies

- **File persistence**: Standard library only
- **Memory persistence**: Standard library only
- **Database persistence**: Requires `github.com/mattn/go-sqlite3`

For production use, consider adding:
- Structured logging library (e.g., `logrus`, `zap`)
- Metrics library (e.g., `prometheus/client_golang`)
- Configuration management library (e.g., `viper`)