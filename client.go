// Client provides a client for the Model Context Protocol
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Client represents an MCP client
type Client struct {
	transport Transport

	// State
	initialized atomic.Bool

	// Protocol negotiation results
	protocolVersion string
	capabilities    map[string]any
	serverInfo      ServerInfo

	// Request tracking
	nextRequestID int64

	// Request configuration
	defaultTimeout time.Duration

	// Synchronization
	mu sync.Mutex

	// Logging
	slog *slog.Logger
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

// ClientOptions contains options for creating a new client
type ClientOptions struct {
	// DefaultTimeout is the default timeout for all requests
	// If zero, no timeout is applied
	DefaultTimeout time.Duration
	Logger         *slog.Logger
}

// DefaultClientOptions returns the default options for a client
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		DefaultTimeout: 30 * time.Second, // Default 30 second timeout
		Logger:         slog.New(noopHandler{}),
	}
}

// NewClient creates a new MCP client with an HTTP transport
func NewClient(baseURL string) *Client {
	return NewClientWithOptions(baseURL, DefaultClientOptions())
}

// NewClientWithOptions creates a new MCP client with the given options
func NewClientWithOptions(baseURL string, options ClientOptions) *Client {
	return &Client{
		transport:      NewStreamableHTTP(baseURL),
		nextRequestID:  1,
		capabilities:   make(map[string]any),
		defaultTimeout: options.DefaultTimeout,
		slog:           options.Logger,
	}
}

// NewClientWithTransport creates a new MCP client with the given transport
func NewClientWithTransport(transport Transport) *Client {
	return NewClientWithTransportAndOptions(transport, DefaultClientOptions())
}

// NewClientWithTransportAndOptions creates a new MCP client with the given transport and options
func NewClientWithTransportAndOptions(transport Transport, options ClientOptions) *Client {
	return &Client{
		transport:      transport,
		nextRequestID:  1,
		capabilities:   make(map[string]any),
		defaultTimeout: options.DefaultTimeout,
	}
}

// // NewSTDIOClient creates a new MCP client with an STDIO transport
// func NewSTDIOClient(cmd *os.Process, stdin io.Writer, stdout io.Reader, stderr io.ReadCloser) *Client {
// 	transport := NewSTDIO(stdin, stdout, stderr, cmd)
// 	return NewClientWithTransport(transport)
// }
//
// // NewSTDIOClientWithOptions creates a new MCP client with an STDIO transport and the given options
// func NewSTDIOClientWithOptions(cmd *os.Process, stdin io.Writer, stdout io.Reader, stderr io.ReadCloser, options ClientOptions) *Client {
// 	transport := NewSTDIO(stdin, stdout, stderr, cmd)
// 	return NewClientWithTransportAndOptions(transport, options)
// }

// SetDefaultTimeout sets the default timeout for all requests
func (c *Client) SetDefaultTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultTimeout = timeout
}

// GetDefaultTimeout returns the default timeout for all requests
func (c *Client) GetDefaultTimeout() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.defaultTimeout
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
	versionSupported := slices.Contains(supportedVersions, result.ProtocolVersion)

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
	if c.IsInitialized() {
		// Try to terminate the session if there is one
		_ = c.TerminateSession(ctx) // Ignore errors since we're shutting down
	}

	// Mark as not initialized
	c.initialized.Store(false)
	return nil
}

// TerminateSession explicitly terminates the session on the server
func (c *Client) TerminateSession(ctx context.Context) error {
	if !c.IsInitialized() {
		return errors.New("client not initialized")
	}

	// Call the underlying transport's TerminateSession method
	if err := c.transport.TerminateSession(ctx); err != nil {
		return fmt.Errorf("failed to terminate session: %w", err)
	}

	return nil
}

// RequestOptions contains options for a single request
type RequestOptions struct {
	// Timeout is the timeout for this specific request
	// If zero, the client's default timeout is used
	Timeout time.Duration
}

// SendRequestWithOptions sends a JSON-RPC request to the server with specific options
func (c *Client) SendRequestWithOptions(ctx context.Context, method string, params any, options RequestOptions) (*Response, error) {
	if !c.IsInitialized() && method != "initialize" {
		return nil, errors.New("client not initialized")
	}

	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parameters: %w", err)
	}

	requestID := c.nextID()
	req := NewRequest(requestID, method, rawParams)

	// Apply timeout if specified or use default
	var cancel context.CancelFunc
	timeout := options.Timeout
	if timeout == 0 {
		c.mu.Lock()
		timeout = c.defaultTimeout
		c.mu.Unlock()
	}

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	// Start a goroutine to send a cancellation notification if the context is cancelled
	go func() {
		<-ctx.Done()
		// If the context was cancelled and not just completed normally, send a cancellation notification
		if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
			cancelParams := map[string]any{
				"id": requestID,
			}
			// Ignore errors since this is best-effort
			_ = c.SendNotification(context.Background(), "$/cancelRequest", cancelParams)
		}
		cancel() // Clean up
	}()

	// Send the request
	resp, err := c.transport.SendRequest(ctx, req)

	// Cancel the timeout goroutine if we get a response
	cancel()

	return resp, err
}

// SendRequest sends a JSON-RPC request to the server using default options
func (c *Client) SendRequest(ctx context.Context, method string, params any) (*Response, error) {
	return c.SendRequestWithOptions(ctx, method, params, RequestOptions{})
}

// SendRequest sends a JSON-RPC request to the server using default options
func (c *Client) SendRequestWithCallback(ctx context.Context, method string, params any, callback func(*SSEEvent) error) (*Response, error) {
	if !c.IsInitialized() && method != "initialize" {
		return nil, errors.New("client not initialized")
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parameters: %w", err)
	}

	requestID := c.nextID()
	req := NewRequest(requestID, method, rawParams)

	return c.transport.SendRequestWithCallback(ctx, req, callback)
}

// SendRequestWithEventsWithOptions sends a request that might generate an SSE stream with specific options
func (c *Client) SendRequestWithEventsWithOptions(ctx context.Context, method string, params any, options RequestOptions) (*Response, <-chan any, <-chan error, error) {
	if !c.IsInitialized() && method != "initialize" {
		return nil, nil, nil, errors.New("client not initialized")
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to marshal parameters: %w", err)
	}

	requestID := c.nextID()
	req := NewRequest(requestID, method, rawParams)

	// Apply timeout if specified or use default
	var cancel context.CancelFunc
	timeout := options.Timeout
	if timeout == 0 {
		c.mu.Lock()
		timeout = c.defaultTimeout
		c.mu.Unlock()
	}

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	// Start a goroutine to send a cancellation notification if the context is cancelled
	go func() {
		<-ctx.Done()
		// If the context was cancelled and not just completed normally, send a cancellation notification
		if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
			cancelParams := map[string]interface{}{
				"id": requestID,
			}
			// Ignore errors since this is best-effort
			_ = c.SendNotification(context.Background(), "$/cancelRequest", cancelParams)
		}
	}()

	// Send the request
	resp, events, errs, err := c.transport.SendRequestWithEvents(ctx, req)

	if err != nil {
		// If there was an error, cancel the timeout goroutine and return
		cancel()
		return resp, events, errs, err
	}

	// Create new event and error channels that we'll forward to
	newEvents := make(chan any)
	newErrs := make(chan error)

	// Start a goroutine to forward events and errors, and clean up when done
	go func() {
		defer close(newEvents)
		defer close(newErrs)
		defer cancel() // Clean up the cancel function when we're done

		// Forward all events to our new channel
		for {
			select {
			case evt, ok := <-events:
				if !ok {
					// Original channel was closed
					return
				}
				select {
				case newEvents <- evt:
				case <-ctx.Done():
					return
				}
			case err, ok := <-errs:
				if !ok {
					// Original channel was closed
					return
				}
				select {
				case newErrs <- err:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				// Context was cancelled, clean up
				return
			}
		}
	}()

	return resp, newEvents, newErrs, nil
}

// SendRequestWithEvents sends a request that might generate an SSE stream using default options
func (c *Client) SendRequestWithEvents(ctx context.Context, method string, params any) (*Response, <-chan any, <-chan error, error) {
	return c.SendRequestWithEventsWithOptions(ctx, method, params, RequestOptions{})
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
func (c *Client) CallTool(ctx context.Context, toolName string, arguments map[string]any) ([]ToolResult, error) {
	if !c.IsInitialized() {
		return []ToolResult{}, errors.New("client not initialized")
	}

	// Prepare the parameters
	params := CallParams{
		Name:      toolName,
		Arguments: arguments,
	}

	callback := func(evt *SSEEvent) error {
		c.slog.Debug("Received SSE event", "event", evt)

		fmt.Println("Received SSE event:", evt)
		return nil
	}

	c.slog.Debug("Calling tool", "toolName", toolName, "arguments", arguments)

	// Send the request
	resp, err := c.SendRequestWithCallback(ctx, "tools/call", params, callback)
	if err != nil {
		return []ToolResult{}, fmt.Errorf("failed to call tool: %w", err)
	}
	if resp.Error != nil {
		return []ToolResult{}, fmt.Errorf("server returned error: %d - %s", resp.Error.Code, resp.Error.Message)
	}

	resultObject, ok := resp.Result.([]json.RawMessage)
	if !ok {
		return []ToolResult{}, fmt.Errorf("invalid response format: expected array of JSON objects")
	}

	c.slog.Debug("Tool call responses", "count", len(resultObject))

	result := make([]ToolResult, 0, len(resultObject))

	for _, obj := range resultObject {
		var response struct {
			JSONRPC string     `json:"jsonrpc"`
			ID      any        `json:"id"`
			Result  ToolResult `json:"result"`
			Error   *Error     `json:"error,omitempty"`
		}
		c.slog.Debug("Unmarshalling tool result", "obj", obj)
		if err := json.Unmarshal(obj, &response); err != nil {
			return []ToolResult{}, fmt.Errorf("failed to unmarshal tool result: %w", err)
		}

		result = append(result, response.Result)
	}

	c.slog.Debug("Tool call result", "result", result)

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
