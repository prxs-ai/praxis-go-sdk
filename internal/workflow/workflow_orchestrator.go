package workflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/praxis/praxis-go-sdk/internal/dsl"
	"github.com/sirupsen/logrus"
)

// NodeStatus represents the status of a workflow node
type NodeStatus string

const (
	NodeStatusPending NodeStatus = "pending"
	NodeStatusRunning NodeStatus = "running"
	NodeStatusSuccess NodeStatus = "success"
	NodeStatusError   NodeStatus = "error"
)

// Node represents a workflow node
type Node struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Position map[string]int         `json:"position"`
	Data     map[string]interface{} `json:"data"`
	Status   NodeStatus             `json:"status"`
}

// Edge represents a connection between nodes
type Edge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// WorkflowGraph represents the workflow structure
type WorkflowGraph struct {
	Nodes map[string]*Node
	Edges []*Edge
	// Adjacency list for quick traversal
	Adjacency map[string][]string
}

// WorkflowOrchestrator manages workflow execution
type WorkflowOrchestrator struct {
	eventBus       *bus.EventBus
	dslAnalyzer    *dsl.Analyzer
	agentInterface AgentInterface
	logger         *logrus.Logger
	mu             sync.RWMutex
	workflows      map[string]*WorkflowExecution
}

// WorkflowExecution represents an active workflow execution
type WorkflowExecution struct {
	ID        string
	Graph     *WorkflowGraph
	Status    string
	StartTime time.Time
	EndTime   *time.Time
	Results   map[string]interface{}
	mu        sync.RWMutex
}

// AgentInterface provides access to agent functionality
type AgentInterface interface {
	HasLocalTool(toolName string) bool
	ExecuteLocalTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error)
	FindAgentWithTool(toolName string) (string, error)
	ExecuteRemoteTool(ctx context.Context, peerID string, toolName string, args map[string]interface{}) (interface{}, error)
}

// NewWorkflowOrchestrator creates a new workflow orchestrator
func NewWorkflowOrchestrator(eventBus *bus.EventBus, dslAnalyzer *dsl.Analyzer, logger *logrus.Logger) *WorkflowOrchestrator {
	return &WorkflowOrchestrator{
		eventBus:    eventBus,
		dslAnalyzer: dslAnalyzer,
		logger:      logger,
		workflows:   make(map[string]*WorkflowExecution),
	}
}

// SetAgentInterface sets the agent interface
func (wo *WorkflowOrchestrator) SetAgentInterface(agent AgentInterface) {
	wo.agentInterface = agent
}

// ExecuteWorkflow executes a workflow from nodes and edges
func (wo *WorkflowOrchestrator) ExecuteWorkflow(ctx context.Context, workflowID string, nodes []interface{}, edges []interface{}) error {
	wo.logger.Infof("Starting workflow execution: %s", workflowID)

	// Build workflow graph
	graph, err := wo.buildGraph(nodes, edges)
	if err != nil {
		wo.eventBus.PublishWorkflowError(workflowID, fmt.Sprintf("Failed to build workflow graph: %v", err), "")
		return err
	}

	// Create workflow execution
	execution := &WorkflowExecution{
		ID:        workflowID,
		Graph:     graph,
		Status:    "running",
		StartTime: time.Now(),
		Results:   make(map[string]interface{}),
	}

	wo.mu.Lock()
	wo.workflows[workflowID] = execution
	wo.mu.Unlock()

	// Log workflow start
	wo.eventBus.PublishWorkflowLog(workflowID, "info", "Starting workflow execution", "orchestrator", "")

	// Find entry nodes (nodes with no incoming edges)
	entryNodes := wo.findEntryNodes(graph)
	if len(entryNodes) == 0 {
		err := fmt.Errorf("no entry nodes found in workflow")
		wo.eventBus.PublishWorkflowError(workflowID, err.Error(), "")
		return err
	}

	// Execute workflow starting from entry nodes
	var wg sync.WaitGroup
	errorChan := make(chan error, len(entryNodes))

	for _, nodeID := range entryNodes {
		wg.Add(1)
		go func(nID string) {
			defer wg.Done()
			if err := wo.executeNode(ctx, execution, nID); err != nil {
				errorChan <- err
			}
		}(nodeID)
	}

	// Wait for all entry nodes to complete
	wg.Wait()
	close(errorChan)

	// Check for errors
	for err := range errorChan {
		if err != nil {
			execution.Status = "error"
			wo.eventBus.PublishWorkflowError(workflowID, err.Error(), "")
			return err
		}
	}

	// Mark workflow as completed
	execution.Status = "completed"
	now := time.Now()
	execution.EndTime = &now

	// Publish completion event
	wo.eventBus.PublishWorkflowComplete(workflowID, map[string]interface{}{
		"message":   "Workflow completed successfully",
		"duration":  execution.EndTime.Sub(execution.StartTime).String(),
		"nodeCount": len(graph.Nodes),
	})

	wo.logger.Infof("Workflow %s completed successfully", workflowID)
	return nil
}

// buildGraph builds a workflow graph from nodes and edges
func (wo *WorkflowOrchestrator) buildGraph(nodes []interface{}, edges []interface{}) (*WorkflowGraph, error) {
	graph := &WorkflowGraph{
		Nodes:     make(map[string]*Node),
		Edges:     make([]*Edge, 0),
		Adjacency: make(map[string][]string),
	}

	// Parse nodes
	for _, nodeData := range nodes {
		nodeMap, ok := nodeData.(map[string]interface{})
		if !ok {
			continue
		}

		node := &Node{
			Status: NodeStatusPending,
		}

		if id, ok := nodeMap["id"].(string); ok {
			node.ID = id
		}
		if nodeType, ok := nodeMap["type"].(string); ok {
			node.Type = nodeType
		}
		if position, ok := nodeMap["position"].(map[string]interface{}); ok {
			node.Position = make(map[string]int)
			if x, ok := position["x"].(float64); ok {
				node.Position["x"] = int(x)
			}
			if y, ok := position["y"].(float64); ok {
				node.Position["y"] = int(y)
			}
		}
		if data, ok := nodeMap["data"].(map[string]interface{}); ok {
			node.Data = data
		}

		graph.Nodes[node.ID] = node
	}

	// Parse edges
	for _, edgeData := range edges {
		edgeMap, ok := edgeData.(map[string]interface{})
		if !ok {
			continue
		}

		edge := &Edge{}
		if id, ok := edgeMap["id"].(string); ok {
			edge.ID = id
		}
		if source, ok := edgeMap["source"].(string); ok {
			edge.Source = source
		}
		if target, ok := edgeMap["target"].(string); ok {
			edge.Target = target
		}
		if edgeType, ok := edgeMap["type"].(string); ok {
			edge.Type = edgeType
		}

		graph.Edges = append(graph.Edges, edge)

		// Build adjacency list
		if graph.Adjacency[edge.Source] == nil {
			graph.Adjacency[edge.Source] = make([]string, 0)
		}
		graph.Adjacency[edge.Source] = append(graph.Adjacency[edge.Source], edge.Target)
	}

	return graph, nil
}

// findEntryNodes finds nodes with no incoming edges
func (wo *WorkflowOrchestrator) findEntryNodes(graph *WorkflowGraph) []string {
	hasIncoming := make(map[string]bool)

	// Mark all nodes that have incoming edges
	for _, edge := range graph.Edges {
		hasIncoming[edge.Target] = true
	}

	// Find nodes without incoming edges
	entryNodes := make([]string, 0)
	for nodeID := range graph.Nodes {
		if !hasIncoming[nodeID] {
			entryNodes = append(entryNodes, nodeID)
		}
	}

	// Debug logging
	wo.logger.Infof("🔍 Workflow analysis: %d nodes, %d edges", len(graph.Nodes), len(graph.Edges))
	wo.logger.Infof("🎯 Found %d entry nodes: %v", len(entryNodes), entryNodes)

	// If no entry nodes found and we have nodes, make first agent node an entry point
	if len(entryNodes) == 0 && len(graph.Nodes) > 0 {
		for nodeID, node := range graph.Nodes {
			if nodeType, ok := node.Data["type"].(string); ok && nodeType == "agent" {
				entryNodes = append(entryNodes, nodeID)
				wo.logger.Infof("🚀 Auto-selected entry node: %s (type: %s)", nodeID, nodeType)
				break
			}
		}

		// If still no entry nodes, just take the first node
		if len(entryNodes) == 0 {
			for nodeID := range graph.Nodes {
				entryNodes = append(entryNodes, nodeID)
				wo.logger.Infof("🚀 Auto-selected first node as entry: %s", nodeID)
				break
			}
		}
	}

	return entryNodes
}

// executeNode executes a single node in the workflow
func (wo *WorkflowOrchestrator) executeNode(ctx context.Context, execution *WorkflowExecution, nodeID string) error {
	node, exists := execution.Graph.Nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Prevent cycles: skip if node is already running or completed
	if node.Status == NodeStatusRunning || node.Status == NodeStatusSuccess {
		return nil
	}

	// Update node status to running
	node.Status = NodeStatusRunning
	wo.eventBus.PublishNodeStatusUpdate(execution.ID, nodeID, string(NodeStatusRunning))
	wo.eventBus.PublishWorkflowLog(execution.ID, "info", fmt.Sprintf("Executing node: %s", nodeID), "orchestrator", nodeID)

	// Execute node based on type
	var result interface{}
	var err error

	switch node.Type {
	case "orchestrator":
		result, err = wo.executeOrchestratorNode(ctx, node)
	case "executor":
		result, err = wo.executeExecutorNode(ctx, node)
	case "tool":
		result, err = wo.executeToolNode(ctx, node)
	case "agent":
		result, err = wo.executeAgentNode(ctx, node)
	default:
		result, err = wo.executeGenericNode(ctx, node)
	}

	if err != nil {
		node.Status = NodeStatusError
		wo.eventBus.PublishNodeStatusUpdate(execution.ID, nodeID, string(NodeStatusError))
		wo.eventBus.PublishWorkflowLog(execution.ID, "error", fmt.Sprintf("Node %s failed: %v", nodeID, err), "orchestrator", nodeID)
		return err
	}

	// Store result
	execution.mu.Lock()
	execution.Results[nodeID] = result
	execution.mu.Unlock()

	// Update node status to success
	node.Status = NodeStatusSuccess
	wo.eventBus.PublishNodeStatusUpdate(execution.ID, nodeID, string(NodeStatusSuccess))
	wo.eventBus.PublishWorkflowLog(execution.ID, "info", fmt.Sprintf("Node %s completed successfully", nodeID), "orchestrator", nodeID)

	// Execute downstream nodes
	if downstream, exists := execution.Graph.Adjacency[nodeID]; exists {
		var wg sync.WaitGroup
		errorChan := make(chan error, len(downstream))

		for _, nextNodeID := range downstream {
			wg.Add(1)
			go func(nID string) {
				defer wg.Done()
				// Add small delay to make execution visible
				time.Sleep(500 * time.Millisecond)
				if err := wo.executeNode(ctx, execution, nID); err != nil {
					errorChan <- err
				}
			}(nextNodeID)
		}

		wg.Wait()
		close(errorChan)

		for err := range errorChan {
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// executeOrchestratorNode executes an orchestrator node
func (wo *WorkflowOrchestrator) executeOrchestratorNode(ctx context.Context, node *Node) (interface{}, error) {
	wo.logger.Debugf("Executing orchestrator node: %s", node.ID)

	// Simulate orchestrator work
	time.Sleep(1 * time.Second)

	return map[string]interface{}{
		"status": "orchestrated",
		"nodeId": node.ID,
	}, nil
}

// executeExecutorNode executes an executor node
func (wo *WorkflowOrchestrator) executeExecutorNode(ctx context.Context, node *Node) (interface{}, error) {
	wo.logger.Debugf("Executing executor node: %s", node.ID)

	// Extract tool information from node data
	toolName, _ := node.Data["tool"].(string)
	args, _ := node.Data["args"].(map[string]interface{})

	if toolName == "" {
		// Simulate executor work if no tool specified
		time.Sleep(1 * time.Second)
		return map[string]interface{}{
			"status": "executed",
			"nodeId": node.ID,
		}, nil
	}

	// Execute tool through agent interface if available
	if wo.agentInterface != nil {
		if wo.agentInterface.HasLocalTool(toolName) {
			return wo.agentInterface.ExecuteLocalTool(ctx, toolName, args)
		}

		// Find and execute on remote agent
		peerID, err := wo.agentInterface.FindAgentWithTool(toolName)
		if err == nil {
			return wo.agentInterface.ExecuteRemoteTool(ctx, peerID, toolName, args)
		}
	}

	// Fallback to simulation
	time.Sleep(2 * time.Second)
	return map[string]interface{}{
		"status": "executed",
		"tool":   toolName,
		"nodeId": node.ID,
	}, nil
}

// executeToolNode executes a tool node
func (wo *WorkflowOrchestrator) executeToolNode(ctx context.Context, node *Node) (interface{}, error) {
	wo.logger.Debugf("Executing tool node: %s", node.ID)

	toolName, _ := node.Data["name"].(string)
	args, _ := node.Data["args"].(map[string]interface{})

	if wo.agentInterface != nil && toolName != "" {
		// Try local execution first
		if wo.agentInterface.HasLocalTool(toolName) {
			return wo.agentInterface.ExecuteLocalTool(ctx, toolName, args)
		}

		// Try remote execution
		peerID, err := wo.agentInterface.FindAgentWithTool(toolName)
		if err == nil {
			return wo.agentInterface.ExecuteRemoteTool(ctx, peerID, toolName, args)
		}
	}

	// Simulate tool execution
	time.Sleep(1500 * time.Millisecond)
	return map[string]interface{}{
		"status": "tool_executed",
		"tool":   toolName,
		"nodeId": node.ID,
	}, nil
}

// executeAgentNode executes an agent node
func (wo *WorkflowOrchestrator) executeAgentNode(ctx context.Context, node *Node) (interface{}, error) {
	wo.logger.Debugf("Executing agent node: %s", node.ID)

	agentName, _ := node.Data["agent"].(string)
	action, _ := node.Data["action"].(string)

	// Simulate agent execution
	time.Sleep(1 * time.Second)

	return map[string]interface{}{
		"status": "agent_executed",
		"agent":  agentName,
		"action": action,
		"nodeId": node.ID,
	}, nil
}

// executeGenericNode executes a generic node
func (wo *WorkflowOrchestrator) executeGenericNode(ctx context.Context, node *Node) (interface{}, error) {
	wo.logger.Debugf("Executing generic node: %s", node.ID)

	// Simulate generic node execution
	time.Sleep(1 * time.Second)

	return map[string]interface{}{
		"status": "executed",
		"nodeId": node.ID,
		"type":   node.Type,
	}, nil
}

// GetWorkflowStatus returns the status of a workflow
func (wo *WorkflowOrchestrator) GetWorkflowStatus(workflowID string) (map[string]interface{}, error) {
	wo.mu.RLock()
	execution, exists := wo.workflows[workflowID]
	wo.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("workflow %s not found", workflowID)
	}

	execution.mu.RLock()
	defer execution.mu.RUnlock()

	status := map[string]interface{}{
		"id":        execution.ID,
		"status":    execution.Status,
		"startTime": execution.StartTime,
		"results":   execution.Results,
	}

	if execution.EndTime != nil {
		status["endTime"] = *execution.EndTime
		status["duration"] = execution.EndTime.Sub(execution.StartTime).String()
	}

	// Add node statuses
	nodeStatuses := make(map[string]string)
	for id, node := range execution.Graph.Nodes {
		nodeStatuses[id] = string(node.Status)
	}
	status["nodeStatuses"] = nodeStatuses

	return status, nil
}
