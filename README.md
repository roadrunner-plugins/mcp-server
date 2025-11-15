# RoadRunner MCP Server Plugin

A RoadRunner plugin that enables PHP applications to expose MCP (Model Context Protocol) compatible tools to AI clients without implementing connection management or protocol details.

## Features

- **Zero Infrastructure Code in PHP** - Go handles all MCP protocol, connections, and sessions
- **Multiple Transports** - SSE (Server-Sent Events) and stdio support
- **Dynamic Tool Registration** - PHP workers register tools via RPC at runtime
- **Session Management** - Built-in authentication and session tracking
- **Worker Pool Integration** - Uses RoadRunner's efficient PHP worker pool
- **Metrics & Monitoring** - Prometheus metrics for observability

## Installation

```bash
go get github.com/roadrunner-plugins/mcp
```

## Configuration

### Minimal Configuration

```yaml
version: "3"

server:
  command: "php worker.php"

mcp:
  transport: "sse"
  address: "127.0.0.1:9333"
  
  pool:
    num_workers: 4
```

### Full Configuration

```yaml
version: "3"

rpc:
  listen: tcp://127.0.0.1:6001

server:
  command: "php worker.php"

mcp:
  # Transport configuration (only one transport at a time)
  transport: "sse"  # Options: "sse", "stdio"
  
  # Address for SSE transports (ignored for stdio)
  address: "127.0.0.1:9333"
  
  # Worker pool configuration
  pool:
    num_workers: 4
    max_jobs: 100
    allocate_timeout: 60s
    destroy_timeout: 60s
    supervisor:
      watch_tick: 1s
      ttl: 0s
      idle_ttl: 300s
      exec_ttl: 30s
      max_worker_memory: 256
  
  # Client session configuration
  clients:
    max_connections: 100
    read_timeout: 60s
    write_timeout: 10s
    ping_interval: 30s
  
  # Tool management
  tools:
    notify_clients_on_change: true
  
  # Authentication
  auth:
    enabled: true
    skip_for_stdio: true
  
  # Logging
  debug: false

logs:
  mode: production
  level: info
  
metrics:
  address: "127.0.0.1:2112"
```

## PHP Worker Integration

### Basic Worker Structure

```php
<?php

use Spiral\RoadRunner\Worker;
use Spiral\RoadRunner\Http\PSR7Worker;
use Nyholm\Psr7\Factory\Psr17Factory;

require __DIR__ . '/vendor/autoload.php';

$worker = Worker::create();
$psrFactory = new Psr17Factory();
$psr7 = new PSR7Worker($worker, $psrFactory, $psrFactory, $psrFactory);

// Declare tools on startup
declareMCPTools();

// Handle incoming events
while ($request = $psr7->waitRequest()) {
    try {
        $response = handleMCPEvent($request, $psrFactory);
        $psr7->respond($response);
    } catch (\Throwable $e) {
        $psr7->respond(
            $psrFactory->createResponse(500)
                ->withBody($psrFactory->createStream(json_encode([
                    'error' => $e->getMessage()
                ])))
        );
    }
}
```

### Declaring Tools via RPC

```php
function declareMCPTools(): void
{
    $rpc = new RPC(
        RPC::create('tcp://127.0.0.1:6001')
            ->withCodec(new JsonCodec())
    );

    $tools = [
        [
            'name' => 'query_database',
            'description' => 'Execute SQL queries against application database',
            'inputSchema' => [
                'type' => 'object',
                'properties' => [
                    'query' => ['type' => 'string'],
                    'params' => ['type' => 'array']
                ],
                'required' => ['query']
            ]
        ],
        [
            'name' => 'send_email',
            'description' => 'Send email via application mailer',
            'inputSchema' => [
                'type' => 'object',
                'properties' => [
                    'to' => ['type' => 'string'],
                    'subject' => ['type' => 'string'],
                    'body' => ['type' => 'string']
                ],
                'required' => ['to', 'subject', 'body']
            ]
        ]
    ];

    $response = $rpc->call('mcp.DeclareTools', ['tools' => $tools]);
    
    echo "Registered tools: " . implode(', ', $response['registered']) . "\n";
}
```

### Handling Events

```php
function handleMCPEvent(ServerRequestInterface $request, Psr17Factory $factory): ResponseInterface
{
    $event = $request->getHeaderLine('X-MCP-Event');
    $sessionId = $request->getHeaderLine('X-Session-ID');
    
    $body = (string) $request->getBody();
    $data = json_decode($body, true);
    
    switch ($event) {
        case 'ClientConnected':
            return handleClientConnected($data, $factory);
            
        case 'CallTool':
            return handleCallTool($data, $factory);
            
        default:
            return $factory->createResponse(400)
                ->withBody($factory->createStream(json_encode([
                    'error' => "Unknown event: {$event}"
                ])));
    }
}
```

### Client Authentication

```php
function handleClientConnected(array $data, Psr17Factory $factory): ResponseInterface
{
    $sessionId = $data['sessionId'];
    $credentials = $data['credentials'];
    $token = $credentials['token'] ?? '';
    
    // Validate token (example: check against database)
    $user = validateToken($token);
    
    if ($user) {
        $sessionToken = generateSessionToken($sessionId, $user);
        
        return $factory->createResponse(200)
            ->withHeader('Content-Type', 'application/json')
            ->withBody($factory->createStream(json_encode([
                'allowed' => true,
                'token' => $sessionToken
            ])));
    }
    
    return $factory->createResponse(200)
        ->withHeader('Content-Type', 'application/json')
        ->withBody($factory->createStream(json_encode([
            'allowed' => false,
            'message' => 'Invalid credentials'
        ])));
}
```

### Tool Execution

```php
function handleCallTool(array $data, Psr17Factory $factory): ResponseInterface
{
    $toolName = $data['toolName'];
    $arguments = $data['arguments'];
    $sessionId = $data['sessionId'];
    
    try {
        switch ($toolName) {
            case 'query_database':
                $result = executeQuery($arguments['query'], $arguments['params'] ?? []);
                break;
                
            case 'send_email':
                $result = sendEmail($arguments['to'], $arguments['subject'], $arguments['body']);
                break;
                
            default:
                throw new \Exception("Unknown tool: {$toolName}");
        }
        
        return $factory->createResponse(200)
            ->withHeader('Content-Type', 'application/json')
            ->withBody($factory->createStream(json_encode([
                'content' => [
                    [
                        'type' => 'text',
                        'text' => json_encode($result, JSON_PRETTY_PRINT)
                    ]
                ],
                'isError' => false
            ])));
            
    } catch (\Throwable $e) {
        return $factory->createResponse(200)
            ->withHeader('Content-Type', 'application/json')
            ->withBody($factory->createStream(json_encode([
                'content' => [
                    [
                        'type' => 'text',
                        'text' => $e->getMessage()
                    ]
                ],
                'isError' => true
            ])));
    }
}
```

## Usage

### Starting the Server

```bash
# SSE transport
rr serve -c .rr.yaml

# stdio transport (for CLI tools)
rr mcp serve -c .rr.yaml
```

### Connecting Clients

#### Claude Desktop (SSE)

Add to Claude Desktop configuration:

```json
{
  "mcpServers": {
    "my-app": {
      "url": "http://127.0.0.1:9333/sse",
      "transport": "sse",
      "headers": {
        "Authorization": "Bearer your-token-here"
      }
    }
  }
}
```

#### MCP Inspector (stdio)

```bash
npx @modelcontextprotocol/inspector rr mcp serve -c .rr.yaml
```

## Metrics

Available Prometheus metrics:

- `mcp_tools_registered` - Total number of registered tools
- `mcp_tool_calls_total` - Total tool calls by tool and status
- `mcp_tool_duration_seconds` - Tool execution duration
- `mcp_active_sessions` - Active MCP sessions by transport
- `mcp_workers_total` - Total PHP workers
- `mcp_workers_active` - Active PHP workers

Access metrics at: `http://127.0.0.1:2112/metrics`

## Architecture

```
┌─────────────────┐
│   MCP Client    │ (Claude Desktop, CLI)
└────────┬────────┘
         │ SSE/stdio
         │
┌────────▼────────────────────────────────────────┐
│           RoadRunner (Go Plugin)                │
│                                                 │
│  ┌──────────────┐      ┌──────────────────┐     │
│  │  Transport   │◄────►│  Session Manager │     │
│  │  Layer       │      │  (Client State)  │     │
│  └──────┬───────┘      └──────────────────┘     │
│         │                                       │
│  ┌──────▼─────────────────────────────────┐     │
│  │     Event Dispatcher                   │     │
│  │  (Go → PHP Worker Communication)       │     │
│  └──────┬─────────────────────────────────┘     │
│         │                                       │
│  ┌──────▼────────┐                              │
│  │  WorkerPool   │                              │
│  └──────┬────────┘                              │
└─────────┼───────────────────────────────────────┘
          │ Goridge Protocol
          │
┌─────────▼────────┐
│  PHP Worker      │
│  - Tool Registry │
│  - Tool Executor │
│  - Auth Handler  │
└──────────────────┘
```

## License

MIT
