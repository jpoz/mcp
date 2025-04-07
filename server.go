// Server provides a server for the Model Context Protocol
package mcp

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Server represents an MCP server
type Server struct {
	// Protocol negotiation results
	protocolVersion string
	capabilities    map[string]any
	serverInfo      ServerInfo

	// Handlers
	resourcesHandler ResourcesHandler
	promptsHandler   PromptsHandler
	toolsHandler     ToolsHandler

	// Notification management
	notificationManager *NotificationManager

	// Logging management
	loggingManager *LoggingManager

	// Session management
	sessions map[string]*Session

	// Synchronization
	mu sync.RWMutex
}

// ServerConfig contains configuration for the server
type ServerConfig struct {
	ProtocolVersion string
	ServerInfo      ServerInfo
	Capabilities    map[string]any
}

// Session represents a client session
type Session struct {
	ID string
	// Additional session state could be added here
}

// NewServer creates a new MCP server
func NewServer(config ServerConfig) *Server {
	server := &Server{
		protocolVersion:     config.ProtocolVersion,
		serverInfo:          config.ServerInfo,
		capabilities:        make(map[string]any),
		sessions:            make(map[string]*Session),
		notificationManager: NewNotificationManager(),
		loggingManager:      NewLoggingManager(),
	}

	// Copy capabilities
	if config.Capabilities != nil {
		for k, v := range config.Capabilities {
			server.capabilities[k] = v
		}
	}

	return server
}

// SetResourcesHandler sets the handler for resource-related requests
func (s *Server) SetResourcesHandler(handler ResourcesHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resourcesHandler = handler

	// Update capabilities
	if handler != nil {
		if s.capabilities == nil {
			s.capabilities = make(map[string]any)
		}
		// By default, add a basic capabilities map if none exists yet
		if _, exists := s.capabilities["resources"]; !exists {
			s.capabilities["resources"] = map[string]any{}
		}
	}
}

// SetPromptsHandler sets the handler for prompt-related requests
func (s *Server) SetPromptsHandler(handler PromptsHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptsHandler = handler

	// Update capabilities
	if handler != nil {
		if s.capabilities == nil {
			s.capabilities = make(map[string]any)
		}
		// By default, add a basic capabilities map if none exists yet
		if _, exists := s.capabilities["prompts"]; !exists {
			s.capabilities["prompts"] = map[string]any{}
		}
	}
}

// SetToolsHandler sets the handler for tool-related requests
func (s *Server) SetToolsHandler(handler ToolsHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolsHandler = handler

	// Update capabilities
	if handler != nil {
		if s.capabilities == nil {
			s.capabilities = make(map[string]any)
		}
		// By default, add a basic capabilities map if none exists yet
		if _, exists := s.capabilities["tools"]; !exists {
			s.capabilities["tools"] = map[string]any{}
		}
	}
}

// Start starts the server on the given address
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.HandleHTTP)

	return http.ListenAndServe(addr, mux)
}

// HandleHTTP processes an HTTP request according to the MCP protocol
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	// Check for session ID
	sessionID := r.Header.Get("Mcp-Session-Id")

	// Create or retrieve session
	session, err := s.getOrCreateSession(sessionID)
	if err != nil {
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	// Set session ID in response header
	w.Header().Set("Mcp-Session-Id", session.ID)

	// Process the request based on method
	switch r.Method {
	case http.MethodPost:
		s.handleJSONRPC(w, r, session)
	case http.MethodGet:
		s.handleSSE(w, r, session)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getOrCreateSession retrieves an existing session or creates a new one
func (s *Server) getOrCreateSession(sessionID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If session ID is provided, look it up
	if sessionID != "" {
		if session, ok := s.sessions[sessionID]; ok {
			return session, nil
		}
	}

	// Create a new session with a unique ID
	newID := generateSessionID()
	session := &Session{ID: newID}
	s.sessions[newID] = session

	return session, nil
}

// generateSessionID creates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("session-%d", time.Now().UnixNano())
}

// SendNotification sends a notification to a specific session
func (s *Server) SendNotification(sessionID string, method string, params interface{}) {
	s.notificationManager.SendNotification(sessionID, method, params)
}

// BroadcastNotification sends a notification to all sessions
func (s *Server) BroadcastNotification(method string, params interface{}) {
	s.notificationManager.BroadcastNotification(method, params)
}

// NotifyResourcesListChanged notifies clients that the resources list has changed
func (s *Server) NotifyResourcesListChanged() {
	s.BroadcastNotification("notifications/resources/list_changed", nil)
}

// NotifyPromptsListChanged notifies clients that the prompts list has changed
func (s *Server) NotifyPromptsListChanged() {
	s.BroadcastNotification("notifications/prompts/list_changed", nil)
}

// NotifyToolsListChanged notifies clients that the tools list has changed
func (s *Server) NotifyToolsListChanged() {
	s.BroadcastNotification("notifications/tools/list_changed", nil)
}

// NotifyResourceUpdated notifies clients that a resource has been updated
func (s *Server) NotifyResourceUpdated(uri string) {
	s.BroadcastNotification("notifications/resources/updated", map[string]string{
		"uri": uri,
	})
}
