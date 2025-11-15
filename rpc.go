package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"
)

// rpcService exposes RPC methods to PHP workers
type rpcService struct {
	plugin *Plugin
}

// DeclareTools registers or updates tools from PHP workers
func (s *rpcService) DeclareTools(req *DeclareToolsRequest, resp *DeclareToolsResponse) error {
	const op = errors.Op("mcp_rpc_declare_tools")

	s.plugin.mu.Lock()
	defer s.plugin.mu.Unlock()

	resp.Registered = []string{}
	resp.Updated = []string{}

	for _, toolDef := range req.Tools {
		// Check if tool already exists
		_, exists := s.plugin.tools[toolDef.Name]

		// Create MCP Tool structure
		tool := &mcp.Tool{
			Name:        toolDef.Name,
			Description: toolDef.Description,
			InputSchema: toolDef.InputSchema,
		}

		// Create handler that delegates to PHP
		handler := s.plugin.createToolHandler(toolDef.Name)

		// Add tool to MCP server using AddTool function
		mcp.AddTool(s.plugin.mcpServer, tool, handler)

		// Update registry
		s.plugin.tools[toolDef.Name] = tool

		// Track response
		if exists {
			resp.Updated = append(resp.Updated, toolDef.Name)
		} else {
			resp.Registered = append(resp.Registered, toolDef.Name)
		}

		s.plugin.log.Info("tool registered",
			zap.String("tool", toolDef.Name),
			zap.Bool("updated", exists),
		)
	}

	// Notify clients if configured
	if s.plugin.cfg.Tools.NotifyClientsOnChange && len(resp.Registered)+len(resp.Updated) > 0 {
		s.plugin.notifyToolsChanged()
	}

	return nil
}

// RemoveTools removes tools from the registry
func (s *rpcService) RemoveTools(names []string, _ *struct{}) error {
	const op = errors.Op("mcp_rpc_remove_tools")

	s.plugin.mu.Lock()
	defer s.plugin.mu.Unlock()

	for _, name := range names {
		delete(s.plugin.tools, name)
		s.plugin.log.Info("tool removed", zap.String("tool", name))
	}

	// Notify clients if configured
	if s.plugin.cfg.Tools.NotifyClientsOnChange && len(names) > 0 {
		s.plugin.notifyToolsChanged()
	}

	return nil
}

// createToolHandler creates a tool handler that delegates execution to PHP workers
func (p *Plugin) createToolHandler(toolName string) func(context.Context, *mcp.CallToolRequest, map[string]interface{}) (*mcp.CallToolResult, interface{}, error) {
	return func(ctx context.Context, request *mcp.CallToolRequest, args map[string]interface{}) (*mcp.CallToolResult, interface{}, error) {
		// Session ID from params (if available)
		sessionID := "unknown"
		if request.Params != nil && request.Params.Meta != nil {
			if sid, ok := request.Params.Meta["sessionId"]; ok {
				sessionID = fmt.Sprintf("%v", sid)
			}
		}

		p.log.Debug("tool execution requested",
			zap.String("tool", toolName),
			zap.String("session_id", sessionID),
		)

		// Update session activity
		p.updateSessionActivity(sessionID)

		// Marshal arguments to JSON
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal arguments: %w", err)
		}

		// Create payload for PHP
		payload := &CallToolPayload{
			SessionID: sessionID,
			ToolName:  toolName,
			Arguments: json.RawMessage(argsJSON),
		}

		// Send event to PHP worker
		phpResp, err := p.sendEvent(ctx, sessionID, EventCallTool, payload)
		if err != nil {
			p.log.Error("tool execution failed",
				zap.String("tool", toolName),
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
			return nil, nil, fmt.Errorf("tool execution failed: %w", err)
		}

		// Parse PHP response
		var result CallToolResponse
		if err := json.Unmarshal(phpResp, &result); err != nil {
			p.log.Error("invalid PHP response",
				zap.String("tool", toolName),
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
			return nil, nil, fmt.Errorf("invalid worker response: %w", err)
		}

		// Convert to MCP result
		mcpContent := make([]mcp.Content, len(result.Content))
		for i, c := range result.Content {
			switch c.Type {
			case "text":
				mcpContent[i] = &mcp.TextContent{Text: c.Text}
			case "image":
				// Image data should be base64 encoded string
				mcpContent[i] = &mcp.ImageContent{Data: c.Data, MIMEType: c.MimeType}
			case "resource":
				// For resource content, use text content as fallback
				mcpContent[i] = &mcp.TextContent{Text: c.Text}
			default:
				mcpContent[i] = &mcp.TextContent{Text: c.Text}
			}
		}

		mcpResult := &mcp.CallToolResult{
			Content: mcpContent,
			IsError: result.IsError,
		}

		p.log.Debug("tool execution completed",
			zap.String("tool", toolName),
			zap.String("session_id", sessionID),
			zap.Bool("is_error", result.IsError),
		)

		return mcpResult, nil, nil
	}
}

// notifyToolsChanged sends notifications to all connected clients
func (p *Plugin) notifyToolsChanged() {
	p.log.Info("notifying clients about tool changes")
	// TODO: Implement notification logic via MCP SDK
}

// updateSessionActivity updates the last activity time for a session
func (p *Plugin) updateSessionActivity(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if info, ok := p.sessions[sessionID]; ok {
		info.LastActivity = time.Now()
	}
}
