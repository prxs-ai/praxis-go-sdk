# Praxis Go SDK

A Go implementation of the Praxis SDK for building P2P AI agents with Model Context Protocol (MCP) support.

## Overview

The Praxis Go SDK provides a framework for creating decentralized AI agents that can communicate via peer-to-peer networks and integrate with MCP servers. This SDK enables developers to build autonomous agents that can discover, connect, and collaborate with other agents in a distributed network.

## Features

- **P2P Communication**: Built on libp2p for robust peer-to-peer networking
- **MCP Integration**: Full support for Model Context Protocol servers
- **Agent Discovery**: Automatic discovery and connection to other agents
- **RESTful API**: HTTP interface for agent interaction and management
- **SSE Support**: Server-Sent Events for real-time updates
- **Docker Support**: Ready-to-use Docker configuration for containerized deployment

## Architecture

The SDK consists of several core components:

- **P2P Layer**: Handles peer-to-peer communication using libp2p
- **MCP Client**: Manages connections to MCP servers
- **MCP Bridge**: Bridges P2P and MCP protocols
- **MCP Server Manager**: Manages multiple MCP server instances
- **SSE Server**: Provides real-time updates via Server-Sent Events

## Installation

### Prerequisites

- Go 1.23.4 or higher
- Docker (optional, for containerized deployment)

### Build from Source

```bash
git clone https://github.com/prxs-ai/praxis-go-sdk.git
cd praxis-go-sdk
./build-docker.sh
docker-compose up -d
docker ps
```

### Usage

### Basic Agent

```go
package main

import (
    "log"
    // Import the SDK
)

func main() {
    // Initialize and run your agent
    // See examples/mcp-server.go for a complete example
}
```

### Configuration

The SDK uses YAML configuration files for MCP server settings. Example configurations are provided in the `config/` directory:

- `mcp_config_sse_node1.yaml` - Configuration for node 1
- `mcp_config_sse_node2.yaml` - Configuration for node 2

### Docker Deployment

```bash
# Using docker-compose
docker-compose up

# Build and run manually
docker build -f Dockerfile.fast -t praxis-agent .
docker run -p 8000:8000 praxis-agent
```

## API Endpoints

The agent exposes several HTTP endpoints:

- `GET /` - Health check endpoint
- `GET /agent/card` - Get agent capabilities card
- `GET /peers` - List connected peers
- `POST /connect` - Connect to a peer
- `GET /mcp/servers` - List MCP servers
- `POST /mcp/server` - Add MCP server
- `DELETE /mcp/server/:name` - Remove MCP server
- `POST /mcp/call` - Call MCP tool
- `GET /mcp/sse` - SSE endpoint for real-time updates

## Development

### Project Structure

```
praxis-go-sdk/
├── main.go                 # Main application entry point
├── mcp_client.go          # MCP client implementation
├── mcp_protocol.go        # MCP protocol definitions
├── mcp_types.go           # MCP type definitions
├── mcp_bridge.go          # P2P-MCP bridge
├── mcp_server_manager.go  # MCP server management
├── mcp_sse_server.go      # SSE server implementation
├── config/                # Configuration files
├── examples/              # Example implementations
├── go.mod                 # Go module dependencies
└── go.sum                 # Dependency checksums
```

### Testing

Run tests with:

```bash
go test ./...
```

### Contributing

Contributions are welcome! Please ensure your code follows Go best practices and includes appropriate tests.

## License

This project is part of the Praxis AI ecosystem. Please refer to the project license for usage terms.

## Support

For issues, questions, or contributions, please visit the [GitHub repository](https://github.com/prxs-ai/praxis-go-sdk).
