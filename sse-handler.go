package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// NotificationManager manages subscriptions to notifications
type NotificationManager struct {
	subscribers map[string]map[string]bool    // sessionID -> eventTypes
	eventChan   map[string]chan *Notification // sessionID -> channel
	mu          sync.RWMutex
}

// NewNotificationManager creates a new notification manager
func NewNotificationManager() *NotificationManager {
	return &NotificationManager{
		subscribers: make(map[string]map[string]bool),
		eventChan:   make(map[string]chan *Notification),
	}
}

// Subscribe adds a subscription for a session
func (m *NotificationManager) Subscribe(sessionID string, eventTypes ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.subscribers[sessionID]; !exists {
		m.subscribers[sessionID] = make(map[string]bool)
		m.eventChan[sessionID] = make(chan *Notification, 100) // Buffered channel
	}

	// If no specific event types, subscribe to all
	if len(eventTypes) == 0 {
		// All events (empty means all)
		m.subscribers[sessionID][""] = true
		return
	}

	// Subscribe to specific event types
	for _, eventType := range eventTypes {
		m.subscribers[sessionID][eventType] = true
	}
}

// Unsubscribe removes a subscription
func (m *NotificationManager) Unsubscribe(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ch, ok := m.eventChan[sessionID]; ok {
		close(ch)
	}

	delete(m.subscribers, sessionID)
	delete(m.eventChan, sessionID)
}

// SendNotification sends a notification to a specific session
func (m *NotificationManager) SendNotification(sessionID string, method string, params interface{}) {
	notification := &Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if the session is subscribed
	if subs, ok := m.subscribers[sessionID]; ok {
		if ch, ok := m.eventChan[sessionID]; ok {
			// Check if subscribed to this event type
			if subs[""] || subs[method] {
				// Non-blocking send
				select {
				case ch <- notification:
					// Sent successfully
				default:
					// Channel full, could log this in a real implementation
				}
			}
		}
	}
}

// BroadcastNotification sends a notification to all subscribed sessions
func (m *NotificationManager) BroadcastNotification(method string, params interface{}) {
	notification := &Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for sessionID, subs := range m.subscribers {
		// Check if subscribed to this event type
		if subs[""] || subs[method] {
			if ch, ok := m.eventChan[sessionID]; ok {
				// Non-blocking send
				select {
				case ch <- notification:
					// Sent successfully
				default:
					// Channel full, could log this in a real implementation
				}
			}
		}
	}
}

// GetEventChannel returns the event channel for a session
func (m *NotificationManager) GetEventChannel(sessionID string) <-chan *Notification {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if ch, ok := m.eventChan[sessionID]; ok {
		return ch
	}

	// Session not found, create a new subscription
	m.mu.RUnlock()
	m.Subscribe(sessionID)
	m.mu.RLock()

	return m.eventChan[sessionID]
}

// handleSSE handles GET requests for SSE events
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request, session *Session) {
	// Check if client accepts SSE
	acceptHeader := r.Header.Get("Accept")
	acceptsSSE := false
	for _, mediaType := range strings.Split(acceptHeader, ",") {
		if strings.TrimSpace(strings.Split(mediaType, ";")[0]) == "text/event-stream" {
			acceptsSSE = true
			break
		}
	}

	if !acceptsSSE {
		http.Error(w, "Client must accept text/event-stream", http.StatusNotAcceptable)
		return
	}

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Check for Last-Event-ID for resumability
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID != "" {
		// In a real implementation, you would handle resuming from this ID
	}

	// Get a flusher for SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set up notification channel
	eventChan := s.notificationManager.GetEventChannel(session.ID)

	// Check if we need to support 2024-11-05 protocol version
	isOldProtocol := s.protocolVersion == "2024-11-05"

	// Send initial comment to establish connection
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// For 2024-11-05 protocol, send an endpoint event with the message endpoint
	if isOldProtocol {
		// Determine the scheme
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		
		// Determine the message endpoint
		messageEndpoint := r.URL.Path // Same endpoint by default
		if s.config.MessageEndpoint != "" {
			messageEndpoint = s.config.MessageEndpoint
		}
		
		// Create full URL for the endpoint
		endpointURL := fmt.Sprintf("%s://%s%s", scheme, r.Host, messageEndpoint)
		
		// Send the endpoint event as per 2024-11-05 spec
		fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
		flusher.Flush()
	}

	// Create a timer for keepalive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Create a context that's canceled when the client disconnects
	ctx := r.Context()

	// Process events until client disconnects
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			s.notificationManager.Unsubscribe(session.ID)
			return

		case notification, ok := <-eventChan:
			if !ok {
				// Channel closed
				return
			}

			// Serialize the notification
			data, err := json.Marshal(notification)
			if err != nil {
				// Log error
				continue
			}

			// Send as SSE event
			if isOldProtocol {
				// For 2024-11-05, always use event: message
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			} else {
				// For newer protocols, just send the data
				fmt.Fprintf(w, "data: %s\n\n", data)
			}
			flusher.Flush()

		case <-ticker.C:
			// Send keepalive comment
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
