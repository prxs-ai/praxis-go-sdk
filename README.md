# Praxis P2P Agent Network

A distributed AI workflow orchestration platform that enables decentralized agent communication and tool sharing through P2P networking.

## 🚀 What is Praxis?

Praxis is a **distributed multi-agent system** where AI agents can:
- 🤝 **Discover each other** automatically through P2P networking
- 🔧 **Share tools and capabilities** using the Model Context Protocol (MCP)
- 💬 **Execute workflows** through natural language or Domain Specific Language (DSL)
- 📁 **Collaborate on tasks** like file operations, data processing, and more

## ✨ Key Features

- 🔗 **P2P Agent Network** - Agents discover and connect automatically via libp2p
- 🧠 **LLM Integration** - Natural language to executable workflows with OpenAI GPT-4
- 🔧 **MCP Protocol** - Tool sharing between agents using Model Context Protocol
- ⚡ **Real-time UI** - React/Next.js frontend with live workflow execution
- 🐳 **Container Ready** - Full Docker deployment with docker-compose
- 📝 **DSL Support** - Direct tool invocation with proper argument parsing

## 🏃 Quick Start

### 1. Build the Docker Image
```bash
docker build -t praxis-agent:latest .
```

### 2. Start the Agent Network
```bash
docker-compose -f docker-compose-alpine.yml up -d
```

### 3. Test File Creation
```bash
# Test the DSL functionality
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "CALL write_file hello.txt \"Hello from Praxis!\""}'

# Check if file was created
docker exec praxis-agent-2 cat /shared/hello.txt
```

### 4. View Agent Status
```bash
# Check agent health
curl http://localhost:8000/health
curl http://localhost:8001/health

# List connected peers
curl http://localhost:8000/peers

# View agent capabilities
curl http://localhost:8000/p2p/cards
```

## 🔧 Environment Variables

Create a `.env` file or set these environment variables:

### Required
- `AGENT_NAME` - Unique agent identifier (e.g., "praxis-agent-1")

### Optional
- `OPENAI_API_KEY` - OpenAI API key for LLM features (enables natural language workflows)
- `HTTP_PORT` - HTTP API port (default: 8000/8001)
- `P2P_PORT` - P2P communication port (default: 4000/4001)
- `WEBSOCKET_PORT` - WebSocket port for frontend (default: 9100/9102)
- `LOG_LEVEL` - Logging level: debug, info, warn, error (default: info)
- `MCP_ENABLED` - Enable MCP server (default: true)

### Example .env file:
```bash
OPENAI_API_KEY=sk-your-key-here
LOG_LEVEL=info
```

## 🏗️ Architecture

```
Frontend (React) ←→ WebSocket ←→ Agent-1 ←→ P2P Network ←→ Agent-2
                                    ↓                        ↓
                                 DSL Parser            Filesystem Tools
                                    ↓                        ↓
                                 LLM Client              MCP Server
```

**Two-Agent Setup:**
- **Agent-1**: Orchestrator with LLM integration and DSL parsing
- **Agent-2**: Filesystem provider with file operation tools
- **P2P Network**: Automatic discovery and secure communication
- **MCP Protocol**: Tool sharing and remote execution

## 📋 Available Commands

### DSL Commands (Direct)
```bash
# File operations
CALL write_file filename.txt "content"
CALL read_file filename.txt
CALL list_files
CALL delete_file filename.txt

# P2P operations
CALL list_peers
CALL send_message peer_id "message"
```

### Natural Language (Requires OpenAI API Key)
```bash
"create a file called data.txt with some JSON content"
"list all files in the shared directory"
"read the contents of config.txt"
```

## 📊 API Endpoints

### Agent-1 (Orchestrator) - Port 8000
- `GET /health` - Health check
- `POST /execute` - Execute DSL/natural language workflow
- `GET /peers` - List connected P2P peers
- `GET /p2p/cards` - Get peer agent capabilities

### Agent-2 (Filesystem) - Port 8001
- `GET /health` - Health check
- `GET /peers` - List connected P2P peers
- MCP tools available via P2P protocol

## 🔍 Monitoring

### View Agent Logs
```bash
# View real-time logs
docker logs -f praxis-agent-1
docker logs -f praxis-agent-2

# Check shared files
docker exec praxis-agent-2 ls -la /shared/
```

### Check P2P Connections
```bash
# List peers from Agent-1
curl http://localhost:8000/peers

# View agent capabilities
curl http://localhost:8000/p2p/cards
```

## 🚀 Production Deployment

1. Set production environment variables
2. Build optimized Docker image
3. Use `docker-compose.yml` for production setup
4. Configure load balancer for multiple agent instances
5. Set up monitoring and logging

## 📚 Documentation

See the `/docs` folder for detailed documentation:
- `ARCHITECTURE_AND_USAGE.md` - System architecture details
- `INTEGRATION_SUMMARY.md` - Integration guide
- `VISUAL_GUIDE.md` - Visual workflow examples
- `PARAMETRIZED_WORKFLOW.md` - Parametrized workflows

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test with the provided examples
5. Submit a pull request

## 📄 License

MIT License - see LICENSE file for details.
