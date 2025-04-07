package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/angellist/mcp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: example <server-url>")
		os.Exit(1)
	}

	serverURL := os.Args[1]
	fmt.Printf("Connecting to MCP server at %s\n", serverURL)

	// Create the client
	client := mcp.NewClient(serverURL)

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C to gracefully exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt, shutting down...")
		cancel()
	}()

	// Initialize the client
	fmt.Println("Initializing client...")
	err := client.Initialize(ctx, mcp.ClientInfo{
		Name:    "MCP Example Client",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	fmt.Println("Successfully connected to server")
	fmt.Printf("Protocol version: %s\n", client.GetProtocolVersion())

	serverInfo := client.GetServerInfo()
	fmt.Printf("Server: %s %s\n", serverInfo.Name, serverInfo.Version)

	capabilities := client.GetCapabilities()
	fmt.Println("Server capabilities:")
	for capability, details := range capabilities {
		fmt.Printf("  - %s: %v\n", capability, details)
	}

	// Send a ping
	fmt.Println("\nSending ping...")
	if err := client.Ping(ctx); err != nil {
		fmt.Printf("Ping failed: %v\n", err)
	} else {
		fmt.Println("Ping successful")
	}

	// Try to list roots if supported
	if _, ok := capabilities["roots"]; ok {
		fmt.Println("\nListing roots...")
		roots, err := client.ListRoots(ctx)
		if err != nil {
			fmt.Printf("Failed to list roots: %v\n", err)
		} else {
			fmt.Printf("Found %d roots:\n", len(roots))
			for _, root := range roots {
				fmt.Printf("  - %s (%s)\n", root.Name, root.URI)
			}
		}

		// Register for root change notifications if supported
		rootsCap, ok := capabilities["roots"].(map[string]interface{})
		if ok {
			if listChanged, ok := rootsCap["listChanged"].(bool); ok && listChanged {
				fmt.Println("\nRegistering for root change notifications...")
				stopHandler, err := client.RegisterRootsChangeHandler(ctx, func(roots []mcp.Root) {
					fmt.Printf("Roots changed, now have %d roots\n", len(roots))
					for _, root := range roots {
						fmt.Printf("  - %s (%s)\n", root.Name, root.URI)
					}
				})
				if err != nil {
					fmt.Printf("Failed to register roots change handler: %v\n", err)
				} else {
					defer stopHandler()
					fmt.Println("Registered for root change notifications")
				}
			}
		}
	}

	// Try to use sampling if supported
	if _, ok := capabilities["sampling"]; ok {
		fmt.Println("\nSending sampling request...")

		// Create a message
		params := mcp.CreateMessageParams{
			Messages: []mcp.Message{
				mcp.TextMessage(mcp.RoleUser, "What is the capital of France?"),
			},
			ModelPreferences: mcp.ModelPreferences{
				Hints: []mcp.ModelHint{
					{Name: "claude-3-sonnet"},
				},
				IntelligencePriority: 0.8,
				SpeedPriority:        0.5,
			},
			SystemPrompt: "You are a helpful assistant.",
			MaxTokens:    100,
		}

		// Add progress tracking
		progressToken := mcp.ProgressToken("progress-1")
		params = mcp.WithProgress(params, progressToken).(mcp.CreateMessageParams)

		// Register progress handler
		stopProgress, err := client.HandleProgress(ctx, progressToken, func(progress, total float64, message string) {
			if total > 0 {
				fmt.Printf("Progress: %.1f%% - %s\n", (progress/total)*100, message)
			} else {
				fmt.Printf("Progress: %.1f - %s\n", progress, message)
			}
		})
		if err != nil {
			fmt.Printf("Failed to register progress handler: %v\n", err)
		} else {
			defer stopProgress()
		}

		// Send the request
		result, err := client.CreateMessage(ctx, params)
		if err != nil {
			fmt.Printf("Failed to create message: %v\n", err)
		} else {
			fmt.Println("\nReceived response:")
			fmt.Printf("Role: %s\n", result.Role)
			fmt.Printf("Content: %s\n", result.Content.Text)
			fmt.Printf("Model: %s\n", result.Model)
			fmt.Printf("Stop reason: %s\n", result.StopReason)
		}
	}

	// Start listening for server-initiated messages
	fmt.Println("\nListening for server messages...")
	msgChan, errChan := client.ListenForMessages(ctx)

	// Wait for messages or context cancellation
	timeout := time.After(10 * time.Second)
	for {
		select {
		case <-ctx.Done():
			fmt.Println("Context cancelled, exiting")
			return
		case <-timeout:
			fmt.Println("Timeout reached, shutting down")
			return
		case err := <-errChan:
			fmt.Printf("Error receiving message: %v\n", err)
		case msg := <-msgChan:
			fmt.Printf("Received message: %#v\n", msg)
		}
	}
}
