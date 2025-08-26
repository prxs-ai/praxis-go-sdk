package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/p2p"
	"github.com/sashabaranov/go-openai"
	"github.com/sirupsen/logrus"
)

// NetworkContext represents the current state of P2P network capabilities
type NetworkContext struct {
	Agents    map[string]*AgentCapability `json:"agents"`
	Tools     map[string][]string         `json:"tools"` // tool_name -> peer_ids
	Timestamp time.Time                   `json:"timestamp"`
}

// AgentCapability represents what an agent can do with full tool specifications
type AgentCapability struct {
	PeerID       string         `json:"peer_id"`
	Name         string         `json:"name"`
	Tools        []p2p.ToolSpec `json:"tools"` // Changed from []string to []p2p.ToolSpec
	Capabilities []string       `json:"capabilities"`
	LastSeen     time.Time      `json:"last_seen"`
}

// WorkflowPlan represents an LLM-generated workflow plan
type WorkflowPlan struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Nodes       []WorkflowNode `json:"nodes"`
	Edges       []WorkflowEdge `json:"edges"`
	Metadata    PlanMetadata   `json:"metadata"`
}

// WorkflowNode represents a single step in workflow
type WorkflowNode struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"` // "agent", "tool", "orchestrator"
	AgentID   string            `json:"agent_id,omitempty"`
	ToolName  string            `json:"tool_name,omitempty"`
	Args      map[string]string `json:"args,omitempty"`
	DependsOn []string          `json:"depends_on,omitempty"`
	Position  map[string]int    `json:"position"`
}

// WorkflowEdge represents connection between nodes
type WorkflowEdge struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"` // "data", "control"
	Weight int    `json:"weight,omitempty"`
}

// PlanMetadata contains optimization info
type PlanMetadata struct {
	EstimatedDuration string   `json:"estimated_duration"` // String like "5s" or "30s"
	ParallelismFactor int      `json:"parallelism_factor"`
	CriticalPath      []string `json:"critical_path"`
	Complexity        string   `json:"complexity"` // "simple", "medium", "complex"
}

// LLMClient handles OpenAI interactions for workflow planning
type LLMClient struct {
	openaiClient *openai.Client
	logger       *logrus.Logger
	enabled      bool
}

// NewLLMClient creates a new LLM client with fallback safety
func NewLLMClient(logger *logrus.Logger) *LLMClient {
	apiKey := os.Getenv("OPENAI_API_KEY")

	client := &LLMClient{
		logger:  logger,
		enabled: apiKey != "",
	}

	if apiKey != "" {
		client.openaiClient = openai.NewClient(apiKey)
		logger.Info("LLM client initialized with OpenAI")
	} else {
		logger.Warn("OPENAI_API_KEY not found - LLM features disabled, falling back to traditional DSL")
	}

	return client
}

// IsEnabled returns whether LLM features are available
func (c *LLMClient) IsEnabled() bool {
	return c.enabled
}

// GenerateWorkflowFromNaturalLanguage converts natural language to executable workflow
func (c *LLMClient) GenerateWorkflowFromNaturalLanguage(ctx context.Context, userRequest string, networkContext *NetworkContext) (*WorkflowPlan, error) {
	if !c.enabled {
		return nil, fmt.Errorf("LLM client not enabled - missing OPENAI_API_KEY")
	}

	c.logger.Infof("Generating workflow from natural language: %s", userRequest)

	// Build intelligent system prompt with network context
	systemPrompt := c.buildSystemPrompt(networkContext)

	// Create the request
	resp, err := c.openaiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT4o,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userRequest,
			},
		},
		MaxTokens:   4000,
		Temperature: 0.1, // Low temperature for consistent results
		TopP:        0.9,
	})

	if err != nil {
		return nil, fmt.Errorf("OpenAI API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	// Parse the JSON response
	var plan WorkflowPlan
	content := resp.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse workflow plan: %w, content: %s", err, content)
	}

	// Generate unique ID and add metadata
	plan.ID = fmt.Sprintf("workflow_%d", time.Now().UnixNano())
	if plan.Metadata.Complexity == "" {
		plan.Metadata.Complexity = c.assessComplexity(&plan)
	}

	c.logger.Infof("Generated workflow plan %s with %d nodes and %d edges", plan.ID, len(plan.Nodes), len(plan.Edges))

	return &plan, nil
}

// buildSystemPrompt creates an intelligent system prompt based on network capabilities
func (c *LLMClient) buildSystemPrompt(ctx *NetworkContext) string {
	// Dynamically build the tools documentation section
	var toolsDocumentation strings.Builder
	toolsDocumentation.WriteString("### AVAILABLE TOOLS (API)\n")
	toolsDocumentation.WriteString("=====================================\n")

	// Build detailed tool specifications from agents
	for _, agent := range ctx.Agents {
		if len(agent.Tools) > 0 {
			toolsDocumentation.WriteString(fmt.Sprintf("\n#### Agent: %s (`%s`)\n", agent.Name, agent.PeerID))
			for _, tool := range agent.Tools {
				toolsDocumentation.WriteString(fmt.Sprintf("- **Tool:** `%s`\n", tool.Name))
				toolsDocumentation.WriteString(fmt.Sprintf("  - **Description:** %s\n", tool.Description))
				if len(tool.Parameters) > 0 {
					toolsDocumentation.WriteString("  - **Parameters:**\n")
					for _, param := range tool.Parameters {
						req := ""
						if param.Required {
							req = "(REQUIRED)"
						}
						toolsDocumentation.WriteString(fmt.Sprintf("    - `%s` (%s) %s: %s\n",
							param.Name, param.Type, req, param.Description))
					}
				}
			}
		}
	}

	return fmt.Sprintf(`You are the brain and main orchestrator of a distributed P2P network of agents.

### YOUR MISSION:
Analyze user requests and available API tools. Your task is to select the most suitable tool on the most appropriate agent and generate a JSON execution plan.

%s

### CRITICALLY IMPORTANT INSTRUCTIONS:
1. **ANALYZE REQUEST:** Understand what the user wants based on any formulation
2. **SELECT TOOL:** Review documentation for all available tools and choose the one that best solves the task
3. **SELECT AGENT:** Choose the agent that has this tool (use peer_id from documentation)
4. **FORM ARGUMENTS:** Extract all necessary parameter values from the user request
5. **RETURN JSON:** Your response is ALWAYS and ONLY valid JSON in the specified format
6. **STRICT PARAMETER MATCHING:** Parameter names in the "args" object must EXACTLY match parameter names from tool documentation, including case sensitivity. Do not invent new names.

### RESPONSE FORMAT (STRICT JSON):
{
  "description": "Brief plan description",
  "nodes": [
    {
      "id": "node_1",
      "type": "tool",
      "agent_id": "peer_id_from_documentation",
      "tool_name": "tool_name_from_documentation",
      "args": {
        "parameter_name_1": "value_from_user_request",
        "parameter_name_2": "value_from_user_request"
      },
      "depends_on": [],
      "position": {"x": 250, "y": 100}
    }
  ],
  "edges": [],
  "metadata": {"complexity": "simple", "parallelism_factor": 1, "estimated_duration": "5s"}
}

### UNIVERSAL UNDERSTANDING:
You must understand ANY requests and find suitable tools:
- If user mentions files → look for tools with "file" in name or description
- If user wants analysis → look for tools with "analyze" or similar words
- If user wants to create something → look for creation tools
- For ANY other request → carefully read descriptions of all tools

### CRITICAL REQUIREMENTS:
- Use ONLY tools from the documentation above
- Parameter names in args must EXACTLY match documentation (case-sensitive)
- agent_id must be a real peer_id from documentation
- All REQUIRED parameters must be present in args
- If no suitable tool exists, try to solve the task with available means`, toolsDocumentation.String())
}

// assessComplexity determines workflow complexity based on structure
func (c *LLMClient) assessComplexity(plan *WorkflowPlan) string {
	nodeCount := len(plan.Nodes)
	edgeCount := len(plan.Edges)

	// Simple heuristics for complexity assessment
	if nodeCount <= 2 && edgeCount <= 1 {
		return "simple"
	} else if nodeCount <= 5 && edgeCount <= 4 {
		return "medium"
	}
	return "complex"
}

// ValidateWorkflowPlan checks if the generated plan is executable given current network state
func (c *LLMClient) ValidateWorkflowPlan(plan *WorkflowPlan, ctx *NetworkContext) error {
	for _, node := range plan.Nodes {
		if node.Type == "tool" {
			// Check if the specified agent actually has the tool
			if node.AgentID != "" && node.ToolName != "" {
				agent, exists := ctx.Agents[node.AgentID]
				if !exists {
					return fmt.Errorf("node %s references non-existent agent %s", node.ID, node.AgentID)
				}

				hasTools := false
				for _, toolSpec := range agent.Tools {
					if toolSpec.Name == node.ToolName {
						hasTools = true
						break
					}
				}
				if !hasTools {
					return fmt.Errorf("node %s: agent %s does not have tool %s", node.ID, node.AgentID, node.ToolName)
				}
			}
		}
	}

	return nil
}

// ConvertPlanToDSLCommands converts workflow plan to executable DSL commands
func (c *LLMClient) ConvertPlanToDSLCommands(plan *WorkflowPlan) []string {
	commands := make([]string, 0, len(plan.Nodes))

	for _, node := range plan.Nodes {
		switch node.Type {
		case "tool":
			if node.ToolName != "" {
				cmd := fmt.Sprintf("CALL %s", node.ToolName)

				// Add arguments in correct order for common tools
				switch node.ToolName {
				case "write_file":
					if filename, ok := node.Args["filename"]; ok {
						if content, ok := node.Args["content"]; ok {
							cmd = fmt.Sprintf("CALL write_file %s \"%s\"", filename, content)
						}
					}
				case "read_file":
					if filename, ok := node.Args["filename"]; ok {
						cmd = fmt.Sprintf("CALL read_file %s", filename)
					}
				case "list_files":
					cmd = "CALL list_files"
				default:
					// Generic argument handling
					for _, value := range node.Args {
						cmd += fmt.Sprintf(" %s", value)
					}
				}

				commands = append(commands, cmd)
			}
		case "orchestrator":
			// Orchestrator nodes don't generate DSL directly
			continue
		}
	}

	return commands
}
