// Package jsonrpc provides JSON-RPC 2.0 message types for MCP
package mcp

import "encoding/json"

// Request represents a JSON-RPC 2.0 request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// TypedResponse is a generic response type for strongly typed results
type TypedResponse[T any] struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  T      `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Notification represents a JSON-RPC 2.0 notification
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Error represents a JSON-RPC 2.0 error
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface
func (e *Error) Error() string {
	return e.Message
}

type CallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// NewRequest creates a new JSON-RPC 2.0 request
func NewRequest(id any, method string, params json.RawMessage) *Request {
	return &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
}

// NewResponse creates a new JSON-RPC 2.0 response with typed result
func NewResponse[T any](id any, result T) *TypedResponse[T] {
	return &TypedResponse[T]{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// NewErrorResponse creates a new JSON-RPC 2.0 error response
func NewErrorResponse[T any](id any, err *Error) *TypedResponse[T] {
	return &TypedResponse[T]{
		JSONRPC: "2.0",
		ID:      id,
		Error:   err,
	}
}

// NewNotification creates a new JSON-RPC 2.0 notification
func NewNotification(method string, params any) *Notification {
	return &Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
}

// ToolInfo contains information about a tool
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolContent represents the content returned by a tool
type ToolContent struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	Data     string           `json:"data,omitempty"`
	MimeType string           `json:"mimeType,omitempty"`
	Resource *ResourceContent `json:"resource,omitempty"`
}

// ToolResult contains the result of a tool call
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolResponse is a typed response specifically for tool calls
type ToolResponse struct {
	JSONRPC string     `json:"jsonrpc"`
	ID      any        `json:"id"`
	Result  ToolResult `json:"result"`
	Error   *Error     `json:"error,omitempty"`
}

// ProgressToken is a token for tracking progress
type ProgressToken string

// InitializeResult contains the result of an initialize request
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

// InitializeResponse is a typed response for initialization
type InitializeResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      any              `json:"id"`
	Result  InitializeResult `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}
