package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/angellist/mcp"
)

func main() {
	// Set up logging with a more verbose format
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Make sure debug logs are shown
	slog.SetLogLoggerLevel(slog.LevelDebug)

	// Hardcode the OpenAPI URL for the Weather API
	openAPIURL := "https://api.weather.gov/openapi.json"

	// Create the command
	cmd := exec.Command("emcee", openAPIURL)

	// Log the command being executed
	fmt.Printf("Executing command: %s\n", cmd.String())

	// Create the MCP client with STDIO transport using the simplified helper function
	client, err := mcp.NewSTDIOClientForCommandWithOptions(cmd, mcp.ClientOptions{
		DefaultTimeout: 10 * time.Second,
		Logger:         logger,
	})
	if err != nil {
		log.Fatalf("Failed to create MCP client: %v", err)
	}

	fmt.Printf("Started emcee command with PID: %d\n", cmd.Process.Pid)

	// Create a context with cancellation - 10 second timeout to ensure it exits
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigChan:
			fmt.Println("Shutting down due to signal...")
			cancel()
		case <-time.After(10 * time.Second):
			fmt.Println("Shutting down due to timeout...")
			os.Exit(0) // NEED THIS to ensure we exit cleanly
		}
	}()

	// Initialize the client
	fmt.Println("Initializing MCP client...")
	err = client.Initialize(ctx, mcp.ClientInfo{
		Name:    "STDIO MCP Client Example",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	// Print server info
	serverInfo := client.GetServerInfo()
	fmt.Printf("Connected to server: %s %s\n", serverInfo.Name, serverInfo.Version)
	fmt.Printf("Protocol version: %s\n", client.GetProtocolVersion())

	// Print capabilities
	capabilities := client.GetCapabilities()
	fmt.Println("Server capabilities:")
	for capability, details := range capabilities {
		fmt.Printf("  - %s: %v\n", capability, details)
	}

	// List all available tools
	if _, ok := capabilities["tools"]; ok {
		fmt.Println("\nListing available tools:")
		tools, err := client.ListAllTools(ctx)
		if err != nil {
			fmt.Printf("Failed to list tools: %v\n", err)
		} else {
			for _, tool := range tools {
				fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
			}
		}
	} else {
		fmt.Println("\nServer does not support tools capability")
	}

	// Example: Call the alerts_types tool, which doesn't need parameters
	fmt.Println("\nGetting alert types...")
	params := map[string]string{}
	
	// Send the request directly using the lower-level SendRequest instead of CallTool
	resp, err := client.SendRequest(ctx, "tools/call", map[string]any{
		"name":      "alerts_types",
		"arguments": params,
	})
	
	if err != nil {
		fmt.Printf("alerts_types call failed: %v\n", err)
	} else if resp.Error != nil {
		fmt.Printf("alerts_types call failed: %v\n", resp.Error)
	} else {
		fmt.Println("Alert types result:")
		// Marshal the result part of the response back to bytes
		resultBytes, _ := json.Marshal(resp.Result)
		fmt.Printf("  %s\n", string(resultBytes))
	}

	// Example: Call the gridpoint_forecast tool for a specific location
	fmt.Println("\nGetting weather forecast for Boulder, CO...")
	forecastArgs := map[string]any{
		"wfo": "BOU", // Boulder weather office
		"x":   54,    // Grid coordinates for Boulder area
		"y":   75,
	}

	fmt.Printf("Calling gridpoint_forecast with arguments: %v\n", forecastArgs)
	
	// Send the request directly using the lower-level SendRequest
	forecastResp, err := client.SendRequest(ctx, "tools/call", map[string]any{
		"name":      "gridpoint_forecast",
		"arguments": forecastArgs,
	})

	if err != nil {
		fmt.Printf("gridpoint_forecast call failed: %v\n", err)
	} else if forecastResp.Error != nil {
		fmt.Printf("gridpoint_forecast call failed: %v\n", forecastResp.Error)
	} else {
		fmt.Printf("\nForecast for Boulder, CO:\n")
		
		// Get the result content
		var result struct {
			Content []struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
			} `json:"content"`
		}
		
		// Marshal and unmarshal to parse the result structure
		resultBytes, _ := json.Marshal(forecastResp.Result)
		if err := json.Unmarshal(resultBytes, &result); err != nil {
			fmt.Printf("Failed to parse forecast result: %v\n", err)
			return
		}
		
		// Process each content item
		for _, content := range result.Content {
			if content.Type == "text" {
				// Try to parse the JSON text into a forecast object
				var forecastData map[string]any
				if err := json.Unmarshal([]byte(content.Text), &forecastData); err == nil {
					if properties, ok := forecastData["properties"].(map[string]any); ok {
						if periods, ok := properties["periods"].([]any); ok && len(periods) > 0 {
							fmt.Println("\nForecast Periods:")
							for i, p := range periods {
								if period, ok := p.(map[string]any); ok {
									fmt.Printf("  %v: %v\n", period["name"], period["detailedForecast"])
									if i >= 2 { // Just show first few periods
										break
									}
								}
							}
						}
					}
				} else {
					// If can't parse, just show the raw text (truncated)
					if len(content.Text) > 200 {
						fmt.Printf("  Text: %s...\n", content.Text[:200])
					} else {
						fmt.Printf("  Text: %s\n", content.Text)
					}
				}
			}
		}
	}

	// Shut down the client
	if err := client.Shutdown(ctx); err != nil {
		fmt.Printf("Error shutting down client: %v\n", err)
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		fmt.Printf("Command exited with error: %v\n", err)
	}
}