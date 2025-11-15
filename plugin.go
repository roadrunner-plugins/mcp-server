package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roadrunner-server/endure/v2/dep"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/pool"
	"github.com/roadrunner-server/pool/static_pool"
	"go.uber.org/zap"
)

// Plugin implements the MCP server plugin for RoadRunner
type Plugin struct {
	mu sync.RWMutex

	// Configuration
	cfg *Config
	log *zap.Logger

	// MCP server instance
	mcpServer *mcp.Server

	// RoadRunnercomponents
	server Server
	pool   pool.Pool

	// Tool registry (name -> definition)
	tools map[string]*mcp.Tool

	// Active sessions (sessionID -> info)
	sessions map[string]*SessionInfo

	// HTTP server for SSE transport
	httpServer *http.Server

	// Context for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// Metrics
	statsExporter *StatsExporter
}

// Server interface for creating worker pools
type Server interface {
	NewPool(ctx context.Context, cfg *pool.Config, env map[string]string, logger *zap.Logger) (*static_pool.Pool, error)
}

// Configurer interface for configuration access
type Configurer interface {
	UnmarshalKey(name string, out any) error
	Has(name string) bool
}

// Logger interface for logging
type Logger interface {
	NamedLogger(name string) *zap.Logger
}

// Init initializes the MCP plugin
func (p *Plugin) Init(cfg Configurer, log Logger, srv Server) error {
	const op = errors.Op("mcp_init")

	// Check if plugin is enabled
	if !cfg.Has(PluginName) {
		return errors.E(op, errors.Disabled)
	}

	// Parse configuration
	p.cfg = &Config{}
	if err := cfg.UnmarshalKey(PluginName, p.cfg); err != nil {
		return errors.E(op, err)
	}

	// Initialize defaults
	if err := p.cfg.InitDefaults(); err != nil {
		return errors.E(op, err)
	}

	// Store dependencies
	p.log = log.NamedLogger(PluginName)
	p.server = srv

	// Initialize internal structures
	p.tools = make(map[string]*mcp.Tool)
	p.sessions = make(map[string]*SessionInfo)

	// Create context for lifecycle management
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Initialize metrics
	p.statsExporter = newStatsExporter(p)

	// Create MCP server
	if err := p.createMCPServer(); err != nil {
		return errors.E(op, err)
	}

	p.log.Info("MCP plugin initialized",
		zap.String("transport", p.cfg.Transport),
		zap.String("address", p.cfg.Address),
		zap.Bool("auth_enabled", p.cfg.Auth.Enabled),
	)

	return nil
}

// Serve starts the MCP plugin
func (p *Plugin) Serve() chan error {
	errCh := make(chan error, 1)

	p.mu.Lock()
	defer p.mu.Unlock()

	// Create worker pool
	var err error
	p.pool, err = p.server.NewPool(
		p.ctx,
		p.cfg.Pool,
		map[string]string{"RR_MODE": "mcp"},
		p.log,
	)
	if err != nil {
		errCh <- errors.E(errors.Op("mcp_serve"), err)
		return errCh
	}

	// Start transport
	go func() {
		var err error
		switch p.cfg.Transport {
		case "sse":
			err = p.serveSSE()
		case "stdio":
			err = p.serveStdio()
		default:
			err = fmt.Errorf("unsupported transport: %s", p.cfg.Transport)
		}

		if err != nil {
			p.log.Error("transport error", zap.Error(err))
			errCh <- err
		}
	}()

	p.log.Info("MCP plugin serving", zap.String("transport", p.cfg.Transport))

	return errCh
}

// Stop gracefully stops the MCP plugin
func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.log.Info("stopping MCP plugin")

	// Cancel context
	if p.cancel != nil {
		p.cancel()
	}

	// Close HTTP server for SSE
	if p.httpServer != nil {
		if err := p.httpServer.Shutdown(ctx); err != nil {
			p.log.Error("failed to shutdown HTTP server", zap.Error(err))
		}
	}

	// Close all sessions
	for sessionID, info := range p.sessions {
		p.log.Debug("closing session", zap.String("session_id", sessionID))
		delete(p.sessions, sessionID)
		_ = info
	}

	// Destroy worker pool
	if p.pool != nil {
		p.pool.Destroy(ctx)
	}

	return nil
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return PluginName
}

// Weight returns plugin weight for dependency resolution
func (p *Plugin) Weight() uint {
	return 10
}

// RPC returns the RPC interface
func (p *Plugin) RPC() interface{} {
	return &rpcService{plugin: p}
}

// Collects dependencies
func (p *Plugin) Collects() []*dep.In {
	return []*dep.In{
		dep.Fits(func(pp any) {
			p.server = pp.(Server)
		}, (*Server)(nil)),
	}
}

// MetricsCollector returns prometheus collectors
func (p *Plugin) MetricsCollector() []interface{} {
	return []interface{}{p.statsExporter}
}

// Workers returns worker states for metrics
func (p *Plugin) Workers() []*static_pool.WorkerState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.pool == nil {
		return nil
	}

	workers := p.pool.Workers()
	states := make([]*static_pool.WorkerState, 0, len(workers))

	for _, w := range workers {
		state, err := static_pool.NewWorkerState(w)
		if err != nil {
			continue
		}
		states = append(states, state)
	}

	return states
}

// createMCPServer creates the MCP server instance
func (p *Plugin) createMCPServer() error {
	// Create server implementation info
	impl := &mcp.Implementation{
		Name:    "roadrunner-mcp",
		Version: "1.0.0",
	}

	// Configure server options
	opts := &mcp.ServerOptions{
		Capabilities: mcp.ServerCapabilities{
			Tools: &mcp.ToolsCapability{
				ListChanged: boolPtr(p.cfg.Tools.NotifyClientsOnChange),
			},
		},
	}

	// Create the server
	p.mcpServer = mcp.NewServer(impl, opts)

	p.log.Info("MCP server created",
		zap.String("name", impl.Name),
		zap.String("version", impl.Version),
	)

	return nil
}

// zapToSlog converts zap logger to slog logger
func (p *Plugin) zapToSlog() *slog.Logger {
	return slog.New(slog.NewJSONHandler(
		zapWriter{p.log},
		&slog.HandlerOptions{
			Level: slog.LevelInfo,
		},
	))
}

// zapWriter wraps zap.Logger to implement io.Writer
type zapWriter struct {
	logger *zap.Logger
}

func (w zapWriter) Write(p []byte) (n int, err error) {
	w.logger.Info(string(p))
	return len(p), nil
}
