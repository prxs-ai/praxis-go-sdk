# HTTP API Reference



## Table of Contents
1. [HTTP API Reference](#http-api-reference)
2. [Health Check Endpoint](#health-check-endpoint)
3. [Agent Status Endpoint](#agent-status-endpoint)
4. [Configuration Retrieval Endpoint](#configuration-retrieval-endpoint)
5. [Execution Trigger Endpoint](#execution-trigger-endpoint)
6. [A2A Protocol Endpoints](#a2a-protocol-endpoints)
7. [P2P Communication Endpoints](#p2p-communication-endpoints)
8. [Diagnostic Endpoints](#diagnostic-endpoints)
9. [Rate Limiting Policy](#rate-limiting-policy)
10. [Error Handling](#error-handling)

## Health Check Endpoint

Provides a simple health check to verify the agent is running and responsive.

**HTTP Method**: `GET`  
**URL Path**: `/health`

### Request Headers
- None required

### Query Parameters
- None

### Request Body
- None

### Response Format (JSON)
```json
{
  "status": "healthy",
  "agent": "praxis-agent",
  "version": "1.0.0"
}
```

**Success Response**
- **200 OK**: Agent is healthy and operational

**Error Responses**
- No specific error codes for this endpoint

### curl Example
```bash
curl -X GET http://localhost:8000/health
```

**Section sources**
- [agent.go](file://internal/agent/agent.go#L717-L722)

## Agent Status Endpoint

Retrieves the agent's card information, which contains metadata about the agent's capabilities, skills, and configuration.

**HTTP Method**: `GET`  
**URL Path**: `/agent/card`

### Request Headers
- None required

### Query Parameters
- None

### Request Body
- None

### Response Format (JSON)
```json
{
  "name": "praxis-agent",
  "version": "1.0.0",
  "protocolVersion": "1.0.0",
  "url": "http://localhost:8000",
  "description": "Praxis P2P Agent with A2A and MCP support",
  "provider": {
    "name": "Praxis",
    "version": "1.0.0",
    "description": "Praxis Agent Framework"
  },
  "capabilities": {
    "streaming": true,
    "pushNotifications": false,
    "stateTransition": true
  },
  "supportedTransports": ["https", "p2p", "websocket"],
  "securitySchemes": {
    "none": {
      "type": "none"
    }
  },
  "skills": [
    {
      "id": "dsl-analysis",
      "name": "DSL Analysis",
      "description": "Analyze and execute DSL workflows with LLM orchestration",
      "tags": ["dsl", "workflow", "orchestration", "llm"]
    },
    {
      "id": "p2p-communication",
      "name": "P2P Communication",
      "description": "Communicate with other agents via P2P network using A2A protocol",
      "tags": ["p2p", "networking", "agent-to-agent", "a2a"]
    },
    {
      "id": "mcp-integration",
      "name": "MCP Integration",
      "description": "Model Context Protocol support for tool invocation and discovery",
      "tags": ["mcp", "tools", "resources", "discovery"]
    },
    {
      "id": "task-management",
      "name": "Task Management",
      "description": "Asynchronous task lifecycle management with A2A protocol",
      "tags": ["a2a", "tasks", "async", "stateful"]
    },
    {
      "id": "multi-engine",
      "name": "Multi-Engine Execution",
      "description": "Support for multiple execution engines (Dagger, Remote MCP)",
      "tags": ["dagger", "mcp", "execution", "engines"]
    }
  ],
  "metadata": {
    "implementation": "praxis-go-sdk",
    "runtime": "go",
    "engines": ["dagger", "remote-mcp"]
  }
}
```

**Success Response**
- **200 OK**: Returns agent card information

**Error Responses**
- No specific error codes for this endpoint

### curl Example
```bash
curl -X GET http://localhost:8000/agent/card
```

**Section sources**
- [agent.go](file://internal/agent/agent.go#L724-L726)
- [agent.go](file://internal/agent/agent.go#L180-L248)

## Configuration Retrieval Endpoint

The agent's configuration is accessible through the agent card endpoint. The configuration is initialized from environment variables and contains settings for ports, logging, and feature flags.

**HTTP Method**: `GET`  
**URL Path**: `/agent/card`

### Configuration Parameters
- **AGENT_NAME**: Name of the agent (default: "praxis-agent")
- **AGENT_VERSION**: Version of the agent (default: "1.0.0")
- **HTTP_PORT**: HTTP server port (default: 8000)
- **P2P_PORT**: P2P communication port (default: 4001)
- **SSE_PORT**: Server-Sent Events port (default: 8090)
- **WEBSOCKET_PORT**: WebSocket gateway port (default: 9000)
- **MCP_ENABLED**: Whether MCP is enabled (default: true)
- **LOG_LEVEL**: Logging level (default: "info")

The configuration is retrieved through the `GetConfigFromEnv()` function and used to initialize the agent.

**Section sources**
- [agent.go](file://internal/agent/agent.go#L325-L357)

## Execution Trigger Endpoint

Triggers execution of a DSL (Domain Specific Language) request or an A2A JSON-RPC request. Supports both legacy DSL format and modern A2A protocol.

**HTTP Method**: `POST`  
**URL Path**: `/execute`

### Request Headers
- Content-Type: application/json

### Query Parameters
- None

### Request Body Schema
The endpoint accepts two formats:

**Legacy DSL Format:**
```json
{
  "dsl": "analyze tweets from @elonmusk about Tesla"
}
```

**A2A JSON-RPC Format:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "message/send",
  "params": {
    "message": {
      "role": "user",
      "parts": [
        {
          "kind": "text",
          "text": "analyze tweets from @elonmusk about Tesla"
        }
      ],
      "messageId": "msg-123",
      "kind": "message"
    }
  }
}
```

### Response Format (JSON)
Returns a JSON-RPC response with task information:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "task-123",
    "contextId": "",
    "status": {
      "state": "submitted",
      "timestamp": "2023-01-01T00:00:00Z"
    },
    "history": [
      {
        "role": "user",
        "parts": [
          {
            "kind": "text",
            "text": "analyze tweets from @elonmusk about Tesla"
          }
        ],
        "messageId": "msg-123",
        "kind": "message"
      }
    ],
    "artifacts": [],
    "kind": "task"
  }
}
```

**Success Response**
- **200 OK**: Execution triggered successfully, returns task information

**Error Responses**
- **400 Bad Request**: Invalid request format or failed to read request body
- **500 Internal Server Error**: Internal processing error

### curl Examples

**Legacy DSL Request:**
```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "analyze tweets from @elonmusk about Tesla"}'
```

**A2A JSON-RPC Request:**
```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "message/send",
    "params": {
      "message": {
        "role": "user",
        "parts": [
          {
            "kind": "text",
            "text": "analyze tweets from @elonmusk about Tesla"
          }
        ],
        "messageId": "msg-123"
      }
    }
  }'
```

**Section sources**
- [agent.go](file://internal/agent/agent.go#L728-L791)

## A2A Protocol Endpoints

Endpoints that implement the Agent-to-Agent (A2A) protocol for task management and message passing.

### Message Send Endpoint

**HTTP Method**: `POST`  
**URL Path**: `/a2a/message/send`

#### Request Body Schema
```json
{
  "message": {
    "role": "user",
    "parts": [
      {
        "kind": "text",
        "text": "analyze tweets from @elonmusk about Tesla"
      }
    ],
    "messageId": "msg-123"
  }
}
```

#### Response
- **200 OK**: Returns JSON-RPC response with created task
- **400 Bad Request**: Invalid parameters

#### curl Example
```bash
curl -X POST http://localhost:8000/a2a/message/send \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "role": "user",
      "parts": [
        {
          "kind": "text",
          "text": "analyze tweets from @elonmusk about Tesla"
        }
      ],
      "messageId": "msg-123"
    }
  }'
```

### Tasks Get Endpoint

**HTTP Method**: `POST`  
**URL Path**: `/a2a/tasks/get`

#### Request Body Schema
```json
{
  "id": "task-123"
}
```

#### Response
- **200 OK**: Returns task details if found
- **400 Bad Request**: Missing or invalid task ID
- **404 Not Found**: Task not found (via A2A error code -32001)

#### curl Example
```bash
curl -X POST http://localhost:8000/a2a/tasks/get \
  -H "Content-Type: application/json" \
  -d '{"id": "task-123"}'
```

### Tasks List Endpoint

**HTTP Method**: `GET`  
**URL Path**: `/a2a/tasks`

#### Response Format
```json
{
  "tasks": [
    {
      "id": "task-123",
      "status": {
        "state": "completed"
      }
    }
  ],
  "counts": {
    "submitted": 0,
    "working": 0,
    "completed": 1,
    "failed": 0,
    "inputRequired": 0
  },
  "agent": "praxis-agent"
}
```

#### curl Example
```bash
curl -X GET http://localhost:8000/a2a/tasks
```

**Section sources**
- [agent.go](file://internal/agent/agent.go#L1347-L1562)
- [types.go](file://internal/a2a/types.go#L0-L215)

## P2P Communication Endpoints

Endpoints for peer-to-peer communication and tool invocation.

### List Peers Endpoint

**HTTP Method**: `GET`  
**URL Path**: `/peers`

#### Response Format
```json
{
  "peers": [
    {
      "id": "12D3KooW...",
      "connected": true,
      "foundAt": "2023-01-01T00:00:00Z",
      "lastSeen": "2023-01-01T00:00:00Z"
    }
  ]
}
```

#### curl Example
```bash
curl -X GET http://localhost:8000/peers
```

### Get P2P Cards Endpoint

**HTTP Method**: `GET`  
**URL Path**: `/p2p/cards`

#### Response Format
```json
{
  "cards": [
    {
      "name": "peer-agent",
      "version": "1.0.0",
      "peerId": "12D3KooW...",
      "capabilities": ["mcp", "dsl"],
      "tools": [],
      "timestamp": 1672531200
    }
  ]
}
```

#### curl Example
```bash
curl -X GET http://localhost:8000/p2p/cards
```

### Invoke P2P Tool Endpoint

**HTTP Method**: `POST`  
**URL Path**: `/p2p/tool`

#### Request Body Schema
```json
{
  "peer_id": "12D3KooW...",
  "tool_name": "twitter_scraper",
  "args": {
    "account": "elonmusk",
    "limit": 10
  }
}
```

#### Response Format
```json
{
  "result": {
    "tweets": [
      {
        "id": "123",
        "text": "Excited about the future of AI!"
      }
    ]
  }
}
```

#### curl Example
```bash
curl -X POST http://localhost:8000/p2p/tool \
  -H "Content-Type: application/json" \
  -d '{
    "peer_id": "12D3KooWS7pDhQ1...",
    "tool_name": "twitter_scraper",
    "args": {
      "account": "elonmusk",
      "limit": 10
    }
  }'
```

**Section sources**
- [agent.go](file://internal/agent/agent.go#L730-L744)
- [agent.go](file://internal/agent/agent.go#L783-L802)

## Diagnostic Endpoints

Endpoints for monitoring and debugging the agent.

### MCP Tools Endpoint

**HTTP Method**: `GET`  
**URL Path**: `/mcp/tools`

#### Response
Returns list of available MCP tools. Specific response format not available in code.

#### curl Example
```bash
curl -X GET http://localhost:8000/mcp/tools
```

### Cache Statistics Endpoint

**HTTP Method**: `GET`  
**URL Path**: `/cache/stats`

#### Response
Returns cache usage statistics. Specific response format not available in code.

#### curl Example
```bash
curl -X GET http://localhost:8000/cache/stats
```

### Clear Cache Endpoint

**HTTP Method**: `DELETE`  
**URL Path**: `/cache`

#### Response
- **200 OK**: Cache cleared successfully

#### curl Example
```bash
curl -X DELETE http://localhost:8000/cache
```

### P2P Info Endpoint

**HTTP Method**: `GET`  
**URL Path**: `/p2p/info`

#### Response
Returns P2P network information. Specific response format not available in code.

#### curl Example
```bash
curl -X GET http://localhost:8000/p2p/info
```

**Section sources**
- [agent.go](file://internal/agent/agent.go#L540-L547)

## Rate Limiting Policy

The API documentation does not specify any rate limiting policies. The agent does not implement request rate limiting based on the available code.

**Section sources**
- [agent.go](file://internal/agent/agent.go#L511-L710)

## Error Handling

The API uses standard HTTP status codes and JSON-RPC error responses for error reporting.

### Standard HTTP Error Codes
- **400 Bad Request**: Invalid request format or missing required parameters
- **401 Unauthorized**: Authentication required (not currently implemented)
- **404 Not Found**: Resource not found
- **500 Internal Server Error**: Internal server error during processing

### A2A JSON-RPC Error Codes
- **-32001 TaskNotFound**: Requested task does not exist
- **-32602 InvalidParams**: Invalid parameters in request
- **-32601 MethodNotFound**: Requested method does not exist
- **-32603 InternalError**: Internal error occurred
- **-32700 ParseError**: Invalid JSON was received by the server

When errors occur, the response follows the JSON-RPC error format:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32602,
    "message": "Invalid params format"
  }
}
```

**Section sources**
- [types.go](file://internal/a2a/types.go#L150-L159)
- [agent.go](file://internal/agent/agent.go#L1347-L1365)

**Referenced Files in This Document**   
- [agent.go](file://internal/agent/agent.go#L511-L710)
- [agent.go](file://internal/agent/agent.go#L717-L916)
- [agent.go](file://internal/agent/agent.go#L1347-L1562)
- [types.go](file://internal/a2a/types.go#L0-L215)