package mcp

import (
	"context"
)

// ResourceInfo contains information about a resource
type ResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

// ResourceContent contains the content of a resource
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // Base64 encoded binary data
}

// ResourcesHandler handles resource-related requests
type ResourcesHandler interface {
	// List returns available resources, with optional pagination
	List(ctx context.Context, cursor string) (resources []ResourceInfo, nextCursor string, err error)

	// Read returns the content of a resource
	Read(ctx context.Context, uri string) (contents []ResourceContent, err error)
}

// PromptArgument defines an argument for a prompt
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptInfo contains information about a prompt
type PromptInfo struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptMessage represents a message in a prompt template
type PromptMessage struct {
	Role    string        `json:"role"`
	Content PromptContent `json:"content"`
}

// PromptContent represents the content of a prompt message
type PromptContent struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	Data     string           `json:"data,omitempty"`
	MimeType string           `json:"mimeType,omitempty"`
	Resource *ResourceContent `json:"resource,omitempty"`
}

// PromptResult contains the result of a prompt request
type PromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// PromptsHandler handles prompt-related requests
type PromptsHandler interface {
	// List returns available prompts, with optional pagination
	List(ctx context.Context, cursor string) (prompts []PromptInfo, nextCursor string, err error)

	// Get returns a prompt template with arguments applied
	Get(ctx context.Context, name string, arguments map[string]any) (result PromptResult, err error)
}

// ToolsHandler handles tool-related requests
type ToolsHandler interface {
	// List returns available tools, with optional pagination
	List(ctx context.Context, cursor string) (tools []ToolInfo, nextCursor string, err error)

	// Call invokes a tool with the given arguments
	Call(ctx context.Context, name string, arguments map[string]any) (result ToolResult, err error)
}
