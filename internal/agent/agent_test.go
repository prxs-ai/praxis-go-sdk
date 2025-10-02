package agent

import (
	"encoding/json"
	"time"

	"context"
	libp2p "github.com/libp2p/go-libp2p"
	libhost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/multiformats/go-multiaddr"
	"github.com/praxis/praxis-go-sdk/internal/p2p"
	"testing"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/praxis/praxis-go-sdk/internal/a2a"
	"github.com/praxis/praxis-go-sdk/internal/contracts"
	"github.com/praxis/praxis-go-sdk/internal/dsl"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Execution Engine ---
type mockEngine struct {
	result string
	err    error
	called bool
}

func (m *mockEngine) Execute(ctx context.Context, contract contracts.ToolContract, args map[string]interface{}) (string, error) {
	m.called = true
	return m.result, m.err
}

// --- Utility tests ---

func TestHumanizeName(t *testing.T) {
	assert.Equal(t, "Twitter Scraper", humanizeName("twitter_scraper"))
	assert.Equal(t, "Tg Poster", humanizeName("tg-poster"))
	assert.Equal(t, "Hello World", humanizeName("hello   world"))
	assert.Equal(t, "", humanizeName(""))
}

func TestNormalizeArgs(t *testing.T) {
	raw := map[string]interface{}{
		"int":  float64(42),
		"flt":  float64(3.14),
		"text": "hello",
	}
	out := normalizeArgs(raw)

	assert.Equal(t, 42, out["int"])
	assert.Equal(t, 3.14, out["flt"])
	assert.Equal(t, "hello", out["text"])
}

func TestRedactSecrets(t *testing.T) {
	in := map[string]interface{}{
		"token": "supersecret",
		"key":   "abc123",
	}
	out := redactSecrets(in)
	assert.Equal(t, "***", out["token"])
	assert.Equal(t, "***", out["key"])
}

// --- Message parsing / helpers ---

func TestParseMessageFromParams_Valid(t *testing.T) {
	a := &PraxisAgent{logger: logrus.New()}

	params := map[string]interface{}{
		"role": "user",
		"parts": []interface{}{
			map[string]interface{}{"kind": "text", "text": "hello"},
		},
	}
	msg, err := a.parseMessageFromParams(params)
	require.NoError(t, err)
	assert.Equal(t, "user", msg.Role)
	assert.Len(t, msg.Parts, 1)
	assert.Equal(t, "hello", msg.Parts[0].Text)
}

func TestParseMessageFromParams_Invalid_NoText(t *testing.T) {
	a := &PraxisAgent{logger: logrus.New()}

	params := map[string]interface{}{
		"role":  "user",
		"parts": []interface{}{map[string]interface{}{"kind": "text", "text": ""}},
	}
	msg, err := a.parseMessageFromParams(params)
	assert.Nil(t, msg)
	assert.Error(t, err)
}

func TestGetTextFromMessage(t *testing.T) {
	a := &PraxisAgent{}
	msg := a2a.Message{
		Parts: []a2a.Part{
			{Kind: "text", Text: "first"},
			{Kind: "text", Text: "second"},
		},
	}
	assert.Equal(t, "first", a.getTextFromMessage(msg))
}

func TestHandleExecuteWorkflow_InjectsParamsAndSecrets(t *testing.T) {
	agent := &PraxisAgent{
		logger:      logrus.New(),
		dslAnalyzer: dsl.NewAnalyzer(logrus.New()),
	}

	dslQuery := "CALL test_tool arg1"

	rawParams := map[string]interface{}{"user": "Alice"}
	rawSecrets := map[string]interface{}{"api_key": "SECRET123"}

	// Arguments map is nested inside Params
	req := mcpTypes.CallToolRequest{}
	req.Params.Name = "execute_workflow"
	req.Params.Arguments = map[string]interface{}{
		"dsl":     dslQuery,
		"params":  rawParams,
		"secrets": rawSecrets,
	}

	result, err := agent.handleExecuteWorkflow(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Content)

	// The result.Content slice can hold TextContent, so cast to that type
	textContent, ok := result.Content[0].(mcpTypes.TextContent)
	require.True(t, ok)

	assert.Contains(t, textContent.Text, "Alice")     // param consumed
	assert.NotContains(t, textContent.Text, "SECRET") // secret must not leak
}

func TestRegisterDIDWithRegistry_Succeeds(t *testing.T) {
	t.Parallel()

	// --- Spin up a fake "registry" libp2p host with the DID handler
	registryHost, registryClose := mustNewHost(t)
	defer registryClose()

	// Minimal handler that echos back {status:"ok", did, peer_info}
	registryHost.SetStreamHandler(p2p.ProtocolDidRegister, func(s network.Stream) {
		defer s.Close()

		var payload map[string]any
		if err := json.NewDecoder(s).Decode(&payload); err != nil {
			_ = json.NewEncoder(s).Encode(map[string]any{
				"error": err.Error(),
				"code":  400,
			})
			return
		}
		_ = json.NewEncoder(s).Encode(map[string]any{
			"status":    "ok",
			"did":       payload["did"],
			"peer_info": payload["peer_info"],
		})
	})

	// Build a full registry multiaddr: /ip4/127.0.0.1/tcp/<port>/p2p/<peerID>
	registryMaddr := firstFullP2pAddr(t, registryHost)

	// --- Build an agent with its own libp2p host
	agentHost, agentClose := mustNewHost(t)
	defer agentClose()

	a := &PraxisAgent{
		logger: logrus.New(),
		host:   agentHost,
		// ctx/cancel aren’t required by RegisterDIDWithRegistry; use a local ctx here.
	}

	// --- Call the new method
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	did := "did:example:test-did-123"
	err := a.RegisterDIDWithRegistry(ctx, registryMaddr, did)

	require.NoError(t, err, "RegisterDIDWithRegistry should succeed")
}

// mustNewHost creates a libp2p host bound to a random local port and returns a closer.
func mustNewHost(t *testing.T) (libhost.Host, func()) {
	t.Helper()

	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
	)
	require.NoError(t, err)

	closer := func() {
		_ = h.Close()
	}
	return h, closer
}

// firstFullP2pAddr returns "<addr>/p2p/<peerID>" built from the host’s first listen addr.
func firstFullP2pAddr(t *testing.T, h libhost.Host) string {
	t.Helper()

	addrs := h.Addrs()
	require.NotEmpty(t, addrs, "host has no listen addrs")

	pid := h.ID()
	base := addrs[0]
	// Ensure we don’t double-append /p2p
	if !hasP2pSuffix(base) {
		s, err := multiaddr.NewMultiaddr(base.String() + "/p2p/" + pid.String())
		require.NoError(t, err)
		return s.String()
	}
	return base.String()
}

func hasP2pSuffix(a multiaddr.Multiaddr) bool {
	return containsSegment(a, "p2p")
}

func containsSegment(a multiaddr.Multiaddr, seg string) bool {
	for _, p := range a.Protocols() {
		if p.Name == seg {
			return true
		}
	}
	return false
}
