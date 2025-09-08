package contracts

import "context"

// ToolContract является языково-независимым "API контрактом" для любого инструмента.
type ToolContract struct {
	Engine     string                 `json:"engine"`
	Name       string                 `json:"name"`
	EngineSpec map[string]interface{} `json:"engineSpec"`
}

// ExecutionEngine определяет абстракцию для выполнения любого ToolContract.
type ExecutionEngine interface {
	Execute(ctx context.Context, contract ToolContract, args map[string]interface{}) (string, error)
}
