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
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// StreamableHTTP implements the MCP Streamable HTTP transport
type StreamableHTTP struct {
	baseURL     string
	httpClient  *http.Client
	sessionID   string
	lastEventID string
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

// SendRequestWithEvents sends a JSON-RPC request that might generate an SSE stream
// It returns both the immediate response (if any) and a channel for events
func (t *StreamableHTTP) SendRequestWithEvents(ctx context.Context, req *Request) (*Response, <-chan interface{}, <-chan error, error) {
	// Create channels for events and errors
	eventChan := make(chan interface{})
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
func (t *StreamableHTTP) ListenForMessages(ctx context.Context) (<-chan interface{}, <-chan error) {
	eventChan := make(chan interface{})
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
func (t *StreamableHTTP) processSSE(resp *http.Response, expectedID interface{}, eventChan chan<- interface{}, errChan chan<- error) {
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
func (t *StreamableHTTP) handleSSEEvent(event *SSEEvent, expectedID interface{}, eventChan chan<- interface{}) error {
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
			if err := t.processessage(msg, expectedID, eventChan); err != nil {
				return err
			}
		}
		return nil
	}

	// Single message
	return t.processessage(raw, expectedID, eventChan)
}

// processessage processes a single JSON-RPC message
func (t *StreamableHTTP) processessage(data json.RawMessage, expectedID interface{}, eventChan chan<- interface{}) error {
	// Try parsing as each type of message, starting with response
	var temp map[string]interface{}
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
	if s.err != nil {
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
