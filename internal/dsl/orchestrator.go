package dsl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/praxis/praxis-go-sdk/internal/llm"
	"github.com/praxis/praxis-go-sdk/internal/p2p"
	"github.com/sirupsen/logrus"
)

// OrchestratorAnalyzer extends Analyzer with orchestration capabilities
type OrchestratorAnalyzer struct {
	*Analyzer
	eventBus  *bus.EventBus
	llmClient *llm.LLMClient
}

// NewOrchestratorAnalyzer creates an analyzer with orchestration support
func NewOrchestratorAnalyzer(logger *logrus.Logger, agent AgentInterface, eventBus *bus.EventBus) *OrchestratorAnalyzer {
	// Initialize LLM client
	llmClient := llm.NewLLMClient(logger)
	
	return &OrchestratorAnalyzer{
		Analyzer:  NewAnalyzerWithAgent(logger, agent),
		eventBus:  eventBus,
		llmClient: llmClient,
	}
}

// AnalyzeWithOrchestration analyzes DSL and builds dynamic workflow
func (o *OrchestratorAnalyzer) AnalyzeWithOrchestration(ctx context.Context, dsl string) (interface{}, error) {
	o.logger.Infof("Orchestrator analyzing request: %s", dsl)
	
	// Check if this is a natural language request or DSL command
	isNaturalLanguage := !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(dsl)), "CALL")
	o.logger.Infof("Request analysis - isNaturalLanguage: %v, LLM enabled: %v", isNaturalLanguage, o.llmClient.IsEnabled())
	
	var ast *AST
	var workflow map[string]interface{}
	var err error
	
	if isNaturalLanguage && o.llmClient.IsEnabled() {
		// Use LLM for natural language processing
		o.logger.Info("Using LLM for natural language understanding")
		o.publishProgress("analyzing", "Understanding your request with AI...", map[string]interface{}{
			"request": dsl,
		})
		
		// Build network context for LLM
		networkContext := o.buildNetworkContext()
		
		// Log network context for debugging
		o.logger.Infof("Network context - Agents: %d, Tools: %d", len(networkContext.Agents), len(networkContext.Tools))
		for agentID, agent := range networkContext.Agents {
			o.logger.Infof("Agent %s (%s) has tools: %v", agent.Name, agentID, agent.Tools)
		}
		for tool, agents := range networkContext.Tools {
			o.logger.Infof("Tool %s available on agents: %v", tool, agents)
		}
		
		// Generate workflow plan using LLM
		plan, err := o.llmClient.GenerateWorkflowFromNaturalLanguage(ctx, dsl, networkContext)
		if err != nil {
			o.logger.Errorf("LLM analysis failed: %v", err)
			o.publishError("LLM Analysis Failed", err.Error())
			// Return error instead of fallback
			return nil, fmt.Errorf("failed to understand request: %v", err)
		}
		
		// Convert LLM plan to AST and workflow
		ast, workflow = o.convertLLMPlanToAST(plan)
		
		// Publish agent selection from LLM plan
		o.publishLLMAgentSelections(plan)
		
	} else {
		// Traditional DSL parsing
		o.logger.Info("Using traditional DSL parser")
		o.publishProgress("parsing", "Parsing DSL command...", map[string]interface{}{
			"command": dsl,
		})
		
		// Parse DSL
		tokens := o.tokenize(dsl)
		if len(tokens) == 0 {
			return nil, fmt.Errorf("failed to tokenize DSL")
		}
		
		ast, err = o.parse(tokens)
		if err != nil {
			return nil, fmt.Errorf("failed to parse DSL: %w", err)
		}
		
		// Analyze complexity
		complexity := o.analyzeComplexity(ast)
		o.logger.Infof("Task complexity: %s", complexity)
		
		// Build workflow based on complexity
		workflow, err = o.buildWorkflow(ctx, ast, complexity)
		if err != nil {
			return nil, fmt.Errorf("failed to build workflow: %w", err)
		}
	}
	
	// Execute with orchestration
	result, err := o.executeWithOrchestration(ctx, ast, workflow)
	if err != nil {
		o.publishError("Execution failed", err.Error())
		return nil, err
	}
	
	// Publish final result with workflow suggestion
	o.publishResult(dsl, result, workflow)
	
	return result, nil
}

// analyzeComplexity determines if task is simple or complex
func (o *OrchestratorAnalyzer) analyzeComplexity(ast *AST) string {
	nodeCount := len(ast.Nodes)
	
	// Check for complex patterns
	hasParallel := false
	hasSequence := false
	hasMultipleCalls := 0
	
	for _, node := range ast.Nodes {
		switch node.Type {
		case NodeTypeParallel:
			hasParallel = true
		case NodeTypeSequence:
			hasSequence = true
		case NodeTypeCall:
			hasMultipleCalls++
		}
	}
	
	if hasParallel || hasSequence || hasMultipleCalls > 2 || nodeCount > 2 {
		return "complex"
	}
	
	return "simple"
}

// buildWorkflow creates a dynamic workflow based on AST and complexity
func (o *OrchestratorAnalyzer) buildWorkflow(ctx context.Context, ast *AST, complexity string) (map[string]interface{}, error) {
	nodes := []map[string]interface{}{}
	edges := []map[string]interface{}{}
	
	// Always add orchestrator node
	orchestratorNode := map[string]interface{}{
		"id": "orchestrator",
		"type": "orchestrator",
		"data": map[string]interface{}{
			"label": "Workflow Orchestrator",
			"type": "orchestrator",
		},
		"position": map[string]interface{}{
			"x": 100,
			"y": 100,
		},
	}
	nodes = append(nodes, orchestratorNode)
	
	// Find and add agent nodes based on tools needed
	agentNodes := o.findAgentsForWorkflow(ctx, ast)
	
	// Add agent nodes and create edges
	xPos := 400
	for i, agentInfo := range agentNodes {
		nodeID := fmt.Sprintf("agent-%d", i+1)
		
		node := map[string]interface{}{
			"id": nodeID,
			"type": "agent",
			"data": map[string]interface{}{
				"label": agentInfo["name"],
				"type": "agent",
				"peerID": agentInfo["peerID"],
				"tools": agentInfo["tools"],
				"selected": true,
			},
			"position": map[string]interface{}{
				"x": xPos,
				"y": 100 + (i * 150),
			},
		}
		nodes = append(nodes, node)
		
		// Create edge from orchestrator to agent
		edge := map[string]interface{}{
			"id": fmt.Sprintf("e%d", i+1),
			"source": "orchestrator",
			"target": nodeID,
			"type": "custom",
		}
		edges = append(edges, edge)
		
		// Publish agent selection event
		o.publishAgentSelection(agentInfo)
	}
	
	// If complex, add more sophisticated workflow structure
	if complexity == "complex" {
		// Add parallel or sequence nodes as needed
		o.addComplexWorkflowNodes(&nodes, &edges, ast)
	}
	
	workflow := map[string]interface{}{
		"nodes": nodes,
		"edges": edges,
		"complexity": complexity,
	}
	
	return workflow, nil
}

// findAgentsForWorkflow finds the best agents for executing the workflow
func (o *OrchestratorAnalyzer) findAgentsForWorkflow(ctx context.Context, ast *AST) []map[string]interface{} {
	agentInfos := []map[string]interface{}{}
	usedAgents := make(map[string]bool)
	
	for _, node := range ast.Nodes {
		if node.Type == NodeTypeCall && node.ToolName != "" {
			toolName := node.ToolName
			
			// Find agent with this tool
			if o.agent != nil {
				// First check if local agent has the tool
				if o.agent.HasLocalTool(toolName) {
					if !usedAgents["local"] {
						agentInfo := map[string]interface{}{
							"name": "Local Orchestrator",
							"peerID": "local",
							"tools": []string{toolName},
							"type": "local",
						}
						agentInfos = append(agentInfos, agentInfo)
						usedAgents["local"] = true
					}
				} else {
					// Find remote agent with the tool
					peerID, err := o.agent.FindAgentWithTool(toolName)
					if err == nil && !usedAgents[peerID] {
						// Get agent name from peer cards if available
						agentName := o.getAgentNameFromPeerID(peerID)
						
						agentInfo := map[string]interface{}{
							"name": agentName,
							"peerID": peerID,
							"tools": []string{toolName},
							"type": "p2p",
						}
						agentInfos = append(agentInfos, agentInfo)
						usedAgents[peerID] = true
						
						o.logger.Infof("Selected P2P agent %s (%s) for tool %s", agentName, peerID, toolName)
					}
				}
			}
		}
	}
	
	// If no specific agents found, add default executor
	if len(agentInfos) == 0 {
		agentInfos = append(agentInfos, map[string]interface{}{
			"name": "P2P Executor",
			"peerID": "auto",
			"tools": []string{},
			"type": "executor",
		})
	}
	
	return agentInfos
}

// getAgentNameFromPeerID retrieves agent name from peer ID
func (o *OrchestratorAnalyzer) getAgentNameFromPeerID(peerID string) string {
	// Try to get the actual agent name from peer cards
	if agent, ok := o.agent.(interface{ GetAgentNameByPeerID(string) string }); ok {
		return agent.GetAgentNameByPeerID(peerID)
	}
	
	// Fallback to formatted name
	if strings.Contains(peerID, "12D3KooW") {
		return fmt.Sprintf("P2P Agent %s", peerID[8:14])
	}
	return "P2P Agent"
}

// addComplexWorkflowNodes adds nodes for complex workflows
func (o *OrchestratorAnalyzer) addComplexWorkflowNodes(nodes *[]map[string]interface{}, edges *[]map[string]interface{}, ast *AST) {
	// This can be extended to add parallel, sequence, and other complex nodes
	// For now, we'll keep it simple
}

// executeWithOrchestration executes the AST with orchestration events
func (o *OrchestratorAnalyzer) executeWithOrchestration(ctx context.Context, ast *AST, workflow map[string]interface{}) (interface{}, error) {
	o.publishProgress("executing", "Starting workflow execution...", workflow)
	
	// Execute using base analyzer
	result, err := o.execute(ctx, ast)
	if err != nil {
		return nil, err
	}
	
	o.publishProgress("completed", "Workflow execution completed", map[string]interface{}{
		"result": result,
	})
	
	return result, nil
}

// Event publishing methods

func (o *OrchestratorAnalyzer) publishProgress(stage string, message string, details map[string]interface{}) {
	if o.eventBus == nil {
		return
	}
	
	event := bus.Event{
		Type: bus.EventDSLProgress,
		Payload: map[string]interface{}{
			"stage": stage,
			"message": message,
			"details": details,
		},
	}
	
	o.eventBus.Publish(event)
	// Add small delay to prevent message batching
	time.Sleep(10 * time.Millisecond)
}

func (o *OrchestratorAnalyzer) publishAgentSelection(agentInfo map[string]interface{}) {
	if o.eventBus == nil {
		return
	}
	
	message := fmt.Sprintf("Selected %s for tool %v", agentInfo["name"], agentInfo["tools"])
	
	event := bus.Event{
		Type: bus.EventChatMessage,
		Payload: map[string]interface{}{
			"content": fmt.Sprintf("🎯 %s", message),
			"type": "system",
		},
	}
	
	o.eventBus.Publish(event)
	o.logger.Info(message)
	// Add small delay to prevent message batching
	time.Sleep(10 * time.Millisecond)
}

func (o *OrchestratorAnalyzer) publishResult(command string, result interface{}, workflow map[string]interface{}) {
	if o.eventBus == nil {
		return
	}
	
	// Add delay to ensure previous messages are sent separately
	time.Sleep(50 * time.Millisecond)
	
	event := bus.Event{
		Type: bus.EventDSLResult,
		Payload: map[string]interface{}{
			"command": command,
			"result": result,
			"success": true,
			"workflowSuggestion": workflow,
		},
	}
	
	o.eventBus.Publish(event)
}

func (o *OrchestratorAnalyzer) publishError(message string, details string) {
	if o.eventBus == nil {
		return
	}
	
	event := bus.Event{
		Type: bus.EventWorkflowError,
		Payload: map[string]interface{}{
			"message": message,
			"details": details,
		},
	}
	
	o.eventBus.Publish(event)
}

// buildNetworkContext builds the current network context for LLM
func (o *OrchestratorAnalyzer) buildNetworkContext() *llm.NetworkContext {
	agents := make(map[string]*llm.AgentCapability)
	tools := make(map[string][]string)
	
	// Add local agent capabilities
	if o.agent != nil {
		localTools := o.getLocalTools()
		localAgentID := "local"
		
		agents[localAgentID] = &llm.AgentCapability{
			PeerID:   localAgentID,
			Name:     "Local Orchestrator",
			Tools:    localTools,
			LastSeen: time.Now(),
		}
		
		// Add tools mapping
		for _, toolSpec := range localTools {
			tools[toolSpec.Name] = append(tools[toolSpec.Name], localAgentID)
		}
	}
	
	// Add P2P agent capabilities
	if agentWithCards, ok := o.agent.(interface{ GetPeerCards() map[string]*p2p.AgentCard }); ok {
		peerCards := agentWithCards.GetPeerCards()
		for peerID, card := range peerCards {
			agents[peerID] = &llm.AgentCapability{
				PeerID:       peerID,
				Name:         card.Name,
				Tools:        card.Tools, // Now this is []p2p.ToolSpec
				Capabilities: card.Capabilities,
				LastSeen:     time.Now(),
			}
			
			// Add tools mapping - now we need to iterate over ToolSpecs
			for _, toolSpec := range card.Tools {
				tools[toolSpec.Name] = append(tools[toolSpec.Name], peerID)
			}
		}
	}
	
	return &llm.NetworkContext{
		Agents:    agents,
		Tools:     tools,
		Timestamp: time.Now(),
	}
}

// getLocalTools returns list of tools available locally with full specifications
func (o *OrchestratorAnalyzer) getLocalTools() []p2p.ToolSpec {
	// Define local orchestrator tools
	tools := []p2p.ToolSpec{
		{
			Name:        "analyze_dsl",
			Description: "Analyze DSL query and generate execution plan",
			Parameters: []p2p.ToolParameter{
				{Name: "query", Type: "string", Description: "DSL query to analyze", Required: true},
				{Name: "validate_only", Type: "boolean", Description: "Only validate without execution", Required: false},
			},
		},
		{
			Name:        "orchestrate",
			Description: "Orchestrate workflow execution across agents",
			Parameters: []p2p.ToolParameter{
				{Name: "workflow", Type: "object", Description: "Workflow definition", Required: true},
			},
		},
	}
	
	// Check filesystem tools if available locally
	if o.agent.HasLocalTool("write_file") {
		tools = append(tools, p2p.ToolSpec{
			Name:        "write_file",
			Description: "Write content to a file",
			Parameters: []p2p.ToolParameter{
				{Name: "filename", Type: "string", Description: "Name of the file to write", Required: true},
				{Name: "content", Type: "string", Description: "Content to write to the file", Required: true},
			},
		})
	}
	
	if o.agent.HasLocalTool("read_file") {
		tools = append(tools, p2p.ToolSpec{
			Name:        "read_file",
			Description: "Read content from a file",
			Parameters: []p2p.ToolParameter{
				{Name: "filename", Type: "string", Description: "Name of the file to read", Required: true},
			},
		})
	}
	
	if o.agent.HasLocalTool("list_files") {
		tools = append(tools, p2p.ToolSpec{
			Name:        "list_files",
			Description: "List files in the directory",
			Parameters: []p2p.ToolParameter{
				{Name: "directory", Type: "string", Description: "Directory to list files from", Required: false},
			},
		})
	}
	
	if o.agent.HasLocalTool("delete_file") {
		tools = append(tools, p2p.ToolSpec{
			Name:        "delete_file",
			Description: "Delete a file",
			Parameters: []p2p.ToolParameter{
				{Name: "filename", Type: "string", Description: "Name of the file to delete", Required: true},
			},
		})
	}
	
	return tools
}

// convertLLMPlanToAST converts LLM workflow plan to AST and workflow
func (o *OrchestratorAnalyzer) convertLLMPlanToAST(plan *llm.WorkflowPlan) (*AST, map[string]interface{}) {
	ast := &AST{
		Nodes: make([]ASTNode, 0),
	}
	
	// Convert LLM nodes to AST nodes
	for _, node := range plan.Nodes {
		o.logger.Infof("🔍 LLM Plan Node: Type=%s, ToolName=%s, Args=%v", node.Type, node.ToolName, node.Args)
		
		if node.Type == "tool" && node.ToolName != "" {
			// Convert map[string]string to map[string]interface{}
			argsMap := make(map[string]interface{}, len(node.Args))
			for k, v := range node.Args {
				argsMap[k] = v
				o.logger.Infof("🔍 Converting LLM arg: %s = %s", k, v)
			}
			
			astNode := ASTNode{
				Type:     NodeTypeCall,
				Value:    "CALL", // Keep for backward compatibility
				ToolName: node.ToolName,
				Args:     argsMap, // Direct assignment of arguments map
			}
			
			// Debug log the generated AST node
			o.logger.Infof("🔧 Generated AST node for %s with args: %v", node.ToolName, argsMap)
			ast.Nodes = append(ast.Nodes, astNode)
		}
	}
	
	// Build workflow structure for UI
	workflow := map[string]interface{}{
		"nodes":      o.convertPlanNodesToUINodes(plan),
		"edges":      o.convertPlanEdgesToUIEdges(plan),
		"complexity": plan.Metadata.Complexity,
	}
	
	return ast, workflow
}

// convertPlanNodesToUINodes converts LLM plan nodes to UI nodes
func (o *OrchestratorAnalyzer) convertPlanNodesToUINodes(plan *llm.WorkflowPlan) []map[string]interface{} {
	nodes := []map[string]interface{}{}
	
	// Add orchestrator node
	orchestratorNode := map[string]interface{}{
		"id":   "orchestrator",
		"type": "orchestrator",
		"data": map[string]interface{}{
			"label": "AI Orchestrator (GPT-4o)",
			"type":  "orchestrator",
		},
		"position": map[string]interface{}{
			"x": 100,
			"y": 100,
		},
	}
	nodes = append(nodes, orchestratorNode)
	
	// Add plan nodes
	for _, node := range plan.Nodes {
		agentName := o.getAgentNameFromPeerID(node.AgentID)
		if node.AgentID == "local" {
			agentName = "Local Orchestrator"
		}
		
		uiNode := map[string]interface{}{
			"id":   node.ID,
			"type": node.Type,
			"data": map[string]interface{}{
				"label":  fmt.Sprintf("%s (%s)", agentName, node.ToolName),
				"type":   node.Type,
				"peerID": node.AgentID,
				"tools":  []string{node.ToolName},
			},
			"position": node.Position,
		}
		nodes = append(nodes, uiNode)
	}
	
	return nodes
}

// convertPlanEdgesToUIEdges converts LLM plan edges to UI edges
func (o *OrchestratorAnalyzer) convertPlanEdgesToUIEdges(plan *llm.WorkflowPlan) []map[string]interface{} {
	edges := []map[string]interface{}{}
	
	for _, edge := range plan.Edges {
		uiEdge := map[string]interface{}{
			"id":     edge.ID,
			"source": edge.From,
			"target": edge.To,
			"type":   "custom",
		}
		edges = append(edges, uiEdge)
	}
	
	// If no edges defined, create edges from orchestrator to each node
	if len(edges) == 0 {
		for i, node := range plan.Nodes {
			edge := map[string]interface{}{
				"id":     fmt.Sprintf("e%d", i+1),
				"source": "orchestrator",
				"target": node.ID,
				"type":   "custom",
			}
			edges = append(edges, edge)
		}
	}
	
	return edges
}

// publishLLMAgentSelections publishes agent selections from LLM plan
func (o *OrchestratorAnalyzer) publishLLMAgentSelections(plan *llm.WorkflowPlan) {
	for _, node := range plan.Nodes {
		if node.Type == "tool" && node.AgentID != "" {
			agentName := o.getAgentNameFromPeerID(node.AgentID)
			message := fmt.Sprintf("🤖 AI selected %s for %s operation", agentName, node.ToolName)
			
			event := bus.Event{
				Type: bus.EventChatMessage,
				Payload: map[string]interface{}{
					"content": message,
					"type":    "system",
				},
			}
			
			o.eventBus.Publish(event)
			o.logger.Info(message)
		}
	}
}

// fallbackToDSLParsing falls back to traditional DSL parsing
func (o *OrchestratorAnalyzer) fallbackToDSLParsing(ctx context.Context, dsl string) (interface{}, error) {
	// Try to interpret as simple file creation
	dsl = o.interpretAsSimpleCommand(dsl)
	
	tokens := o.tokenize(dsl)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("failed to tokenize DSL")
	}
	
	ast, err := o.parse(tokens)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSL: %w", err)
	}
	
	complexity := o.analyzeComplexity(ast)
	workflow, err := o.buildWorkflow(ctx, ast, complexity)
	if err != nil {
		return nil, fmt.Errorf("failed to build workflow: %w", err)
	}
	
	result, err := o.executeWithOrchestration(ctx, ast, workflow)
	if err != nil {
		return nil, err
	}
	
	o.publishResult(dsl, result, workflow)
	return result, nil
}

// interpretAsSimpleCommand tries to interpret natural language as simple command
func (o *OrchestratorAnalyzer) interpretAsSimpleCommand(input string) string {
	lower := strings.ToLower(input)
	
	// Extract filename from input if possible
	extractFilename := func(parts []string) string {
		for _, part := range parts {
			if strings.Contains(part, ".") && !strings.HasPrefix(part, ".") {
				return part
			}
		}
		return "file.txt"
	}
	
	parts := strings.Fields(input)
	
	// Simple pattern matching for common requests
	if strings.Contains(lower, "create") && strings.Contains(lower, "file") {
		filename := extractFilename(parts)
		return fmt.Sprintf("CALL write_file %s \"File created by orchestrator\"", filename)
	}
	
	// Delete file
	if strings.Contains(lower, "delete") || strings.Contains(lower, "remove") {
		filename := extractFilename(parts)
		return fmt.Sprintf("CALL delete_file %s", filename)
	}
	
	// Read file
	if strings.Contains(lower, "read") || strings.Contains(lower, "show") || strings.Contains(lower, "display") {
		filename := extractFilename(parts)
		return fmt.Sprintf("CALL read_file %s", filename)
	}
	
	// List files
	if strings.Contains(lower, "list") || strings.Contains(lower, "ls") || strings.Contains(lower, "dir") {
		return "CALL list_files"
	}
	
	// Default to original input
	return input
}