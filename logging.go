package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// LogLevel represents the severity level of a log message
type LogLevel string

// Log levels from RFC 5424
const (
	LogLevelDebug     LogLevel = "debug"
	LogLevelInfo      LogLevel = "info"
	LogLevelNotice    LogLevel = "notice"
	LogLevelWarning   LogLevel = "warning"
	LogLevelError     LogLevel = "error"
	LogLevelCritical  LogLevel = "critical"
	LogLevelAlert     LogLevel = "alert"
	LogLevelEmergency LogLevel = "emergency"
)

// LoggingManager handles log messages and level filtering
type LoggingManager struct {
	// Minimum log level by session
	minLevel map[string]LogLevel

	// Default minimum level
	defaultLevel LogLevel

	// Synchronization
	mu sync.RWMutex
}

// NewLoggingManager creates a new logging manager
func NewLoggingManager() *LoggingManager {
	return &LoggingManager{
		minLevel:     make(map[string]LogLevel),
		defaultLevel: LogLevelInfo, // Default to INFO level
	}
}

// SetLevel sets the minimum log level for a session
func (m *LoggingManager) SetLevel(sessionID string, level LogLevel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.minLevel[sessionID] = level
}

// GetLevel returns the minimum log level for a session
func (m *LoggingManager) GetLevel(sessionID string) LogLevel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	level, ok := m.minLevel[sessionID]
	if !ok {
		return m.defaultLevel
	}
	return level
}

// SetDefaultLevel sets the default minimum log level
func (m *LoggingManager) SetDefaultLevel(level LogLevel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultLevel = level
}

// ShouldLog determines if a message with the given level should be logged
func (m *LoggingManager) ShouldLog(sessionID string, level LogLevel) bool {
	minLevel := m.GetLevel(sessionID)
	return logLevelSeverity(level) >= logLevelSeverity(minLevel)
}

// logLevelSeverity returns the numeric severity of a log level
func logLevelSeverity(level LogLevel) int {
	switch level {
	case LogLevelDebug:
		return 0
	case LogLevelInfo:
		return 1
	case LogLevelNotice:
		return 2
	case LogLevelWarning:
		return 3
	case LogLevelError:
		return 4
	case LogLevelCritical:
		return 5
	case LogLevelAlert:
		return 6
	case LogLevelEmergency:
		return 7
	default:
		// Default to info level
		return 1
	}
}

// LogMessage represents a log message
type LogMessage struct {
	Level  LogLevel        `json:"level"`
	Logger string          `json:"logger,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// LogManager adds logging methods to the server
type LogManager struct {
	server         *Server
	loggingManager *LoggingManager
}

// NewLogManager creates a new log manager
func NewLogManager(server *Server) *LogManager {
	return &LogManager{
		server:         server,
		loggingManager: NewLoggingManager(),
	}
}

// Log sends a log message to all clients
func (m *LogManager) Log(level LogLevel, logger string, data interface{}) {
	// Convert data to JSON
	var dataJSON json.RawMessage
	if data != nil {
		var err error
		dataJSON, err = json.Marshal(data)
		if err != nil {
			// If we can't marshal the data, use a simple error message
			dataJSON = []byte(fmt.Sprintf(`{"error": "Failed to marshal log data: %s"}`, err))
		}
	}

	// Create the log message
	msg := LogMessage{
		Level:  level,
		Logger: logger,
		Data:   dataJSON,
	}

	// Broadcast the message to all sessions
	m.server.BroadcastNotification("notifications/message", msg)
}

// LogSessionMessage sends a log message to a specific session
func (m *LogManager) LogSessionMessage(sessionID string, level LogLevel, logger string, data interface{}) {
	// Check if the message should be logged based on the session's log level
	if !m.loggingManager.ShouldLog(sessionID, level) {
		return
	}

	// Convert data to JSON
	var dataJSON json.RawMessage
	if data != nil {
		var err error
		dataJSON, err = json.Marshal(data)
		if err != nil {
			// If we can't marshal the data, use a simple error message
			dataJSON = []byte(fmt.Sprintf(`{"error": "Failed to marshal log data: %s"}`, err))
		}
	}

	// Create the log message
	msg := LogMessage{
		Level:  level,
		Logger: logger,
		Data:   dataJSON,
	}

	// Send the message to the specific session
	m.server.SendNotification(sessionID, "notifications/message", msg)
}

// HandleLoggingSetLevel handles the logging/setLevel method
func (s *Server) handleLoggingSetLevel(ctx context.Context, session *Session, params json.RawMessage) (interface{}, error) {
	var setLevelParams struct {
		Level LogLevel `json:"level"`
	}

	if err := json.Unmarshal(params, &setLevelParams); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Validate the log level
	switch setLevelParams.Level {
	case LogLevelDebug, LogLevelInfo, LogLevelNotice, LogLevelWarning,
		LogLevelError, LogLevelCritical, LogLevelAlert, LogLevelEmergency:
		// Valid level
	default:
		return nil, fmt.Errorf("invalid log level: %s", setLevelParams.Level)
	}

	// Set the log level for the session
	s.loggingManager.SetLevel(session.ID, setLevelParams.Level)

	// Return an empty result
	return struct{}{}, nil
}

// Debug logs a debug message
func (m *LogManager) Debug(logger string, data interface{}) {
	m.Log(LogLevelDebug, logger, data)
}

// Info logs an info message
func (m *LogManager) Info(logger string, data interface{}) {
	m.Log(LogLevelInfo, logger, data)
}

// Notice logs a notice message
func (m *LogManager) Notice(logger string, data interface{}) {
	m.Log(LogLevelNotice, logger, data)
}

// Warning logs a warning message
func (m *LogManager) Warning(logger string, data interface{}) {
	m.Log(LogLevelWarning, logger, data)
}

// Error logs an error message
func (m *LogManager) Error(logger string, data interface{}) {
	m.Log(LogLevelError, logger, data)
}

// Critical logs a critical message
func (m *LogManager) Critical(logger string, data interface{}) {
	m.Log(LogLevelCritical, logger, data)
}

// Alert logs an alert message
func (m *LogManager) Alert(logger string, data interface{}) {
	m.Log(LogLevelAlert, logger, data)
}

// Emergency logs an emergency message
func (m *LogManager) Emergency(logger string, data interface{}) {
	m.Log(LogLevelEmergency, logger, data)
}
