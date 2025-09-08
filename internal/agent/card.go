package agent

// AgentCard represents agent capabilities (A2A compatible)
type AgentCard struct {
	Name               string                 `json:"name"`
	Description        string                 `json:"description"`
	URL                string                 `json:"url"`
	Version            string                 `json:"version"`
	ProtocolVersion    string                 `json:"protocolVersion"`
	Provider           *AgentProvider         `json:"provider,omitempty"`
	Capabilities       AgentCapabilities      `json:"capabilities"`
	Skills             []AgentSkill           `json:"skills"`
	SecuritySchemes    map[string]interface{} `json:"securitySchemes,omitempty"`
	SupportedTransports []string              `json:"supportedTransports"`
	Metadata           interface{}            `json:"metadata,omitempty"`
}

type AgentProvider struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	// Legacy field for backward compatibility
	Organization string `json:"organization,omitempty"`
}

type AgentCapabilities struct {
	Streaming         *bool `json:"streaming,omitempty"`
	PushNotifications *bool `json:"pushNotifications,omitempty"`
	StateTransition   *bool `json:"stateTransition,omitempty"`
}

type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}
