package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/praxis/praxis-go-sdk/internal/p2p"
	"github.com/sirupsen/logrus"
)

// AgentCapabilitiesProvider provides access to current network state for LLM context
type AgentCapabilitiesProvider interface {
	GetLocalTools() []string
	GetPeerCards() map[peer.ID]*p2p.AgentCard
	GetAgentName() string
	GetPeerID() peer.ID
}

// AgentCard represents peer capabilities (reusing existing structure)
type AgentCard struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	PeerID       string   `json:"peer_id"`
	Tools        []string `json:"tools"`
	Capabilities []string `json:"capabilities"`
	Timestamp    int64    `json:"timestamp"`
}

// LLMWorkflowTool is the MCP tool that provides LLM-powered workflow generation
type LLMWorkflowTool struct {
	llmClient     *LLMClient
	agentProvider AgentCapabilitiesProvider
	logger        *logrus.Logger
}

// NewLLMWorkflowTool creates a new LLM workflow MCP tool
func NewLLMWorkflowTool(llmClient *LLMClient, agentProvider AgentCapabilitiesProvider, logger *logrus.Logger) *LLMWorkflowTool {
	return &LLMWorkflowTool{
		llmClient:     llmClient,
		agentProvider: agentProvider,
		logger:        logger,
	}
}

// GetGenerateWorkflowTool returns the MCP tool specification for workflow generation
func (t *LLMWorkflowTool) GetGenerateWorkflowTool() mcpTypes.Tool {
	return mcpTypes.NewTool("generate_workflow_from_natural_language",
		mcpTypes.WithDescription("Converts natural language requests into executable P2P workflow plans using LLM intelligence. Analyzes current network capabilities and generates optimal task distribution across agents."),
		mcpTypes.WithString("request",
			mcpTypes.Required(),
			mcpTypes.Description("Natural language description of what you want to accomplish (e.g., 'create a report by reading log files and analyzing errors')")),
		mcpTypes.WithBoolean("include_network_analysis",
			mcpTypes.DefaultBool(true),
			mcpTypes.Description("Whether to include current network topology analysis in the response")),
	)
}

// GenerateWorkflowHandler handles the workflow generation requests
func (t *LLMWorkflowTool) GenerateWorkflowHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	userRequest, _ := args["request"].(string)
	includeNetworkAnalysis, _ := args["include_network_analysis"].(bool)

	if userRequest == "" {
		return mcpTypes.NewToolResultError("Natural language request is required"), nil
	}

	t.logger.Infof("LLM workflow generation requested: %s", userRequest)

	// Step 1: Check if LLM is available
	if !t.llmClient.IsEnabled() {
		t.logger.Warn("LLM not available, falling back to simple DSL suggestion")
		return t.generateSimpleDSLSuggestion(userRequest)
	}

	// Step 2: Build current network context
	networkContext, err := t.buildNetworkContext()
	if err != nil {
		t.logger.Errorf("Failed to build network context: %v", err)
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to analyze network: %v", err)), nil
	}

	// Step 3: Generate workflow using LLM
	plan, err := t.llmClient.GenerateWorkflowFromNaturalLanguage(ctx, userRequest, networkContext)
	if err != nil {
		t.logger.Errorf("LLM workflow generation failed: %v", err)
		// Fallback to simple DSL suggestion
		t.logger.Info("Falling back to simple DSL suggestion due to LLM error")
		return t.generateSimpleDSLSuggestion(userRequest)
	}

	// Step 4: Validate the generated plan
	if err := t.llmClient.ValidateWorkflowPlan(plan, networkContext); err != nil {
		t.logger.Errorf("Generated workflow plan is invalid: %v", err)
		return mcpTypes.NewToolResultError(fmt.Sprintf("Generated workflow is invalid: %v", err)), nil
	}

	// Step 5: Convert to executable DSL commands
	dslCommands := t.llmClient.ConvertPlanToDSLCommands(plan)

	// Step 6: Build response
	response := map[string]interface{}{
		"success":            true,
		"workflow_plan":      plan,
		"dsl_commands":       dslCommands,
		"execution_strategy": t.buildExecutionStrategy(plan),
	}

	// Include network analysis if requested
	if includeNetworkAnalysis {
		response["network_context"] = networkContext
	}

	// Add summary for UI
	response["summary"] = fmt.Sprintf(
		"Generated %s workflow with %d nodes and %d edges. Estimated execution time: %s. Ready for execution.",
		plan.Metadata.Complexity,
		len(plan.Nodes),
		len(plan.Edges),
		plan.Metadata.EstimatedDuration,
	)

	responseJSON, _ := json.Marshal(response)

	t.logger.Infof("Successfully generated LLM workflow plan %s", plan.ID)
	return mcpTypes.NewToolResultText(string(responseJSON)), nil
}

// buildNetworkContext creates current network context for LLM planning
func (t *LLMWorkflowTool) buildNetworkContext() (*NetworkContext, error) {
	context := &NetworkContext{
		Agents:    make(map[string]*AgentCapability),
		Tools:     make(map[string][]string),
		Timestamp: time.Now(),
	}

	// Add local agent
	localPeerID := t.agentProvider.GetPeerID().String()
	localToolNames := t.agentProvider.GetLocalTools()

	// Convert local tool names to ToolSpecs (basic conversion without full spec)
	var localToolSpecs []p2p.ToolSpec
	for _, toolName := range localToolNames {
		localToolSpecs = append(localToolSpecs, p2p.ToolSpec{
			Name:        toolName,
			Description: fmt.Sprintf("Local tool: %s", toolName),
			Parameters:  []p2p.ToolParameter{},
		})
	}

	context.Agents[localPeerID] = &AgentCapability{
		PeerID:       localPeerID,
		Name:         t.agentProvider.GetAgentName(),
		Tools:        localToolSpecs,
		Capabilities: []string{"local", "mcp", "dsl"},
		LastSeen:     time.Now(),
	}

	// Index local tools
	for _, toolSpec := range localToolSpecs {
		context.Tools[toolSpec.Name] = append(context.Tools[toolSpec.Name], localPeerID)
	}

	// Add peer agents
	peerCards := t.agentProvider.GetPeerCards()
	for peerID, card := range peerCards {
		peerIDStr := peerID.String()
		context.Agents[peerIDStr] = &AgentCapability{
			PeerID:       peerIDStr,
			Name:         card.Name,
			Tools:        card.Tools,
			Capabilities: card.Capabilities,
			LastSeen:     time.Unix(card.Timestamp, 0),
		}

		// Index peer tools
		for _, toolSpec := range card.Tools {
			context.Tools[toolSpec.Name] = append(context.Tools[toolSpec.Name], peerIDStr)
		}
	}

	t.logger.Debugf("Built network context: %d agents, %d unique tools", len(context.Agents), len(context.Tools))
	return context, nil
}

// generateSimpleDSLSuggestion creates a fallback DSL suggestion when LLM is not available
func (t *LLMWorkflowTool) generateSimpleDSLSuggestion(userRequest string) (*mcpTypes.CallToolResult, error) {
	t.logger.Info("Generating simple DSL suggestion as LLM fallback")

	// Simple keyword-based DSL suggestions
	suggestion := "CALL write_file output.txt \"Generated content\""

	// Basic keyword analysis for better suggestions
	lowerRequest := fmt.Sprintf("%s", userRequest) // Simple string processing
	if contains(lowerRequest, "read") || contains(lowerRequest, "file") {
		suggestion = "CALL read_file filename.txt"
	} else if contains(lowerRequest, "list") {
		suggestion = "CALL list_files"
	} else if contains(lowerRequest, "write") || contains(lowerRequest, "create") {
		suggestion = "CALL write_file output.txt \"Your content here\""
	}

	response := map[string]interface{}{
		"success":       true,
		"fallback_mode": true,
		"suggested_dsl": suggestion,
		"message":       "LLM not available - providing simple DSL suggestion. For intelligent workflow generation, configure OPENAI_API_KEY.",
		"workflow_plan": t.createSimpleWorkflowPlan(suggestion),
	}

	responseJSON, _ := json.Marshal(response)
	return mcpTypes.NewToolResultText(string(responseJSON)), nil
}

// createSimpleWorkflowPlan creates a basic workflow plan for simple DSL
func (t *LLMWorkflowTool) createSimpleWorkflowPlan(dslCommand string) *WorkflowPlan {
	return &WorkflowPlan{
		ID:          fmt.Sprintf("simple_%d", time.Now().UnixNano()),
		Description: "Simple DSL-based workflow (LLM fallback mode)",
		Nodes: []WorkflowNode{
			{
				ID:       "orchestrator",
				Type:     "orchestrator",
				Position: map[string]int{"x": 100, "y": 100},
			},
			{
				ID:       "executor",
				Type:     "tool",
				Position: map[string]int{"x": 300, "y": 100},
			},
		},
		Edges: []WorkflowEdge{
			{
				ID:   "e1",
				From: "orchestrator",
				To:   "executor",
				Type: "control",
			},
		},
		Metadata: PlanMetadata{
			EstimatedDuration: "10s",
			ParallelismFactor: 1,
			Complexity:        "simple",
		},
	}
}

// buildExecutionStrategy provides execution recommendations for the workflow
func (t *LLMWorkflowTool) buildExecutionStrategy(plan *WorkflowPlan) map[string]interface{} {
	strategy := map[string]interface{}{
		"execution_order": t.calculateExecutionOrder(plan),
		"parallel_groups": t.identifyParallelGroups(plan),
		"critical_path":   plan.Metadata.CriticalPath,
		"recommendations": t.generateExecutionRecommendations(plan),
	}

	return strategy
}

// calculateExecutionOrder determines the order nodes should be executed
func (t *LLMWorkflowTool) calculateExecutionOrder(plan *WorkflowPlan) []string {
	// Simple topological sort for execution order
	order := make([]string, 0, len(plan.Nodes))
	processed := make(map[string]bool)

	// Process nodes with no dependencies first
	for _, node := range plan.Nodes {
		if len(node.DependsOn) == 0 {
			order = append(order, node.ID)
			processed[node.ID] = true
		}
	}

	// Process remaining nodes
	for len(order) < len(plan.Nodes) {
		for _, node := range plan.Nodes {
			if processed[node.ID] {
				continue
			}

			// Check if all dependencies are processed
			canProcess := true
			for _, dep := range node.DependsOn {
				if !processed[dep] {
					canProcess = false
					break
				}
			}

			if canProcess {
				order = append(order, node.ID)
				processed[node.ID] = true
			}
		}
	}

	return order
}

// identifyParallelGroups identifies nodes that can be executed in parallel
func (t *LLMWorkflowTool) identifyParallelGroups(plan *WorkflowPlan) [][]string {
	// Simple parallel group identification
	groups := make([][]string, 0)

	// Group nodes by dependency level
	levels := make(map[int][]string)
	for _, node := range plan.Nodes {
		level := len(node.DependsOn)
		levels[level] = append(levels[level], node.ID)
	}

	// Convert levels to groups
	for _, nodeIDs := range levels {
		if len(nodeIDs) > 1 {
			groups = append(groups, nodeIDs)
		}
	}

	return groups
}

// generateExecutionRecommendations provides optimization suggestions
func (t *LLMWorkflowTool) generateExecutionRecommendations(plan *WorkflowPlan) []string {
	recommendations := make([]string, 0)

	if plan.Metadata.ParallelismFactor > 1 {
		recommendations = append(recommendations, "Consider parallel execution for better performance")
	}

	if plan.Metadata.Complexity == "complex" {
		recommendations = append(recommendations, "Monitor execution closely due to workflow complexity")
	}

	if len(plan.Nodes) > 5 {
		recommendations = append(recommendations, "Consider breaking into smaller sub-workflows for easier debugging")
	}

	return recommendations
}

// contains checks if a string contains a substring (case-insensitive helper)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || (len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				indexOf(s, substr) >= 0)))
}

// indexOf finds the index of substr in s
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
