package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	"github.com/angellist/mcp"
	_ "github.com/lib/pq"
)

// PostgreSQL connection parameters for the docker container
const (
	host     = "localhost"
	port     = 5435
	user     = "postgres"
	password = "postgres"
	dbname   = "example_db"
)

type Task struct {
	ID          int
	Title       string
	Description string
	Status      string
	DueDate     time.Time
	CreatedAt   time.Time
}

func main() {
	ensureData()

	// Create a new server with specified configuration
	config := mcp.ServerConfig{
		ProtocolVersion: "2025-03-2", // Latest MCP protocol version
		ServerInfo: mcp.ServerInfo{
			Name:    "Example MCP Server",
			Version: "1.0.0",
		},
		Capabilities: map[string]interface{}{
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

	// Set up tools handler
	toolsHandler := mcp.NewDefaultToolsHandler()

	// Add a database query tool
	toolsHandler.AddTool(
		mcp.ToolInfo{
			Name:        "query",
			Description: "Query the database.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"psql_query": map[string]interface{}{
						"type":        "string",
						"description": "A pSQL query to execute",
					},
				},
				"required": []string{"psql_query"},
			},
		},
		func(ctx context.Context, arguments map[string]interface{}) (mcp.ToolResult, error) {
			// Extract the query argument
			query, ok := arguments["psql_query"].(string)
			if !ok {
				return mcp.ToolResult{
					Content: []mcp.ToolContent{
						{
							Type: "text",
							Text: "Missing or invalid `psql_query` parameter",
						},
					},
					IsError: true,
				}, nil
			}

			// Connection string
			psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
				host, port, user, password, dbname)

			// Connect to database
			db, err := sql.Open("postgres", psqlInfo)
			if err != nil {
				return mcp.ToolResult{
					Content: []mcp.ToolContent{
						{
							Type: "text",
							Text: fmt.Sprintf("Failed to connect to database: %v", err),
						},
					},
					IsError: true,
				}, nil
			}
			defer db.Close()

			// Execute the query
			rows, err := db.QueryContext(ctx, query)
			if err != nil {
				return mcp.ToolResult{
					Content: []mcp.ToolContent{
						{
							Type: "text",
							Text: fmt.Sprintf("Query execution failed: %v", err),
						},
					},
					IsError: true,
				}, nil
			}
			defer rows.Close()

			// Get column names
			columns, err := rows.Columns()
			if err != nil {
				return mcp.ToolResult{
					Content: []mcp.ToolContent{
						{
							Type: "text",
							Text: fmt.Sprintf("Failed to get column names: %v", err),
						},
					},
					IsError: true,
				}, nil
			}

			// Prepare a slice of interface{} to hold the row values
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range columns {
				valuePtrs[i] = &values[i]
			}

			// Build response
			var result strings.Builder
			result.WriteString("Results:\n\n")

			// Write column headers
			for i, col := range columns {
				if i > 0 {
					result.WriteString(" | ")
				}
				result.WriteString(col)
			}
			result.WriteString("\n")

			// Write separator line
			for i := range columns {
				if i > 0 {
					result.WriteString("-+-")
				}
				result.WriteString("----")
			}
			result.WriteString("\n")

			// Process result rows
			rowCount := 0
			for rows.Next() {
				// Scan the row into the valuePtrs
				if err := rows.Scan(valuePtrs...); err != nil {
					return mcp.ToolResult{
						Content: []mcp.ToolContent{
							{
								Type: "text",
								Text: fmt.Sprintf("Failed to scan row: %v", err),
							},
						},
						IsError: true,
					}, nil
				}

				// Write row values
				for i, val := range values {
					if i > 0 {
						result.WriteString(" | ")
					}

					// Handle different types of values
					if val == nil {
						result.WriteString("NULL")
					} else {
						switch v := val.(type) {
						case []byte:
							result.WriteString(string(v))
						default:
							result.WriteString(fmt.Sprintf("%v", v))
						}
					}
				}
				result.WriteString("\n")
				rowCount++
			}

			// Check for errors during row iteration
			if err := rows.Err(); err != nil {
				return mcp.ToolResult{
					Content: []mcp.ToolContent{
						{
							Type: "text",
							Text: fmt.Sprintf("Error during row iteration: %v", err),
						},
					},
					IsError: true,
				}, nil
			}

			result.WriteString(fmt.Sprintf("\n%d row(s) returned", rowCount))

			return mcp.ToolResult{
				Content: []mcp.ToolContent{
					{
						Type: "text",
						Text: result.String(),
					},
				},
				IsError: false,
			}, nil
		},
	)

	server.SetToolsHandler(toolsHandler)

	// Start the server
	fmt.Println("Starting MCP server on :8081...")
	if err := server.Start(":8081"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// ensureData ensures that the database and tables are set up
func ensureData() {
	// Connection string
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	// Connect to database
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		log.Fatalf("Failed to open database connection: %v", err)
	}
	defer db.Close()

	// Check connection
	err = db.Ping()
	if err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	fmt.Println("Successfully connected to database!")

	// Check if tasks table exists
	var tableExists bool
	err = db.QueryRow("SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'tasks')").Scan(&tableExists)
	if err != nil {
		log.Fatalf("Failed to check if table exists: %v", err)
	}

	if !tableExists {
		fmt.Println("Tasks table does not exist. Creating table...")
		// Create tasks table
		_, err = db.Exec(`
			CREATE TABLE tasks (
				id SERIAL PRIMARY KEY,
				title VARCHAR(100) NOT NULL,
				description TEXT,
				status VARCHAR(20) NOT NULL DEFAULT 'pending',
				due_date TIMESTAMP,
				created_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`)
		if err != nil {
			log.Fatalf("Failed to create tasks table: %v", err)
		}
		fmt.Println("Tasks table created successfully")

		// Insert example tasks
		insertExampleTasks(db)
	} else {
		fmt.Println("\tTasks table already exists. Checking for migrations...")

		// Check if status column exists (example migration)
		var statusColumnExists bool
		err = db.QueryRow("SELECT EXISTS (SELECT FROM information_schema.columns WHERE table_name = 'tasks' AND column_name = 'status')").Scan(&statusColumnExists)
		if err != nil {
			log.Fatalf("Failed to check if status column exists: %v", err)
		}

		if !statusColumnExists {
			fmt.Println("Adding status column...")
			_, err = db.Exec("ALTER TABLE tasks ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'pending'")
			if err != nil {
				log.Fatalf("Failed to add status column: %v", err)
			}
			fmt.Println("Migration completed: Added status column")
		}

		// Check if due_date column exists (example migration)
		var dueDateColumnExists bool
		err = db.QueryRow("SELECT EXISTS (SELECT FROM information_schema.columns WHERE table_name = 'tasks' AND column_name = 'due_date')").Scan(&dueDateColumnExists)
		if err != nil {
			log.Fatalf("Failed to check if due_date column exists: %v", err)
		}

		if !dueDateColumnExists {
			fmt.Println("Adding due_date column...")
			_, err = db.Exec("ALTER TABLE tasks ADD COLUMN due_date TIMESTAMP")
			if err != nil {
				log.Fatalf("Failed to add due_date column: %v", err)
			}
			fmt.Println("Migration completed: Added due_date column")
		}
	}

	// Query to verify tasks
	rows, err := db.Query("SELECT COUNT(*) FROM tasks")
	if err != nil {
		log.Fatalf("Failed to query tasks: %v", err)
	}
	defer rows.Close()

	var count int
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			log.Fatalf("Failed to scan row: %v", err)
		}
	}

	fmt.Printf("\tNumber of tasks in database: %d\n", count)

	if count == 0 {
		fmt.Println("No tasks found. Inserting example tasks...")
		insertExampleTasks(db)
	}
}

func insertExampleTasks(db *sql.DB) {
	exampleTasks := []Task{
		{
			Title:       "Complete project documentation",
			Description: "Write comprehensive documentation for the API endpoints",
			Status:      "pending",
			DueDate:     time.Now().AddDate(0, 0, 7), // Due in 7 days
		},
		{
			Title:       "Fix authentication bug",
			Description: "Debug and fix the authentication issue in the login flow",
			Status:      "in_progress",
			DueDate:     time.Now().AddDate(0, 0, 2), // Due in 2 days
		},
		{
			Title:       "Deploy to staging",
			Description: "Deploy the latest changes to the staging environment",
			Status:      "pending",
			DueDate:     time.Now().AddDate(0, 0, 3), // Due in 3 days
		},
	}

	for _, task := range exampleTasks {
		_, err := db.Exec(`
			INSERT INTO tasks (title, description, status, due_date)
			VALUES ($1, $2, $3, $4)
		`, task.Title, task.Description, task.Status, task.DueDate)

		if err != nil {
			log.Fatalf("Failed to insert task: %v", err)
		}
	}

	fmt.Println("Example tasks inserted successfully")
}
