package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/angellist/mcp" // Replace with your actual package import path
)

func main() {
	// Create a new server with specified configuration
	config := mcp.ServerConfig{
		ProtocolVersion: "2025-03-2", // Latest MCP protocol version
		ServerInfo: mcp.ServerInfo{
			Name:    "Example MCP Server",
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
			"completions": map[string]interface{}{},
			"logging":     map[string]interface{}{},
		},
	}

	server := mcp.NewServer(config)
	server.SetLogger(slog.Default())
	slog.SetLogLoggerLevel(slog.LevelDebug)

	// Set up resource handler
	resourceHandler := mcp.NewDefaultResourcesHandler()

	// Add some example resources
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

	resourceHandler.AddResource(
		mcp.ResourceInfo{
			URI:         "file:///example.json",
			Name:        "example.json",
			Description: "An example JSON file",
			MimeType:    "application/json",
		},
		mcp.ResourceContent{
			URI:      "file:///example.json",
			MimeType: "application/json",
			Text:     `{"key": "value", "nested": {"array": [1, 2, 3]}}`,
		},
	)

	server.SetResourcesHandler(resourceHandler)

	// Set up prompts handler
	promptsHandler := mcp.NewDefaultPromptsHandler()

	// Add an example prompt
	promptsHandler.AddPrompt(
		mcp.PromptInfo{
			Name:        "code_review",
			Description: "Review code for quality and suggest improvements",
			Arguments: []mcp.PromptArgument{
				{
					Name:        "code",
					Description: "The code to review",
					Required:    true,
				},
				{
					Name:        "language",
					Description: "The programming language",
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
						Text: "Please review this {language} code and suggest improvements:\n\n{code}",
					},
				},
			},
		},
	)

	server.SetPromptsHandler(promptsHandler)

	// Set up tools handler
	toolsHandler := mcp.NewDefaultToolsHandler()

	// Add an example tool
	toolsHandler.AddTool(
		mcp.ToolInfo{
			Name:        "get_weather",
			Description: "Get current weather information for a location",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "City name or zip code",
					},
				},
				"required": []string{"location"},
			},
		},
		func(ctx context.Context, arguments map[string]interface{}) (mcp.ToolResult, error) {
			// Extract the location argument
			location, ok := arguments["location"].(string)
			if !ok {
				return mcp.ToolResult{
					Content: []mcp.ToolContent{
						{
							Type: "text",
							Text: "Missing or invalid location parameter",
						},
					},
					IsError: true,
				}, nil
			}

			// In a real implementation, you would call a weather API
			// For this example, we'll just return some mock data
			return mcp.ToolResult{
				Content: []mcp.ToolContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Current weather in %s:\nTemperature: 72°F\nConditions: Partly cloudy\nHumidity: 45%%", location),
					},
				},
				IsError: false,
			}, nil
		},
	)

	server.SetToolsHandler(toolsHandler)

	// Set up server notifications
	go func() {
		// Simulate some resource updates over time
		time.Sleep(30 * time.Second)
		fmt.Println("Notifying clients of resource list changes...")
		server.NotifyResourcesListChanged()

		time.Sleep(30 * time.Second)
		fmt.Println("Notifying clients of prompts list changes...")
		server.NotifyPromptsListChanged()
	}()

	// Start the server
	fmt.Println("Starting MCP server on :8081...")
	if err := server.Start(":8081"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
