# SSE Transport Backward Compatibility Tasks

This document outlines the tasks required to make the current MCP implementation backward compatible with the old specification's SSE transport (protocol version 2024-11-05).

## Background

The old specification (2024-11-05) defines an SSE transport with specific requirements:

1. The server must provide two endpoints:
   - An SSE endpoint for clients to establish a connection and receive messages
   - A regular HTTP POST endpoint for clients to send messages

2. When a client connects, the server must send an `endpoint` event containing a URI for the client to use for sending messages.

3. Server messages are sent as SSE `message` events with JSON-encoded data.

Our current implementation doesn't fully match these requirements, as it:
- Doesn't send an initial `endpoint` event
- Uses the same endpoint for both SSE connections and message posting
- Doesn't explicitly validate the Origin header for security

## Tasks

### 1. Add Version Detection in Server

- [ ] Modify `Server.Initialize()` to detect and store the negotiated protocol version
- [ ] Add logic to determine feature behavior based on version (e.g., `isOldVersion()` helper)

### 2. Update SSE Handler for Backward Compatibility

- [ ] Modify `handleSSE()` to check protocol version and behave appropriately
- [ ] For 2024-11-05 clients, send an `endpoint` event immediately after connection with the URL for sending messages
- [ ] Update the event format to use the `message` event type consistently for 2024-11-05 clients

```go
// Example addition to handleSSE
if s.protocolVersion == "2024-11-05" {
    // Send initial endpoint event with URL for client to send messages
    endpointURL := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.Path)
    fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
    flusher.Flush()
}
```

### 3. Implement Security Requirements

- [ ] Add Origin header validation to prevent DNS rebinding attacks
- [ ] Ensure server binds only to localhost by default
- [ ] Document authentication recommendations

```go
// Example Origin validation to add
originHeader := r.Header.Get("Origin")
if originHeader != "" {
    // Check against allowed origins
    allowedOrigins := []string{"null", "localhost", "127.0.0.1"}
    allowed := false
    for _, origin := range allowedOrigins {
        if strings.Contains(originHeader, origin) {
            allowed = true
            break
        }
    }
    if !allowed {
        http.Error(w, "Origin not allowed", http.StatusForbidden)
        return
    }
}
```

### 4. Update Event Processing

- [ ] Update the `processSSE()` and `handleSSEEvent()` methods to handle both protocol versions
- [ ] For 2024-11-05, ensure events are always sent with `event: message` type
- [ ] Update the client's SSE scanner to handle both styles of events

### 5. Modify Client Transport

- [ ] Update the `StreamableHTTP` transport to handle the endpoint event from 2024-11-05 servers
- [ ] Store the endpoint URL for sending future messages
- [ ] Add protocol-version-specific behavior for the client

```go
// Example handling of endpoint event in client
if event.Event == "endpoint" {
    t.mu.Lock()
    t.sendURL = event.Data // Store the URL for sending messages
    t.mu.Unlock()
}
```

### 6. Update Documentation

- [ ] Add documentation about version compatibility
- [ ] Document security considerations for SSE transport
- [ ] Update examples to demonstrate both protocol versions

### 7. Add Tests for Backward Compatibility

- [ ] Create tests that verify both protocol versions work correctly
- [ ] Test specific behavior changes between versions
- [ ] Ensure security measures work properly

### 8. Make Endpoints Configurable

- [ ] Add configuration options for SSE and message endpoints
- [ ] Create a `ServerOptions` struct with endpoint configuration fields
- [ ] Allow custom endpoint paths in server initialization
- [ ] Enable separate paths for SSE and message posting endpoints
- [ ] Update router/handler logic to support custom endpoint paths

```go
// Example updates to ServerConfig
type ServerConfig struct {
    ProtocolVersion string
    ServerInfo      ServerInfo
    Capabilities    map[string]any
    SSEEndpoint     string // Default: "/"
    MessageEndpoint string // Default: "/"
    AllowedOrigins  []string // Default: ["null", "localhost", "127.0.0.1"]
}
```

### 9. Deployment Considerations

- [ ] Default server bind address should be localhost (127.0.0.1) for security
- [ ] Document how to enable wider network access when needed
- [ ] Ensure clear warnings about security implications

## Implementation Strategy

1. First, implement version detection during initialization
2. Implement configurable endpoints to support the different transport patterns
3. Build backward compatibility in layers, starting with server-side changes
4. Add client compatibility for working with older servers
5. Finally, add tests and security measures

This approach allows incremental testing and verification throughout the implementation process.

## Security Considerations

The 2024-11-05 specification explicitly mentions security concerns:

1. Servers MUST validate the Origin header on incoming connections
2. Servers SHOULD bind only to localhost by default
3. Proper authentication SHOULD be implemented

These security measures should be applied regardless of protocol version to prevent DNS rebinding attacks that could allow unauthorized access to the local MCP server.