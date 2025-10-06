package agent

// AgentCard represents agent capabilities (A2A v0.2.9 compatible)
type AgentCard struct {
	Name                              string                 `json:"name"`
	Description                       string                 `json:"description"`
	URL                               string                 `json:"url"` // main A2A endpoint
	PreferredTransport                string                 `json:"preferredTransport"`
	AdditionalInterfaces              []AgentInterface       `json:"additionalInterfaces,omitempty"`
	Version                           string                 `json:"version"`
	ProtocolVersion                   string                 `json:"protocolVersion"` // e.g., "0.2.9"
	Provider                          *AgentProvider         `json:"provider,omitempty"`
	Capabilities                      AgentCapabilities      `json:"capabilities"`
	Skills                            []AgentSkill           `json:"skills"`
	SecuritySchemes                   map[string]interface{} `json:"securitySchemes,omitempty"`
	DefaultInputModes                 []string               `json:"defaultInputModes,omitempty"`
	DefaultOutputModes                []string               `json:"defaultOutputModes,omitempty"`
	Security                          []map[string][]string  `json:"security,omitempty"`
	SupportsAuthenticatedExtendedCard bool                   `json:"supportsAuthenticatedExtendedCard,omitempty"`
	IconURL                           string                 `json:"iconUrl,omitempty"`
	DocumentationURL                  string                 `json:"documentationUrl,omitempty"`
	Metadata                          interface{}            `json:"metadata,omitempty"`
	DID                               string                 `json:"did,omitempty"`
	DIDDocURI                         string                 `json:"didDocUri,omitempty"`
}

type AgentProvider struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	// Legacy field for backward compatibility
	Organization string `json:"organization,omitempty"`
}

// AgentInterface represents an additional interface endpoint for the agent
type AgentInterface struct {
	URL       string `json:"url"`
	Transport string `json:"transport"` // "JSONRPC" | "GRPC" | "HTTP+JSON"
}

// AgentCapabilities describes what the agent is capable of
type AgentCapabilities struct {
	Streaming              *bool `json:"streaming,omitempty"`
	PushNotifications      *bool `json:"pushNotifications,omitempty"`
	StateTransitionHistory *bool `json:"stateTransitionHistory,omitempty"` // renamed from StateTransition
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
