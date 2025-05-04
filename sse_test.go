package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServerVersionDetection tests that the server correctly detects and stores the negotiated protocol version
func TestServerVersionDetection(t *testing.T) {
	// Create server with default config
	config := ServerConfig{
		ProtocolVersion: "2025-03-266",
		ServerInfo: ServerInfo{
			Name:    "Test Server",
			Version: "1.0.0",
		},
	}
	server := NewServer(config)

	// Create a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	// Test 2025-03-266 (current version)
	t.Run("Current Version", func(t *testing.T) {
		client := NewClient(ts.URL)
		err := client.Initialize(context.Background(), ClientInfo{
			Name:    "Test Client",
			Version: "1.0.0",
		})
		if err != nil {
			t.Fatalf("Failed to initialize client: %v", err)
		}

		// Check negotiated protocol version
		if client.GetProtocolVersion() != "2025-03-266" {
			t.Errorf("Expected protocol version 2025-03-266, got %s", client.GetProtocolVersion())
		}
	})

	// Test 2024-11-05 (old version)
	t.Run("Old Version", func(t *testing.T) {
		// Create a new server that supports the old version
		oldConfig := ServerConfig{
			ProtocolVersion: "2024-11-05",
			ServerInfo: ServerInfo{
				Name:    "Test Server",
				Version: "1.0.0",
			},
		}
		oldServer := NewServer(oldConfig)
		
		// Create a temporary router that handles request properly
		oldTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			oldServer.HandleHTTP(w, r)
		}))
		defer oldTs.Close()

		client := NewClient(oldTs.URL)
		
		// Client should try with 2025-03-266 first, then fall back to 2024-11-05
		err := client.Initialize(context.Background(), ClientInfo{
			Name:    "Test Client",
			Version: "1.0.0",
		})
		if err != nil {
			t.Fatalf("Failed to initialize client: %v", err)
		}

		// Check negotiated protocol version
		if client.GetProtocolVersion() != "2024-11-05" {
			t.Errorf("Expected protocol version 2024-11-05, got %s", client.GetProtocolVersion())
		}
	})
}