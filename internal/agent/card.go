package agent

type AgentCard struct {
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	URL             string             `json:"url"`
	Version         string             `json:"version"`
	ProtocolVersion string             `json:"protocolVersion"`
	Provider        *AgentProvider     `json:"provider,omitempty"`
	Capabilities    AgentCapabilities  `json:"capabilities"`
	Skills          []AgentSkill       `json:"skills"`
}

type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url"`
}

type AgentCapabilities struct {
	Streaming         *bool `json:"streaming,omitempty"`
	PushNotifications *bool `json:"pushNotifications,omitempty"`
	StateTransition   *bool `json:"stateTransitionHistory,omitempty"`
}

type AgentSkill struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Tags         []string `json:"tags,omitempty"`
	Examples     []string `json:"examples,omitempty"`
	InputModes   []string `json:"inputModes,omitempty"`
	OutputModes  []string `json:"outputModes,omitempty"`
}