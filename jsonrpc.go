package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// JSON-RPC error codes
const (
	ErrorCodeParse            = -32700
	ErrorCodeInvalidRequest   = -32600
	ErrorCodeMethodNotFound   = -32601
	ErrorCodeInvalidParams    = -32602
	ErrorCodeInternalError    = -32603
	ErrorCodeResourceNotFound = -32002
)

// JSONRPCResponse represents a JSON-RPC response
type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// handleJSONRPC processes a JSON-RPC request
func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request, session *Session) {
	// Check content type
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		s.slog.Error("Unsupported content type", "contentType", contentType, "sessionID", session.ID)
		http.Error(w, "Unsupported content type", http.StatusUnsupportedMediaType)
		return
	}

	// Check if client expects SSE
	acceptHeader := r.Header.Get("Accept")
	expectsSSE := false
	if acceptHeader != "" {
		// Use a range loop over the split result for more efficiency
		for typ := range strings.SplitSeq(acceptHeader, ",") {
			if strings.TrimSpace(strings.Split(typ, ";")[0]) == "text/event-stream" {
				expectsSSE = true
				break
			}
		}
	}

	// Decode the request
	var rawRequest json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawRequest); err != nil {
		s.sendErrorResponse(w, nil, ErrorCodeParse, "Failed to parse request", nil)
		return
	}

	// Check if it's a batch request
	var isBatch bool
	if bytes.HasPrefix(rawRequest, []byte("[")) {
		isBatch = true
	}

	if isBatch {
		// Batch requests are not fully implemented yet
		s.sendErrorResponse(w, nil, ErrorCodeInvalidRequest, "Batch requests are not supported", nil)
		return
	} else {
		// Single request
		var req Request
		if err := json.Unmarshal(rawRequest, &req); err != nil {
			s.sendErrorResponse(w, nil, ErrorCodeInvalidRequest, "Invalid JSON-RPC request", nil)
			return
		}

		// Process the request
		if expectsSSE && isStreamingMethod(req.Method) {
			s.handleRequestWithEvents(w, r, session, &req)
		} else {
			s.handleSingleRequest(w, session, &req)
		}
	}
}

// isStreamingMethod determines if a method may generate an event stream
func isStreamingMethod(method string) bool {
	// These methods may generate event streams
	switch method {
	case "tools/call":
		return true
	default:
		return false
	}
}

// handleSingleRequest processes a single JSON-RPC request
func (s *Server) handleSingleRequest(w http.ResponseWriter, session *Session, req *Request) {
	ctx := context.Background()

	// Process request based on method
	var result any
	var err error

	switch {
	// Handle standard methods
	case req.Method == "initialize":
		result, err = s.handleInitialize(ctx, session, req.Params)

	case req.Method == "notifications/initialized":
		result, err = s.handleInitializeNotification(ctx, session, req.Params)

	case req.Method == "ping":
		result, err = s.handlePing(ctx, session, req.Params)

	// Handle resource methods
	case strings.HasPrefix(req.Method, "resources/"):
		result, err = s.handleResourcesMethod(ctx, session, req)

	// Handle prompt methods
	case strings.HasPrefix(req.Method, "prompts/"):
		result, err = s.handlePromptsMethod(ctx, session, req)

	// Handle tool methods
	case strings.HasPrefix(req.Method, "tools/"):
		result, err = s.handleToolsMethod(ctx, session, req)

	// Handle subscription methods
	case req.Method == "resources/subscribe":
		result, err = s.handleResourcesSubscribe(ctx, session, req.Params)

	// Unknown method
	default:
		err = fmt.Errorf("json-rpc method not found: %s", req.Method)
	}

	// Send response
	if err != nil {
		message := err.Error()
		code := ErrorCodeInternalError

		// Check for specific error types
		if strings.Contains(message, "not found") {
			code = ErrorCodeResourceNotFound
		} else if strings.Contains(message, "invalid parameters") {
			code = ErrorCodeInvalidParams
		} else if strings.Contains(message, "method not found") {
			code = ErrorCodeMethodNotFound
		}

		s.sendErrorResponse(w, req.ID, code, message, nil)
	} else {
		s.sendSuccessResponse(w, req.ID, result)
	}
}

// handleRequestWithEvents handles a request that may generate an SSE stream
func (s *Server) handleRequestWithEvents(w http.ResponseWriter, r *http.Request, session *Session, req *Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	s.slog.Info("Handling SSE request", "method", req.Method, "sessionID", session.ID)

	// Create channels for events
	eventChan := make(chan any)
	errChan := make(chan error)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Process the request in a goroutine
	go func() {
		defer close(eventChan)
		defer close(errChan)

		var result any
		var err error

		switch {
		// Handle tool calls that might generate events
		case req.Method == "tools/call":
			result, err = s.handleToolsCallWithEvents(ctx, session, req, eventChan)
		default:
			// For other methods, just get the result
			switch {
			case strings.HasPrefix(req.Method, "resources/"):
				result, err = s.handleResourcesMethod(ctx, session, req)
			case strings.HasPrefix(req.Method, "prompts/"):
				result, err = s.handlePromptsMethod(ctx, session, req)
			case strings.HasPrefix(req.Method, "tools/"):
				result, err = s.handleToolsMethod(ctx, session, req)
			default:
				err = fmt.Errorf("method not found: %s", req.Method)
			}
		}

		// Send result or error
		if err != nil {
			errChan <- err
		} else if result != nil {
			// Create a JSON-RPC response
			resp := &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  result,
			}
			eventChan <- resp
		}
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Process events and errors
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				// Channel closed
				return
			}

			// Serialize and send event
			data, err := json.Marshal(event)
			if err != nil {
				// Log error
				continue
			}

			s.slog.Debug("Sending event", "data", string(data))

			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case err, ok := <-errChan:
			if !ok {
				// Channel closed
				return
			}

			// Create error response
			code := ErrorCodeInternalError
			message := err.Error()

			errResp := &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &Error{
					Code:    code,
					Message: message,
				},
			}

			data, _ := json.Marshal(errResp)

			s.slog.Error("Sending error", "data", string(data))

			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		case <-ctx.Done():
			// Client disconnected
			return
		}
	}
}

// sendSuccessResponse sends a successful JSON-RPC response
func (s *Server) sendSuccessResponse(w http.ResponseWriter, id any, result any) {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	s.slog.Debug("Sending success response", "id", id, "result", result)

	// First marshal the JSON to check for encoding errors
	jsonData, err := json.Marshal(resp)
	if err != nil {
		s.slog.Error("Error marshalling JSON response", "error", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}

	// Now set headers and status code
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	_, err = w.Write(jsonData)
	if err != nil {
		s.slog.Error("[CRITICAL] Error writing response", "error", err)
	}
}

// sendErrorResponse sends a JSON-RPC error response
func (s *Server) sendErrorResponse(w http.ResponseWriter, id any, code int, message string, data any) {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}

	s.slog.Debug("Sending error response", "id", id, "code", code, "message", message)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
	}
}
