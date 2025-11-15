package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/pool/payload"
	"go.uber.org/zap"
)

// sendEvent sends an event to PHP worker via WorkerPool
func (p *Plugin) sendEvent(ctx context.Context, sessionID, eventName string, payload interface{}) ([]byte, error) {
	const op = errors.Op("mcp_send_event")

	// Marshal payload to JSON
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.E(op, fmt.Errorf("failed to marshal payload: %w", err))
	}

	// Get session info for token
	p.mu.RLock()
	sessionInfo := p.sessions[sessionID]
	p.mu.RUnlock()

	// Build headers
	headers := map[string][]string{
		"X-MCP-Event":  {eventName},
		"X-Session-ID": {sessionID},
		"Content-Type": {"application/json"},
		"X-MCP-Method": {"POST"},
	}

	if sessionInfo != nil && sessionInfo.Token != "" {
		headers["X-Client-Token"] = []string{sessionInfo.Token}
	}

	// Create payload for worker
	workerPayload := payload.Payload{
		Context: payloadJSON,
		Body:    payloadJSON,
	}

	// Execute via worker pool
	p.log.Debug("sending event to worker",
		zap.String("event", eventName),
		zap.String("session_id", sessionID),
	)

	// Create stop channel
	stopCh := make(chan struct{}, 1)

	// Execute on pool
	responseCh, err := p.pool.Exec(ctx, &workerPayload, stopCh)
	if err != nil {
		return nil, errors.E(op, fmt.Errorf("worker execution failed: %w", err))
	}

	// Read response from channel
	for response := range responseCh {
		if response.Error() != nil {
			return nil, errors.E(op, response.Error())
		}

		return response.Body(), nil
	}

	return nil, errors.E(op, errors.Str("no response from worker"))
}

// authenticateSession authenticates a new client session via PHP worker
func (p *Plugin) authenticateSession(ctx context.Context, sessionID string, credentials map[string]string) (string, error) {
	const op = errors.Op("mcp_authenticate_session")

	// Skip authentication if disabled
	if !p.cfg.Auth.Enabled {
		return "", nil
	}

	// Create payload
	payload := &ClientConnectedPayload{
		SessionID:   sessionID,
		Credentials: credentials,
	}

	// Send event to PHP
	phpResp, err := p.sendEvent(ctx, sessionID, EventClientConnected, payload)
	if err != nil {
		return "", errors.E(op, err)
	}

	// Parse response
	var authResp ClientConnectedResponse
	if err := json.Unmarshal(phpResp, &authResp); err != nil {
		return "", errors.E(op, fmt.Errorf("invalid worker response: %w", err))
	}

	// Check if allowed
	if !authResp.Allowed {
		return "", errors.E(op, fmt.Errorf("authentication failed: %s", authResp.Message))
	}

	p.log.Info("session authenticated",
		zap.String("session_id", sessionID),
	)

	return authResp.Token, nil
}
