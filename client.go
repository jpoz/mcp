// Client provides a client for the Model Context Protocol
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Client represents an MCP client
type Client struct {
	transport *StreamableHTTP

	// State
	initialized atomic.Bool

	// Protocol negotiation results
	protocolVersion string
	capabilities    map[string]any
	serverInfo      ServerInfo

	// Request tracking
	nextRequestID int64

	// Synchronization
	mu sync.Mutex
}

// ServerInfo contains information about the server
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientInfo contains information about the client
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// NewClient creates a new MCP client
func NewClient(baseURL string) *Client {
	return &Client{
		transport:     NewStreamableHTTP(baseURL),
		nextRequestID: 1,
		capabilities:  make(map[string]any),
	}
}

// IsInitialized returns whether the client has been initialized
func (c *Client) IsInitialized() bool {
	return c.initialized.Load()
}

// GetServerInfo returns information about the server
func (c *Client) GetServerInfo() ServerInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverInfo
}

// GetProtocolVersion returns the negotiated protocol version
func (c *Client) GetProtocolVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.protocolVersion
}

// GetCapabilities returns the negotiated capabilities
func (c *Client) GetCapabilities() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]any)
	for k, v := range c.capabilities {
		result[k] = v
	}
	return result
}

// GetSessionID returns the current session ID
func (c *Client) GetSessionID() string {
	return c.transport.GetSessionID()
}

// nextID generates a unique request ID
func (c *Client) nextID() int64 {
	return atomic.AddInt64(&c.nextRequestID, 1)
}

// Initialize initializes the client by negotiating capabilities with the server
func (c *Client) Initialize(ctx context.Context, clientInfo ClientInfo) error {
	if c.IsInitialized() {
		return errors.New("client already initialized")
	}

	// Latest supported protocol versions, in order of preference (latest first)
	supportedVersions := []string{"2025-03-26", "2024-11-05"}
	preferredVersion := supportedVersions[0]

	// Prepare initialization request
	params := map[string]any{
		"protocolVersion": preferredVersion,
		"capabilities": map[string]any{
			// Declare client capabilities
			"roots": map[string]any{
				"listChanged": true,
			},
			"sampling": map[string]any{},
		},
		"clientInfo": clientInfo,
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal initialization parameters: %w", err)
	}

	// Send initialization request
	req := NewRequest(c.nextID(), "initialize", rawParams)
	resp, err := c.transport.SendRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("initialization failed: %w", err)
	}

	// Check for errors in the response
	if resp.Error != nil {
		// Check if the error is about unsupported protocol version
		if resp.Error.Code == -32602 {
			var errorData struct {
				Supported []string `json:"supported"`
				Requested string   `json:"requested"`
			}

			if resp.Error.Data != nil {
				dataBytes, err := json.Marshal(resp.Error.Data)
				if err == nil {
					if err := json.Unmarshal(dataBytes, &errorData); err == nil {
						// Try to find a compatible version
						for _, supportedByClient := range supportedVersions {
							for _, supportedByServer := range errorData.Supported {
								if supportedByClient == supportedByServer {
									// Found a compatible version, retry initialization
									c.mu.Lock()
									c.nextRequestID = 1 // Reset request ID to ensure deterministic behavior
									c.mu.Unlock()
									
									// Update params with compatible version
									params["protocolVersion"] = supportedByClient
									rawParams, _ = json.Marshal(params)
									
									// Send new initialization request
									req = NewRequest(c.nextID(), "initialize", rawParams)
									resp, err = c.transport.SendRequest(ctx, req)
									if err != nil {
										return fmt.Errorf("initialization retry failed: %w", err)
									}
									
									// Check for errors in the retry response
									if resp.Error != nil {
										return fmt.Errorf("server returned error on retry: %d - %s", resp.Error.Code, resp.Error.Message)
									}
									
									// Continue with successful response
									goto ProcessSuccessfulResponse
								}
							}
						}
					}
				}
			}
			
			return fmt.Errorf("no compatible protocol version found: server supports %v, client supports %v", 
				errorData.Supported, supportedVersions)
		}
		
		return fmt.Errorf("server returned error: %d - %s", resp.Error.Code, resp.Error.Message)
	}

ProcessSuccessfulResponse:
	// Parse the result
	var result struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ServerInfo      ServerInfo     `json:"serverInfo"`
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return fmt.Errorf("failed to re-marshal result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return fmt.Errorf("failed to unmarshal initialization result: %w", err)
	}

	// Verify the protocol version is supported
	versionSupported := false
	for _, version := range supportedVersions {
		if result.ProtocolVersion == version {
			versionSupported = true
			break
		}
	}

	if !versionSupported {
		return fmt.Errorf("server negotiated unsupported protocol version: %s", result.ProtocolVersion)
	}

	// Store the results
	c.mu.Lock()
	c.protocolVersion = result.ProtocolVersion
	c.capabilities = result.Capabilities
	c.serverInfo = result.ServerInfo
	c.mu.Unlock()

	// Send initialized notification
	notif := NewNotification("notifications/initialized", nil)
	if err := c.transport.SendNotification(ctx, notif); err != nil {
		return fmt.Errorf("failed to send initialized notification: %w", err)
	}

	// Mark as initialized
	c.initialized.Store(true)
	return nil
}

// Shutdown properly shuts down the client
func (c *Client) Shutdown(ctx context.Context) error {
	// For Streamable HTTP, no explicit shutdown is necessary
	// Just mark as not initialized
	c.initialized.Store(false)
	return nil
}

// SendRequest sends a JSON-RPC request to the server
func (c *Client) SendRequest(ctx context.Context, method string, params any) (*Response, error) {
	if !c.IsInitialized() && method != "initialize" {
		return nil, errors.New("client not initialized")
	}

	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parameters: %w", err)
	}

	req := NewRequest(c.nextID(), method, rawParams)
	return c.transport.SendRequest(ctx, req)
}

// SendRequestWithEvents sends a request that might generate an SSE stream
func (c *Client) SendRequestWithEvents(ctx context.Context, method string, params any) (*Response, <-chan any, <-chan error, error) {
	if !c.IsInitialized() && method != "initialize" {
		return nil, nil, nil, errors.New("client not initialized")
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to marshal parameters: %w", err)
	}

	req := NewRequest(c.nextID(), method, rawParams)
	return c.transport.SendRequestWithEvents(ctx, req)
}

// SendNotification sends a JSON-RPC notification to the server
func (c *Client) SendNotification(ctx context.Context, method string, params any) error {
	if !c.IsInitialized() && method != "notifications/initialized" {
		return errors.New("client not initialized")
	}

	notif := NewNotification(method, params)
	return c.transport.SendNotification(ctx, notif)
}

// CallTool invokes a server-side tool with the provided arguments
func (c *Client) CallTool(ctx context.Context, toolName string, arguments map[string]any) (ToolResult, error) {
	if !c.IsInitialized() {
		return ToolResult{}, errors.New("client not initialized")
	}

	// Prepare the parameters
	params := CallParams{
		Name:      toolName,
		Arguments: arguments,
	}

	// Send the request
	resp, err := c.SendRequest(ctx, "tools/call", params)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to call tool: %w", err)
	}

	// Check for errors in the response
	if resp.Error != nil {
		return ToolResult{}, fmt.Errorf("server returned error: %d - %s", resp.Error.Code, resp.Error.Message)
	}

	// Parse the result
	var result ToolResult
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to re-marshal result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return ToolResult{}, fmt.Errorf("failed to unmarshal tool result: %w", err)
	}

	return result, nil
}

// ListenForMessages starts listening for server-initiated messages
func (c *Client) ListenForMessages(ctx context.Context) (<-chan any, <-chan error) {
	if !c.IsInitialized() {
		errChan := make(chan error, 1)
		msgChan := make(chan any)
		errChan <- errors.New("client not initialized")
		close(msgChan)
		close(errChan)
		return msgChan, errChan
	}

	return c.transport.ListenForMessages(ctx)
}
