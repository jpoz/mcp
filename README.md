# Model Context Protocol (MCP) Server Implementation

This package provides a Go implementation of the Model Context Protocol (MCP) server based on the 2025-03-26 specification. It allows applications to expose prompts, resources, and tools to language models through a standardized interface.

## Features

- **Complete MCP Implementation**: Supports all major features of the MCP specification, including prompts, resources, tools, and utilities.
- **Server-Sent Events**: Implements the SSE protocol for streaming responses and server-initiated notifications.
- **Extensible Architecture**: Easily extend with custom handlers for resources, prompts, and tools.
- **Default Implementations**: Provides ready-to-use default implementations for all required interfaces.
- **Pagination Support**: Built-in support for paginated list operations.

## Components

The implementation consists of the following main components:

- `Server`: The main server implementation that handles HTTP requests and routes them to the appropriate handler.
- `ResourcesHandler`: Interface for handling resource-related operations.
- `PromptsHandler`: Interface for handling prompt-related operations.
- `ToolsHandler`: Interface for handling tool-related operations.
- `CompletionHandler`: Interface for handling completion requests.
- `NotificationManager`: Manages server-sent events and notifications.
- `LoggingManager`: Handles logging operations.

## Usage

### Creating a Server

```go
config := mcp.ServerConfig{
    ProtocolVersion: "2025-03-26",
    ServerInfo: mcp.ServerInfo{
        Name:    "My MCP Server",
        Version: "1.0.0",
    },
    Capabilities: map[string]interface{}{
        "resources": map[string]interface{}{
            "subscribe":   true,
            "listChanged": true,
        },
        "prompts": map[string]interface{}{
            "listChanged": true,
        },
        "tools": map[string]interface{}{
            "listChanged": true,
        },
    },
}

server := mcp.NewServer(config)
```

### Adding Resources

```go
resourceHandler := mcp.NewDefaultResourcesHandler()

resourceHandler.AddResource(
    mcp.ResourceInfo{
        URI:         "file:///example.txt",
        Name:        "example.txt",
        Description: "An example text file",
        MimeType:    "text/plain",
    },
    mcp.ResourceContent{
        URI:      "file:///example.txt",
        MimeType: "text/plain",
        Text:     "This is an example text file content.",
    },
)

server.SetResourcesHandler(resourceHandler)
```

### Adding Prompts

```go
promptsHandler := mcp.NewDefaultPromptsHandler()

promptsHandler.AddPrompt(
    mcp.PromptInfo{
        Name:        "code_review",
        Description: "Review code for quality",
        Arguments: []mcp.PromptArgument{
            {
                Name:        "code",
                Description: "The code to review",
                Required:    true,
            },
        },
    },
    mcp.PromptResult{
        Description: "Code review prompt",
        Messages: []mcp.PromptMessage{
            {
                Role: "user",
                Content: mcp.PromptContent{
                    Type: "text",
                    Text: "Please review this code: {code}",
                },
            },
        },
    },
)

server.SetPromptsHandler(promptsHandler)
```

### Adding Tools

```go
toolsHandler := mcp.NewDefaultToolsHandler()

toolsHandler.AddTool(
    mcp.ToolInfo{
        Name:        "get_weather",
        Description: "Get weather information",
        InputSchema: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "location": map[string]interface{}{
                    "type":        "string",
                    "description": "City name",
                },
            },
            "required": []string{"location"},
        },
    },
    func(ctx context.Context, arguments map[string]interface{}) (mcp.ToolResult, error) {
        location := arguments["location"].(string)
        return mcp.ToolResult{
            Content: []mcp.ToolContent{
                {
                    Type: "text",
                    Text: fmt.Sprintf("Weather in %s: Sunny, 72°F", location),
                },
            },
        }, nil
    },
)

server.SetToolsHandler(toolsHandler)
```

### Starting the Server

```go
if err := server.Start(":8080"); err != nil {
    log.Fatalf("Server error: %v", err)
}
```

## Implementing Custom Handlers

You can implement custom handlers by implementing the appropriate interfaces:

```go
type MyResourcesHandler struct {
    // Your implementation
}

func (h *MyResourcesHandler) List(ctx context.Context, cursor string) ([]mcp.ResourceInfo, string, error) {
    // Your implementation
}

func (h *MyResourcesHandler) Read(ctx context.Context, uri string) ([]mcp.ResourceContent, error) {
    // Your implementation
}

// Set your custom handler
server.SetResourcesHandler(&MyResourcesHandler{})
```

## Notifications

The server can send notifications to clients to inform them of changes:

```go
// Notify clients that the resources list has changed
server.NotifyResourcesListChanged()

// Notify clients that a resource has been updated
server.NotifyResourceUpdated("file:///example.txt")

// Notify clients that the prompts list has changed
server.NotifyPromptsListChanged()

// Notify clients that the tools list has changed
server.NotifyToolsListChanged()
```

## Logging

The server includes a built-in logging system:

```go
logManager := mcp.NewLogManager(server)

// Log messages with different severity levels
logManager.Debug("component", map[string]interface{}{"message": "Debug information"})
logManager.Info("component", map[string]interface{}{"message": "Information"})
logManager.Warning("component", map[string]interface{}{"message": "Warning"})
logManager.Error("component", map[string]interface{}{"message": "Error occurred", "error": "details"})
```

## License

[Your License Here]
