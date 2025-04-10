package mcp

import (
	"context"
	"errors"
	"fmt"
)

// Ping sends a ping request to the server
func (c *Client) Ping(ctx context.Context) error {
	if !c.IsInitialized() {
		return errors.New("client not initialized")
	}

	resp, err := c.SendRequest(ctx, "ping", nil)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("server returned error: %d - %s", resp.Error.Code, resp.Error.Message)
	}

	return nil
}

// CancelRequest sends a cancellation notification for an in-progress request
func (c *Client) CancelRequest(ctx context.Context, requestID any, reason string) error {
	if !c.IsInitialized() {
		return errors.New("client not initialized")
	}

	params := map[string]any{
		"requestId": requestID,
	}

	if reason != "" {
		params["reason"] = reason
	}

	return c.SendNotification(ctx, "notifications/cancelled", params)
}

// WithProgress adds a progress token to a request's parameters
func WithProgress(params any, token ProgressToken) any {
	if params == nil {
		return map[string]any{
			"_meta": map[string]any{
				"progressToken": token,
			},
		}
	}

	// If params is a map, add the _meta field
	if paramsMap, ok := params.(map[string]any); ok {
		meta, ok := paramsMap["_meta"].(map[string]any)
		if !ok {
			meta = make(map[string]any)
			paramsMap["_meta"] = meta
		}
		meta["progressToken"] = token
		return paramsMap
	}

	// Otherwise, wrap the params in a new map
	return map[string]any{
		"params": params,
		"_meta": map[string]any{
			"progressToken": token,
		},
	}
}

// HandleProgress sets up a handler for progress notifications
// The handler function will be called whenever a progress notification is received
// Returns a function that should be called to stop handling progress notifications
func (c *Client) HandleProgress(ctx context.Context, token ProgressToken, handler func(progress, total float64, message string)) (func(), error) {
	if !c.IsInitialized() {
		return nil, errors.New("client not initialized")
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
				// Check if it's a progress notification
				notification, ok := msg.(*Notification)
				if !ok || notification.Method != "notifications/progress" {
					continue
				}

				// Parse the parameters
				params, ok := notification.Params.(map[string]any)
				if !ok {
					continue
				}

				// Check if it's for our token
				msgToken, ok := params["progressToken"].(string)
				if !ok || ProgressToken(msgToken) != token {
					continue
				}

				// Extract progress information
				progress, _ := params["progress"].(float64)
				total, _ := params["total"].(float64)
				message, _ := params["message"].(string)

				// Call the handler
				handler(progress, total, message)
			}
		}
	}()

	return cancel, nil
}
