// Package a2a contains structures for A2A protocol v0.2.9 specification
package a2a

type TransportProtocol string

const (
	TransportJSONRPC  TransportProtocol = "JSONRPC"
	TransportGRPC     TransportProtocol = "GRPC"
	TransportHTTPJSON TransportProtocol = "HTTP+JSON"
)

// Доп. интерфейсы (транспорт + URL/URI)
type AgentInterface struct {
	Transport TransportProtocol `json:"transport"`
	URL       string            `json:"url"`
}

type AgentProvider struct {
    Organization string `json:"organization,omitempty"`
    URL          string `json:"url,omitempty"`
}

// === ERC-8004 extensions ===
// Registration record proving on-chain identity binding for the agent.
// agentAddress must be encoded as CAIP-10, e.g. "eip155:11155111:0xABC...".
type ERC8004Registration struct {
    AgentID      uint64 `json:"agentId"`
    AgentAddress string `json:"agentAddress"`
    Signature    string `json:"signature"` // EIP-191/EIP-712 proof of address ownership
}

type AgentExtension struct {
	URI         string                 `json:"uri"`
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

type AgentCapabilities struct {
	Streaming              bool             `json:"streaming,omitempty"`
	PushNotifications      bool             `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool             `json:"stateTransitionHistory,omitempty"`
	Extensions             []AgentExtension `json:"extensions,omitempty"`
}

type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

type AgentCardSignature struct {
	Protected string                 `json:"protected"`
	Signature string                 `json:"signature"`
	Header    map[string]interface{} `json:"header,omitempty"`
}

// Каноническая A2A Agent Card (см. §5.5, протокол по умолчанию "0.2.9")
type AgentCard struct {
    ProtocolVersion                   string                  `json:"protocolVersion"`
    Name                              string                  `json:"name"`
    Description                       string                  `json:"description"`
    URL                               string                  `json:"url,omitempty"`
    PreferredTransport                TransportProtocol       `json:"preferredTransport,omitempty"`
    AdditionalInterfaces              []AgentInterface        `json:"additionalInterfaces,omitempty"`
    IconURL                           string                  `json:"iconUrl,omitempty"`
    Provider                          *AgentProvider          `json:"provider,omitempty"`
    Version                           string                  `json:"version,omitempty"`
    DocumentationURL                  string                  `json:"documentationUrl,omitempty"`
    Capabilities                      AgentCapabilities       `json:"capabilities"`
    SecuritySchemes                   map[string]any          `json:"securitySchemes,omitempty"`
    Security                          []map[string][]string   `json:"security,omitempty"`
    DefaultInputModes                 []string                `json:"defaultInputModes"`
    DefaultOutputModes                []string                `json:"defaultOutputModes"`
    Skills                            []AgentSkill            `json:"skills"`
    SupportsAuthenticatedExtendedCard bool                    `json:"supportsAuthenticatedExtendedCard,omitempty"`
    Signatures                        []AgentCardSignature    `json:"signatures,omitempty"`

    // ERC-8004 specific data at top-level per reference card
    Registrations []ERC8004Registration `json:"registrations,omitempty"`
    TrustModels   []string              `json:"trustModels,omitempty"`
}
