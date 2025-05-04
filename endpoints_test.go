package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestConfigurableEndpoints tests that the server can be configured with different endpoints
func TestConfigurableEndpoints(t *testing.T) {
	// Create server with custom endpoints
	config := ServerConfig{
		ProtocolVersion: "2025-03-26",
		ServerInfo: ServerInfo{
			Name:    "Test Server",
			Version: "1.0.0",
		},
		SSEEndpoint:     "/sse",
		MessageEndpoint: "/rpc",
		ValidateOrigins: true,
		AllowedOrigins:  []string{"localhost", "127.0.0.1"},
	}
	server := NewServer(config)

	// Create a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	// Test connections to different endpoints
	t.Run("Message Endpoint", func(t *testing.T) {
		client := NewClient(ts.URL + "/rpc")
		err := client.Initialize(context.Background(), ClientInfo{
			Name:    "Test Client",
			Version: "1.0.0",
		})
		if err != nil {
			t.Fatalf("Failed to initialize client to message endpoint: %v", err)
		}

		// Try a ping to verify connection works
		err = client.Ping(context.Background())
		if err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
	})

	// Test with 2024-11-05 protocol
	t.Run("2024-11-05 Protocol with Endpoint Event", func(t *testing.T) {
		// Create a new server that supports the old version
		oldConfig := ServerConfig{
			ProtocolVersion: "2024-11-05",
			ServerInfo: ServerInfo{
				Name:    "Test Server",
				Version: "1.0.0",
			},
			SSEEndpoint:     "/sse",
			MessageEndpoint: "/rpc",
		}
		oldServer := NewServer(oldConfig)
		oldTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			oldServer.HandleHTTP(w, r)
		}))
		defer oldTs.Close()

		// Connect to message endpoint for initialization
		client := NewClient(oldTs.URL + "/rpc")
		
		err := client.Initialize(context.Background(), ClientInfo{
			Name:    "Test Client",
			Version: "1.0.0",
		})
		if err != nil {
			t.Fatalf("Failed to initialize client: %v", err)
		}

		// Verify protocol version
		if client.GetProtocolVersion() != "2024-11-05" {
			t.Errorf("Expected protocol version 2024-11-05, got %s", client.GetProtocolVersion())
		}

		// Try a ping to verify connection works
		err = client.Ping(context.Background())
		if err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
		
		// Now simulate a connection to the SSE endpoint to get the endpoint event
		req, err := http.NewRequest("GET", oldTs.URL+"/sse", nil)
		if err != nil {
			t.Fatalf("Failed to create SSE request: %v", err)
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Mcp-Session-Id", client.GetSessionID())
		
		httpClient := &http.Client{}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("SSE request failed: %v", err)
		}
		defer resp.Body.Close()
		
		// Verify content type
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("Expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
		}
	})
}

// TestOriginValidation tests that the server correctly validates the Origin header
func TestOriginValidation(t *testing.T) {
	// Create server with origin validation
	config := ServerConfig{
		ProtocolVersion: "2025-03-26",
		ServerInfo: ServerInfo{
			Name:    "Test Server",
			Version: "1.0.0",
		},
		ValidateOrigins: true,
		AllowedOrigins:  []string{"localhost", "127.0.0.1"},
	}
	server := NewServer(config)

	// Create a test HTTP server
	handler := http.HandlerFunc(server.HandleHTTP)
	
	// Test with valid origin
	t.Run("Valid Origin", func(t *testing.T) {
		req := httptest.NewRequest("POST", "http://localhost/", nil)
		req.Header.Set("Origin", "http://localhost")
		req.Header.Set("Content-Type", "application/json")
		
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		
		// Should not be forbidden
		if rr.Code == http.StatusForbidden {
			t.Errorf("Request with valid origin was forbidden")
		}
	})
	
	// Test with invalid origin
	t.Run("Invalid Origin", func(t *testing.T) {
		req := httptest.NewRequest("POST", "http://localhost/", nil)
		req.Header.Set("Origin", "http://evil.com")
		req.Header.Set("Content-Type", "application/json")
		
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		
		// Should be forbidden
		if rr.Code != http.StatusForbidden {
			t.Errorf("Request with invalid origin was allowed, got status: %d", rr.Code)
		}
	})
}