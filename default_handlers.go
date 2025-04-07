package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// DefaultResourcesHandler provides a basic implementation of ResourcesHandler
type DefaultResourcesHandler struct {
	resources map[string]ResourceInfo
	contents  map[string]ResourceContent
	mu        sync.RWMutex
}

// NewDefaultResourcesHandler creates a new default resources handler
func NewDefaultResourcesHandler() *DefaultResourcesHandler {
	return &DefaultResourcesHandler{
		resources: make(map[string]ResourceInfo),
		contents:  make(map[string]ResourceContent),
	}
}

// AddResource adds a resource to the handler
func (h *DefaultResourcesHandler) AddResource(info ResourceInfo, content ResourceContent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resources[info.URI] = info
	h.contents[info.URI] = content
}

// RemoveResource removes a resource
func (h *DefaultResourcesHandler) RemoveResource(uri string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.resources, uri)
	delete(h.contents, uri)
}

// List returns the list of available resources
func (h *DefaultResourcesHandler) List(ctx context.Context, cursor string) ([]ResourceInfo, string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	// Extract resources
	var resources []ResourceInfo
	for _, info := range h.resources {
		resources = append(resources, info)
	}
	
	// Sort for deterministic order
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].URI < resources[j].URI
	})
	
	// Simple pagination - no actual cursor implementation in this example
	// A real implementation would parse the cursor and return the appropriate page
	if cursor != "" {
		// Find where to continue from
		start := 0
		for i, res := range resources {
			if res.URI == cursor {
				start = i + 1
				break
			}
		}
		
		if start >= len(resources) {
			return []ResourceInfo{}, "", nil
		}
		
		resources = resources[start:]
	}
	
	// Limit page size
	pageSize := 50
	nextCursor := ""
	if len(resources) > pageSize {
		nextCursor = resources[pageSize-1].URI
		resources = resources[:pageSize]
	}
	
	return resources, nextCursor, nil
}

// Read returns the content of a resource
func (h *DefaultResourcesHandler) Read(ctx context.Context, uri string) ([]ResourceContent, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	content, ok := h.contents[uri]
	if !ok {
		return nil, fmt.Errorf("resource not found: %s", uri)
	}
	
	return []ResourceContent{content}, nil
}

// DefaultPromptsHandler provides a basic implementation of PromptsHandler
type DefaultPromptsHandler struct {
	prompts   map[string]PromptInfo
	templates map[string]PromptResult
	mu        sync.RWMutex
}

// NewDefaultPromptsHandler creates a new default prompts handler
func NewDefaultPromptsHandler() *DefaultPromptsHandler {
	return &DefaultPromptsHandler{
		prompts:   make(map[string]PromptInfo),
		templates: make(map[string]PromptResult),
	}
}

// AddPrompt adds a prompt to the handler
func (h *DefaultPromptsHandler) AddPrompt(info PromptInfo, template PromptResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prompts[info.Name] = info
	h.templates[info.Name] = template
}

// RemovePrompt removes a prompt
func (h *DefaultPromptsHandler) RemovePrompt(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.prompts, name)
	delete(h.templates, name)
}

// List returns the list of available prompts
func (h *DefaultPromptsHandler) List(ctx context.Context, cursor string) ([]PromptInfo, string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	// Extract prompts
	var prompts []PromptInfo
	for _, info := range h.prompts {
		prompts = append(prompts, info)
	}
	
	// Sort for deterministic order
	sort.Slice(prompts, func(i, j int) bool {
		return prompts[i].Name < prompts[j].Name
	})
	
	// Simple pagination - a real implementation would parse the cursor
	if cursor != "" {
		// Find where to continue from
		start := 0
		for i, prompt := range prompts {
			if prompt.Name == cursor {
				start = i + 1
				break
			}
		}
		
		if start >= len(prompts) {
			return []PromptInfo{}, "", nil
		}
		
		prompts = prompts[start:]
	}
	
	// Limit page size
	pageSize := 50
	nextCursor := ""
	if len(prompts) > pageSize {
		nextCursor = prompts[pageSize-1].Name
		prompts = prompts[:pageSize]
	}
	
	return prompts, nextCursor, nil
}

// Get returns a prompt template with arguments applied
func (h *DefaultPromptsHandler) Get(ctx context.Context, name string, arguments map[string]any) (PromptResult, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	// Check if the prompt exists
	template, ok := h.templates[name]
	if !ok {
		return PromptResult{}, fmt.Errorf("prompt not found: %s", name)
	}
	
	// Check if the prompt info exists
	info, ok := h.prompts[name]
	if !ok {
		return PromptResult{}, errors.New("prompt metadata not found")
	}
	
	// Validate required arguments
	for _, arg := range info.Arguments {
		if arg.Required {
			if _, ok := arguments[arg.Name]; !ok {
				return PromptResult{}, fmt.Errorf("missing required argument: %s", arg.Name)
			}
		}
	}
	
	// In a real implementation, you would apply the arguments to the template
	// by substituting placeholders in the messages with argument values
	// For simplicity, we'll just return a copy of the template here
	result := PromptResult{
		Description: template.Description,
		Messages:    make([]PromptMessage, len(template.Messages)),
	}
	
	// Make a deep copy
	for i, msg := range template.Messages {
		result.Messages[i] = msg
	}
	
	return result, nil
}

// DefaultToolsHandler provides a basic implementation of ToolsHandler
type DefaultToolsHandler struct {
	tools    map[string]ToolInfo
	handlers map[string]func(ctx context.Context, arguments map[string]any) (ToolResult, error)
	mu       sync.RWMutex
}

// NewDefaultToolsHandler creates a new default tools handler
func NewDefaultToolsHandler() *DefaultToolsHandler {
	return &DefaultToolsHandler{
		tools:    make(map[string]ToolInfo),
		handlers: make(map[string]func(ctx context.Context, arguments map[string]any) (ToolResult, error)),
	}
}

// AddTool adds a tool to the handler
func (h *DefaultToolsHandler) AddTool(info ToolInfo, handler func(ctx context.Context, arguments map[string]any) (ToolResult, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tools[info.Name] = info
	h.handlers[info.Name] = handler
}

// RemoveTool removes a tool
func (h *DefaultToolsHandler) RemoveTool(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.tools, name)
	delete(h.handlers, name)
}

// List returns the list of available tools
func (h *DefaultToolsHandler) List(ctx context.Context, cursor string) ([]ToolInfo, string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	// Extract tools
	var tools []ToolInfo
	for _, info := range h.tools {
		tools = append(tools, info)
	}
	
	// Sort for deterministic order
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	
	// Simple pagination - a real implementation would parse the cursor
	if cursor != "" {
		// Find where to continue from
		start := 0
		for i, tool := range tools {
			if tool.Name == cursor {
				start = i + 1
				break
			}
		}
		
		if start >= len(tools) {
			return []ToolInfo{}, "", nil
		}
		
		tools = tools[start:]
	}
	
	// Limit page size
	pageSize := 50
	nextCursor := ""
	if len(tools) > pageSize {
		nextCursor = tools[pageSize-1].Name
		tools = tools[:pageSize]
	}
	
	return tools, nextCursor, nil
}

// Call invokes a tool with the given arguments
func (h *DefaultToolsHandler) Call(ctx context.Context, name string, arguments map[string]any) (ToolResult, error) {
	h.mu.RLock()
	handler, ok := h.handlers[name]
	h.mu.RUnlock()
	
	if !ok {
		return ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}
	
	return handler(ctx, arguments)
}
