# A2A (Agent2Agent) Implementation Analysis

## Overview
The A2A implementation in the Praxis Go SDK provides full Agent2Agent protocol compliance with JSON-RPC 2.0, asynchronous task processing, and backward compatibility with legacy DSL requests.

## 🏗️ Architecture Components

### 1. Core Data Models (`internal/a2a/types.go`)

#### Task Structure
```go
type Task struct {
    ID        string      `json:"id"`
    ContextID string      `json:"contextId"`
    Status    TaskStatus  `json:"status"`
    History   []Message   `json:"history,omitempty"`
    Artifacts []Artifact  `json:"artifacts,omitempty"`
    Metadata  interface{} `json:"metadata,omitempty"`
    Kind      string      `json:"kind"` // always "task"
}
```

#### Task Status States
- `submitted` - Task created and queued
- `working` - Agent is processing the task
- `completed` - Task finished successfully
- `failed` - Task encountered an error
- `input-required` - Task needs additional user input

#### Message Structure (A2A Compliant)
```go
type Message struct {
    Role      string `json:"role"` // "user" or "agent"
    Parts     []Part `json:"parts"`
    MessageID string `json:"messageId"`
    TaskID    string `json:"taskId,omitempty"`
    ContextID string `json:"contextId,omitempty"`
    Kind      string `json:"kind"` // always "message"
}
```

#### JSON-RPC 2.0 Support
- Full JSON-RPC 2.0 request/response format
- Standard error codes (-32001 to -32700)
- Method dispatch: `message/send`, `tasks/get`

### 2. Task Management (`internal/a2a/task_manager.go`)

#### Key Features
- **Thread-safe task storage** with RWMutex
- **Event-driven architecture** with EventBus integration
- **Task lifecycle management** with state transitions
- **Artifact management** for task outputs
- **Cleanup capabilities** for completed tasks

#### Core Methods
```go
func (tm *TaskManager) CreateTask(msg Message) *Task
func (tm *TaskManager) UpdateTaskStatus(id, state string, agentMessage *Message)
func (tm *TaskManager) AddArtifactToTask(id string, artifact Artifact)
func (tm *TaskManager) GetTask(id string) (*Task, bool)
```

### 3. Agent Integration (`internal/agent/agent.go`)

#### A2A Protocol Handler
```go
func (a *PraxisAgent) DispatchA2ARequest(req a2a.JSONRPCRequest) a2a.JSONRPCResponse
```

#### HTTP Endpoints
- `POST /execute` - Main endpoint (handles both A2A JSON-RPC and legacy DSL)
- `POST /a2a/message/send` - Direct A2A message endpoint
- `POST /a2a/tasks/get` - Direct A2A task status endpoint
- `GET /a2a/tasks` - Task listing for debugging
- `GET /agent/card` - A2A agent specification

#### Asynchronous Processing
```go
func (a *PraxisAgent) processTask(ctx context.Context, task *a2a.Task)
```
- Tasks processed in separate goroutines
- Integration with orchestrator analyzer for complex workflows
- Automatic artifact creation for task outputs

## 🔄 Request Flow

### 1. A2A Message/Send Flow
```
Client → POST /execute (JSON-RPC) → DispatchA2ARequest → handleMessageSend → CreateTask → processTask (async) → Update Status
```

### 2. Legacy DSL Compatibility
```
Client → POST /execute (DSL) → Convert to A2A Message → DispatchA2ARequest → handleMessageSend → CreateTask → processTask
```

### 3. Task Status Polling
```
Client → POST /execute (tasks/get) → DispatchA2ARequest → handleTasksGet → GetTask → Return Current Status
```

## 📡 JSON-RPC Protocol

### Message/Send Request
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
          "text": "Hello, analyze this message."
        }
      ],
      "messageId": "test-001",
      "kind": "message"
    }
  }
}
```

### Task Creation Response
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "uuid-task-id",
    "contextId": "uuid-context-id",
    "status": {
      "state": "submitted",
      "timestamp": "2025-09-01T15:30:00Z"
    },
    "history": [...],
    "artifacts": [],
    "kind": "task"
  }
}
```

### Tasks/Get Request
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tasks/get",
  "params": {
    "id": "uuid-task-id"
  }
}
```

## 🎫 A2A Agent Card

The agent card follows the A2A specification with:
- `protocolVersion`: "1.0.0"
- `capabilities`: Streaming, StateTransition
- `skills`: DSL analysis, P2P communication, MCP integration, task management
- `supportedTransports`: ["https", "p2p", "websocket"]

## 🔧 Integration Points

### Event Bus Integration
- Task creation events (`bus.EventTaskCreated`)
- Status update events (`bus.EventTaskStatusUpdate`)
- Artifact creation events (`bus.EventArtifactAdded`)

### LLM Integration
- Tasks processed through `orchestratorAnalyzer`
- Support for complex DSL workflow orchestration
- Automatic response generation

### P2P Protocol Support
- A2A protocol can work over P2P networks
- Agent card exchange for capability discovery
- Remote task execution capabilities

## 🧪 Testing Strategy

### Unit Tests Needed
- Task state transitions
- JSON-RPC request/response parsing
- Message conversion (legacy DSL → A2A)
- TaskManager thread safety

### Integration Tests Created
1. **Health Check**: Verify agent startup
2. **A2A Message/Send**: Test JSON-RPC message creation
3. **Legacy DSL**: Test backward compatibility
4. **Task Status Polling**: Test async status updates
5. **Direct Endpoints**: Test /a2a/* endpoints
6. **Async Workflow**: Test full task lifecycle

### Test Files Created
- `test_a2a.sh` - Full automated test suite
- `test_demo_safe.sh` - Safe demo without starting agent
- `test_curl_commands.md` - Manual testing documentation

## 🎯 Key Features Implemented

### ✅ A2A Protocol Compliance
- Complete JSON-RPC 2.0 implementation
- Proper A2A data structures (Task, Message, Part, Artifact)
- Standard error handling and codes

### ✅ Task Lifecycle Management
- Asynchronous task processing
- State transitions with timestamps
- Event-driven updates
- Artifact collection

### ✅ Backward Compatibility
- Automatic legacy DSL → A2A conversion
- Transparent protocol upgrade
- No breaking changes to existing API

### ✅ Extensibility
- Event bus integration for monitoring
- P2P protocol support for distributed tasks
- Plugin architecture for execution engines

## 🚀 Production Readiness

### Performance Considerations
- Tasks processed asynchronously to avoid blocking
- Thread-safe operations with proper locking
- Event bus for decoupled monitoring
- Configurable cleanup for completed tasks

### Error Handling
- Comprehensive JSON-RPC error codes
- Graceful degradation for missing components
- Detailed logging for debugging

### Monitoring & Debugging
- Task count metrics
- Status transition logging
- Event bus integration for external monitoring
- Debug endpoints for task listing

## 📝 Testing Instructions

### Prerequisites
```bash
export OPENAI_API_KEY=your_key_here
go build -o bin/agent ./agent/main.go
```

### Run Tests
```bash
# Full test suite
./test_a2a.sh

# Individual tests
./test_a2a.sh --message    # Test message sending
./test_a2a.sh --legacy     # Test legacy DSL
./test_a2a.sh --async      # Test async workflow
```

### Manual Testing
```bash
# Start agent
./bin/agent -config=configs/agent.yaml

# Test A2A message
curl -X POST http://localhost:8000/execute -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"text","text":"Hello"}],"messageId":"test-001","kind":"message"}}}'

# Check task status
curl -X POST http://localhost:8000/execute -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"TASK_ID"}}'
```

This implementation provides a complete, production-ready A2A protocol integration with the Praxis agent framework.
