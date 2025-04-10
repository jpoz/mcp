package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// handleInitialize handles the initialize method
func (s *Server) handleInitialize(ctx context.Context, session *Session, params json.RawMessage) (any, error) {
	var initParams struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ClientInfo      ClientInfo     `json:"clientInfo"`
	}

	if err := json.Unmarshal(params, &initParams); err != nil {
		return nil, fmt.Errorf("invalid initialization parameters: %w", err)
	}

	// Check if the requested protocol version is supported
	// For now, we'll accept what the client requests
	negotiatedVersion := initParams.ProtocolVersion

	// In a production implementation, you might check against supported versions
	// and negotiate a compatible version

	// Prepare the result
	result := struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ServerInfo      ServerInfo     `json:"serverInfo"`
	}{
		ProtocolVersion: negotiatedVersion,
		Capabilities:    s.capabilities,
		ServerInfo:      s.serverInfo,
	}

	s.slog.Info("Client initialized", "clientInfo", initParams.ClientInfo, "protocolVersion", negotiatedVersion, "sessionID", session.ID)

	return result, nil
}

func (s *Server) handlePing(ctx context.Context, session *Session, params json.RawMessage) (any, error) {
	s.slog.Info("Ping received", "sessionID", session.ID)
	return struct{}{}, nil
}

// handleResourcesMethod routes resource-related method calls
func (s *Server) handleResourcesMethod(ctx context.Context, session *Session, req *Request) (interface{}, error) {
	s.mu.RLock()
	handler := s.resourcesHandler
	s.mu.RUnlock()

	if handler == nil {
		return nil, errors.New("resources not supported")
	}

	s.slog.Info("Handling resource method", "method", req.Method, "sessionID", session.ID)

	switch req.Method {
	case "resources/list":
		return s.handleResourcesList(ctx, session, req.Params)
	case "resources/read":
		return s.handleResourcesRead(ctx, session, req.Params)
	default:
		return nil, fmt.Errorf("method not found: %s", req.Method)
	}
}

// handleResourcesList handles the resources/list method
func (s *Server) handleResourcesList(ctx context.Context, session *Session, params json.RawMessage) (interface{}, error) {
	var listParams struct {
		Cursor string `json:"cursor,omitempty"`
	}

	if params != nil && len(params) > 0 {
		if err := json.Unmarshal(params, &listParams); err != nil {
			s.slog.Error("Failed to unmarshal parameters", "error", err, "sessionID", session.ID)
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
	}

	resources, nextCursor, err := s.resourcesHandler.List(ctx, listParams.Cursor)
	if err != nil {
		s.slog.Error("Failed to list resources", "error", err, "sessionID", session.ID)
		return nil, err
	}

	result := struct {
		Resources  []ResourceInfo `json:"resources"`
		NextCursor string         `json:"nextCursor,omitempty"`
	}{
		Resources:  resources,
		NextCursor: nextCursor,
	}

	s.slog.Info("Resources listed", "count", len(resources), "nextCursor", nextCursor, "sessionID", session.ID)

	return result, nil
}

// handleResourcesRead handles the resources/read method
func (s *Server) handleResourcesRead(ctx context.Context, session *Session, params json.RawMessage) (interface{}, error) {
	var readParams struct {
		URI string `json:"uri"`
	}

	if err := json.Unmarshal(params, &readParams); err != nil {
		s.slog.Error("Failed to unmarshal parameters", "error", err, "sessionID", session.ID)
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	contents, err := s.resourcesHandler.Read(ctx, readParams.URI)
	if err != nil {
		s.slog.Error("Failed to read resource", "error", err, "uri", readParams.URI, "sessionID", session.ID)
		return nil, err
	}

	result := struct {
		Contents []ResourceContent `json:"contents"`
	}{
		Contents: contents,
	}

	s.slog.Info("Resource read", "uri", readParams.URI, "sessionID", session.ID)

	return result, nil
}

// handleResourcesSubscribe handles resource subscription requests
func (s *Server) handleResourcesSubscribe(ctx context.Context, session *Session, params json.RawMessage) (interface{}, error) {
	// Check if resources support subscription
	s.mu.RLock()
	capabilities, ok := s.capabilities["resources"].(map[string]any)
	s.mu.RUnlock()

	if !ok || capabilities["subscribe"] != true {
		s.slog.Error("Resource subscriptions not supported", "sessionID", session.ID)
		return nil, errors.New("resource subscriptions not supported")
	}

	var subParams struct {
		URI string `json:"uri"`
	}

	if err := json.Unmarshal(params, &subParams); err != nil {
		s.slog.Error("Failed to unmarshal parameters", "error", err, "sessionID", session.ID)
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Register subscription
	// In a full implementation, you would track which sessions are subscribed to which resources
	// Return empty result for successful subscription

	s.slog.Info("Resource subscription registered", "uri", subParams.URI, "sessionID", session.ID)
	return struct{}{}, nil
}

// handlePromptsMethod routes prompt-related method calls
func (s *Server) handlePromptsMethod(ctx context.Context, session *Session, req *Request) (interface{}, error) {
	s.mu.RLock()
	handler := s.promptsHandler
	s.mu.RUnlock()

	if handler == nil {
		s.slog.Error("Prompts not supported", "sessionID", session.ID)
		return nil, errors.New("prompts not supported")
	}

	s.slog.Info("Handling prompt method", "method", req.Method, "sessionID", session.ID)

	switch req.Method {
	case "prompts/list":
		return s.handlePromptsList(ctx, session, req.Params)
	case "prompts/get":
		return s.handlePromptsGet(ctx, session, req.Params)
	default:
		return nil, fmt.Errorf("method not found: %s", req.Method)
	}
}

// handlePromptsList handles the prompts/list method
func (s *Server) handlePromptsList(ctx context.Context, session *Session, params json.RawMessage) (interface{}, error) {
	var listParams struct {
		Cursor string `json:"cursor,omitempty"`
	}

	if params != nil && len(params) > 0 {
		if err := json.Unmarshal(params, &listParams); err != nil {
			s.slog.Error("Failed to unmarshal parameters", "error", err, "sessionID", session.ID)
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
	}

	prompts, nextCursor, err := s.promptsHandler.List(ctx, listParams.Cursor)
	if err != nil {
		s.slog.Error("Failed to list prompts", "error", err, "sessionID", session.ID)
		return nil, err
	}

	result := struct {
		Prompts    []PromptInfo `json:"prompts"`
		NextCursor string       `json:"nextCursor,omitempty"`
	}{
		Prompts:    prompts,
		NextCursor: nextCursor,
	}

	s.slog.Info("Prompts listed", "count", len(prompts), "nextCursor", nextCursor, "sessionID", session.ID)

	return result, nil
}

// handlePromptsGet handles the prompts/get method
func (s *Server) handlePromptsGet(ctx context.Context, session *Session, params json.RawMessage) (interface{}, error) {
	var getParams struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments,omitempty"`
	}

	if err := json.Unmarshal(params, &getParams); err != nil {
		s.slog.Error("Failed to unmarshal parameters", "error", err, "sessionID", session.ID)
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	result, err := s.promptsHandler.Get(ctx, getParams.Name, getParams.Arguments)
	if err != nil {
		s.slog.Error("Failed to get prompt", "error", err, "name", getParams.Name, "sessionID", session.ID)
		return nil, err
	}

	return result, nil
}

// handleToolsMethod routes tool-related method calls
func (s *Server) handleToolsMethod(ctx context.Context, session *Session, req *Request) (interface{}, error) {
	s.mu.RLock()
	handler := s.toolsHandler
	s.mu.RUnlock()

	if handler == nil {
		return nil, errors.New("tools not supported")
	}

	s.slog.Info("Handling tool method", "method", req.Method, "sessionID", session.ID)

	switch req.Method {
	case "tools/list":
		return s.handleToolsList(ctx, session, req.Params)
	case "tools/call":
		return s.handleToolsCall(ctx, session, req.Params)
	default:
		return nil, fmt.Errorf("method not found: %s", req.Method)
	}
}

// handleToolsList handles the tools/list method
func (s *Server) handleToolsList(ctx context.Context, session *Session, params json.RawMessage) (interface{}, error) {
	var listParams struct {
		Cursor string `json:"cursor,omitempty"`
	}

	if params != nil && len(params) > 0 {
		if err := json.Unmarshal(params, &listParams); err != nil {
			s.slog.Error("Failed to unmarshal parameters", "error", err, "sessionID", session.ID)
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
	}

	tools, nextCursor, err := s.toolsHandler.List(ctx, listParams.Cursor)
	if err != nil {
		s.slog.Error("Failed to list tools", "error", err, "sessionID", session.ID)
		return nil, err
	}

	result := struct {
		Tools      []ToolInfo `json:"tools"`
		NextCursor string     `json:"nextCursor,omitempty"`
	}{
		Tools:      tools,
		NextCursor: nextCursor,
	}

	s.slog.Info("Tools listed", "count", len(tools), "nextCursor", nextCursor, "sessionID", session.ID)

	return result, nil
}

// handleToolsCall handles the tools/call method
func (s *Server) handleToolsCall(ctx context.Context, session *Session, params json.RawMessage) (any, error) {
	var callParams struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments,omitempty"`
	}

	if err := json.Unmarshal(params, &callParams); err != nil {
		s.slog.Error("Failed to unmarshal parameters", "error", err, "sessionID", session.ID)
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	result, err := s.toolsHandler.Call(ctx, callParams.Name, callParams.Arguments)
	if err != nil {
		s.slog.Error("Failed to call tool", "error", err, "name", callParams.Name, "sessionID", session.ID)
		return nil, err
	}

	s.slog.Info("Tool called", "name", callParams.Name, "sessionID", session.ID)

	return result, nil
}

// handleToolsCallWithEvents handles tool calls that may generate events
func (s *Server) handleToolsCallWithEvents(ctx context.Context, session *Session, req *Request, eventChan chan<- interface{}) (any, error) {
	var callParams CallParams

	if err := json.Unmarshal(req.Params, &callParams); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// In a real implementation with streaming tools, you would implement a ToolsStreamHandler interface
	// that can send events during tool execution

	// For now, we'll just call the normal handler
	s.mu.RLock()
	handler := s.toolsHandler
	s.mu.RUnlock()

	if handler == nil {
		return nil, errors.New("tools not supported")
	}

	result, err := handler.Call(ctx, callParams.Name, callParams.Arguments)
	if err != nil {
		return nil, err
	}

	return result, nil
}
