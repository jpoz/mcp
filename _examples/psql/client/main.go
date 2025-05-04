package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/angellist/mcp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <server-url>")
		os.Exit(1)
	}

	serverURL := os.Args[1]
	fmt.Printf("Connecting to MCP PostgreSQL server at %s\n", serverURL)

	// Determine if using old protocol with SSE endpoints
	usingSSE := strings.Contains(serverURL, "/rpc") || strings.Contains(serverURL, "/sse")
	protocolVersion := "2025-03-26"
	if usingSSE {
		fmt.Println("Using 2024-11-05 protocol with SSE transport")
		protocolVersion = "2024-11-05"
	} else {
		fmt.Println("Using latest protocol (2025-03-26)")
	}

	// Create the client
	client := mcp.NewClientWithOptions(serverURL, mcp.ClientOptions{
		DefaultTimeout: time.Second * 30,
		Logger:         slog.Default(),
	})

	slog.SetLogLoggerLevel(slog.LevelDebug)

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a reader for user input
	reader := bufio.NewReader(os.Stdin)

	// Handle Ctrl+C to gracefully exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt, shutting down...")
		cancel()
		os.Exit(0)
	}()

	// Initialize the client
	fmt.Println("Initializing client...")
	err := client.Initialize(ctx, mcp.ClientInfo{
		Name:    "MCP PostgreSQL Client",
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
	
	// Set up listening for server notifications
	fmt.Println("\nListening for server notifications...")
	// Start listening for messages in a goroutine
	msgChan, errChan := client.ListenForMessages(ctx)
	
	// Handle notifications in a background goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				// Context cancelled, exit goroutine
				return
			case err := <-errChan:
				if err != nil {
					fmt.Printf("\n❌ Error receiving notifications: %v\n", err)
				}
			case msg := <-msgChan:
				// Check if it's a notification
				notification, ok := msg.(*mcp.Notification)
				if ok {
					// Print database heartbeat messages
					if notification.Method == "database/stats" {
						// Format the notification as JSON for display
						data, _ := json.MarshalIndent(notification.Params, "", "  ")
						fmt.Printf("\n📊 Database Status Update Received:\n%s\n", string(data))
					} else {
						// Print other notifications
						fmt.Printf("\n🔔 Notification received: %s\n", notification.Method)
					}
				}
			}
		}
	}()
	
	fmt.Println("(Database heartbeat notifications will appear every 10 seconds)")

	// Check if tools capability is available
	_, toolsAvailable := capabilities["tools"]
	if !toolsAvailable {
		log.Fatalf("Server does not support tools capability which is required for database operations")
	}

	// Start interactive prompt
	for {
		fmt.Println("\n--- PostgreSQL Client Menu ---")
		fmt.Println("1. List all tasks")
		fmt.Println("2. View task details")
		fmt.Println("3. Create new task")
		fmt.Println("4. Update task status")
		fmt.Println("5. Delete task")
		fmt.Println("6. Execute custom SQL query")
		fmt.Println("0. Exit")
		fmt.Print("\nEnter your choice: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			listAllTasks(ctx, client, reader)
		case "2":
			viewTaskDetails(ctx, client, reader)
		case "3":
			createNewTask(ctx, client, reader)
		case "4":
			updateTaskStatus(ctx, client, reader)
		case "5":
			deleteTask(ctx, client, reader)
		case "6":
			executeCustomQuery(ctx, client, reader)
		case "0":
			fmt.Println("Exiting client...")
			return
		default:
			fmt.Println("Invalid choice. Please try again.")
		}
	}
}

// executeQuery executes a SQL query using the query tool
func executeQuery(ctx context.Context, client *mcp.Client, query string) error {
	args := map[string]interface{}{
		"psql_query": query,
	}

	results, err := client.CallTool(ctx, "query", args)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	for _, result := range results {
		for _, content := range result.Content {
			if content.Type == "text" {
				fmt.Println(content.Text)
			}
		}
		if result.IsError {
			return fmt.Errorf("query execution error: %v", result.Content)
		}
	}

	return nil
}

// listAllTasks displays all tasks from the database
func listAllTasks(ctx context.Context, client *mcp.Client, reader *bufio.Reader) {
	query := "SELECT id, title, status, due_date FROM tasks ORDER BY id"
	fmt.Println("Listing all tasks...")
	
	err := executeQuery(ctx, client, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

// viewTaskDetails displays detailed information about a specific task
func viewTaskDetails(ctx context.Context, client *mcp.Client, reader *bufio.Reader) {
	fmt.Print("Enter task ID: ")
	input, _ := reader.ReadString('\n')
	taskID := strings.TrimSpace(input)

	query := fmt.Sprintf("SELECT * FROM tasks WHERE id = %s", taskID)
	fmt.Printf("Fetching details for task #%s...\n", taskID)
	
	err := executeQuery(ctx, client, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

// createNewTask creates a new task in the database
func createNewTask(ctx context.Context, client *mcp.Client, reader *bufio.Reader) {
	fmt.Print("Enter task title: ")
	title, _ := reader.ReadString('\n')
	title = strings.TrimSpace(title)

	fmt.Print("Enter task description: ")
	description, _ := reader.ReadString('\n')
	description = strings.TrimSpace(description)

	fmt.Print("Enter task status (pending/in_progress/completed): ")
	status, _ := reader.ReadString('\n')
	status = strings.TrimSpace(status)
	if status == "" {
		status = "pending"
	}

	fmt.Print("Enter due date (YYYY-MM-DD) or leave blank for 7 days from now: ")
	dueDate, _ := reader.ReadString('\n')
	dueDate = strings.TrimSpace(dueDate)
	if dueDate == "" {
		dueDate = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	}

	query := fmt.Sprintf(`
		INSERT INTO tasks (title, description, status, due_date)
		VALUES ('%s', '%s', '%s', '%s')
		RETURNING id
	`, title, description, status, dueDate)

	fmt.Println("Creating new task...")
	err := executeQuery(ctx, client, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println("Task created successfully!")
	}
}

// updateTaskStatus updates the status of an existing task
func updateTaskStatus(ctx context.Context, client *mcp.Client, reader *bufio.Reader) {
	fmt.Print("Enter task ID: ")
	input, _ := reader.ReadString('\n')
	taskID := strings.TrimSpace(input)

	fmt.Print("Enter new status (pending/in_progress/completed): ")
	status, _ := reader.ReadString('\n')
	status = strings.TrimSpace(status)

	query := fmt.Sprintf(`
		UPDATE tasks
		SET status = '%s'
		WHERE id = %s
	`, status, taskID)

	fmt.Printf("Updating status for task #%s...\n", taskID)
	err := executeQuery(ctx, client, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println("Task status updated successfully!")
	}
}

// deleteTask deletes a task from the database
func deleteTask(ctx context.Context, client *mcp.Client, reader *bufio.Reader) {
	fmt.Print("Enter task ID to delete: ")
	input, _ := reader.ReadString('\n')
	taskID := strings.TrimSpace(input)

	fmt.Printf("Are you sure you want to delete task #%s? (y/n): ", taskID)
	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(strings.ToLower(confirm))

	if confirm == "y" || confirm == "yes" {
		query := fmt.Sprintf("DELETE FROM tasks WHERE id = %s", taskID)
		fmt.Printf("Deleting task #%s...\n", taskID)
		
		err := executeQuery(ctx, client, query)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Println("Task deleted successfully!")
		}
	} else {
		fmt.Println("Deletion cancelled.")
	}
}

// executeCustomQuery executes a custom SQL query provided by the user
func executeCustomQuery(ctx context.Context, client *mcp.Client, reader *bufio.Reader) {
	fmt.Println("Enter your SQL query (type on a single line):")
	query, _ := reader.ReadString('\n')
	query = strings.TrimSpace(query)

	if query == "" {
		fmt.Println("Empty query, operation cancelled.")
		return
	}

	fmt.Println("Executing query...")
	err := executeQuery(ctx, client, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}