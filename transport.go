// Transport provides transport implementations for MCP
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Transport defines the interface for MCP transport implementations
type Transport interface {
	// SendRequest sends a JSON-RPC request and returns the response
	SendRequest(ctx context.Context, req *Request) (*Response, error)

	// SendRequestWithCallback sends a JSON-RPC request and calls the callback with the response
	SendRequestWithCallback(ctx context.Context, req *Request, callback func(*SSEEvent) error) (*Response, error)

	// SendRequestWithEvents sends a JSON-RPC request that might generate an event stream
	SendRequestWithEvents(ctx context.Context, req *Request) (*Response, <-chan any, <-chan error, error)

	// SendNotification sends a JSON-RPC notification
	SendNotification(ctx context.Context, notif *Notification) error

	// ListenForMessages starts listening for server-initiated messages
	ListenForMessages(ctx context.Context) (<-chan any, <-chan error)

	// GetSessionID returns the current session ID
	GetSessionID() string

	// SetSessionID sets the session ID for this transport
	SetSessionID(sessionID string)

	// TerminateSession explicitly terminates the session on the server
	TerminateSession(ctx context.Context) error
}

// StreamableHTTP implements the MCP Streamable HTTP transport
type StreamableHTTP struct {
	baseURL         string
	httpClient      *http.Client
	sessionID       string
	lastEventID     string
	messageEndpoint string  // For 2024-11-05 protocol
	protocolVersion string  // Detected protocol version
	mu              sync.Mutex
}

// STDIO implements the MCP stdio transport
type STDIO struct {
	cmd         *os.Process
	stdin       io.Writer
	stdout      *bufio.Reader
	stderr      io.ReadCloser // For reading logs output to stderr
	sessionID   string        // Not used in stdio but kept for interface compatibility
	nextLineID  atomic.Int64  // For providing unique event IDs
	requestMap  sync.Map      // Maps request IDs to response channels
	initialized atomic.Bool
	eventChan   chan any      // Channel for server-initiated events
	errChan     chan error    // Channel for transport errors
	stopChan    chan struct{} // Channel to signal reader to stop
	mu          sync.Mutex
}

// NewStreamableHTTP creates a new Streamable HTTP transport
func NewStreamableHTTP(baseURL string) *StreamableHTTP {
	return &StreamableHTTP{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// SetSessionID sets the session ID for this transport
func (t *StreamableHTTP) SetSessionID(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessionID = sessionID
}

// GetSessionID gets the current session ID
func (t *StreamableHTTP) GetSessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

// SendRequest sends a JSON-RPC request and returns the response
// For requests that might generate an SSE stream, use SendRequestWithEvents instead
func (t *StreamableHTTP) SendRequest(ctx context.Context, req *Request) (*Response, error) {
	// Serialize the request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Determine URL to use
	t.mu.Lock()
	url := t.baseURL
	// If using 2024-11-05 protocol and we have a specific message endpoint, use that
	if t.protocolVersion == "2024-11-05" && t.messageEndpoint != "" {
		url = t.messageEndpoint
	}
	t.mu.Unlock()

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Include session ID if available
	t.mu.Lock()
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	// Send the request
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for session ID in response
	if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
		t.mu.Lock()
		t.sessionID = sessionID
		t.mu.Unlock()
	}

	// Store protocol version from Initialize response
	if req.Method == "initialize" && resp.StatusCode == http.StatusAccepted {
		var jsonResp Response
		if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
			return nil, fmt.Errorf("failed to decode JSON response: %w", err)
		}
		
		// Extract protocol version from response
		if jsonResp.Result != nil {
			if resultMap, ok := jsonResp.Result.(map[string]interface{}); ok {
				if version, ok := resultMap["protocolVersion"].(string); ok {
					t.mu.Lock()
					t.protocolVersion = version
					t.mu.Unlock()
				}
			}
		}
		
		return &jsonResp, nil
	}

	// Check response status
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("authentication required")
	} else if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Handle different content types
	contentType := resp.Header.Get("Content-Type")

	if contentType == "application/json" {
		// Parse JSON response
		var jsonResp Response
		if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
			return nil, fmt.Errorf("failed to decode JSON response: %w", err)
		}
		return &jsonResp, nil
	} else if contentType == "text/event-stream" {
		// This shouldn't happen with SendRequest - use SendRequestWithEvents instead
		return nil, fmt.Errorf("received SSE stream but not expecting one, use SendRequestWithEvents instead")
	}

	return nil, fmt.Errorf("unexpected content type: %s", contentType)
}

func (t *StreamableHTTP) SendRequestWithCallback(ctx context.Context, req *Request, callback func(*SSEEvent) error) (*Response, error) {
	// Serialize the request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Include session ID if available
	t.mu.Lock()
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}

	// Include Last-Event-ID if available for resumability
	if t.lastEventID != "" {
		httpReq.Header.Set("Last-Event-ID", t.lastEventID)
	}
	t.mu.Unlock()

	slog.Debug("Sending request", "method", req.Method, "params", req.Params)

	// Send the request
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check for session ID in response
	if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
		if t.sessionID != sessionID {
			// Session ID changed, update it
			t.mu.Lock()
			t.sessionID = sessionID
			t.mu.Unlock()
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, errors.New("authentication required")
	}

	if resp.StatusCode == http.StatusNotFound && t.sessionID != "" {
		// Session likely expired
		resp.Body.Close()
		return nil, errors.New("session expired or not found (HTTP 404)")
	}

	if resp.StatusCode != http.StatusAccepted {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")

	if contentType == "application/json" {
		slog.Debug("Received JSON response", "status", resp.StatusCode)
		// Parse JSON response and close the body
		var jsonResp Response
		if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode JSON response: %w", err)
		}
		resp.Body.Close()
		return &jsonResp, nil
	} else if contentType == "text/event-stream" {
		slog.Debug("Received SSE stream", "status", resp.StatusCode)
		defer resp.Body.Close()
		jsonResp := Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  nil,
			Error:   nil,
		}

		scanner := NewSSEScanner(resp.Body)
		var event SSEEvent

		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				if event.Data != "" {
					// Parse the data as JSON-RPC message
					var raw json.RawMessage
					err := json.Unmarshal([]byte(event.Data), &raw)
					if err != nil {
						if jsonResp.Error == nil {
							jsonResp.Error = &Error{
								Code:    -32603,
								Message: "Invalid JSON-RPC message",
							}
						}

						slog.Error("Failed to parse event data as JSON", "error", err)
						return &jsonResp, fmt.Errorf("failed to parse event data as JSON: %w", err)
					}

					// Append data on to jsonResp.Result
					if jsonResp.Result == nil {
						jsonResp.Result = make([]json.RawMessage, 0)
					}
					jsonResp.Result = append(jsonResp.Result.([]json.RawMessage), raw)

					callback(&event)

					if event.ID != "" {
						t.mu.Lock()
						t.lastEventID = event.ID
						t.mu.Unlock()
					}

					event = SSEEvent{}
				}

				continue
			}

			// Parse the line
			if len(line) > 3 && line[0:2] == "id" && (line[2] == ':' || line[2] == ' ') {
				event.ID = trim(line[3:])
			} else if len(line) > 6 && line[0:5] == "event" && (line[5] == ':' || line[5] == ' ') {
				event.Event = trim(line[6:])
			} else if len(line) > 5 && line[0:4] == "data" && (line[4] == ':' || line[4] == ' ') {
				if event.Data != "" {
					event.Data += "\n"
				}
				event.Data += trim(line[5:])
			}
		}

		return &jsonResp, nil
	}

	return nil, fmt.Errorf("unexpected content type: %s", contentType)
}

// SendRequestWithEvents sends a JSON-RPC request that might generate an SSE stream
// It returns both the immediate response (if any) and a channel for events
func (t *StreamableHTTP) SendRequestWithEvents(ctx context.Context, req *Request) (*Response, <-chan any, <-chan error, error) {
	// Create channels for events and errors
	eventChan := make(chan any)
	errChan := make(chan error)

	// Serialize the request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(reqData))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Include session ID if available
	t.mu.Lock()
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}

	// Include Last-Event-ID if available for resumability
	if t.lastEventID != "" {
		httpReq.Header.Set("Last-Event-ID", t.lastEventID)
	}
	t.mu.Unlock()

	// Send the request
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check for session ID in response
	if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
		t.mu.Lock()
		t.sessionID = sessionID
		t.mu.Unlock()
	}

	// Check response status
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, nil, nil, errors.New("authentication required")
	} else if resp.StatusCode == http.StatusNotFound && t.sessionID != "" {
		// Session likely expired
		resp.Body.Close()
		return nil, nil, nil, errors.New("session expired or not found (HTTP 404)")
	} else if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, nil, nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Handle different content types
	contentType := resp.Header.Get("Content-Type")

	if contentType == "application/json" {
		// Parse JSON response and close the body
		var jsonResp Response
		if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
			resp.Body.Close()
			return nil, nil, nil, fmt.Errorf("failed to decode JSON response: %w", err)
		}
		resp.Body.Close()
		// Return the response but no events
		close(eventChan)
		close(errChan)
		return &jsonResp, eventChan, errChan, nil
	} else if contentType == "text/event-stream" {
		// Start processing the SSE stream in a goroutine
		go t.processSSE(resp, req.ID, eventChan, errChan)
		// Return nil for the response, the actual response(s) will come through the event channel
		return nil, eventChan, errChan, nil
	}

	resp.Body.Close()
	return nil, nil, nil, fmt.Errorf("unexpected content type: %s", contentType)
}

// SendNotification sends a JSON-RPC notification
func (t *StreamableHTTP) SendNotification(ctx context.Context, notif *Notification) error {
	// Serialize the notification
	notifData, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(notifData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Include session ID if available
	t.mu.Lock()
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	// Send the request
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// For notifications, server should return 202 Accepted
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code for notification: %d", resp.StatusCode)
	}

	return nil
}

// ListenForMessages opens a GET connection to listen for server-initiated messages
func (t *StreamableHTTP) ListenForMessages(ctx context.Context) (<-chan any, <-chan error) {
	eventChan := make(chan any)
	errChan := make(chan error)

	go func() {
		// Create HTTP request
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL, nil)
		if err != nil {
			errChan <- fmt.Errorf("failed to create HTTP request: %w", err)
			close(eventChan)
			close(errChan)
			return
		}

		// Set headers
		httpReq.Header.Set("Accept", "text/event-stream")

		// Include session ID if available
		t.mu.Lock()
		if t.sessionID != "" {
			httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
		}
		// Include Last-Event-ID if available for resumability
		if t.lastEventID != "" {
			httpReq.Header.Set("Last-Event-ID", t.lastEventID)
		}
		t.mu.Unlock()

		// Send the request
		resp, err := t.httpClient.Do(httpReq)
		if err != nil {
			errChan <- fmt.Errorf("HTTP request failed: %w", err)
			close(eventChan)
			close(errChan)
			return
		}

		// Check response status
		if resp.StatusCode == http.StatusMethodNotAllowed {
			// Server does not support GET endpoint, which is allowed by the spec
			resp.Body.Close()
			errChan <- errors.New("server does not support server-initiated messages")
			close(eventChan)
			close(errChan)
			return
		} else if resp.StatusCode == http.StatusNotFound && t.sessionID != "" {
			// Session likely expired
			resp.Body.Close()
			errChan <- errors.New("session expired or not found (HTTP 404)")
			close(eventChan)
			close(errChan)
			return
		} else if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			errChan <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			close(eventChan)
			close(errChan)
			return
		}

		// Check content type
		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/event-stream" {
			resp.Body.Close()
			errChan <- fmt.Errorf("unexpected content type: %s", contentType)
			close(eventChan)
			close(errChan)
			return
		}

		// Process the SSE stream
		t.processSSE(resp, nil, eventChan, errChan)
	}()

	return eventChan, errChan
}

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

// processSSE processes an SSE stream from an HTTP response
func (t *StreamableHTTP) processSSE(resp *http.Response, expectedID any, eventChan chan<- any, errChan chan<- error) {
	defer resp.Body.Close()
	defer close(eventChan)
	defer close(errChan)

	// Use a scanner to read line by line
	scanner := NewSSEScanner(resp.Body)
	var event SSEEvent

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line signals the end of an event
		if line == "" {
			if event.Data != "" {
				// Process the completed event
				if err := t.handleSSEEvent(&event, expectedID, eventChan); err != nil {
					errChan <- err
				}

				// Save the last event ID for resumability
				if event.ID != "" {
					t.mu.Lock()
					t.lastEventID = event.ID
					t.mu.Unlock()
				}

				// Reset for next event
				event = SSEEvent{}
			}
			continue
		}

		// Parse the line
		if len(line) > 3 && line[0:2] == "id" && (line[2] == ':' || line[2] == ' ') {
			event.ID = trim(line[3:])
		} else if len(line) > 6 && line[0:5] == "event" && (line[5] == ':' || line[5] == ' ') {
			event.Event = trim(line[6:])
		} else if len(line) > 5 && line[0:4] == "data" && (line[4] == ':' || line[4] == ' ') {
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += trim(line[5:])
		}
	}

	if err := scanner.Err(); err != nil {
		errChan <- fmt.Errorf("SSE stream error: %w", err)
	}
}

// handleSSEEvent processes an SSE event and sends the result to the event channel
func (t *StreamableHTTP) handleSSEEvent(event *SSEEvent, expectedID any, eventChan chan<- any) error {
	// Handle endpoint event for 2024-11-05 protocol
	if event.Event == "endpoint" && event.Data != "" {
		t.mu.Lock()
		t.messageEndpoint = event.Data
		t.protocolVersion = "2024-11-05" // Mark as old protocol version
		t.mu.Unlock()
		
		// Don't need to forward this to the event channel, it's for transport setup
		return nil
	}
	
	if event.Data == "" {
		return nil // Skip events with no data
	}

	// Parse the data as JSON-RPC message
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(event.Data), &raw); err != nil {
		return fmt.Errorf("failed to parse event data as JSON: %w", err)
	}

	// Check if it's a batch (array)
	if bytes.HasPrefix(raw, []byte("[")) {
		var batch []json.RawMessage
		if err := json.Unmarshal(raw, &batch); err != nil {
			return fmt.Errorf("failed to parse batch: %w", err)
		}

		// Process each message in the batch
		for _, msg := range batch {
			if err := t.processMessage(msg, expectedID, eventChan); err != nil {
				return err
			}
		}
		return nil
	}

	// Single message
	return t.processMessage(raw, expectedID, eventChan)
}

// processMessage processes a single JSON-RPC message
func (t *StreamableHTTP) processMessage(data json.RawMessage, expectedID any, eventChan chan<- any) error {
	// Try parsing as each type of message, starting with response
	var temp map[string]any
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("invalid JSON-RPC message: %w", err)
	}

	// Check for ID field to determine if it's a request or response
	if _, hasID := temp["id"]; hasID {
		// This is a request or response
		if _, hasMethod := temp["method"]; hasMethod {
			// This is a request
			var req Request
			if err := json.Unmarshal(data, &req); err != nil {
				return fmt.Errorf("invalid JSON-RPC request: %w", err)
			}
			eventChan <- &req
			return nil
		} else {
			// This is a response
			var resp Response
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("invalid JSON-RPC response: %w", err)
			}
			eventChan <- &resp
			return nil
		}
	} else if _, hasMethod := temp["method"]; hasMethod {
		// This is a notification
		var notif Notification
		if err := json.Unmarshal(data, &notif); err != nil {
			return fmt.Errorf("invalid JSON-RPC notification: %w", err)
		}
		eventChan <- &notif
		return nil
	}

	return fmt.Errorf("unknown JSON-RPC message type")
}

// SSEScanner is a scanner for Server-Sent Events
type SSEScanner struct {
	reader *bufio.Reader
	line   string
	err    error
}

// NewSSEScanner creates a new SSE scanner
func NewSSEScanner(r io.Reader) *SSEScanner {
	return &SSEScanner{
		reader: bufio.NewReader(r),
	}
}

// Scan advances the Scanner to the next line
func (s *SSEScanner) Scan() bool {
	s.line, s.err = s.reader.ReadString('\n')
	slog.Debug("SSEScanner line", "line", s.line)
	if s.err != nil {
		slog.Debug("SSEScanner error", "error", s.err)
		return false
	}
	// Trim the trailing newline character(s)
	s.line = strings.TrimSuffix(s.line, "\r\n")
	s.line = strings.TrimSuffix(s.line, "\n")
	return true
}

// Text returns the current line
func (s *SSEScanner) Text() string {
	return s.line
}

// Err returns the current error
func (s *SSEScanner) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// trim removes leading colon/space from SSE field values
func trim(s string) string {
	if len(s) > 0 && s[0] == ' ' {
		return s[1:]
	}
	return s
}

// NewSTDIO creates a new STDIO transport with the given input/output streams
func NewSTDIO(stdin io.Writer, stdout io.Reader, stderr io.ReadCloser, cmd *os.Process) *STDIO {
	transport := &STDIO{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    bufio.NewReader(stdout),
		stderr:    stderr,
		eventChan: make(chan any, 100),
		errChan:   make(chan error, 10),
		stopChan:  make(chan struct{}),
	}

	// Start the reader goroutine
	go transport.readLoop()

	return transport
}

// readLoop continuously reads from stdout and processes messages
func (t *STDIO) readLoop() {
	defer close(t.eventChan)
	defer close(t.errChan)

	// Also read from stderr for debugging
	go t.readStderr()

	slog.Debug("STDIO readLoop started")
	scanner := bufio.NewScanner(t.stdout)
	// Increase scanner buffer size to handle large messages
	const maxScannerSize = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, maxScannerSize)
	scanner.Buffer(buf, maxScannerSize)

	for {
		select {
		case <-t.stopChan:
			slog.Debug("STDIO readLoop stopping")
			return
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil && err != io.EOF {
					errMsg := fmt.Errorf("error reading from stdout: %w", err)
					slog.Error("STDIO read error", "error", err)
					t.errChan <- errMsg
				}
				slog.Debug("STDIO readLoop ending (EOF or error)")
				return
			}

			line := scanner.Text()
			if line == "" {
				continue
			}

			slog.Debug("STDIO received line", "line", line)

			// Parse the JSON-RPC message
			var raw json.RawMessage
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				errMsg := fmt.Errorf("error parsing JSON-RPC message: %w", err)
				slog.Error("STDIO JSON parse error", "error", err, "line", line)
				t.errChan <- errMsg
				continue
			}

			// Check if it's a batch
			if bytes.HasPrefix(raw, []byte("[")) {
				var batch []json.RawMessage
				if err := json.Unmarshal(raw, &batch); err != nil {
					errMsg := fmt.Errorf("error parsing JSON-RPC batch: %w", err)
					slog.Error("STDIO JSON batch parse error", "error", err)
					t.errChan <- errMsg
					continue
				}

				slog.Debug("STDIO processing batch", "size", len(batch))
				for _, msg := range batch {
					t.processMessage(msg)
				}
				continue
			}

			// Process single message
			t.processMessage(raw)
		}
	}
}

// readStderr reads from stderr for debugging purposes
func (t *STDIO) readStderr() {
	if t.stderr == nil {
		return
	}

	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			slog.Debug("STDIO stderr", "line", line)
		}
	}
}

// processMessage processes a single JSON-RPC message
func (t *STDIO) processMessage(data json.RawMessage) {
	// Try to determine the message type
	var temp map[string]any
	if err := json.Unmarshal(data, &temp); err != nil {
		errMsg := fmt.Errorf("invalid JSON-RPC message: %w", err)
		slog.Error("STDIO message parse error", "error", err)
		t.errChan <- errMsg
		return
	}

	// Log the message type
	slog.Debug("STDIO processing message", "has_id", temp["id"] != nil, "has_method", temp["method"] != nil)

	// Check if it's a request, response, or notification
	if _, hasID := temp["id"]; hasID {
		if _, hasMethod := temp["method"]; hasMethod {
			// This is a request
			var req Request
			if err := json.Unmarshal(data, &req); err != nil {
				errMsg := fmt.Errorf("invalid JSON-RPC request: %w", err)
				slog.Error("STDIO request parse error", "error", err)
				t.errChan <- errMsg
				return
			}
			slog.Debug("STDIO received request", "id", req.ID, "method", req.Method)
			t.eventChan <- &req
		} else {
			// This is a response
			var resp Response
			if err := json.Unmarshal(data, &resp); err != nil {
				errMsg := fmt.Errorf("invalid JSON-RPC response: %w", err)
				slog.Error("STDIO response parse error", "error", err)
				t.errChan <- errMsg
				return
			}

			slog.Debug("STDIO received response", "id", resp.ID, "has_error", resp.Error != nil)

			// Show all waiting requests for debugging
			var waitingIds []any
			t.requestMap.Range(func(key, value any) bool {
				waitingIds = append(waitingIds, key)
				return true
			})
			slog.Debug("STDIO waiting requests", "ids", waitingIds, "response_id", resp.ID, "response_id_type", fmt.Sprintf("%T", resp.ID))

			// Try to find a request ID that matches our response ID
			var found bool
			t.requestMap.Range(func(key, value any) bool {
				slog.Debug("STDIO comparing IDs", "request_id", key, "request_id_type", fmt.Sprintf("%T", key),
					"response_id", resp.ID, "response_id_type", fmt.Sprintf("%T", resp.ID))

				// Try to match the IDs, which might be of different types
				keyID, keyOk := key.(int64)
				respID, respOk := resp.ID.(float64)

				if keyOk && respOk && keyID == int64(respID) {
					// IDs match but with different types
					slog.Debug("STDIO found matching request with different type", "key", key, "resp_id", resp.ID)
					respChan := value.(chan *Response)
					respChan <- &resp
					close(respChan)
					t.requestMap.Delete(key)
					found = true
					return false // Stop iterating
				} else if key == resp.ID {
					// Direct match
					slog.Debug("STDIO found exact matching request", "id", resp.ID)
					respChan := value.(chan *Response)
					respChan <- &resp
					close(respChan)
					t.requestMap.Delete(key)
					found = true
					return false // Stop iterating
				}
				return true // Continue iterating
			})

			if !found {
				// No waiting request, send to event channel
				slog.Debug("STDIO no matching channel for response", "id", resp.ID)
				t.eventChan <- &resp
			}
		}
	} else if _, hasMethod := temp["method"]; hasMethod {
		// This is a notification
		var notif Notification
		if err := json.Unmarshal(data, &notif); err != nil {
			errMsg := fmt.Errorf("invalid JSON-RPC notification: %w", err)
			slog.Error("STDIO notification parse error", "error", err)
			t.errChan <- errMsg
			return
		}
		slog.Debug("STDIO received notification", "method", notif.Method)
		t.eventChan <- &notif
	} else {
		errMsg := errors.New("unknown JSON-RPC message type")
		slog.Error("STDIO unknown message type", "data", string(data))
		t.errChan <- errMsg
	}
}

// SetSessionID implements the Transport interface (no-op for STDIO)
func (t *STDIO) SetSessionID(sessionID string) {
	// No-op for STDIO transport
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessionID = sessionID
}

// GetSessionID implements the Transport interface (no-op for STDIO)
func (t *STDIO) GetSessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

// SendRequest sends a JSON-RPC request over STDIO and waits for the response
func (t *STDIO) SendRequest(ctx context.Context, req *Request) (*Response, error) {
	// Create a channel to receive the response
	respChan := make(chan *Response, 1)

	// Store our response channel in the request map
	t.requestMap.Store(req.ID, respChan)
	slog.Debug("STDIO registered response channel", "id", req.ID)

	// Create a goroutine to watch for responses in the event channel
	// This is a backup in case the response comes through the event channel instead
	// of being caught directly in processMessage
	go func() {
		// Exit if context done
		select {
		case <-ctx.Done():
			return
		default:
			// Continue
		}

		// Start a timer to check for the response
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Check if we can find our response in the event channel
				select {
				case evt := <-t.eventChan:
					// Check if it's a response with our ID
					if resp, ok := evt.(*Response); ok {
						if resp.ID == req.ID {
							slog.Debug("STDIO found response in event channel", "id", req.ID)
							// Forward the response to our channel
							select {
							case respChan <- resp:
								return
							default:
								// If the channel is already closed or full, just return
								return
							}
						} else {
							// Put it back in the event channel
							t.eventChan <- resp
						}
					} else {
						// Put other events back
						t.eventChan <- evt
					}
				default:
					// No event
				}
			}
		}
	}()

	// Serialize the request
	jsonData, err := json.Marshal(req)
	if err != nil {
		t.requestMap.Delete(req.ID)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log the request for debugging
	slog.Debug("STDIO sending request", "id", req.ID, "method", req.Method, "data", string(jsonData))

	// Send the request
	t.mu.Lock()
	_, err = fmt.Fprintf(t.stdin, "%s\n", jsonData)

	// Try to flush if possible
	if flusher, ok := t.stdin.(interface{ Flush() error }); ok {
		err = flusher.Flush()
		if err != nil {
			slog.Error("Failed to flush stdin", "error", err)
		}
	}

	t.mu.Unlock()

	if err != nil {
		t.requestMap.Delete(req.ID)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for the response or context cancellation
	select {
	case resp := <-respChan:
		slog.Debug("STDIO received response through channel", "id", resp.ID, "error", resp.Error)
		return resp, nil
	case <-ctx.Done():
		t.requestMap.Delete(req.ID)
		slog.Debug("STDIO request context cancelled", "id", req.ID, "error", ctx.Err())
		return nil, ctx.Err()
	}
}

// SendRequestWithCallback sends a JSON-RPC request and calls the callback with any events
func (t *STDIO) SendRequestWithCallback(ctx context.Context, req *Request, callback func(*SSEEvent) error) (*Response, error) {
	// Since STDIO doesn't support SSE naturally, we just send the request and return the response
	return t.SendRequest(ctx, req)
}

// SendRequestWithEvents sends a JSON-RPC request over STDIO
// For STDIO, this is the same as SendRequest since we don't have SSE streams
func (t *STDIO) SendRequestWithEvents(ctx context.Context, req *Request) (*Response, <-chan any, <-chan error, error) {
	resp, err := t.SendRequest(ctx, req)
	if err != nil {
		return nil, nil, nil, err
	}

	// Create empty channels to satisfy the interface
	eventChan := make(chan any)
	errChan := make(chan error)
	close(eventChan)
	close(errChan)

	return resp, eventChan, errChan, nil
}

// SendNotification sends a JSON-RPC notification over STDIO
func (t *STDIO) SendNotification(ctx context.Context, notif *Notification) error {
	// Serialize the notification
	jsonData, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Send the notification
	t.mu.Lock()
	_, err = fmt.Fprintf(t.stdin, "%s\n", jsonData)
	t.mu.Unlock()
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	return nil
}

// ListenForMessages returns channels for receiving messages from the server
// For STDIO, this just returns the existing event and error channels
func (t *STDIO) ListenForMessages(ctx context.Context) (<-chan any, <-chan error) {
	return t.eventChan, t.errChan
}

// TerminateSession explicitly terminates the session on the server
// Only applicable for StreamableHTTP transport
func (t *StreamableHTTP) TerminateSession(ctx context.Context) error {
	// Check if we have a session ID
	t.mu.Lock()
	sessionID := t.sessionID
	t.mu.Unlock()

	if sessionID == "" {
		return errors.New("no active session to terminate")
	}

	// Create DELETE request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set session ID header
	httpReq.Header.Set("Mcp-Session-Id", sessionID)

	// Send the request
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// According to the spec, the server may respond with 405 Method Not Allowed
	// if it doesn't allow clients to terminate sessions
	if resp.StatusCode == http.StatusMethodNotAllowed {
		return errors.New("server does not allow clients to terminate sessions")
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted &&
		resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code for session termination: %d", resp.StatusCode)
	}

	// Clear the session ID
	t.mu.Lock()
	t.sessionID = ""
	t.mu.Unlock()

	return nil
}

// TerminateSession is a no-op for STDIO transport
func (t *STDIO) TerminateSession(ctx context.Context) error {
	// No-op for STDIO transport
	return nil
}
