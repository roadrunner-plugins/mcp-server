package mcp

import (
	"encoding/json"
	"time"
)

// DeclareToolsRequest is sent from PHP to register tools
type DeclareToolsRequest struct {
	Tools []ToolDefinition `json:"tools"`
}

// ToolDefinition represents a tool definition from PHP
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// DeclareToolsResponse is returned to PHP after tool registration
type DeclareToolsResponse struct {
	Registered []string `json:"registered"`
	Updated    []string `json:"updated"`
}

// ClientConnectedPayload is sent to PHP for authentication
type ClientConnectedPayload struct {
	SessionID   string            `json:"sessionId"`
	Credentials map[string]string `json:"credentials"`
}

// ClientConnectedResponse is expected from PHP after authentication
type ClientConnectedResponse struct {
	Allowed bool   `json:"allowed"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message,omitempty"`
}

// CallToolPayload is sent to PHP for tool execution
type CallToolPayload struct {
	SessionID string          `json:"sessionId"`
	ToolName  string          `json:"toolName"`
	Arguments json.RawMessage `json:"arguments"`
}

// CallToolResponse is expected from PHP after tool execution
type CallToolResponse struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError"`
}

// MCPContent represents MCP response content
type MCPContent struct {
	Type     string `json:"type"`               // "text", "image", "resource"
	Text     string `json:"text,omitempty"`     // For type="text"
	Data     string `json:"data,omitempty"`     // Base64 for type="image"
	URI      string `json:"uri,omitempty"`      // For type="resource"
	MimeType string `json:"mimeType,omitempty"` // MIME type
}

// SessionInfo represents an active MCP client session
type SessionInfo struct {
	ID           string
	Token        string
	ConnectedAt  time.Time
	LastActivity time.Time
	Transport    string
	Metadata     map[string]interface{}
}

// Event names for PHP worker communication
const (
	EventClientConnected = "ClientConnected"
	EventCallTool        = "CallTool"
)
