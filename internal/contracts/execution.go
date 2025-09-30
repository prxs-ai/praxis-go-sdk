package contracts

import "context"

type ToolContract struct {
	Engine     string                 `json:"engine"`
	Name       string                 `json:"name"`
	EngineSpec map[string]interface{} `json:"engineSpec"`

	Params  map[string]interface{} `json:"params,omitempty"`
	Secrets map[string]interface{} `json:"secrets,omitempty"`
}

// ExecutionEngine defines an abstraction for executing any ToolContract.
type ExecutionEngine interface {
	Execute(ctx context.Context, contract ToolContract, args map[string]interface{}) (string, error)
}
