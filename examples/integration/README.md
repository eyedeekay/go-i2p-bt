# Integration Examples

This directory contains comprehensive examples demonstrating how to integrate and use the three major extensibility systems implemented in Phase 3.3:

1. **Plugin Architecture** (`rpc/plugin.go`)
2. **Event Notification System** (`rpc/event_notification.go`)
3. **Custom Metadata Support** (`rpc/metadata.go`)

## Examples

### 1. Plugin Persistence Example (`plugin_persistence.go`)

Demonstrates how to create a persistence plugin that:
- Implements multiple plugin interfaces (Plugin, BehaviorPlugin, MetricsPlugin)
- Automatically saves torrent metadata to disk
- Loads metadata on startup
- Integrates with the metadata manager
- Provides health monitoring and metrics

**Key Features:**
- File-based persistence with JSON serialization
- Periodic auto-save functionality
- Metadata integration for tracking save operations
- Error handling and recovery
- Plugin lifecycle management

**Usage:**
```go
// Create persistence plugin
plugin := NewPersistencePlugin("/data/torrents", 30*time.Second)

// Register with plugin manager
pluginManager.RegisterPlugin(plugin, map[string]interface{}{
    "enabled": true,
    "auto_save": true,
})
```

### 2. Event-Driven Analytics Example (`event_analytics.go`)

Shows how to build comprehensive analytics using the event notification system:
- Real-time event processing and aggregation
- Performance metrics collection
- Event filtering and routing
- Dashboard-style reporting
- Error tracking and recovery

**Key Features:**
- Multi-threaded event processing
- Configurable event filters
- Real-time statistics calculation
- Memory-efficient data structures
- Performance monitoring

**Usage:**
```go
// Create analytics subscriber
analytics := NewAnalyticsSubscriber()

// Subscribe to events
eventSystem.Subscribe(analytics)

// Generate real-time reports
report := analytics.GenerateReport()
```

### 3. Advanced Metadata Management Example (`metadata_advanced.go`)

Demonstrates sophisticated metadata usage patterns:
- Category and tagging systems
- Quality ratings and community feedback
- Download tracking and analytics
- User preferences and custom flags
- Complex search and filtering

**Key Features:**
- Schema-based metadata organization
- Type-safe value handling
- Validation and constraints
- Optimistic locking support
- Advanced query capabilities

**Usage:**
```go
// Create metadata extensions
extensions := NewMetadataExtensions(metadataManager)

// Set torrent category
extensions.SetTorrentCategory(torrentID, "movies", "action", 
    []string{"sci-fi", "2024"}, "video")

// Track download progress
extensions.StartTorrentTracking(torrentID, "tracker.example.com")

// Search by criteria
results := extensions.GetTorrentsByCategory("movies")
```

### 4. Complete Integration Example (`complete_integration.go`)

Shows how all three systems work together in a realistic torrent manager:
- Integrated plugin management
- Event-driven architecture
- Comprehensive metadata tracking
- Real-time monitoring and reporting
- Graceful shutdown procedures

**Key Features:**
- Plugin-based extensibility
- Event-driven state updates
- Metadata persistence
- System health monitoring
- Configuration management

**Usage:**
```go
// Configure integrated manager
config := IntegratedConfig{
    EnablePersistence: true,
    EnableAnalytics:   true,
    EnableLogging:     true,
    PersistenceDir:    "/data",
    LogLevel:          "info",
}

// Create and use manager
manager := NewIntegratedTorrentManager(config)
manager.AddTorrent(1001, "magnet:?xt=...", "/downloads")
```

## Running the Examples

Each example is self-contained and can be run independently:

```bash
# Run plugin persistence example
go run examples/integration/plugin_persistence.go

# Run event analytics example  
go run examples/integration/event_analytics.go

# Run metadata management example
go run examples/integration/metadata_advanced.go

# Run complete integration example
go run examples/integration/complete_integration.go
```

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    Integrated Torrent Manager                   │
├─────────────────────────────────────────────────────────────────┤
│  Plugin System        │  Event System       │  Metadata System  │
│  ┌─────────────────┐  │  ┌─────────────────┐ │  ┌─────────────────┐ │
│  │ Plugin Manager  │  │  │ Event Publisher │ │  │ Metadata Mgr    │ │
│  │ - Registration  │  │  │ - Async Dispatch│ │  │ - Type Safety   │ │
│  │ - Lifecycle     │  │  │ - Filtering     │ │  │ - Validation    │ │
│  │ - Health Check  │  │  │ - Routing       │ │  │ - Versioning    │ │
│  └─────────────────┘  │  └─────────────────┘ │  └─────────────────┘ │
│           │            │           │          │           │          │
│  ┌─────────────────┐  │  ┌─────────────────┐ │  ┌─────────────────┐ │
│  │ Behavior Plugins│  │  │ Event Subscriber│ │  │ Metadata Query  │ │
│  │ - Persistence   │  │  │ - Analytics     │ │  │ - Search        │ │
│  │ - Validation    │  │  │ - Logging       │ │  │ - Aggregation   │ │
│  │ - Custom Logic  │  │  │ - Metrics       │ │  │ - Reporting     │ │
│  └─────────────────┘  │  └─────────────────┘ │  └─────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

## Best Practices

### Plugin Development
- Implement proper error handling and recovery
- Use structured logging for debugging
- Provide meaningful health checks
- Follow the plugin lifecycle pattern
- Keep plugins focused on single responsibilities

### Event Handling
- Use appropriate event filters to reduce noise
- Implement non-blocking event processing
- Handle events idempotently when possible
- Provide graceful degradation on errors
- Monitor event processing performance

### Metadata Management
- Define clear schemas for metadata structure
- Use appropriate validation constraints
- Implement efficient search patterns
- Consider metadata versioning for evolution
- Monitor metadata storage growth

### Integration Patterns
- Use dependency injection for testability
- Implement graceful shutdown procedures
- Provide comprehensive configuration options
- Use structured logging throughout
- Monitor system health and performance

## Testing

Each example includes comprehensive test coverage:

```bash
# Run tests for all examples
go test ./examples/integration/...

# Run specific example tests
go test ./examples/integration/ -run TestPersistencePlugin
go test ./examples/integration/ -run TestEventAnalytics
go test ./examples/integration/ -run TestMetadataAdvanced
go test ./examples/integration/ -run TestCompleteIntegration
```

## Contributing

When adding new integration examples:

1. Follow the established patterns and naming conventions
2. Include comprehensive documentation and comments
3. Provide realistic usage scenarios
4. Add appropriate test coverage
5. Update this README with new example descriptions

## Dependencies

These examples depend on the core RPC systems:
- `rpc/plugin.go` - Plugin architecture
- `rpc/event_notification.go` - Event system
- `rpc/metadata.go` - Metadata management

All examples are designed to work with the existing go-i2p-bt torrent implementation.## Integration Patterns

### Plugin Integration
- Plugin lifecycle management
- Custom behavior injection
- Metrics collection
- Request/response interception

### Event Integration
- Real-time event broadcasting
- Event filtering and routing
- Async notification handling
- Performance monitoring

### Metadata Integration
- Custom metadata validation
- Metadata querying and filtering
- Version control and optimistic locking
- Callback-based change tracking