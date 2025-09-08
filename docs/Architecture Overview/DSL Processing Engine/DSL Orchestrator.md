# DSL Orchestrator



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
The DSL Orchestrator is a central component in the Praxis Go SDK that transforms validated Abstract Syntax Trees (ASTs) into executable workflows. It enables intelligent orchestration of complex multi-agent workflows by leveraging AI-driven planning, dependency resolution, and dynamic execution strategies. The orchestrator integrates with local execution engines (Dagger), remote MCP tools, and A2A task delegation to coordinate distributed agent interactions based on resource availability and capabilities. This document provides a comprehensive analysis of the orchestrator's architecture, execution lifecycle, integration points, and optimization strategies.

## Project Structure
The project follows a modular Go application structure with clear separation of concerns. Core orchestrator functionality resides in the `internal/dsl` and `internal/workflow` packages, while supporting components are organized into dedicated modules for agents, execution engines, event handling, and communication protocols.

```mermaid
graph TD
subgraph "Core Orchestrator"
DSL[internal/dsl]
Workflow[internal/workflow]
end
subgraph "Execution Engines"
Dagger[internal/dagger]
MCP[internal/mcp]
end
subgraph "Agent & Communication"
Agent[internal/agent]
P2P[internal/p2p]
API[internal/api]
end
subgraph "Infrastructure"
Bus[internal/bus]
LLM[internal/llm]
Config[internal/config]
end
DSL --> Workflow
DSL --> LLM
DSL --> Bus
Workflow --> Agent
Agent --> Dagger
Agent --> MCP
Agent --> Bus
Bus --> API
```

**Diagram sources**
- [agent.go](file://internal/agent/agent.go#L0-L1563)
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)

**Section sources**
- [agent.go](file://internal/agent/agent.go#L0-L1563)
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)

## Core Components
The DSL Orchestrator comprises several key components that work together to transform natural language requests into executable workflows. The `OrchestratorAnalyzer` extends the base `Analyzer` with AI-powered planning capabilities, while the `WorkflowOrchestrator` manages the execution of complex workflows with proper state tracking and error handling. The system integrates with the `EventBus` for real-time status updates and uses the `LLMClient` for intelligent agent and tool selection.

**Section sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)

## Architecture Overview
The DSL Orchestrator implements a layered architecture that separates workflow planning from execution. The system uses AI-driven analysis to convert natural language into executable workflows, then coordinates their execution across multiple agents and execution engines.

```mermaid
graph TD
User[User Request] --> LLM[LLM Client]
LLM --> Plan[Workflow Plan]
Plan --> AST[AST Generation]
AST --> Orchestrator[OrchestratorAnalyzer]
Orchestrator --> Workflow[WorkflowOrchestrator]
subgraph "Execution Engines"
Workflow --> Dagger[Dagger Engine]
Workflow --> RemoteMCP[Remote MCP]
Workflow --> A2A[A2A Task Delegation]
end
subgraph "Event System"
Orchestrator --> EventBus[EventBus]
Workflow --> EventBus
EventBus --> WebSocket[WebSocket Gateway]
EventBus --> Logger[Logger]
end
style LLM fill:#f9f,stroke:#333
style EventBus fill:#bbf,stroke:#333
```

**Diagram sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)
- [event_bus.go](file://internal/bus/event_bus.go#L0-L188)

## Detailed Component Analysis

### OrchestratorAnalyzer Analysis
The `OrchestratorAnalyzer` is responsible for transforming natural language requests into executable workflow plans using AI-driven analysis. It leverages the LLM client to generate workflow plans based on network context and available agent capabilities.

#### For Object-Oriented Components:
```mermaid
classDiagram
class OrchestratorAnalyzer {
+*Analyzer analyzer
+*bus.EventBus eventBus
+*llm.LLMClient llmClient
+AnalyzeWithOrchestration(ctx, dsl) (interface{}, error)
+buildNetworkContext() *llm.NetworkContext
+findAgentsForWorkflow(ctx, ast) []map[string]interface{}
+publishProgress(stage, message, details) void
+publishResult(command, result, workflow) void
}
class Analyzer {
+*logrus.Logger logger
+*AgentInterface agent
+AnalyzeDSL(ctx, dsl) (interface{}, error)
+tokenize(dsl) []string
+parse(tokens) (*AST, error)
}
OrchestratorAnalyzer --> Analyzer : "extends"
OrchestratorAnalyzer --> bus.EventBus : "uses"
OrchestratorAnalyzer --> llm.LLMClient : "uses"
OrchestratorAnalyzer --> AgentInterface : "depends on"
```

**Diagram sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)

**Section sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)

### WorkflowOrchestrator Analysis
The `WorkflowOrchestrator` manages the execution of complex workflows by coordinating node execution, tracking state, and handling dependencies. It implements a graph-based execution model with support for parallel and sequential execution patterns.

#### For Object-Oriented Components:
```mermaid
classDiagram
class WorkflowOrchestrator {
+*bus.EventBus eventBus
+*dsl.Analyzer dslAnalyzer
+AgentInterface agentInterface
+*logrus.Logger logger
+map[string]*WorkflowExecution workflows
+ExecuteWorkflow(ctx, workflowID, nodes, edges) error
+GetWorkflowStatus(workflowID) (map[string]interface{}, error)
+buildGraph(nodes, edges) (*WorkflowGraph, error)
+findEntryNodes(graph) []string
+executeNode(ctx, execution, nodeID) error
}
class WorkflowExecution {
+string ID
+*WorkflowGraph Graph
+string Status
+time.Time StartTime
+*time.Time EndTime
+map[string]interface{} Results
}
class WorkflowGraph {
+map[string]*Node Nodes
+[]*Edge Edges
+map[string][]string Adjacency
}
class Node {
+string ID
+string Type
+map[string]int Position
+map[string]interface{} Data
+NodeStatus Status
}
class Edge {
+string ID
+string Source
+string Target
+string Type
}
class NodeStatus {
<<enumeration>>
pending
running
success
error
}
WorkflowOrchestrator --> WorkflowExecution : "manages"
WorkflowOrchestrator --> WorkflowGraph : "uses"
WorkflowGraph --> Node : "contains"
WorkflowGraph --> Edge : "contains"
WorkflowOrchestrator --> AgentInterface : "depends on"
WorkflowOrchestrator --> bus.EventBus : "notifies"
```

**Diagram sources**
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)

**Section sources**
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)

### Execution Engine Integration
The orchestrator coordinates between multiple execution engines based on resource availability and agent capabilities. It supports local execution via Dagger, remote execution via MCP, and cross-agent delegation via A2A protocols.

#### For API/Service Components:
```mermaid
sequenceDiagram
participant UA as "User Application"
participant OA as "OrchestratorAnalyzer"
participant WO as "WorkflowOrchestrator"
participant DE as "Dagger Engine"
participant RMCP as "Remote MCP"
participant A2A as "A2A Task Manager"
UA->>OA : Natural Language Request
OA->>LLM : Generate Workflow Plan
LLM-->>OA : Structured Workflow Plan
OA->>OA : Validate Plan
OA->>WO : Execute Workflow
WO->>WO : Build Execution Graph
loop For Each Node
WO->>WO : Determine Execution Strategy
alt Local Tool Available
WO->>DE : Execute Locally
DE-->>WO : Result
else Remote Tool Required
WO->>RMCP : Execute via MCP
RMCP-->>WO : Result
else Cross-Agent Task
WO->>A2A : Delegate Task
A2A-->>WO : Task Result
end
WO->>EventBus : Publish Node Status
end
WO->>EventBus : Publish Workflow Complete
EventBus-->>UA : Real-time Updates
```

**Diagram sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)
- [engine.go](file://internal/dagger/engine.go#L0-L184)
- [client.go](file://internal/mcp/client.go#L0-L292)

**Section sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)

### Workflow Execution Lifecycle
The orchestrator manages the complete lifecycle of workflow execution from initialization to completion, with comprehensive state tracking and error handling.

#### For Complex Logic Components:
```mermaid
flowchart TD
Start([Workflow Initiated]) --> Parse["Parse DSL to AST"]
Parse --> Plan{"LLM Planning Enabled?"}
Plan --> |Yes| LLM["Generate Workflow Plan via LLM"]
Plan --> |No| Traditional["Use Traditional Parser"]
LLM --> Validate["Validate Plan"]
Traditional --> Build["Build Workflow Graph"]
Validate --> Build
Build --> Entry["Find Entry Nodes"]
Entry --> Execute["Execute Entry Nodes in Parallel"]
Execute --> Check{"All Nodes Complete?"}
Check --> |No| Next["Execute Downstream Nodes"]
Next --> Execute
Check --> |Yes| Complete["Mark Workflow Complete"]
Complete --> Aggregate["Aggregate Results"]
Aggregate --> Publish["Publish Completion Event"]
Publish --> End([Workflow Complete])
Execute --> |Error| HandleError["Handle Node Error"]
HandleError --> |Recoverable| Retry["Retry Node"]
HandleError --> |Irrecoverable| Fail["Mark Workflow Failed"]
Fail --> Rollback["Initiate Rollback"]
Rollback --> PublishError["Publish Error Event"]
PublishError --> End
```

**Diagram sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)

**Section sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)

## Dependency Analysis
The DSL Orchestrator has a well-defined dependency structure that enables modular integration with various execution engines and communication protocols. The system uses dependency injection to maintain loose coupling between components.

```mermaid
graph TD
OrchestratorAnalyzer --> EventBus
OrchestratorAnalyzer --> LLMClient
OrchestratorAnalyzer --> AgentInterface
WorkflowOrchestrator --> EventBus
WorkflowOrchestrator --> AgentInterface
AgentInterface --> DaggerEngine
AgentInterface --> MCPClient
AgentInterface --> A2ATaskManager
EventBus --> WebSocketGateway
EventBus --> Logger
style OrchestratorAnalyzer fill:#f96,stroke:#333
style WorkflowOrchestrator fill:#f96,stroke:#333
style EventBus fill:#69f,stroke:#333
```

**Diagram sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)
- [event_bus.go](file://internal/bus/event_bus.go#L0-L188)
- [agent.go](file://internal/agent/agent.go#L0-L1563)

**Section sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)

## Performance Considerations
The orchestrator implements several performance optimizations to ensure efficient workflow execution:

- **Step Batching**: Multiple independent nodes are executed in parallel to maximize throughput
- **Resource Pooling**: Execution engines maintain persistent connections to reduce initialization overhead
- **Timeout Management**: Configurable timeouts prevent hanging operations from blocking workflow progress
- **Caching**: Results are cached to avoid redundant computation for identical operations
- **Lazy Initialization**: Execution engines are initialized on first use to reduce startup time

The system also implements backpressure mechanisms through the EventBus's bounded channel to prevent overwhelming downstream components during high-load scenarios.

## Troubleshooting Guide
Common issues and their solutions when working with the DSL Orchestrator:

**Section sources**
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)
- [event_bus.go](file://internal/bus/event_bus.go#L0-L188)

### Workflow Not Starting
- **Symptom**: Workflow remains in "pending" state
- **Cause**: No entry nodes detected in the workflow graph
- **Solution**: Ensure at least one node has no incoming edges, or verify the graph structure

### Node Execution Failures
- **Symptom**: Individual nodes fail with execution errors
- **Cause**: Tool not available, invalid parameters, or execution engine issues
- **Solution**: Check agent capabilities, validate input parameters, and verify execution engine status

### Event Bus Overload
- **Symptom**: Event channel full warnings in logs
- **Cause**: High event volume overwhelming the EventBus
- **Solution**: Increase event channel buffer size or optimize event publishing frequency

### LLM Planning Failures
- **Symptom**: "LLM analysis failed" errors
- **Cause**: LLM service unavailable or invalid network context
- **Solution**: Verify LLM service connectivity and check peer discovery status

## Conclusion
The DSL Orchestrator provides a robust framework for transforming natural language requests into executable workflows across distributed agent networks. By leveraging AI-driven planning, the system intelligently selects appropriate agents and tools based on capabilities and availability. The modular architecture enables seamless integration with various execution engines and communication protocols, while the EventBus provides real-time visibility into workflow execution. Future enhancements could include more sophisticated error recovery mechanisms, enhanced parallelization strategies, and improved resource optimization algorithms.

**Referenced Files in This Document**   
- [agent.go](file://internal/agent/agent.go#L0-L1563)
- [orchestrator.go](file://internal/dsl/orchestrator.go#L0-L1172)
- [workflow_orchestrator.go](file://internal/workflow/workflow_orchestrator.go#L0-L517)
- [event_bus.go](file://internal/bus/event_bus.go#L0-L188)
- [engine.go](file://internal/dagger/engine.go#L0-L184)
- [client.go](file://internal/mcp/client.go#L0-L292)