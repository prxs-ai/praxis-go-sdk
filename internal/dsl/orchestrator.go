package dsl

import (
    "encoding/json"
    "context"
    "fmt"
    "os"
    "path/filepath"
    "regexp"
    "strconv"
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

	var ast *AST
	var workflow map[string]interface{}
	var err error

	// НОВАЯ ЛОГИКА: ВСЕ сообщения идут в LLM - нет парсинга CALL
	// Любое сообщение является DSL командой для оркестратора
	if o.llmClient.IsEnabled() {
		// Всегда используем LLM для анализа - даже если есть CALL
		// Это позволяет LLM самой решать какой агент и инструмент использовать
		o.logger.Info("Using LLM for intelligent orchestration (all messages are DSL)")
		o.publishProgress("analyzing", "AI is analyzing your request...", map[string]interface{}{
			"request": dsl,
		})
		
		// Add small delay to show progress
		time.Sleep(500 * time.Millisecond)

		// Build network context for LLM
		networkContext := o.buildNetworkContext()
		
		// Publish discovering stage
		o.publishProgress("discovering", "Discovering available agents...", map[string]interface{}{
			"agentCount": len(networkContext.Agents),
		})
		time.Sleep(500 * time.Millisecond)

		// Log network context for debugging
		o.logger.Infof("Network context - Agents: %d, Tools: %d", len(networkContext.Agents), len(networkContext.Tools))
		for agentID, agent := range networkContext.Agents {
			o.logger.Infof("Agent %s (%s) has tools: %v", agent.Name, agentID, agent.Tools)
		}
		for tool, agents := range networkContext.Tools {
			o.logger.Infof("Tool %s available on agents: %v", tool, agents)
		}

		// Publish matching stage
		o.publishProgress("matching", "Matching agent capabilities...", map[string]interface{}{
			"analyzing": true,
		})
		time.Sleep(500 * time.Millisecond)
		
		// Generate workflow plan using LLM
		o.publishProgress("generating", "Generating workflow plan...", map[string]interface{}{})
		plan, err := o.llmClient.GenerateWorkflowFromNaturalLanguage(ctx, dsl, networkContext)
		if err != nil {
			o.logger.Errorf("LLM analysis failed: %v", err)
			o.publishError("LLM Analysis Failed", err.Error())
			// Return error instead of fallback
			return nil, fmt.Errorf("failed to understand request: %v", err)
		}

		// Validate the generated plan
		if err := o.llmClient.ValidateWorkflowPlan(plan, networkContext); err != nil {
			o.logger.Warnf("LLM plan validation failed: %v", err)
			o.publishError("Plan Validation Failed", fmt.Sprintf("Generated plan is not executable: %v", err))
			return nil, fmt.Errorf("generated plan is invalid: %v", err)
		}

		// Log successful plan generation
		o.logger.Infof("✅ LLM generated valid workflow plan with %d nodes", len(plan.Nodes))

		// Convert LLM plan to AST and workflow
		ast, workflow = o.convertLLMPlanToAST(plan)
		
		// Publish complete stage (but don't execute yet)
		o.publishProgress("complete", "Workflow ready for execution", map[string]interface{}{
			"nodes": len(plan.Nodes),
			"edges": len(plan.Edges),
		})
		time.Sleep(200 * time.Millisecond)

		// Verify we have at least one executable node
		if len(ast.Nodes) == 0 {
			o.publishError("Empty Workflow", "LLM generated plan with no executable nodes")
			return nil, fmt.Errorf("LLM generated empty workflow - no executable nodes found")
		}

		// Publish agent selection from LLM plan
		o.publishLLMAgentSelections(plan)

	} else {
		// Оставляем старый парсер как fallback, если LLM отключен
		o.logger.Info("LLM is disabled, using traditional DSL parser")
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

	// DON'T execute immediately - just publish the workflow for approval
	// The frontend will send an executeWorkflow command when user clicks Execute
	
	// Generate workflow ID if not present
	if workflow["id"] == nil {
		workflow["id"] = fmt.Sprintf("workflow_%d", time.Now().UnixNano())
	}
	workflowID := workflow["id"].(string)
	
	// Publish workflow immediately (before execution) 
	o.publishResult(dsl, nil, workflow)
	
	// Store the AST and workflow for later execution
	o.storeWorkflowForExecution(workflowID, ast, workflow, dsl)
	
	// Don't execute anything - wait for Execute button
	return workflow, nil
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
		"id":   "orchestrator",
		"type": "orchestrator",
		"data": map[string]interface{}{
			"label": "Workflow Orchestrator",
			"type":  "orchestrator",
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
			"id":   nodeID,
			"type": "agent",
			"data": map[string]interface{}{
				"label":    agentInfo["name"],
				"type":     "agent",
				"peerID":   agentInfo["peerID"],
				"tools":    agentInfo["tools"],
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
			"id":     fmt.Sprintf("e%d", i+1),
			"source": "orchestrator",
			"target": nodeID,
			"type":   "custom",
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
		"nodes":      nodes,
		"edges":      edges,
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
							"name":   "Local Orchestrator",
							"peerID": "local",
							"tools":  []string{toolName},
							"type":   "local",
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
							"name":   agentName,
							"peerID": peerID,
							"tools":  []string{toolName},
							"type":   "p2p",
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
			"name":   "P2P Executor",
			"peerID": "auto",
			"tools":  []string{},
			"type":   "executor",
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
			"stage":   stage,
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
			"type":    "system",
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
            "command":            command,
            "result":             result,
            "success":            true,
            "workflow":           workflow,  // For new execute button
            "workflowSuggestion": workflow,  // Keep for backwards compatibility
        },
    }

    o.eventBus.Publish(event)
    
    // Check if this was a twitter_scraper command and handle it
    if strings.Contains(strings.ToLower(command), "tweet") || strings.Contains(strings.ToLower(command), "twitter") {
        go o.handleTwitterScraperResult(command)
    }

    // Try to extract artifacts (e.g., saved JSON path) from tool results and publish a chat message with a download link
    // The twitter scraper prints a JSON payload with data.saved_to and optionally data.download_url
    go func(res interface{}) {
        defer func() { _ = recover() }()
        rmap, ok := res.(map[string]interface{})
        if !ok {
            return
        }
        results, ok := rmap["results"].([]interface{})
        if !ok || len(results) == 0 {
            return
        }
        for _, item := range results {
            imap, ok := item.(map[string]interface{})
            if !ok {
                continue
            }
            rawResult, ok := imap["result"].(string)
            if !ok || rawResult == "" {
                continue
            }
            // Parse JSON safely
            var parsed map[string]interface{}
            if err := json.Unmarshal([]byte(rawResult), &parsed); err != nil {
                continue
            }
            data, _ := parsed["data"].(map[string]interface{})
            if data == nil {
                continue
            }
            filename, _ := data["saved_to"].(string)
            downloadURL, _ := data["download_url"].(string)
            datasetURL, _ := data["apify_dataset_url"].(string)
            username, _ := data["username"].(string)
            tweetsCount, _ := data["tweets_count"].(float64)
            latestTweets, _ := data["latest_tweets"]
            
            if filename == "" && downloadURL == "" && datasetURL == "" {
                continue
            }
            // Build fallback download URL if not provided by tool
            if downloadURL == "" && filename != "" {
                downloadURL = fmt.Sprintf("http://localhost:8000/reports/%s", filename)
            }
            
            // Send tool_result message for UI to render ToolResultCard
            o.eventBus.Publish(bus.Event{
                Type: bus.EventChatMessage,
                Payload: map[string]interface{}{
                    "type":    "tool_result",
                    "content": fmt.Sprintf("Twitter scraping complete for @%s", username),
                    "sender":  "assistant",
                    "metadata": map[string]interface{}{
                        "toolName":     "twitter_scraper",
                        "fileName":     filename,
                        "downloadUrl":  downloadURL,
                        "datasetUrl":   datasetURL,
                        "username":     username,
                        "tweetsFound":  tweetsCount,
                        "preview":      latestTweets,
                    },
                },
            })
            
            o.logger.Infof("✅ Published tool_result for Twitter scraper - @%s", username)
            
            // Start async summarization if we have tweets
            if o.llmClient != nil && o.llmClient.IsEnabled() {
                go o.generateAndSendSummary(filename, username)
            }
        }
    }(result)
}

// handleTwitterScraperResult finds the latest Twitter JSON file and sends it to UI
func (o *OrchestratorAnalyzer) handleTwitterScraperResult(command string) {
	// Wait a bit for file to be written
	time.Sleep(2 * time.Second)
	
	// Extract tweet count from command if specified
	tweetCount := o.extractTweetCountFromCommand(command)
	
	// Find the latest Twitter JSON file
	reportsDir := "/app/shared/reports"
	files, err := os.ReadDir(reportsDir)
	if err != nil {
		o.logger.Errorf("Failed to read reports directory: %v", err)
		return
	}
	
	// Find latest twitter_*.json file
	var latestFile os.DirEntry
	var latestTime time.Time
	
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "twitter_") || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		
		info, err := file.Info()
		if err != nil {
			continue
		}
		
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = file
		}
	}
	
	if latestFile == nil {
		o.logger.Warn("No Twitter JSON files found in reports directory")
		return
	}
	
	// Read and parse the file
	filePath := filepath.Join(reportsDir, latestFile.Name())
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		o.logger.Errorf("Failed to read file %s: %v", filePath, err)
		return
	}
	
	// Parse JSON
	var reportData map[string]interface{}
	if err := json.Unmarshal(fileContent, &reportData); err != nil {
		o.logger.Errorf("Failed to parse JSON from %s: %v", latestFile.Name(), err)
		return
	}
	
	// Extract metadata
	metadata, _ := reportData["metadata"].(map[string]interface{})
	username, _ := metadata["username"].(string)
	tweetsFound, _ := metadata["tweets_found"].(float64)
	
	// Extract preview (first 5 tweets)
	var preview []interface{}
	if tweets, ok := reportData["tweets"].([]interface{}); ok && len(tweets) > 0 {
		limit := 5
		if len(tweets) < limit {
			limit = len(tweets)
		}
		preview = tweets[:limit]
	}
	
	// Build download URL
	downloadURL := fmt.Sprintf("http://localhost:8000/reports/%s", latestFile.Name())
	
	o.logger.Infof("📄 Found latest Twitter file: %s for @%s with %v tweets", latestFile.Name(), username, tweetsFound)
	
	// Send tool_result message for UI to render ToolResultCard
	o.eventBus.Publish(bus.Event{
		Type: bus.EventChatMessage,
		Payload: map[string]interface{}{
			"type":    "tool_result",
			"content": fmt.Sprintf("Twitter scraping complete for @%s", username),
			"sender":  "assistant",
			"metadata": map[string]interface{}{
				"toolName":     "twitter_scraper",
				"fileName":     latestFile.Name(),
				"downloadUrl":  downloadURL,
				"username":     username,
				"tweetsFound":  tweetsFound,
				"preview":      preview,
			},
		},
	})
	
	o.logger.Infof("✅ Published tool_result for Twitter scraper - @%s", username)
	
	// Start async summarization if we have tweets and LLM is enabled
	if tweets, ok := reportData["tweets"].([]interface{}); ok && len(tweets) > 0 {
		if o.llmClient != nil && o.llmClient.IsEnabled() {
			go o.generateSummaryAndSendWithCount(username, tweets, tweetCount)
		}
	}
}

// generateSummaryAndSend generates and sends summary for tweets
func (o *OrchestratorAnalyzer) generateSummaryAndSend(username string, tweets []interface{}) {
	o.generateSummaryAndSendWithCount(username, tweets, len(tweets))
}

// generateSummaryAndSendWithCount generates and sends summary for a specific number of tweets
func (o *OrchestratorAnalyzer) generateSummaryAndSendWithCount(username string, tweets []interface{}, count int) {
	ctx := context.Background()
	
	// Use the specified count or all tweets if count is 0
	if count <= 0 || count > len(tweets) {
		count = len(tweets)
	}
	
	o.logger.Infof("🤖 Generating AI summary for %d tweets (out of %d) from @%s", count, len(tweets), username)
	
	// Generate summary using LLM with count
	summary, err := o.llmClient.SummarizeTweetsWithCount(ctx, tweets, count)
	if err != nil {
		o.logger.Errorf("Failed to generate summary: %v", err)
		return
	}
	
	// Send summary as a follow-up message
	o.eventBus.Publish(bus.Event{
		Type: bus.EventChatMessage,
		Payload: map[string]interface{}{
			"type":    "system",
			"content": fmt.Sprintf("📊 AI Summary for @%s:\n\n%s", username, summary),
			"sender":  "assistant",
		},
	})
	
	o.logger.Infof("✅ Published AI summary for @%s", username)
}

// generateAndSendSummary reads the JSON file and generates a summary using LLM
func (o *OrchestratorAnalyzer) generateAndSendSummary(filename string, username string) {
	ctx := context.Background()
	
	// Read the JSON file
	filePath := fmt.Sprintf("/app/shared/reports/%s", filename)
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		o.logger.Errorf("Failed to read file %s: %v", filePath, err)
		return
	}
	
	// Parse JSON
	var reportData map[string]interface{}
	if err := json.Unmarshal(fileContent, &reportData); err != nil {
		o.logger.Errorf("Failed to parse JSON from %s: %v", filename, err)
		return
	}
	
	// Extract tweets
	tweets, ok := reportData["tweets"].([]interface{})
	if !ok || len(tweets) == 0 {
		o.logger.Warn("No tweets found in report")
		return
	}
	
	o.logger.Infof("Generating summary for %d tweets from @%s", len(tweets), username)
	
	// Generate summary using LLM
	summary, err := o.llmClient.SummarizeTweets(ctx, tweets)
	if err != nil {
		o.logger.Errorf("Failed to generate summary: %v", err)
		return
	}
	
	// Send summary as a follow-up message
	o.eventBus.Publish(bus.Event{
		Type: bus.EventChatMessage,
		Payload: map[string]interface{}{
			"type":    "system",
			"content": fmt.Sprintf("📊 AI Summary for @%s:\n\n%s", username, summary),
			"sender":  "assistant",
		},
	})
	
	o.logger.Infof("✅ Published tweet summary for @%s", username)
}

// extractTweetCountFromCommand extracts the tweet count from commands like "Summarize latest 20 username tweets"
func (o *OrchestratorAnalyzer) extractTweetCountFromCommand(command string) int {
	// Try to extract number from patterns like "latest 20", "last 30", etc.
	lowerCmd := strings.ToLower(command)
	
	// Regular expression to match patterns like "latest 20", "last 30", "20 tweets", etc.
	patterns := []string{
		`latest\s+(\d+)`,
		`last\s+(\d+)`,
		`(\d+)\s+tweet`,
		`top\s+(\d+)`,
		`first\s+(\d+)`,
	}
	
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(lowerCmd)
		if len(matches) > 1 {
			if count, err := strconv.Atoi(matches[1]); err == nil {
				o.logger.Infof("📊 Extracted tweet count from command: %d", count)
				return count
			}
		}
	}
	
	// Default to 0 (which means use all tweets)
	return 0
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
	if agentWithCards, ok := o.agent.(interface {
		GetPeerCards() map[string]*p2p.AgentCard
	}); ok {
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
	tools := []p2p.ToolSpec{}
	
	// Get ALL registered tools from the agent, including Dagger tools
	if agentWithTools, ok := o.agent.(interface{ GetLocalTools() []string }); ok {
		toolNames := agentWithTools.GetLocalTools()
		o.logger.Infof("🔍 Found %d registered tools in agent", len(toolNames))
		
		for _, toolName := range toolNames {
			// Create ToolSpec for each registered tool
			toolSpec := p2p.ToolSpec{
				Name:        toolName,
				Description: fmt.Sprintf("Tool: %s", toolName),
				Parameters:  []p2p.ToolParameter{},
			}
			
			// Add specific descriptions and parameters for known tools
			switch toolName {
			case "analyze_dsl":
				toolSpec.Description = "Analyze DSL query and generate execution plan for complex workflows"
				toolSpec.Parameters = []p2p.ToolParameter{
					{Name: "query", Type: "string", Description: "DSL query to analyze", Required: true},
					{Name: "validate_only", Type: "boolean", Description: "Only validate without execution", Required: false},
				}
			case "orchestrate":
				toolSpec.Description = "Orchestrate workflow execution across multiple agents in the P2P network"
				toolSpec.Parameters = []p2p.ToolParameter{
					{Name: "workflow", Type: "object", Description: "Workflow definition with nodes and edges", Required: true},
				}
			case "write_file":
				toolSpec.Description = "Write text content to a file in the shared directory"
				toolSpec.Parameters = []p2p.ToolParameter{
					{Name: "filename", Type: "string", Description: "Name of the file to create/write", Required: true},
					{Name: "content", Type: "string", Description: "Text content to write to the file", Required: true},
				}
			case "python_analyzer":
				toolSpec.Description = "Analyze text files using Python script (word count, content analysis)"
				toolSpec.Parameters = []p2p.ToolParameter{
					{Name: "input_file", Type: "string", Description: "Name of the file to analyze", Required: true},
				}
			case "twitter_scraper":
				toolSpec.Description = "Scrape tweets from Twitter/X using Apify API - provide username without @ symbol"
				toolSpec.Parameters = []p2p.ToolParameter{
					{Name: "username", Type: "string", Description: "Twitter username to scrape (without @ symbol, e.g. 'elonmusk')", Required: true},
					{Name: "tweets_count", Type: "number", Description: "Number of tweets to scrape (default: 50)", Required: false},
				}
			case "test_twitter":
				toolSpec.Description = "Test Twitter scraper with mock data - provide username without @ symbol"
				toolSpec.Parameters = []p2p.ToolParameter{
					{Name: "username", Type: "string", Description: "Twitter username to test (without @ symbol)", Required: true},
				}
			}
			
			tools = append(tools, toolSpec)
		}
	} else {
		// Fallback to hardcoded tools if agent doesn't support GetLocalTools
		o.logger.Warn("Agent doesn't support GetLocalTools(), using fallback")
		tools = []p2p.ToolSpec{
			{
				Name:        "analyze_dsl",
				Description: "Analyze DSL query and generate execution plan for complex workflows",
				Parameters: []p2p.ToolParameter{
					{Name: "query", Type: "string", Description: "DSL query to analyze", Required: true},
					{Name: "validate_only", Type: "boolean", Description: "Only validate without execution", Required: false},
				},
			},
			{
				Name:        "orchestrate",
				Description: "Orchestrate workflow execution across multiple agents in the P2P network",
				Parameters: []p2p.ToolParameter{
					{Name: "workflow", Type: "object", Description: "Workflow definition with nodes and edges", Required: true},
				},
			},
		}
	}

	// Log the tools being registered for debugging
	o.logger.Infof("🛠️  Registering %d local tools for LLM context", len(tools))
	for _, tool := range tools {
		o.logger.Infof("🔧 Local tool: %s - %s", tool.Name, tool.Description)
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
			// Convert map[string]string to map[string]interface{} with better validation
			argsMap := make(map[string]interface{}, len(node.Args))
			
			for k, v := range node.Args {
				// Handle special cases and type conversions
				// Convert interface{} to string for validation
				valStr := fmt.Sprintf("%v", v)
				if strings.TrimSpace(valStr) == "" {
					o.logger.Warnf("⚠️ Empty parameter value for %s in tool %s", k, node.ToolName)
					continue // Skip empty parameters
				}
				
				argsMap[k] = v // Keep original value type
				o.logger.Infof("🔍 Converting LLM arg: %s = '%v'", k, v)
			}

			// Add sensible defaults for common parameters if missing
			switch node.ToolName {
			case "list_files":
				if _, exists := argsMap["directory"]; !exists {
					argsMap["directory"] = "/shared"
					o.logger.Infof("🔧 Added default directory parameter for list_files: /shared")
				}
			case "write_file":
				if _, exists := argsMap["filename"]; !exists {
					argsMap["filename"] = "output.txt"
					o.logger.Infof("🔧 Added default filename parameter for write_file: output.txt")
				}
				if _, exists := argsMap["content"]; !exists {
					argsMap["content"] = "Content generated by AI orchestrator"
					o.logger.Infof("🔧 Added default content parameter for write_file")
				}
			}

			astNode := ASTNode{
				Type:     NodeTypeCall,
				Value:    "CALL", // Keep for backward compatibility
				ToolName: node.ToolName,
				Args:     argsMap, // Enhanced arguments map with validation
			}

			// Debug log the final AST node
			o.logger.Infof("🔧 Generated AST node for %s with %d args: %v", node.ToolName, len(argsMap), argsMap)
			ast.Nodes = append(ast.Nodes, astNode)
		} else {
			o.logger.Warnf("⚠️ Skipping invalid LLM node: Type=%s, ToolName=%s", node.Type, node.ToolName)
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

// Storage for workflows waiting to be executed
var storedWorkflows = make(map[string]struct {
	ast      *AST
	workflow map[string]interface{}
	dsl      string
})

// storeWorkflowForExecution stores workflow for later execution
func (o *OrchestratorAnalyzer) storeWorkflowForExecution(workflowID string, ast *AST, workflow map[string]interface{}, dsl string) {
	storedWorkflows[workflowID] = struct {
		ast      *AST
		workflow map[string]interface{}
		dsl      string
	}{
		ast:      ast,
		workflow: workflow,
		dsl:      dsl,
	}
	o.logger.Infof("📦 Stored workflow %s for execution (command: %s)", workflowID, dsl)
}

// ExecuteStoredWorkflow executes a previously stored workflow
func (o *OrchestratorAnalyzer) ExecuteStoredWorkflow(ctx context.Context, workflowID string) (interface{}, error) {
	stored, exists := storedWorkflows[workflowID]
	if !exists {
		return nil, fmt.Errorf("workflow %s not found", workflowID)
	}
	
	o.logger.Infof("▶️ Executing stored workflow %s (command: %s)", workflowID, stored.dsl)
	
	// Check if this is a Twitter command - handle specially
	if strings.Contains(strings.ToLower(stored.dsl), "tweet") || strings.Contains(strings.ToLower(stored.dsl), "twitter") {
		// Execute and handle Twitter result
		result, err := o.executeWithOrchestration(ctx, stored.ast, stored.workflow)
		if err != nil {
			return nil, fmt.Errorf("execution failed: %v", err)
		}
		
		// Handle Twitter scraper result
		go o.handleTwitterScraperResult(stored.dsl)
		
		return result, nil
	}
	
	// Execute the stored AST normally
	result, err := o.executeWithOrchestration(ctx, stored.ast, stored.workflow)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %v", err)
	}
	
	// Clean up after execution
	delete(storedWorkflows, workflowID)
	
	return result, nil
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
