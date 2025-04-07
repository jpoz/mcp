package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Root represents a filesystem root
type Root struct {
	URI  string `json:"uri"`            // URI for the root, must be a file:// URI
	Name string `json:"name,omitempty"` // Optional human-readable name
}

// ListRoots requests the list of roots from the server
func (c *Client) ListRoots(ctx context.Context) ([]Root, error) {
	if !c.IsInitialized() {
		return nil, errors.New("client not initialized")
	}

	// Check if the server supports the roots capability
	c.mu.Lock()
	_, hasRoots := c.capabilities["roots"]
	c.mu.Unlock()
	if !hasRoots {
		return nil, errors.New("server does not support roots capability")
	}

	// Send the request
	resp, err := c.SendRequest(ctx, "roots/list", nil)
	if err != nil {
		return nil, fmt.Errorf("list roots request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("server returned error: %d - %s", resp.Error.Code, resp.Error.Message)
	}

	// Parse the result
	var result struct {
		Roots []Root `json:"roots"`
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal roots list result: %w", err)
	}

	return result.Roots, nil
}

// RegisterRootsChangeHandler registers a handler for root list changes
// The handler function will be called whenever the list of roots changes
// Returns a function that should be called to stop handling roots change notifications
func (c *Client) RegisterRootsChangeHandler(ctx context.Context, handler func([]Root)) (func(), error) {
	if !c.IsInitialized() {
		return nil, errors.New("client not initialized")
	}

	// Check if the server supports the roots capability with listChanged
	c.mu.Lock()
	rootsCap, hasRoots := c.capabilities["roots"].(map[string]any)
	c.mu.Unlock()

	if !hasRoots {
		return nil, errors.New("server does not support roots capability")
	}

	listChanged, ok := rootsCap["listChanged"].(bool)
	if !ok || !listChanged {
		return nil, errors.New("server does not support roots list change notifications")
	}

	msgChan, errChan := c.ListenForMessages(ctx)

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(ctx)

	// Start a goroutine to handle messages
	go func() {
		defer cancel()

		for {
			select {
			case <-ctx.Done():
				return
			case err := <-errChan:
				// Log the error and continue
				fmt.Printf("Error receiving message: %v\n", err)
			case msg := <-msgChan:
				// Check if it's a roots list change notification
				notification, ok := msg.(*Notification)
				if !ok || notification.Method != "notifications/roots/list_changed" {
					continue
				}

				// When we get a notification, we need to request the new list
				roots, err := c.ListRoots(ctx)
				if err != nil {
					fmt.Printf("Failed to get updated roots list: %v\n", err)
					continue
				}

				// Call the handler with the new list
				handler(roots)
			}
		}
	}()

	return cancel, nil
}
