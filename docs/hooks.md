# Hook System Documentation

## Overview

The hook system provides a flexible way for downstream applications to register callback functions that are executed during torrent lifecycle events. This enables custom behavior, monitoring, and integration with external systems without modifying the core library.

## Architecture

The hook system consists of several key components:

- **HookManager**: Central manager for registering, unregistering, and executing hooks
- **Hook**: Individual callback registration with metadata
- **HookContext**: Context passed to callbacks containing event data
- **HookEvent**: Enumerated lifecycle events

## Supported Events

| Event | Description | When Triggered |
|-------|-------------|----------------|
| `HookEventTorrentAdded` | New torrent added | When `AddTorrent()` is called |
| `HookEventTorrentStarted` | Torrent begins downloading/seeding | When torrent becomes active |
| `HookEventTorrentStopped` | Torrent paused or stopped | When torrent becomes inactive |
| `HookEventTorrentCompleted` | Download completed | When `PercentDone` reaches 1.0 |
| `HookEventTorrentRemoved` | Torrent removed | When `RemoveTorrent()` is called |
| `HookEventTorrentMetadata` | Metadata received | When .torrent metadata is processed |
| `HookEventTorrentError` | Error occurred | When torrent encounters errors |

## Usage Examples

### Basic Hook Registration

```go
// Create TorrentManager
tm, err := rpc.NewTorrentManager(config)
if err != nil {
    log.Fatal(err)
}

// Register a hook for completion events
hook := &rpc.Hook{
    ID: "completion-notifier",
    Callback: func(ctx *rpc.HookContext) error {
        fmt.Printf("Torrent %d completed: %s\n", 
            ctx.Torrent.ID, 
            ctx.Data["action"])
        return nil
    },
    Events:   []rpc.HookEvent{rpc.HookEventTorrentCompleted},
    Priority: 1,
    Timeout:  10 * time.Second,
    ContinueOnError: true,
}

err = tm.RegisterHook(hook)
if err != nil {
    log.Printf("Failed to register hook: %v", err)
}
```

### Advanced Example: Multi-Event Hook with Database Integration

```go
// Database integration hook
dbHook := &rpc.Hook{
    ID: "database-sync",
    Callback: func(ctx *rpc.HookContext) error {
        // Extract torrent information
        torrent := ctx.Torrent
        event := string(ctx.Event)
        timestamp := ctx.Timestamp
        
        // Insert into database
        return database.InsertTorrentEvent(torrent.ID, event, timestamp)
    },
    Events: []rpc.HookEvent{
        rpc.HookEventTorrentAdded,
        rpc.HookEventTorrentCompleted,
        rpc.HookEventTorrentRemoved,
    },
    Priority: 5,
    Timeout:  30 * time.Second,
    ContinueOnError: true,
}

err = tm.RegisterHook(dbHook)
```

### Global Hook (Responds to All Events)

```go
// Logging hook that captures all events
loggingHook := &rpc.Hook{
    ID: "global-logger",
    Callback: func(ctx *rpc.HookContext) error {
        log.Printf("[%s] Torrent %d: %s", 
            ctx.Event, 
            ctx.Torrent.ID, 
            ctx.Data["action"])
        return nil
    },
    Events:   []rpc.HookEvent{}, // Empty = global hook
    Priority: 10, // High priority for logging
    Timeout:  5 * time.Second,
    ContinueOnError: true,
}

err = tm.RegisterHook(loggingHook)
```

### Monitoring and Metrics

```go
// Get hook execution metrics
metrics := tm.GetHookMetrics()
fmt.Printf("Hook Executions: %d\n", metrics.TotalExecutions)
fmt.Printf("Hook Errors: %d\n", metrics.TotalErrors)
fmt.Printf("Hook Timeouts: %d\n", metrics.TotalTimeouts)
fmt.Printf("Average Latency: %v\n", metrics.AverageLatency)

// List registered hooks
hooks := tm.ListHooks()
for event, hookIDs := range hooks {
    fmt.Printf("Event %s: %v\n", event, hookIDs)
}

// Unregister a hook
err = tm.UnregisterHook("completion-notifier")
if err != nil {
    log.Printf("Failed to unregister hook: %v", err)
}
```

## Hook Configuration

### Hook Fields

- **ID**: Unique identifier for the hook (required)
- **Callback**: Function to execute (required)
- **Events**: List of events to respond to (empty = global hook)
- **Priority**: Execution order (higher executes first)
- **Timeout**: Maximum execution time (0 = use default)
- **ContinueOnError**: Whether to continue if this hook fails

### Hook Context Fields

- **Event**: The event type that triggered the hook
- **Torrent**: Snapshot of torrent state at event time
- **Data**: Event-specific additional data
- **Context**: Go context for cancellation/timeout
- **Timestamp**: When the event occurred

### Event-Specific Data

#### HookEventTorrentAdded
```go
ctx.Data["action"] = "added"
```

#### HookEventTorrentStarted
```go
ctx.Data["queue_type"] = "download" // or "seed"
ctx.Data["action"] = "activated"
```

#### HookEventTorrentStopped
```go
ctx.Data["action"] = "stopped" // or "deactivated"
```

#### HookEventTorrentCompleted
```go
ctx.Data["action"] = "completed"
ctx.Data["percent_done"] = 1.0
```

#### HookEventTorrentRemoved
```go
ctx.Data["delete_data"] = true // or false
ctx.Data["action"] = "removed"
```

#### HookEventTorrentMetadata
```go
ctx.Data["action"] = "metadata_received"
ctx.Data["file_count"] = 10
ctx.Data["total_size"] = 1024000
```

## Best Practices

### Error Handling

```go
hook := &rpc.Hook{
    ID: "robust-hook",
    Callback: func(ctx *rpc.HookContext) error {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("Hook panic recovered: %v", r)
            }
        }()
        
        // Your logic here
        if err := doSomething(ctx.Torrent); err != nil {
            log.Printf("Hook error: %v", err)
            return err
        }
        
        return nil
    },
    Events: []rpc.HookEvent{rpc.HookEventTorrentAdded},
    ContinueOnError: true, // Don't stop other hooks if this fails
}
```

### Performance Considerations

1. **Keep hooks lightweight**: Avoid blocking operations
2. **Use appropriate timeouts**: Default is 10 seconds
3. **Handle context cancellation**: Check `ctx.Context.Done()`
4. **Set appropriate priorities**: Higher priority for critical hooks

```go
hook := &rpc.Hook{
    ID: "performance-conscious",
    Callback: func(ctx *rpc.HookContext) error {
        // Check for cancellation
        select {
        case <-ctx.Context.Done():
            return ctx.Context.Err()
        default:
        }
        
        // Do lightweight work
        go func() {
            // Heavy work in background
            heavyOperation(ctx.Torrent)
        }()
        
        return nil
    },
    Events:  []rpc.HookEvent{rpc.HookEventTorrentAdded},
    Timeout: 1 * time.Second, // Short timeout for quick response
}
```

### Integration with External Systems

```go
// Webhook notification example
webhookHook := &rpc.Hook{
    ID: "webhook-notifier",
    Callback: func(ctx *rpc.HookContext) error {
        payload := map[string]interface{}{
            "event":     ctx.Event,
            "torrent_id": ctx.Torrent.ID,
            "timestamp": ctx.Timestamp,
            "data":      ctx.Data,
        }
        
        return sendWebhook("https://api.example.com/torrent-events", payload)
    },
    Events: []rpc.HookEvent{
        rpc.HookEventTorrentCompleted,
        rpc.HookEventTorrentError,
    },
    Timeout: 15 * time.Second,
    ContinueOnError: true,
}
```

## Thread Safety

The hook system is fully thread-safe:

- Multiple hooks can be registered/unregistered concurrently
- Hook execution is asynchronous and non-blocking
- Torrent state snapshots prevent race conditions
- Metrics are updated atomically

## Comparison with Script Hooks

| Feature | Lifecycle Hooks | Script Hooks |
|---------|----------------|--------------|
| Language | Go callbacks | Shell scripts |
| Performance | Fast (in-process) | Slower (subprocess) |
| Error Handling | Rich Go error handling | Exit codes |
| Integration | Direct API access | Environment variables |
| Flexibility | Full language features | Limited to shell |
| Security | Same process space | Separate process |

Both systems work together - you can use lifecycle hooks for performance-critical operations and script hooks for simple shell integrations.

## Migration from Script Hooks

If you're currently using script hooks, you can gradually migrate:

```go
// Keep existing script hooks and add lifecycle hooks
tm.sessionConfig.ScriptTorrentDoneEnabled = true
tm.sessionConfig.ScriptTorrentDoneFilename = "/path/to/script.sh"

// Add complementary lifecycle hook
completionHook := &rpc.Hook{
    ID: "completion-handler",
    Callback: func(ctx *rpc.HookContext) error {
        // Direct Go integration
        return notifyCompletionAPI(ctx.Torrent)
    },
    Events: []rpc.HookEvent{rpc.HookEventTorrentCompleted},
}
tm.RegisterHook(completionHook)
```

## Troubleshooting

### Hook Not Executing

1. Check hook registration: `tm.ListHooks()`
2. Verify event type matches your expectation
3. Check hook timeout settings
4. Review error logs for failures

### Performance Issues

1. Check hook metrics: `tm.GetHookMetrics()`
2. Reduce hook timeout if appropriate
3. Move heavy operations to background goroutines
4. Consider hook priorities

### Memory Leaks

1. Unregister hooks when no longer needed
2. Avoid capturing large objects in hook closures
3. Use context cancellation properly
4. Monitor goroutine counts in long-running applications

## Example Applications

### 1. Download Completion Notification

```go
notificationHook := &rpc.Hook{
    ID: "download-complete",
    Callback: func(ctx *rpc.HookContext) error {
        // Send desktop notification
        return sendDesktopNotification(
            "Download Complete", 
            fmt.Sprintf("Torrent %d finished downloading", ctx.Torrent.ID),
        )
    },
    Events: []rpc.HookEvent{rpc.HookEventTorrentCompleted},
}
```

### 2. Bandwidth Management

```go
bandwidthHook := &rpc.Hook{
    ID: "bandwidth-manager",
    Callback: func(ctx *rpc.HookContext) error {
        switch ctx.Event {
        case rpc.HookEventTorrentStarted:
            return adjustBandwidthLimits(true)
        case rpc.HookEventTorrentStopped:
            return adjustBandwidthLimits(false)
        }
        return nil
    },
    Events: []rpc.HookEvent{
        rpc.HookEventTorrentStarted,
        rpc.HookEventTorrentStopped,
    },
}
```

### 3. Statistics Collection

```go
statsHook := &rpc.Hook{
    ID: "stats-collector",
    Callback: func(ctx *rpc.HookContext) error {
        stats := TorrentStats{
            Event:     string(ctx.Event),
            TorrentID: ctx.Torrent.ID,
            Timestamp: ctx.Timestamp,
            Downloaded: ctx.Torrent.Downloaded,
            Uploaded:   ctx.Torrent.Uploaded,
        }
        return statsDB.Insert(stats)
    },
    Events: []rpc.HookEvent{}, // Global hook for all events
    Priority: 1, // Low priority
}
```

## Summary

The lifecycle hook system provides powerful extensibility for the go-i2p-bt library while maintaining:

- **Performance**: Hooks execute asynchronously without blocking torrent operations
- **Reliability**: Comprehensive error handling and timeout management
- **Flexibility**: Support for both event-specific and global hooks
- **Observability**: Built-in metrics and monitoring capabilities
- **Safety**: Thread-safe operations and proper resource management

This enables downstream applications to implement custom behaviors ranging from simple notifications to complex integrations with external systems.