package dsl

type WorkflowOptions struct {
	Params  map[string]interface{} `json:"params,omitempty"`
	Secrets map[string]string      `json:"secrets,omitempty"`
}
