# Go P2P Agent

A Go-based peer-to-peer agent with LLM integration and MCP (Machine Capability Protocol) support.

## Features

- Peer-to-peer communication using libp2p
- LLM integration with OpenAI
- MCP bridge for tool execution
- HTTP API for agent control
- Agent card standard support

## Architecture

The project has been refactored with a modular structure:

```
go-p2p-agent/
├── cmd/
│   └── agent/              # Application entry point
├── internal/
│   ├── agent/              # P2P agent core functionality
│   ├── config/             # Configuration management
│   ├── llm/                # LLM integration
│   ├── mcp/                # MCP bridge
│   └── p2p/                # P2P networking
├── pkg/
│   ├── agentcard/          # Agent card format
│   └── utils/              # Utility functions
```

## Getting Started

### Prerequisites

- Go 1.19 or higher
- OpenAI API key (for LLM functionality)

### Installation

1. Clone the repository:
   ```
   git clone https://github.com/yourusername/go-p2p-agent.git
   cd go-p2p-agent
   ```

2. Build the application:
   ```
   go build -o agent ./cmd/agent
   ```

### Configuration

Create a configuration file at `config/config.yaml`:

```yaml
agent:
  name: "go-agent"
  version: "1.0.0"
  description: "Go P2P Agent"
  url: "http://localhost:8000"

p2p:
  enabled: true
  port: 4001
  secure: true
  rendezvous: "praxis-agents"
  enable_mdns: true
  enable_dht: true

http:
  enabled: true
  port: 8000
  host: "0.0.0.0"

mcp:
  enabled: true
  servers:
    - name: "local"
      transport: "stdio"
      command: "python"
      args: ["./scripts/mcp_server.py"]
      enabled: true

llm:
  enabled: true
  provider: "openai"
  api_key: "${OPENAI_API_KEY}"
  model: "gpt-4o-mini"
  max_tokens: 4096
  temperature: 0.1

logging:
  level: "info"
  format: "text"
```

### Running

1. Set your OpenAI API key:
   ```
   export OPENAI_API_KEY=your-api-key
   ```

2. Run the agent:
   ```
   ./agent
   ```

## API Endpoints

The agent exposes the following HTTP endpoints:

- `/card` - Get agent card
- `/health` - Health check
- `/p2p/info` - P2P information
- `/p2p/connect/:peer_name` - Connect to peer
- `/mcp/tools` - List MCP tools
- `/llm/chat` - Process LLM request

## Docker

A Dockerfile is provided to run the agent in a container:

```
docker build -t go-p2p-agent .
docker run -p 8000:8000 -e OPENAI_API_KEY=your-api-key go-p2p-agent
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
