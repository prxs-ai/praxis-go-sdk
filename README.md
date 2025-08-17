# Praxis Go SDK - Comprehensive Documentation

## System Overview

### Architecture Description

The Praxis Go SDK is a sophisticated peer-to-peer agent communication system that enables intelligent agents to discover, connect, and collaborate using multiple protocols and communication channels. The architecture is designed around four core components working in harmony.

### Core Components

#### 1. P2P Agent (LibP2P-based)

- **Purpose**: Establishes secure, encrypted peer-to-peer connections between agents
- **Technology**: Built on libp2p with Noise security protocol and Yamux multiplexing
- **Protocols Supported**:
  - `/ai-agent/card/1.0.0` - A2A card exchange protocol
  - `/mcp/bridge/1.0.0` - MCP tool invocation protocol
- **Features**:
  - Automatic peer discovery and connection management
  - Bidirectional communication streams
  - Connection persistence and recovery
  - Multi-address support with fallback mechanisms

#### 2. MCP Bridge

- **Purpose**: Enables agents to share and execute tools across the network
- **Technology**: Model Context Protocol (MCP) implementation
- **Transport Types**:
  - **SSE (Server-Sent Events)**: HTTP-based streaming for web-compatible servers
  - **stdio**: Process stdin/stdout for command-line MCP servers
- **Features**:
  - Tool discovery and caching
  - Cross-agent tool execution
  - Resource sharing and management
  - Connection pooling and retry mechanisms

#### 3. LLM Agent

- **Purpose**: Provides natural language processing and intelligent function calling
- **Technology**: OpenAI GPT-4o-mini integration
- **Capabilities**:
  - Natural language understanding and generation
  - Function calling with strict mode support
  - Local and remote tool orchestration
  - Context management and caching
- **Built-in Functions**:
  - `echo` - Message echoing with timestamps
  - `calculate` - Mathematical calculations
  - `get_current_time` - Time retrieval in various formats
  - `generate_uuid` - UUID v4 generation
  - `hash_text` - SHA256/MD5 text hashing
  - `manipulate_text` - Text operations (case, reverse, word count)
  - `get_system_info` - System and agent information
  - `execute_remote_tool` - **Key feature for P2P tool execution**

#### 4. HTTP API Layer

- **Purpose**: Provides RESTful access to all system capabilities
- **Technology**: Gin web framework with JSON APIs
- **Features**:
  - Health monitoring and status endpoints
  - P2P connection management
  - LLM chat interface
  - MCP tool invocation
  - A2A card serving

### Communication Flow

1. **Agent Initialization**: Each agent starts with its own HTTP API, P2P node, MCP bridge, and LLM agent
2. **Peer Discovery**: Agents discover each other through HTTP info exchange or P2P discovery
3. **Connection Establishment**: Secure P2P connections are established using libp2p with Noise encryption
4. **Card Exchange**: Agents exchange A2A-compliant capability cards via P2P protocols
5. **Tool Discovery**: Available tools from remote agents are discovered and cached via MCP bridge
6. **Intelligent Orchestration**: LLM agent orchestrates local and remote tool execution based on natural language requests
7. **Cross-Agent Execution**: Tools are executed on remote agents through P2P MCP bridge communication

---
