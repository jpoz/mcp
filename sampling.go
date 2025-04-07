package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// MessageRole represents the role of a message
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

// ContentType represents the type of content in a message
type ContentType string

const (
	ContentTypeText  ContentType = "text"
	ContentTypeImage ContentType = "image"
	ContentTypeAudio ContentType = "audio"
)

// MessageContent represents the content of a message
type MessageContent struct {
	Type     ContentType `json:"type"`
	Text     string      `json:"text,omitempty"`
	Data     string      `json:"data,omitempty"`     // Base64-encoded data for images/audio
	MimeType string      `json:"mimeType,omitempty"` // MIME type for binary data
}

// Message represents a message in a sampling request
type Message struct {
	Role    MessageRole    `json:"role"`
	Content MessageContent `json:"content"`
}

// ModelHint represents a hint for model selection
type ModelHint struct {
	Name string `json:"name"`
}

// ModelPreferences represents preferences for model selection
type ModelPreferences struct {
	Hints                []ModelHint `json:"hints,omitempty"`
	CostPriority         float64     `json:"costPriority,omitempty"`
	SpeedPriority        float64     `json:"speedPriority,omitempty"`
	IntelligencePriority float64     `json:"intelligencePriority,omitempty"`
}

// CreateMessageParams represents the parameters for creating a message
type CreateMessageParams struct {
	Messages         []Message        `json:"messages"`
	ModelPreferences ModelPreferences `json:"modelPreferences"`
	SystemPrompt     string           `json:"systemPrompt,omitempty"`
	MaxTokens        int              `json:"maxTokens,omitempty"`
}

// CreateMessageResult represents the result of creating a message
type CreateMessageResult struct {
	Role       MessageRole    `json:"role"`
	Content    MessageContent `json:"content"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stopReason,omitempty"`
}

// CreateMessage sends a sampling/createMessage request to the server
func (c *Client) CreateMessage(ctx context.Context, params CreateMessageParams) (*CreateMessageResult, error) {
	if !c.IsInitialized() {
		return nil, errors.New("client not initialized")
	}

	// Check if the server supports the sampling capability
	c.mu.Lock()
	_, hasSampling := c.capabilities["sampling"]
	c.mu.Unlock()
	if !hasSampling {
		return nil, errors.New("server does not support sampling capability")
	}

	// Send the request
	resp, err := c.SendRequest(ctx, "sampling/createMessage", params)
	if err != nil {
		return nil, fmt.Errorf("create message request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("server returned error: %d - %s", resp.Error.Code, resp.Error.Message)
	}

	// Parse the result
	var result CreateMessageResult

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal create message result: %w", err)
	}

	return &result, nil
}

// TextMessage creates a simple text message
func TextMessage(role MessageRole, text string) Message {
	return Message{
		Role: role,
		Content: MessageContent{
			Type: ContentTypeText,
			Text: text,
		},
	}
}

// ImageMessage creates an image message
func ImageMessage(role MessageRole, data, mimeType string) Message {
	return Message{
		Role: role,
		Content: MessageContent{
			Type:     ContentTypeImage,
			Data:     data,
			MimeType: mimeType,
		},
	}
}

// AudioMessage creates an audio message
func AudioMessage(role MessageRole, data, mimeType string) Message {
	return Message{
		Role: role,
		Content: MessageContent{
			Type:     ContentTypeAudio,
			Data:     data,
			MimeType: mimeType,
		},
	}
}
