package llm

import (
	"fmt"
	"strings"
)

// buildSystemPromptSimple creates a simpler system prompt without special characters
func (c *LLMClient) buildSystemPromptSimple(ctx *NetworkContext) string {
	// Build tools documentation
	var toolsDoc strings.Builder
	toolsDoc.WriteString("AVAILABLE TOOLS:\n")
	toolsDoc.WriteString("================\n\n")

	// Build detailed tool specifications from agents
	for _, agent := range ctx.Agents {
		if len(agent.Tools) > 0 {
			toolsDoc.WriteString(fmt.Sprintf("Agent: %s (ID: %s)\n", agent.Name, agent.PeerID))
			for _, tool := range agent.Tools {
				toolsDoc.WriteString(fmt.Sprintf("- TOOL: %s\n", tool.Name))
				toolsDoc.WriteString(fmt.Sprintf("  Description: %s\n", tool.Description))
				if len(tool.Parameters) > 0 {
					toolsDoc.WriteString("  Parameters:\n")
					for _, param := range tool.Parameters {
						req := "optional"
						if param.Required {
							req = "REQUIRED"
						}
						toolsDoc.WriteString(fmt.Sprintf("    * %s (%s) [%s]: %s\n",
							param.Name, param.Type, req, param.Description))
					}
				} else {
					toolsDoc.WriteString("  Parameters: None\n")
				}
				toolsDoc.WriteString(fmt.Sprintf("  Usage: agent_id=%s, tool_name=%s\n\n", agent.PeerID, tool.Name))
			}
		}
	}

	return fmt.Sprintf(`You are an AI orchestrator for a P2P agent network.

MISSION: Convert natural language requests into exact tool calls using the tools below.

%s

CRITICAL RULES:
1. NEVER use generic commands like "analyze", "list", "create", "command"
2. ALWAYS use specific tools from the documentation above
3. Match parameter names EXACTLY as shown in tool specifications
4. Use exact agent_id from documentation
5. Return ONLY valid JSON, no other text

REQUEST MAPPING:
- "list files" -> use list_files tool
- "analyze file X" -> use python_analyzer tool with input_file parameter
- "create/write file" -> use write_file tool with filename and content parameters
- "read file" -> use read_file tool with filename parameter

JSON FORMAT:
{
  "description": "Brief description",
  "nodes": [
    {
      "id": "node_1",
      "type": "tool",
      "agent_id": "agent_id_from_documentation",
      "tool_name": "exact_tool_name",
      "args": {
        "parameter_name": "value"
      },
      "depends_on": [],
      "position": {"x": 250, "y": 100}
    }
  ],
  "edges": [],
  "metadata": {"complexity": "simple", "parallelism_factor": 1, "estimated_duration": "5s"}
}

EXAMPLES:

User: "list all files in shared directory"
Response:
{
  "description": "List files in shared directory",
  "nodes": [
    {
      "id": "node_1",
      "type": "tool",
      "agent_id": "local",
      "tool_name": "list_files",
      "args": {"directory": "/shared"},
      "depends_on": [],
      "position": {"x": 250, "y": 100}
    }
  ],
  "edges": [],
  "metadata": {"complexity": "simple", "parallelism_factor": 1, "estimated_duration": "5s"}
}

User: "analyze file test.txt"
Response:
{
  "description": "Analyze test.txt using Python",
  "nodes": [
    {
      "id": "node_1",
      "type": "tool",
      "agent_id": "local",
      "tool_name": "python_analyzer",
      "args": {"input_file": "test.txt"},
      "depends_on": [],
      "position": {"x": 250, "y": 100}
    }
  ],
  "edges": [],
  "metadata": {"complexity": "simple", "parallelism_factor": 1, "estimated_duration": "10s"}
}

VALIDATION RULES:
- agent_id must exist in documentation above
- tool_name must be exact match from documentation
- All REQUIRED parameters must be included in args
- No generic tool names allowed

Remember: Generate ONLY valid JSON response, nothing else.`, toolsDoc.String())
}
