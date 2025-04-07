package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CompletionHandler handles completion requests
type CompletionHandler interface {
	// Complete returns completion suggestions for prompt arguments or resource URIs
	Complete(ctx context.Context, ref interface{}, argument *CompletionArgument) (CompletionResult, error)
}

// CompletionArgument represents an argument to be completed
type CompletionArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CompletionResult contains completion suggestions
type CompletionResult struct {
	Values  []string `json:"values"`
	Total   int      `json:"total,omitempty"`
	HasMore bool     `json:"hasMore,omitempty"`
}

// PromptReference identifies a prompt for completion
type PromptReference struct {
	Type string `json:"type"` // "ref/prompt"
	Name string `json:"name"` // Prompt name
}

// ResourceReference identifies a resource for completion
type ResourceReference struct {
	Type string `json:"type"` // "ref/resource"
	URI  string `json:"uri"`  // Resource URI
}

// DefaultCompletionHandler provides a basic implementation of CompletionHandler
type DefaultCompletionHandler struct {
	resourcesHandler ResourcesHandler
	promptsHandler   PromptsHandler
}

// NewDefaultCompletionHandler creates a new default completion handler
func NewDefaultCompletionHandler(resources ResourcesHandler, prompts PromptsHandler) *DefaultCompletionHandler {
	return &DefaultCompletionHandler{
		resourcesHandler: resources,
		promptsHandler:   prompts,
	}
}

// Complete provides completion suggestions
func (h *DefaultCompletionHandler) Complete(ctx context.Context, ref interface{}, argument *CompletionArgument) (CompletionResult, error) {
	// Parse reference to determine what we're completing
	refMap, ok := ref.(map[string]interface{})
	if !ok {
		return CompletionResult{}, fmt.Errorf("invalid reference format")
	}

	refType, ok := refMap["type"].(string)
	if !ok {
		return CompletionResult{}, fmt.Errorf("missing reference type")
	}

	switch refType {
	case "ref/prompt":
		return h.completePromptArgument(ctx, refMap, argument)
	case "ref/resource":
		return h.completeResourceURI(ctx, refMap, argument)
	default:
		return CompletionResult{}, fmt.Errorf("unsupported reference type: %s", refType)
	}
}

// completePromptArgument provides completion for prompt arguments
func (h *DefaultCompletionHandler) completePromptArgument(ctx context.Context, ref map[string]interface{}, argument *CompletionArgument) (CompletionResult, error) {
	if h.promptsHandler == nil {
		return CompletionResult{}, fmt.Errorf("prompt handler not available")
	}

	// Get prompt name from reference
	_, ok := ref["name"].(string)
	if !ok {
		return CompletionResult{}, fmt.Errorf("invalid prompt reference")
	}

	// In a real implementation, you would fetch prompt-specific completion values
	// For this example, we'll provide some basic values based on argument name

	prefix := strings.ToLower(argument.Value)
	var suggestions []string

	// Example completion logic - would be customized in real implementation
	switch strings.ToLower(argument.Name) {
	case "language":
		allLanguages := []string{
			"python", "javascript", "typescript", "java", "go", "rust",
			"c", "c++", "c#", "ruby", "php", "swift", "kotlin",
		}
		for _, lang := range allLanguages {
			if strings.HasPrefix(lang, prefix) {
				suggestions = append(suggestions, lang)
			}
		}
	case "format":
		formats := []string{"json", "xml", "yaml", "text", "markdown", "html", "csv"}
		for _, format := range formats {
			if strings.HasPrefix(format, prefix) {
				suggestions = append(suggestions, format)
			}
		}
	}

	// Limit number of suggestions
	total := len(suggestions)
	hasMore := false
	if len(suggestions) > 100 {
		suggestions = suggestions[:100]
		hasMore = true
	}

	return CompletionResult{
		Values:  suggestions,
		Total:   total,
		HasMore: hasMore,
	}, nil
}

// completeResourceURI provides completion for resource URIs
func (h *DefaultCompletionHandler) completeResourceURI(ctx context.Context, ref map[string]interface{}, argument *CompletionArgument) (CompletionResult, error) {
	if h.resourcesHandler == nil {
		return CompletionResult{}, fmt.Errorf("resource handler not available")
	}

	// Get URI template from reference
	uriTemplate, ok := ref["uri"].(string)
	if !ok {
		return CompletionResult{}, fmt.Errorf("invalid resource reference")
	}

	// Get resource listing
	resources, _, err := h.resourcesHandler.List(ctx, "")
	if err != nil {
		return CompletionResult{}, err
	}

	// In a real implementation, you would match the URI template against resources
	// and extract the relevant parts for completion

	// For this example, we'll do simple prefix matching on resource names
	prefix := strings.ToLower(argument.Value)
	var suggestions []string

	// Example: completing a {path} parameter in file:///{path}
	if strings.Contains(uriTemplate, "{path}") {
		for _, res := range resources {
			// Extract path from resource URI
			path := ""
			if strings.HasPrefix(res.URI, "file:///") {
				path = res.URI[8:]
			} else {
				continue
			}

			if strings.HasPrefix(strings.ToLower(path), prefix) {
				suggestions = append(suggestions, path)
			}
		}
	}

	// Limit number of suggestions
	total := len(suggestions)
	hasMore := false
	if len(suggestions) > 100 {
		suggestions = suggestions[:100]
		hasMore = true
	}

	return CompletionResult{
		Values:  suggestions,
		Total:   total,
		HasMore: hasMore,
	}, nil
}

// handleCompletionRequest processes completion requests
func (s *Server) handleCompletionRequest(ctx context.Context, session *Session, req *Request) (interface{}, error) {
	var params struct {
		Ref      json.RawMessage    `json:"ref"`
		Argument CompletionArgument `json:"argument"`
	}

	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Parse ref as a generic map since we don't know the type yet
	var ref interface{}
	if err := json.Unmarshal(params.Ref, &ref); err != nil {
		return nil, fmt.Errorf("invalid reference format: %w", err)
	}

	// Check if completion is supported
	s.mu.RLock()
	_, ok := s.capabilities["completions"]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("completions not supported")
	}

	// In a real implementation, you would have a proper completion handler
	// For this example, we'll create a default one on the fly
	handler := NewDefaultCompletionHandler(s.resourcesHandler, s.promptsHandler)

	result, err := handler.Complete(ctx, ref, &params.Argument)
	if err != nil {
		return nil, err
	}

	return struct {
		Completion CompletionResult `json:"completion"`
	}{
		Completion: result,
	}, nil
}
