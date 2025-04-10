// Server provides a server for the Model Context Protocol
package mcp

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"maps"
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
	sessions              map[string]*Session
	sessionExpirationTime time.Duration
	sessionCleanupTicker  *time.Ticker
	sessionCleanupDone    chan struct{}

	// Synchronization
	mu sync.RWMutex

	// logging
	slog *slog.Logger
}

// ServerConfig contains configuration for the server
type ServerConfig struct {
	ProtocolVersion       string
	ServerInfo            ServerInfo
	Capabilities          map[string]any
	SessionExpirationTime time.Duration // Default session expiration time
}

type SessionState int

const (
	SessionStateUnknown SessionState = iota
	SessionStateInitializing
	SessionStateInitialized
)

// Session represents a client session
type Session struct {
	ID           string
	State        SessionState
	CreatedAt    time.Time
	LastAccessAt time.Time
	ExpiresAt    time.Time
}

// NewServer creates a new MCP server
func NewServer(config ServerConfig) *Server {
	// Set default session expiration if not specified
	expirationTime := config.SessionExpirationTime
	if expirationTime == 0 {
		expirationTime = 30 * time.Minute // Default 30 minute session timeout
	}

	server := &Server{
		protocolVersion:       config.ProtocolVersion,
		serverInfo:            config.ServerInfo,
		capabilities:          make(map[string]any),
		sessions:              make(map[string]*Session),
		sessionExpirationTime: expirationTime,
		sessionCleanupDone:    make(chan struct{}),
		notificationManager:   NewNotificationManager(),
		loggingManager:        NewLoggingManager(),
		slog:                  NewNoopLogger(),
	}

	// Copy capabilities
	if config.Capabilities != nil {
		maps.Copy(server.capabilities, config.Capabilities)
	}

	// Start session cleanup goroutine
	server.startSessionCleanup()

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
	mux.HandleFunc("/", s.HandleHTTP)

	// Start session cleanup if not already running
	if s.sessionCleanupTicker == nil {
		s.startSessionCleanup()
	}

	return http.ListenAndServe(addr, mux)
}

// Stop gracefully stops the server and associated goroutines
func (s *Server) Stop() {
	// Stop the session cleanup goroutine if it's running
	s.stopSessionCleanup()
}

// startSessionCleanup starts a background goroutine that periodically cleans up expired sessions
func (s *Server) startSessionCleanup() {
	// Run cleanup every 5 minutes or 1/6 of the session timeout, whichever is shorter
	cleanupInterval := s.sessionExpirationTime / 6
	if cleanupInterval > 5*time.Minute {
		cleanupInterval = 5 * time.Minute
	}
	if cleanupInterval < 30*time.Second {
		cleanupInterval = 30 * time.Second // Minimum 30 seconds between cleanups
	}

	s.sessionCleanupTicker = time.NewTicker(cleanupInterval)
	s.sessionCleanupDone = make(chan struct{})

	go func() {
		for {
			select {
			case <-s.sessionCleanupTicker.C:
				s.cleanupExpiredSessions()
			case <-s.sessionCleanupDone:
				s.sessionCleanupTicker.Stop()
				return
			}
		}
	}()

	s.slog.Info("Session cleanup started", "interval", cleanupInterval)
}

// stopSessionCleanup stops the session cleanup goroutine
func (s *Server) stopSessionCleanup() {
	if s.sessionCleanupTicker != nil {
		close(s.sessionCleanupDone)
		s.sessionCleanupTicker = nil
		s.slog.Info("Session cleanup stopped")
	}
}

// cleanupExpiredSessions removes all expired sessions
func (s *Server) cleanupExpiredSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	count := 0

	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
			count++
		}
	}

	if count > 0 {
		s.slog.Info("Expired sessions cleaned up", "count", count)
	}
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

	// Process the request based on method
	switch r.Method {
	case http.MethodPost:
		s.handleJSONRPC(w, r, session)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getOrCreateSession retrieves an existing session or creates a new one
func (s *Server) getOrCreateSession(sessionID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// If session ID is provided, look it up
	if sessionID != "" {
		if session, ok := s.sessions[sessionID]; ok {
			// Check if session has expired
			if now.After(session.ExpiresAt) {
				s.slog.Info("Session expired", "sessionID", sessionID)
				delete(s.sessions, sessionID)
			} else {
				// Update session access time and expiration
				session.LastAccessAt = now
				session.ExpiresAt = now.Add(s.sessionExpirationTime)
				return session, nil
			}
		}
	}

	// Create a new session with a unique ID
	newID := generateSessionID()
	session := &Session{
		ID:           newID,
		State:        SessionStateUnknown,
		CreatedAt:    now,
		LastAccessAt: now,
		ExpiresAt:    now.Add(s.sessionExpirationTime),
	}
	s.sessions[newID] = session
	s.slog.Info("New session created", "sessionID", newID)

	return session, nil
}

// generateSessionID creates a cryptographically secure session ID
// using a UUID v4 according to RFC 4122
func generateSessionID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// Fall back to a timestamp-based ID if crypto rand fails
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	// Configure bits according to RFC 4122 for UUID v4
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant RFC 4122

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
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
