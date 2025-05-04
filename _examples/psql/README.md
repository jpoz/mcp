# Postgres MCP Example with SSE Support

This example demonstrates an MCP server with PostgreSQL integration that supports both the current protocol version (2025-03-26) and backward compatibility with the 2024-11-05 protocol version using Server-Sent Events (SSE).

## Features

- PostgreSQL database integration with a simple tasks table
- JSON-RPC SQL query tool
- Support for both protocol versions:
  - Current protocol (2025-03-26)
  - Legacy protocol (2024-11-05) with SSE transport
- Separate endpoints for SSE events and JSON-RPC messages
- Periodic database heartbeat notifications sent via SSE
- Security features including Origin header validation

## Getting Started

### 1. Start the Postgres instance

Start the PostgreSQL database using Docker Compose:

```sh
docker compose -f docker-compose.yml up
```

### 2. Start the MCP Server

Run the server which will automatically set up the database schema and seed data:

```sh
go run server/main.go
```

The server will output connection information for different protocol versions:
- Latest protocol (2025-03-26): http://localhost:8081/
- Old protocol with SSE (2024-11-05):
  - SSE events endpoint: http://localhost:8081/sse
  - JSON-RPC endpoint: http://localhost:8081/rpc

### 3. Run the Client

For the current protocol version:

```sh
go run client/main.go http://localhost:8081
```

For the old protocol version with SSE:

```sh
# Using the RPC endpoint
go run client/main.go http://localhost:8081/rpc
```

## SSE Notifications

The server sends periodic "database heartbeat" notifications every 10 seconds.
These notifications are broadcast to all connected clients using the appropriate
format for their protocol version.

You can observe these notifications by:
1. Connecting to the SSE endpoint directly with curl:
   ```sh
   curl -H "Accept: text/event-stream" http://localhost:8081/sse
   ```

2. Or by monitoring the client output when connected.

## Security

The server has security features enabled by default:
- Only allows connections from localhost
- Validates the Origin header to prevent DNS rebinding attacks
- Binds only to localhost (127.0.0.1) by default
