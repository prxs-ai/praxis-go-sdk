package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/sirupsen/logrus"
	"github.com/praxis/praxis-go-sdk/internal/p2p"
)

// NetworkContext represents the current state of the P2P network
type NetworkContext struct {
	Agents    map[string]*AgentCapability `json:"agents"`
	Tools     map[string][]string         `json:"tools"` // tool_name -> peer_ids
	Timestamp time.Time                   `json:"timestamp"`
}

// AgentCapability describes a peer's capabilities and available tools
type AgentCapability struct {
	PeerID       string         `json:"peer_id"`
	Name         string         `json:"name"`
	Tools        []p2p.ToolSpec `json:"tools"` // Changed from []string to []p2p.ToolSpec
	Capabilities []string       `json:"capabilities"`
	LastSeen     time.Time      `json:"last_seen"`
}

// WorkflowPlan represents the output that the LLM should generate
type WorkflowPlan struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Nodes       []WorkflowNode `json:"nodes"`
	Edges       []WorkflowEdge `json:"edges"`
	Metadata    PlanMetadata   `json:"metadata"`
}

// WorkflowNode represents a single action in the workflow
type WorkflowNode struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"` // "agent", "tool", "orchestrator"
	AgentID   string                 `json:"agent_id,omitempty"`
	ToolName  string                 `json:"tool_name,omitempty"`
	Args      map[string]interface{} `json:"args,omitempty"`
	DependsOn []string               `json:"depends_on,omitempty"`
	Position  map[string]int         `json:"position"`
}

// WorkflowEdge represents dependencies between nodes
type WorkflowEdge struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"` // "data", "control"
	Weight int    `json:"weight,omitempty"`
}

// PlanMetadata contains execution metadata
type PlanMetadata struct {
	EstimatedDuration string   `json:"estimated_duration"` // String like "5s" or "30s"
	ParallelismFactor int      `json:"parallelism_factor"`
	CriticalPath      []string `json:"critical_path"`
	Complexity        string   `json:"complexity"` // "simple", "medium", "complex"
}

// LLMClient handles LLM interactions for workflow generation
type LLMClient struct {
	client *openai.Client
	logger *logrus.Logger
}

// NewLLMClient creates a new LLM client
func NewLLMClient(logger *logrus.Logger) *LLMClient {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		logger.Warn("OPENAI_API_KEY not set, LLM features will be disabled")
		return &LLMClient{
			logger: logger,
			client: nil,
		}
	}
	
	// Create OpenAI client
	client := openai.NewClient(apiKey)
	
	return &LLMClient{
		logger: logger,
		client: client,
	}
}

// IsEnabled checks if LLM functionality is available
func (c *LLMClient) IsEnabled() bool {
	return c.client != nil
}

// GenerateWorkflowFromNaturalLanguage converts natural language requests to workflow plans
func (c *LLMClient) GenerateWorkflowFromNaturalLanguage(ctx context.Context, userRequest string, networkContext *NetworkContext) (*WorkflowPlan, error) {
	if !c.IsEnabled() {
		return nil, fmt.Errorf("LLM client not enabled")
	}

	systemPrompt := c.buildSystemPrompt(networkContext)
	
	req := openai.ChatCompletionRequest{
		Model:     openai.GPT4o,
		MaxTokens: 4096,
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
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from LLM")
	}

	// Parse the JSON response
	var plan WorkflowPlan
	content := resp.Choices[0].Message.Content
	
	// Log the raw LLM response for debugging
	c.logger.Infof("🤖 Raw LLM Response: %s", content)
	
	// Clean markdown code blocks if present
	content = c.cleanMarkdownJSON(content)
	
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		c.logger.Errorf("❌ Failed to parse LLM JSON response: %v", err)
		c.logger.Errorf("📝 Raw response content: %s", content)
		return nil, fmt.Errorf("failed to parse LLM response as JSON: %w", err)
	}

	// Validate the plan
	if err := c.ValidateWorkflowPlan(&plan, networkContext); err != nil {
		return nil, fmt.Errorf("generated plan failed validation: %w", err)
	}

	// Set complexity if not set
	if plan.Metadata.Complexity == "" {
		plan.Metadata.Complexity = c.assessComplexity(&plan)
	}

	c.logger.Infof("✅ Successfully generated workflow plan: %s", plan.Description)
	return &plan, nil
}

func (c *LLMClient) buildSystemPrompt(ctx *NetworkContext) string {
	// Dynamically build the tools documentation section
	var toolsDocumentation strings.Builder
	toolsDocumentation.WriteString("AVAILABLE TOOLS:\n")

	// Build detailed tool specifications from agents
	for _, agent := range ctx.Agents {
		if len(agent.Tools) > 0 {
			toolsDocumentation.WriteString(fmt.Sprintf("Agent: %s (ID: %s)\n", agent.Name, agent.PeerID))
			for _, tool := range agent.Tools {
				toolsDocumentation.WriteString(fmt.Sprintf("TOOL: %s\n", tool.Name))
				toolsDocumentation.WriteString(fmt.Sprintf("Description: %s\n", tool.Description))
				if len(tool.Parameters) > 0 {
					toolsDocumentation.WriteString("Parameters:\n")
					for _, param := range tool.Parameters {
						req := "optional"
						if param.Required {
							req = "required"
						}
						toolsDocumentation.WriteString(fmt.Sprintf("  %s (%s) [%s]: %s\n",
							param.Name, param.Type, req, param.Description))
					}
				}
				toolsDocumentation.WriteString(fmt.Sprintf("Usage: agent_id=%s, tool_name=%s\n\n", agent.PeerID, tool.Name))
			}
		}
	}

    return fmt.Sprintf("You are an intelligent AI ORCHESTRATOR for a distributed P2P agent network.\n\n"+
        "YOUR ROLE:\n"+
        "- Understand any user message as a task request (NO 'CALL' keyword needed)\n"+
        "- Select the BEST agent and tool for each task\n"+
        "- Route tasks to the most appropriate agent based on capabilities\n"+
        "- Create efficient workflows using available tools\n\n"+
		"AGENT SELECTION CRITERIA:\n"+
		"1. Tool availability - Does the agent have the required tool?\n"+
		"2. Agent specialization - Some agents may be specialized for certain tasks\n"+
		"3. Load balancing - Distribute work across multiple agents when possible\n"+
		"4. Locality - Use 'local' agent when tools are available locally\n\n"+
        "%s\n\n"+
        "IMPORTANT:\n"+
        "- ANY message from the user is a DSL command - interpret it and execute\n"+
        "- Don't require specific keywords like 'CALL' - understand intent\n"+
        "- Choose the most suitable agent for each task\n"+
        "- If multiple agents have the same tool, choose based on context\n"+
        "CRITICAL: ALWAYS return a valid JSON object in the specified format. Even for a single tool call, represent it as a one-node workflow. Do not include prose outside JSON.\n\n"+
        "Return valid JSON in this format:\n"+
        "{\n"+
        "  \"description\": \"Task description\",\n"+
        "  \"nodes\": [\n"+
        "    {\n"+
		"      \"id\": \"node_1\",\n"+
		"      \"type\": \"tool\",\n"+
		"      \"agent_id\": \"peer_id_from_docs\",\n"+
		"      \"tool_name\": \"exact_tool_name\",\n"+
		"      \"args\": {\"param\": \"value\"},\n"+
		"      \"depends_on\": [],\n"+
		"      \"position\": {\"x\": 100, \"y\": 100}\n"+
		"    }\n"+
		"  ],\n"+
		"  \"edges\": [],\n"+
		"  \"metadata\": {\"complexity\": \"simple\", \"parallelism_factor\": 1, \"estimated_duration\": \"5s\"}\n"+
		"}", toolsDocumentation.String())
}

// assessComplexity determines workflow complexity based on structure
func (c *LLMClient) assessComplexity(plan *WorkflowPlan) string {
	nodeCount := len(plan.Nodes)
	edgeCount := len(plan.Edges)

	if nodeCount <= 1 && edgeCount == 0 {
		return "simple"
	} else if nodeCount <= 3 && edgeCount <= 2 {
		return "medium"
	} else {
		return "complex"
	}
}

// ValidateWorkflowPlan validates the generated plan against network context
func (c *LLMClient) ValidateWorkflowPlan(plan *WorkflowPlan, ctx *NetworkContext) error {
	if plan.Description == "" {
		return fmt.Errorf("plan must have a description")
	}

	if len(plan.Nodes) == 0 {
		return fmt.Errorf("plan must have at least one node")
	}

	for _, node := range plan.Nodes {
		if node.ID == "" {
			return fmt.Errorf("node must have an ID")
		}
		if node.Type == "" {
			return fmt.Errorf("node must have a type")
		}
		if node.Type == "tool" {
			if node.AgentID == "" {
				return fmt.Errorf("tool node must have an agent_id")
			}
			if node.ToolName == "" {
				return fmt.Errorf("tool node must have a tool_name")
			}

			// Validate agent exists
			if node.AgentID != "local" {
				if _, exists := ctx.Agents[node.AgentID]; !exists {
					return fmt.Errorf("agent %s not found in network", node.AgentID)
				}
			}
		}
	}

	return nil
}

// ConvertPlanToDSLCommands converts a workflow plan to DSL commands
func (c *LLMClient) ConvertPlanToDSLCommands(plan *WorkflowPlan) []string {
	var commands []string
	
	for _, node := range plan.Nodes {
		if node.Type == "tool" {
			if node.AgentID == "local" {
				// Local tool execution
				switch node.ToolName {
				case "read_file":
					if filename, ok := node.Args["filename"]; ok {
						cmd := fmt.Sprintf("CALL read_file %v", filename)
						commands = append(commands, cmd)
					}
				case "list_files":
					cmd := "CALL list_files"
					commands = append(commands, cmd)
				default:
					// Generic argument handling
					var argPairs []string
					for k, v := range node.Args {
						// Convert interface{} to string properly
						argPairs = append(argPairs, fmt.Sprintf("--%s %v", k, v))
					}
					cmd := fmt.Sprintf("CALL %s %s", node.ToolName, strings.Join(argPairs, " "))
					commands = append(commands, cmd)
				}
			} else {
				// Remote tool execution
				var argPairs []string
				for k, v := range node.Args {
					// Convert interface{} to string properly
					argPairs = append(argPairs, fmt.Sprintf("--%s %v", k, v))
				}
				cmd := fmt.Sprintf("CALL_REMOTE %s %s %s", node.AgentID, node.ToolName, strings.Join(argPairs, " "))
				commands = append(commands, cmd)
			}
		}
	}
	
	return commands
}

// cleanMarkdownJSON removes markdown code block formatting from JSON response
func (c *LLMClient) cleanMarkdownJSON(content string) string {
	// Remove ```json and ``` from the response
	content = strings.TrimSpace(content)
	
	// Remove leading ```json
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSpace(content)
	}
	
	// Remove trailing ```
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}
	
	c.logger.Debugf("🧹 Cleaned JSON content: %s", content)
	return content
}

// SummarizeTweets generates a summary of tweets using GPT-4o-mini
func (c *LLMClient) SummarizeTweets(ctx context.Context, tweets []interface{}) (string, error) {
	return c.SummarizeTweetsWithCount(ctx, tweets, len(tweets))
}

// SummarizeTweetsWithCount generates a summary of a specific number of tweets
func (c *LLMClient) SummarizeTweetsWithCount(ctx context.Context, tweets []interface{}, count int) (string, error) {
	if !c.IsEnabled() {
		return "", fmt.Errorf("LLM client is not enabled")
	}
	
	// Limit tweets to requested count
	if count > 0 && count < len(tweets) {
		tweets = tweets[:count]
	}
	
	// Prepare tweets text
	var tweetTexts []string
	for i, tweet := range tweets {
		if tweetMap, ok := tweet.(map[string]interface{}); ok {
			text, _ := tweetMap["text"].(string)
			if text != "" {
				tweetTexts = append(tweetTexts, fmt.Sprintf("%d. %s", i+1, text))
			}
		}
	}
	
	if len(tweetTexts) == 0 {
		return "", fmt.Errorf("no tweets to summarize")
	}
	
	prompt := fmt.Sprintf(`Analyze these %d tweets and provide a concise summary highlighting:
1. Main topics and themes
2. Key announcements or news
3. General sentiment and engagement patterns
4. Any notable trends or patterns

Tweets:
%s

Provide a 3-4 paragraph summary in a professional tone.`, len(tweetTexts), strings.Join(tweetTexts, "\n\n"))
	
	req := openai.ChatCompletionRequest{
		Model: openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are a social media analyst specializing in Twitter content analysis. Provide concise, insightful summaries.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		Temperature: 0.3,
		MaxTokens:   500,
	}
	
	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		c.logger.Errorf("Failed to generate tweet summary: %v", err)
		return "", err
	}
	
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}
	
	summary := resp.Choices[0].Message.Content
	c.logger.Infof("✅ Generated tweet summary (%d chars)", len(summary))
	
	return summary, nil
}
