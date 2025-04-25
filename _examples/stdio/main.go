package main

import (
	"context"
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

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <openapi-json-url>")
		os.Exit(1)
	}

	// Get the OpenAPI URL from command line args
	openAPIURL := os.Args[1]

	// Start the emcee command with the OpenAPI URL and --raw flag to use JSONRPC
	cmd := exec.Command("emcee", openAPIURL)

	// Log the command being executed
	fmt.Printf("Executing command: %s\n", cmd.String())

	// Get pipes for stdin and stdout
	cmdStdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatalf("Failed to create stdin pipe: %v", err)
	}

	cmdStdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Failed to create stdout pipe: %v", err)
	}

	// Get stderr pipe
	cmdStderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatalf("Failed to create stderr pipe: %v", err)
	}

	// Start the command
	err = cmd.Start()
	if err != nil {
		log.Fatalf("Failed to start emcee command: %v", err)
	}

	fmt.Printf("Started emcee command with PID: %d\n", cmd.Process.Pid)

	// Create the MCP client with STDIO transport
	client := mcp.NewSTDIOClientWithOptions(
		cmd.Process,
		cmdStdin,
		cmdStdout,
		cmdStderr,
		mcp.ClientOptions{
			DefaultTimeout: 30 * time.Second,
			Logger:         logger,
		},
	)

	// Create a context with cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("Shutting down...")
		cancel()
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

	// Shut down the client
	if err := client.Shutdown(ctx); err != nil {
		fmt.Printf("Error shutting down client: %v\n", err)
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		fmt.Printf("Command exited with error: %v\n", err)
	}
}

