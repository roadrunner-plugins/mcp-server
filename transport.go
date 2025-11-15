package mcp

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"
)

// serveSSE starts the SSE transport server
func (p *Plugin) serveSSE() error {
	const op = errors.Op("mcp_serve_sse")

	// Create SSE server using the SDK
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate session ID
		sessionID := uuid.New().String()

		// Extract credentials from Authorization header
		credentials := make(map[string]string)
		if authHeader := r.Header.Get("Authorization"); authHeader != "" {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			credentials["token"] = token
		}
		credentials["ip"] = r.RemoteAddr
		credentials["user_agent"] = r.UserAgent()

		// Authenticate session if auth is enabled
		var sessionToken string
		var err error
		if p.cfg.Auth.Enabled {
			sessionToken, err = p.authenticateSession(r.Context(), sessionID, credentials)
			if err != nil {
				p.log.Warn("authentication failed",
					zap.String("session_id", sessionID),
					zap.Error(err),
				)
				http.Error(w, "Authentication failed", http.StatusUnauthorized)
				return
			}
		}

		// Track session
		credentialsMap := make(map[string]interface{})
		for k, v := range credentials {
			credentialsMap[k] = v
		}
		p.trackSession(sessionID, sessionToken, "sse", credentialsMap)

		p.log.Info("SSE client connected",
			zap.String("session_id", sessionID),
			zap.String("remote_addr", r.RemoteAddr),
		)

		defer func() {
			p.removeSession(sessionID)
			p.log.Info("SSE client disconnected",
				zap.String("session_id", sessionID),
			)
		}()

		// Create SSE transport for this connection
		transport := mcp.NewSSETransport("/sse", w, r)

		// Connect server to transport with proper context
		_, err = p.mcpServer.Connect(r.Context(), transport, nil)
		if err != nil {
			p.log.Error("failed to connect SSE transport",
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
			http.Error(w, "Failed to establish SSE connection", http.StatusInternalServerError)
			return
		}
	})

	// Create HTTP server
	p.httpServer = &http.Server{
		Addr:         p.cfg.Address,
		Handler:      handler,
		ReadTimeout:  p.cfg.Clients.ReadTimeout,
		WriteTimeout: p.cfg.Clients.WriteTimeout,
	}

	// Start server
	p.log.Info("SSE transport listening", zap.String("address", p.cfg.Address))

	if err := p.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return errors.E(op, err)
	}

	return nil
}

// serveStdio starts the stdio transport
func (p *Plugin) serveStdio() error {
	const op = errors.Op("mcp_serve_stdio")

	// Create stdio transport
	transport := mcp.NewStdioTransport()

	// Generate session ID
	sessionID := uuid.New().String()

	// Skip authentication for stdio if configured
	var sessionToken string
	var err error
	if p.cfg.Auth.Enabled && !p.cfg.Auth.SkipForStdio {
		sessionToken, err = p.authenticateSession(p.ctx, sessionID, map[string]string{})
		if err != nil {
			return errors.E(op, fmt.Errorf("authentication failed: %w", err))
		}
	}

	// Track session
	p.trackSession(sessionID, sessionToken, "stdio", nil)

	p.log.Info("stdio transport connected", zap.String("session_id", sessionID))

	defer func() {
		p.removeSession(sessionID)
		p.log.Info("stdio transport disconnected", zap.String("session_id", sessionID))
	}()

	// Connect server to transport - this blocks until connection ends
	_, err = p.mcpServer.Connect(p.ctx, transport, nil)
	if err != nil {
		return errors.E(op, fmt.Errorf("failed to connect stdio transport: %w", err))
	}

	return nil
}

// trackSession adds a new session to the registry
func (p *Plugin) trackSession(sessionID, token, transport string, metadata map[string]interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()

	info := &SessionInfo{
		ID:           sessionID,
		Token:        token,
		ConnectedAt:  time.Now(),
		LastActivity: time.Now(),
		Transport:    transport,
		Metadata:     metadata,
	}

	p.sessions[sessionID] = info

	p.log.Debug("session tracked",
		zap.String("session_id", sessionID),
		zap.String("transport", transport),
	)
}

// removeSession removes a session from the registry
func (p *Plugin) removeSession(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.sessions, sessionID)

	p.log.Debug("session removed", zap.String("session_id", sessionID))
}
