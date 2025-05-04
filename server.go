package mcp

import (
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Server represents an MCP server
type Server struct {
	// Protocol negotiation results
	protocolVersion string
	capabilities    map[string]any
	serverInfo      ServerInfo
	
	// Server configuration
	config ServerConfig

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

	// logging
	slog *slog.Logger
}

// ServerConfig contains configuration for the server
type ServerConfig struct {
	ProtocolVersion string
	ServerInfo      ServerInfo
	Capabilities    map[string]any
	
	// Endpoint configuration
	SSEEndpoint     string   // Default: ""  (use root path)
	MessageEndpoint string   // Default: ""  (use root path)
	ValidateOrigins bool     // Default: true
	AllowedOrigins  []string // Default: ["null", "localhost", "127.0.0.1"]
}

type SessionState int

const (
	SessionStateUnknown SessionState = iota
	SessionStateInitializing
	SessionStateInitialized
)

// Session represents a client session
type Session struct {
	ID    string
	State SessionState
}

// NewServer creates a new MCP server
func NewServer(config ServerConfig) *Server {
	// Set defaults for config if not provided
	if config.AllowedOrigins == nil {
		config.AllowedOrigins = []string{"null", "localhost", "127.0.0.1"}
	}
	if !config.ValidateOrigins {
		config.ValidateOrigins = true // Default is to validate origins
	}
	
	server := &Server{
		protocolVersion:     config.ProtocolVersion,
		serverInfo:          config.ServerInfo,
		capabilities:        make(map[string]any),
		sessions:            make(map[string]*Session),
		notificationManager: NewNotificationManager(),
		loggingManager:      NewLoggingManager(),
		slog:                NewNoopLogger(),
		config:              config,
	}

	// Copy capabilities
	if config.Capabilities != nil {
		maps.Copy(server.capabilities, config.Capabilities)
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

func (s *Server) SetLogger(logger *slog.Logger) {
	s.slog = logger
}

// Start starts the server on the given address
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	
	// If separate endpoints are specified, register them
	if s.config.SSEEndpoint != "" && s.config.MessageEndpoint != "" && 
	   s.config.SSEEndpoint != s.config.MessageEndpoint {
		// Register SSE endpoint
		mux.HandleFunc(s.config.SSEEndpoint, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			r.URL.Path = s.config.SSEEndpoint // Ensure path is set
			s.HandleHTTP(w, r)
		})
		
		// Register message endpoint
		mux.HandleFunc(s.config.MessageEndpoint, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			r.URL.Path = s.config.MessageEndpoint // Ensure path is set
			s.HandleHTTP(w, r)
		})
	} else if s.config.SSEEndpoint == s.config.MessageEndpoint && s.config.SSEEndpoint != "" {
		// Same endpoint for both
		mux.HandleFunc(s.config.SSEEndpoint, func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = s.config.SSEEndpoint // Ensure path is set
			s.HandleHTTP(w, r)
		})
	} else {
		// Default behavior - use root path
		mux.HandleFunc("/", s.HandleHTTP)
	}

	// By default, ensure we're binding to localhost for security
	if !strings.Contains(addr, ":") {
		addr = "127.0.0.1:" + addr
	} else if addr == ":" || strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	
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

	s.slog.Info("Handling request", "method", r.Method, "sessionID", session.ID)

	// Validate Origin header for security
	if s.config.ValidateOrigins {
		originHeader := r.Header.Get("Origin")
		if originHeader != "" {
			allowed := false
			for _, origin := range s.config.AllowedOrigins {
				if strings.Contains(originHeader, origin) {
					allowed = true
					break
				}
			}
			if !allowed {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}
		}
	}

	// Process the request based on method and URL path
	switch {
	case r.Method == http.MethodPost && (s.config.MessageEndpoint == "" || r.URL.Path == s.config.MessageEndpoint):
		s.handleJSONRPC(w, r, session)
	case r.Method == http.MethodGet && (s.config.SSEEndpoint == "" || r.URL.Path == s.config.SSEEndpoint):
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
func (s *Server) SendNotification(sessionID string, method string, params any) {
	s.notificationManager.SendNotification(sessionID, method, params)
}

// BroadcastNotification sends a notification to all sessions
func (s *Server) BroadcastNotification(method string, params any) {
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
