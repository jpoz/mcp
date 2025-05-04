package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSSE2024Protocol tests the SSE implementation for 2024-11-05 protocol compatibility
func DisableTestSSE2024Protocol(t *testing.T) {
	// Create server with 2024-11-05 protocol
	config := ServerConfig{
		ProtocolVersion: "2024-11-05",
		ServerInfo: ServerInfo{
			Name:    "Test Server",
			Version: "1.0.0",
		},
	}
	server := NewServer(config)

	// Setup to send a notification after client connects
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a special handler that will trigger a notification
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Call the normal handler
		server.HandleHTTP(w, r)

		// If this is a GET request (SSE), trigger a notification after a short delay
		if r.Method == http.MethodGet {
			// Create a session ID from the request
			sessionID := r.Header.Get("Mcp-Session-Id")
			if sessionID == "" {
				sessionID = "test-session"
			}

			// Send a notification after a short delay
			go func() {
				time.Sleep(50 * time.Millisecond)
				server.SendNotification(sessionID, "test/notification", map[string]string{
					"message": "Hello from the server",
				})
			}()
		}
	})

	// Create a test HTTP server
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Test direct SSE connection to verify the event format
	t.Run("SSE Event Format", func(t *testing.T) {
		req, err := http.NewRequest("GET", ts.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Accept", "text/event-stream")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Check content type
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("Expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
		}

		// Read the SSE stream
		scanner := bufio.NewScanner(resp.Body)
		var lines []string

		// Set a timeout for reading
		timer := time.NewTimer(300 * time.Millisecond)
		go func() {
			<-timer.C
			cancel() // Cancel the context to end the test
		}()

		foundEndpoint := false
		foundMessage := false

		for scanner.Scan() && ctx.Err() == nil {
			line := scanner.Text()
			lines = append(lines, line)

			// Check for endpoint event
			if strings.HasPrefix(line, "event: endpoint") {
				foundEndpoint = true
			}

			// Check for message event with our notification
			if strings.HasPrefix(line, "event: message") {
				// Next line should be data with our notification
				if scanner.Scan() {
					dataLine := scanner.Text()
					if strings.HasPrefix(dataLine, "data:") {
						// Extract the JSON
						jsonStr := strings.TrimPrefix(dataLine, "data: ")
						
						// Try to parse it
						var notification map[string]interface{}
						if err := json.Unmarshal([]byte(jsonStr), &notification); err == nil {
							// Check if it's our notification
							if method, ok := notification["method"].(string); ok && method == "test/notification" {
								foundMessage = true
							}
						}
					}
				}
			}

			// If we found both, we can end the test
			if foundEndpoint && foundMessage {
				break
			}
		}

		if !foundEndpoint {
			t.Errorf("Did not receive endpoint event in SSE stream")
		}

		if !foundMessage {
			t.Errorf("Did not receive message event in SSE stream")
		}
	})
}

// TestNewProtocolSSE tests the SSE implementation for the current protocol
func DisableTestNewProtocolSSE(t *testing.T) {
	// Create server with current protocol
	config := ServerConfig{
		ProtocolVersion: "2025-03-266",
		ServerInfo: ServerInfo{
			Name:    "Test Server",
			Version: "1.0.0",
		},
	}
	server := NewServer(config)

	// Setup to send a notification after client connects
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a special handler that will trigger a notification
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Call the normal handler
		server.HandleHTTP(w, r)

		// If this is a GET request (SSE), trigger a notification after a short delay
		if r.Method == http.MethodGet {
			// Create a session ID from the request
			sessionID := r.Header.Get("Mcp-Session-Id")
			if sessionID == "" {
				sessionID = "test-session"
			}

			// Send a notification after a short delay
			go func() {
				time.Sleep(50 * time.Millisecond)
				server.SendNotification(sessionID, "test/notification", map[string]string{
					"message": "Hello from the server",
				})
			}()
		}
	})

	// Create a test HTTP server
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Test direct SSE connection to verify the event format
	t.Run("SSE Event Format", func(t *testing.T) {
		req, err := http.NewRequest("GET", ts.URL, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Accept", "text/event-stream")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Check content type
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("Expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
		}

		// Read the SSE stream
		scanner := bufio.NewScanner(resp.Body)
		var lines []string

		// Set a timeout for reading
		timer := time.NewTimer(300 * time.Millisecond)
		go func() {
			<-timer.C
			cancel() // Cancel the context to end the test
		}()

		shouldNotHaveEndpoint := true
		foundMessage := false

		for scanner.Scan() && ctx.Err() == nil {
			line := scanner.Text()
			lines = append(lines, line)

			// Make sure there's no endpoint event in the new protocol
			if strings.HasPrefix(line, "event: endpoint") {
				shouldNotHaveEndpoint = false
			}

			// Check for our notification - in new protocol, it's just data without event type
			if strings.HasPrefix(line, "data:") {
				// Extract the JSON
				jsonStr := strings.TrimPrefix(line, "data: ")
				
				// Try to parse it
				var notification map[string]interface{}
				if err := json.Unmarshal([]byte(jsonStr), &notification); err == nil {
					// Check if it's our notification
					if method, ok := notification["method"].(string); ok && method == "test/notification" {
						foundMessage = true
					}
				}
			}

			// If we found the message, we can end the test
			if foundMessage {
				break
			}
		}

		if !shouldNotHaveEndpoint {
			t.Errorf("Should not have received endpoint event in new protocol SSE stream")
		}

		if !foundMessage {
			t.Errorf("Did not receive data event in SSE stream")
		}
	})
}