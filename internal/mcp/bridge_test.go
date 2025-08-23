package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"go-p2p-agent/internal/config"
)

func TestMCPBridge_StartAndInvokeEchoTool(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.yaml")
	serverDir := filepath.Join("testdata", "echoserver")
	absServerDir, err := filepath.Abs(serverDir)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	binPath := filepath.Join(dir, "echoserver")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = absServerDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build server: %v\n%s", err, string(out))
	}

	cfg := config.MCPBridgeConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{
				Name:      "echo",
				Transport: "stdio",
				Command:   binPath,
				Args:      []string{},
				Enabled:   true,
			},
		},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	bridge, err := NewMCPBridge(nil, cfgPath, logger)
	if err != nil {
		t.Fatalf("NewMCPBridge: %v", err)
	}
	defer bridge.Shutdown()

	if err := bridge.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	caps := bridge.GetCapabilities()
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	if caps[0].ServerName != "echo" {
		t.Fatalf("unexpected server name: %s", caps[0].ServerName)
	}
	if len(caps[0].Tools) != 1 || caps[0].Tools[0].Name != "echo" {
		t.Fatalf("expected echo tool, got %+v", caps[0].Tools)
	}

	client := bridge.GetClient()
	result, err := client.InvokeTool(context.Background(), nil, "echo", "echo", map[string]interface{}{"text": "hello"})
	if err != nil {
		t.Fatalf("InvokeTool: %v", err)
	}

	resp, ok := result.(*mcp.ToolResponse)
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if len(resp.Content) != 1 || resp.Content[0].TextContent == nil {
		t.Fatalf("unexpected content %#v", resp.Content)
	}
	if resp.Content[0].TextContent.Text != "hello" {
		t.Fatalf("unexpected response text: %s", resp.Content[0].TextContent.Text)
	}
}