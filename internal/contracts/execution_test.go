package contracts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolContract(t *testing.T) {
	contract := ToolContract{
		Engine: "dagger",
		Name:   "test_contract",
		EngineSpec: map[string]interface{}{
			"image":   "python:3.11-slim",
			"command": []string{"python", "script.py"},
			"mounts":  map[string]string{"/shared": "./shared"},
			"address": "",
		},
	}

	assert.Equal(t, "dagger", contract.Engine)
	assert.Equal(t, "test_contract", contract.Name)
	assert.Equal(t, "python:3.11-slim", contract.EngineSpec["image"])
	command := contract.EngineSpec["command"].([]string)
	assert.Len(t, command, 2)
	assert.Contains(t, command, "python")
	assert.Contains(t, command, "script.py")
	mounts := contract.EngineSpec["mounts"].(map[string]string)
	assert.Equal(t, "./shared", mounts["/shared"])
}
