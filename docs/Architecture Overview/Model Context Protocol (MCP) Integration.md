# Model Context Protocol (MCP) Integration



## Table of Contents
1. [Introduction](#introduction)
2. [Project Structure](#project-structure)
3. [Core Components](#core-components)
4. [Architecture Overview](#architecture-overview)
5. [Detailed Component Analysis](#detailed-component-analysis)
6. [Dependency Analysis](#dependency-analysis)
7. [Performance Considerations](#performance-considerations)
8. [Troubleshooting Guide](#troubleshooting-guide)
9. [Conclusion](#conclusion)

## Introduction
The Model Context Protocol (MCP) subsystem enables distributed agents to expose and discover tools across multiple transport mechanisms. This documentation details the implementation of MCP servers that expose tools via Server-Sent Events (SSE), HTTP, and STDIO transports. It explains the client-side discovery mechanism for locating remote MCP servers and retrieving tool manifests. The adapter pattern bridges local execution engines with external MCP endpoints, enabling seamless integration. The system supports dynamic tool registration and capability exposure, integrates with LLM clients for natural language tool invocation, and provides comprehensive configuration options for transport selection, timeout settings, and error handling strategies in unreliable network environments.

## Project Structure
The project follows a modular architecture with clear separation of concerns. The MCP subsystem is primarily located in the `internal/mcp` directory, with supporting components in `internal/llm`, `internal/p2p`, and `internal/agent`. Configuration files in the `configs` directory define agent behavior and discovery settings, while examples in the `examples` directory demonstrate practical implementations of MCP servers.

```mermaid
graph TB
subgraph "Core MCP"
server[server.go]
client[client.go]
discovery[discovery.go]
transport[transport.go]
remote_engine[remote_engine.go]
end
subgraph "Integration"
mcp_tool[mcp_tool.go]
llm_client[llm/client.go]
bridge[bridge.go]
protocol[protocol.go]
end
subgraph "Agent"
agent[agent.go]
orchestrator[orchestrator.go]
end
subgraph "Configuration"
config[agent_with_mcp_discovery.yaml]
end
subgraph "Examples"
mcp_server[mcp-server.go]
end
server --> client
client --> discovery
transport --> remote_engine
mcp_tool --> llm_client
agent --> server
agent --> transport
agent --> mcp_tool
orchestrator --> mcp_tool
bridge --> protocol
agent --> bridge
config --> agent
mcp_server --> server
```

**Diagram sources**
- [server.go](file://internal/mcp/server.go#L0-L327)
- [client.go](file://internal/mcp/client.go#L0-L292)
- [discovery.go](file://internal/mcp/discovery.go#L0-L226)
- [transport.go](file://internal/mcp/transport.go#L0-L295)
- [remote_engine.go](file://internal/mcp/remote_engine.go#L0-L53)
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [client.go](file://internal/llm/client.go#L0-L419)
- [bridge.go](file://internal/p2p/bridge.go#L0-L472)
- [protocol.go](file://internal/p2p/protocol.go#L0-L536)
- [agent.go](file://internal/agent/agent.go#L0-L1563)
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [agent_with_mcp_discovery.yaml](file://configs/agent_with_mcp_discovery.yaml#L0-L79)
- [mcp-server.go](file://examples/mcp-server.go#L0-L591)

**Section sources**
- [server.go](file://internal/mcp/server.go#L0-L327)
- [client.go](file://internal/mcp/client.go#L0-L292)
- [discovery.go](file://internal/mcp/discovery.go#L0-L226)
- [transport.go](file://internal/mcp/transport.go#L0-L295)
- [remote_engine.go](file://internal/mcp/remote_engine.go#L0-L53)
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [client.go](file://internal/llm/client.go#L0-L419)
- [bridge.go](file://internal/p2p/bridge.go#L0-L472)
- [protocol.go](file://internal/p2p/protocol.go#L0-L536)
- [agent.go](file://internal/agent/agent.go#L0-L1563)
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [agent_with_mcp_discovery.yaml](file://configs/agent_with_mcp_discovery.yaml#L0-L79)
- [mcp-server.go](file://examples/mcp-server.go#L0-L591)

## Core Components
The MCP subsystem consists of several core components that work together to enable distributed tool execution and discovery. The `MCPServerWrapper` in `server.go` manages tool registration and exposes endpoints via SSE, HTTP, and STDIO transports. The `MCPClientWrapper` in `client.go` handles communication with remote servers and provides methods for listing and calling tools. The `ToolDiscoveryService` in `discovery.go` implements the discovery mechanism for locating remote MCP servers and retrieving their tool manifests. The `TransportManager` in `transport.go` manages multiple client connections and provides resilient communication with remote endpoints.

**Section sources**
- [server.go](file://internal/mcp/server.go#L0-L327)
- [client.go](file://internal/mcp/client.go#L0-L292)
- [discovery.go](file://internal/mcp/discovery.go#L0-L226)
- [transport.go](file://internal/mcp/transport.go#L0-L295)

## Architecture Overview
The MCP architecture enables distributed agents to expose, discover, and invoke tools across a P2P network. Agents can host MCP servers that expose local tools via multiple transport mechanisms. Other agents can discover these servers and their available tools, then invoke them remotely. The system integrates with LLM clients to enable natural language invocation of tools, where the LLM determines the appropriate tool and agent for a given request.

```mermaid
graph TD
subgraph "Local Agent"
A[Agent]
B[MCP Server]
C[LLM Client]
D[Execution Engines]
end
subgraph "Remote Agent"
E[Agent]
F[MCP Server]
G[Tools]
end
A --> B
A --> C
A --> D
B --> |SSE/HTTP/STDIO| F
C --> |Natural Language| D
D --> |Remote MCP Engine| B
E --> F
F --> G
B --> |Discovery| F
B --> |Tool Invocation| G
```

**Diagram sources**
- [server.go](file://internal/mcp/server.go#L0-L327)
- [client.go](file://internal/mcp/client.go#L0-L292)
- [discovery.go](file://internal/mcp/discovery.go#L0-L226)
- [transport.go](file://internal/mcp/transport.go#L0-L295)
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [client.go](file://internal/llm/client.go#L0-L419)
- [agent.go](file://internal/agent/agent.go#L0-L1563)

## Detailed Component Analysis

### MCP Server Implementation
The MCP server implementation in `server.go` exposes tools via SSE, HTTP, and STDIO transports. It uses the `MCPServerWrapper` struct to manage the underlying MCP server and track registered tools and handlers. The server supports dynamic tool registration through the `AddTool` method, which stores both the tool specification and its handler function.

```mermaid
classDiagram
class MCPServerWrapper {
+*server.MCPServer : server
+*server.SSEServer : sseServer
+*server.StreamableHTTPServer : httpServer
+*logrus.Logger : logger
+string : agentName
+string : agentVersion
+map[string]server.ToolHandlerFunc : toolHandlers
+[]mcpTypes.Tool : registeredTools
+NewMCPServer(config: ServerConfig) : MCPServerWrapper
+AddTool(tool: Tool, handler: ToolHandlerFunc)
+FindToolHandler(toolName: string) : ToolHandlerFunc
+HasTool(toolName: string) : bool
+GetRegisteredTools() : List<Tool>
+StartSSE(port: string) : error
+StartHTTP(port: string) : error
+StartSTDIO() : error
+Shutdown(ctx: Context) : error
}
class ServerConfig {
+string : Name
+string : Version
+TransportType : Transport
+string : Port
+*logrus.Logger : Logger
+bool : EnableTools
+bool : EnableResources
+bool : EnablePrompts
}
class TransportType {
+TransportSTDIO
+TransportSSE
+TransportHTTP
}
MCPServerWrapper --> ServerConfig : "uses"
MCPServerWrapper --> TransportType : "references"
```

**Diagram sources**
- [server.go](file://internal/mcp/server.go#L0-L327)

**Section sources**
- [server.go](file://internal/mcp/server.go#L0-L327)

### Client-Side Discovery Mechanism
The client-side discovery mechanism in `discovery.go` enables agents to locate remote MCP servers and retrieve their tool manifests. The `ToolDiscoveryService` makes HTTP requests to server endpoints, initializes connections, and lists available tools. It parses the responses to extract tool information and returns a list of discovered tools with their specifications.

```mermaid
sequenceDiagram
participant Client as "ToolDiscoveryService"
participant Server as "MCP Server"
Client->>Server : POST /mcp
Note over Client,Server : Initialize connection
Server-->>Client : {result : {serverInfo : {...}}}
Client->>Server : POST /mcp
Note over Client,Server : List available tools
Server-->>Client : {result : {tools : [...]}}
Client->>Client : Parse tool specifications
Client->>Client : Return discovered tools
```

**Diagram sources**
- [discovery.go](file://internal/mcp/discovery.go#L0-L226)

**Section sources**
- [discovery.go](file://internal/mcp/discovery.go#L0-L226)

### Adapter Pattern for External Endpoints
The adapter pattern in `remote_engine.go` bridges local execution engines with external MCP endpoints. The `RemoteMCPEngine` uses a `TransportManager` to manage connections to remote servers and route tool calls to the appropriate endpoint. This allows local tools to be implemented as proxies to remote MCP servers.

```mermaid
classDiagram
class RemoteMCPEngine {
+*TransportManager : transportManager
+NewRemoteMCPEngine(tm: TransportManager) : RemoteMCPEngine
+Execute(ctx: Context, contract: ToolContract, args: Map<string, interface>) : (string, error)
}
class TransportManager {
+map[string]*MCPClientWrapper : clients
+*ClientFactory : factory
+*logrus.Logger : logger
+RegisterSSEEndpoint(name: string, url: string, headers: Map<string, string>)
+GetClient(name: string) : (MCPClientWrapper, error)
+CallRemoteTool(ctx: Context, clientName: string, toolName: string, args: Map<string, interface>) : (CallToolResult, error)
}
class ExecutionEngine {
<<interface>>
+Execute(ctx: Context, contract: ToolContract, args: Map<string, interface>) : (string, error)
}
RemoteMCPEngine --> TransportManager : "uses"
RemoteMCPEngine --|> ExecutionEngine : "implements"
```

**Diagram sources**
- [remote_engine.go](file://internal/mcp/remote_engine.go#L0-L53)
- [transport.go](file://internal/mcp/transport.go#L0-L295)

**Section sources**
- [remote_engine.go](file://internal/mcp/remote_engine.go#L0-L53)
- [transport.go](file://internal/mcp/transport.go#L0-L295)

### Tool Registration and Dynamic Capability Exposure
The tool registration process in `server.go` allows for dynamic capability exposure. Tools are registered with their specifications and handler functions, which are stored in maps for quick lookup. The server tracks all registered tools and their specifications, enabling discovery and invocation.

```mermaid
flowchart TD
Start([AddTool]) --> ValidateInput["Validate Tool Specification"]
ValidateInput --> StoreHandler["Store Handler in toolHandlers map"]
StoreHandler --> StoreSpec["Store Tool Spec in registeredTools slice"]
StoreSpec --> LogSuccess["Log Tool Addition"]
LogSuccess --> End([Tool Registered])
```

**Diagram sources**
- [server.go](file://internal/mcp/server.go#L0-L327)

**Section sources**
- [server.go](file://internal/mcp/server.go#L0-L327)

### LLM Integration for Natural Language Invocation
The integration with LLM clients in `mcp_tool.go` enables natural language tool invocation. The `LLMWorkflowTool` converts natural language requests into executable workflow plans using an LLM. It analyzes the current network context to determine the best agents and tools for each task.

```mermaid
sequenceDiagram
participant User as "User"
participant LLMTool as "LLMWorkflowTool"
participant LLMClient as "LLMClient"
participant Network as "Network Context"
User->>LLMTool : Natural language request
LLMTool->>LLMTool : Check LLM availability
LLMTool->>LLMTool : Build network context
LLMTool->>LLMClient : Generate workflow plan
LLMClient->>LLMClient : Call LLM API
LLMClient-->>LLMTool : Return workflow plan
LLMTool->>LLMTool : Validate plan
LLMTool->>LLMTool : Convert to DSL commands
LLMTool-->>User : Return executable workflow
```

**Diagram sources**
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [client.go](file://internal/llm/client.go#L0-L419)

**Section sources**
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [client.go](file://internal/llm/client.go#L0-L419)

## Dependency Analysis
The MCP subsystem has a well-defined dependency structure that enables modularity and extensibility. The core components depend on the `mcp-go` library for protocol implementation, while integration components depend on the core MCP classes. The agent orchestrates all components, creating a dependency on both the MCP subsystem and the P2P networking layer.

```mermaid
graph TD
A[agent.go] --> B[server.go]
A --> C[transport.go]
A --> D[mcp_tool.go]
A --> E[bridge.go]
B --> F[mcp-go/server]
C --> G[mcp-go/client]
D --> H[llm/client.go]
E --> I[protocol.go]
H --> J[mcp-go/client]
B --> K[logrus]
C --> K
D --> K
E --> K
A --> K
```

**Diagram sources**
- [agent.go](file://internal/agent/agent.go#L0-L1563)
- [server.go](file://internal/mcp/server.go#L0-L327)
- [transport.go](file://internal/mcp/transport.go#L0-L295)
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [bridge.go](file://internal/p2p/bridge.go#L0-L472)
- [protocol.go](file://internal/p2p/protocol.go#L0-L536)
- [client.go](file://internal/llm/client.go#L0-L419)

**Section sources**
- [agent.go](file://internal/agent/agent.go#L0-L1563)
- [server.go](file://internal/mcp/server.go#L0-L327)
- [transport.go](file://internal/mcp/transport.go#L0-L295)
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [bridge.go](file://internal/p2p/bridge.go#L0-L472)
- [protocol.go](file://internal/p2p/protocol.go#L0-L536)
- [client.go](file://internal/llm/client.go#L0-L419)

## Performance Considerations
The MCP subsystem includes several performance optimizations and configuration options. The `agent_with_mcp_discovery.yaml` configuration file defines limits for concurrent requests, request timeouts, and response sizes. The `ResilientSSEClient` in `transport.go` implements reconnection logic with exponential backoff to handle unreliable networks. The system uses connection pooling and caching to improve performance in high-throughput scenarios.

**Section sources**
- [agent_with_mcp_discovery.yaml](file://configs/agent_with_mcp_discovery.yaml#L0-L79)
- [transport.go](file://internal/mcp/transport.go#L0-L295)

## Troubleshooting Guide
Common issues with the MCP subsystem include connection failures, tool not found errors, and LLM integration problems. Connection failures can be diagnosed by checking the server URL and network connectivity. Tool not found errors may indicate a registration issue or mismatched tool names. LLM integration problems can be caused by missing API keys or network issues.

**Section sources**
- [client.go](file://internal/mcp/client.go#L0-L292)
- [discovery.go](file://internal/mcp/discovery.go#L0-L226)
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [client.go](file://internal/llm/client.go#L0-L419)

## Conclusion
The Model Context Protocol subsystem provides a robust framework for distributed tool execution and discovery. It supports multiple transport mechanisms, enables dynamic capability exposure, and integrates with LLM clients for natural language invocation. The adapter pattern allows seamless bridging between local execution engines and external MCP endpoints. The system is configurable for various network conditions and provides comprehensive error handling for unreliable environments. This architecture enables flexible and scalable agent-to-agent communication in distributed systems.

**Referenced Files in This Document**
- [server.go](file://internal/mcp/server.go#L0-L327)
- [client.go](file://internal/mcp/client.go#L0-L292)
- [discovery.go](file://internal/mcp/discovery.go#L0-L226)
- [transport.go](file://internal/mcp/transport.go#L0-L295)
- [remote_engine.go](file://internal/mcp/remote_engine.go#L0-L53)
- [mcp_tool.go](file://internal/llm/mcp_tool.go#L0-L362)
- [agent.go](file://internal/agent/agent.go#L0-L1563)
- [bridge.go](file://internal/p2p/bridge.go#L0-L472)
- [protocol.go](file://internal/p2p/protocol.go#L0-L536)
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [client.go](file://internal/llm/client.go#L0-L419)
- [agent_with_mcp_discovery.yaml](file://configs/agent_with_mcp_discovery.yaml#L0-L79)
- [mcp-server.go](file://examples/mcp-server.go#L0-L591)
