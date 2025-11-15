package mcp

import (
	"time"

	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/pool/pool"
)

const PluginName = "mcp"

// Config represents the MCP plugin configuration
type Config struct {
	// Transport type: "sse", "stdio"
	Transport string `mapstructure:"transport"`

	// Address for SSE transports (ignored for stdio)
	Address string `mapstructure:"address"`

	// Worker pool configuration (uses RoadRunner's standard pool)
	Pool *pool.Config `mapstructure:"pool"`

	// Client session configuration
	Clients struct {
		MaxConnections int           `mapstructure:"max_connections"`
		ReadTimeout    time.Duration `mapstructure:"read_timeout"`
		WriteTimeout   time.Duration `mapstructure:"write_timeout"`
		PingInterval   time.Duration `mapstructure:"ping_interval"`
	} `mapstructure:"clients"`

	// Tool management
	Tools struct {
		NotifyClientsOnChange bool `mapstructure:"notify_clients_on_change"`
	} `mapstructure:"tools"`

	// Authentication
	Auth struct {
		Enabled      bool `mapstructure:"enabled"`
		SkipForStdio bool `mapstructure:"skip_for_stdio"`
	} `mapstructure:"auth"`

	// Logging
	Debug bool `mapstructure:"debug"`
}

// InitDefaults sets default values for configuration
func (c *Config) InitDefaults() error {
	if c.Transport == "" {
		c.Transport = "sse"
	}

	if c.Address == "" {
		c.Address = "127.0.0.1:9333"
	}

	// Initialize pool defaults
	if c.Pool == nil {
		c.Pool = &pool.Config{}
	}
	c.Pool.InitDefaults()

	// Client defaults
	if c.Clients.MaxConnections == 0 {
		c.Clients.MaxConnections = 100
	}
	if c.Clients.ReadTimeout == 0 {
		c.Clients.ReadTimeout = 60 * time.Second
	}
	if c.Clients.WriteTimeout == 0 {
		c.Clients.WriteTimeout = 10 * time.Second
	}
	if c.Clients.PingInterval == 0 {
		c.Clients.PingInterval = 30 * time.Second
	}

	// Tool defaults
	c.Tools.NotifyClientsOnChange = true

	// Auth defaults
	c.Auth.SkipForStdio = true

	return c.Validate()
}

// Validate validates the configuration
func (c *Config) Validate() error {
	const op = errors.Op("mcp_config_validate")

	if c.Transport != "sse" && c.Transport != "stdio" {
		return errors.E(op, errors.Str("transport must be 'sse' or 'stdio'"))
	}

	if c.Transport == "sse" && c.Address == "" {
		return errors.E(op, errors.Str("address is required for SSE transport"))
	}

	if c.Clients.MaxConnections < 1 {
		return errors.E(op, errors.Str("max_connections must be at least 1"))
	}

	if c.Clients.ReadTimeout < time.Second {
		return errors.E(op, errors.Str("read_timeout must be at least 1 second"))
	}

	if c.Clients.WriteTimeout < time.Second {
		return errors.E(op, errors.Str("write_timeout must be at least 1 second"))
	}

	if c.Clients.PingInterval < time.Second {
		return errors.E(op, errors.Str("ping_interval must be at least 1 second"))
	}

	return nil
}
