package agentcard

// AgentSkill represents a capability or function offered by an agent
type AgentSkill struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Tags         []string `json:"tags,omitempty"`
	Examples     []string `json:"examples,omitempty"`
	InputModes   []string `json:"inputModes,omitempty"`
	OutputModes  []string `json:"outputModes,omitempty"`
}

// AgentProvider contains information about the organization behind the agent
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url"`
}

// AgentCapabilities defines what features the agent supports
type AgentCapabilities struct {
	Streaming               *bool `json:"streaming,omitempty"`
	PushNotifications       *bool `json:"pushNotifications,omitempty"`
	StateTransitionHistory  *bool `json:"stateTransitionHistory,omitempty"`
}

// AgentCard is the standard format for describing an agent's capabilities
type AgentCard struct {
	Name                string             `json:"name"`
	Description         string             `json:"description"`
	URL                 string             `json:"url"`
	Version             string             `json:"version"`
	ProtocolVersion     string             `json:"protocolVersion"`
	Provider            *AgentProvider     `json:"provider,omitempty"`
	Capabilities        AgentCapabilities  `json:"capabilities"`
	DefaultInputModes   []string           `json:"defaultInputModes"`
	DefaultOutputModes  []string           `json:"defaultOutputModes"`
	Skills              []AgentSkill       `json:"skills"`
	SecuritySchemes     interface{}        `json:"securitySchemes,omitempty"`
	Security            interface{}        `json:"security,omitempty"`
}

// ExtendedAgentCard includes MCP capabilities in addition to standard AgentCard
type ExtendedAgentCard struct {
	AgentCard
	MCPServers []MCPCapability `json:"mcp_servers,omitempty"`
}

// MCPCapability describes an MCP server available on an agent
type MCPCapability struct {
	ServerName   string        `json:"server_name"`
	Transport    string        `json:"transport"`
	Tools        []MCPTool     `json:"tools"`
	Resources    []MCPResource `json:"resources,omitempty"`
	Status       string        `json:"status"`
	LastSeen     interface{}   `json:"last_seen,omitempty"` // Using interface{} for time.Time to avoid import cycle
}

// MCPTool describes a tool provided by an MCP server
type MCPTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema"`
	OutputSchema map[string]interface{} `json:"output_schema,omitempty"`
}

// MCPResource describes a resource provided by an MCP server
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}