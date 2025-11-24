package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/gin-gonic/gin"
	p2pforge "github.com/ipshipyard/p2p-forge/client"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ws "github.com/libp2p/go-libp2p/p2p/transport/websocket"
	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/praxis/praxis-go-sdk/internal/bus"
	appconfig "github.com/praxis/praxis-go-sdk/internal/config"
	"github.com/praxis/praxis-go-sdk/internal/contracts"
	"github.com/praxis/praxis-go-sdk/internal/dagger"
	"github.com/praxis/praxis-go-sdk/internal/did"
	didweb "github.com/praxis/praxis-go-sdk/internal/did/web"
	didwebvh "github.com/praxis/praxis-go-sdk/internal/did/webvh"
	applogger "github.com/praxis/praxis-go-sdk/internal/logger"
	"github.com/praxis/praxis-go-sdk/internal/mcp"
	"github.com/praxis/praxis-go-sdk/internal/metrics"
	"github.com/praxis/praxis-go-sdk/internal/p2p"
	"github.com/sirupsen/logrus"
)

const autoTLSUserAgent = "praxis-agent/autotls"

type PraxisAgent struct {
	name             string
	version          string
	host             host.Host
	discovery        *p2p.Discovery
	mcpServer        *mcp.MCPServerWrapper
	p2pBridge        *p2p.P2PMCPBridge
	p2pProtocol      *p2p.P2PProtocolHandler
	httpServer       *gin.Engine
	httpPort         int
	p2pPort          int
	ssePort          int
	websocketPort    int
	eventBus         *bus.EventBus
	logger           *logrus.Logger
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	cardMu           sync.RWMutex
	card             *AgentCard
	transportManager *mcp.TransportManager
	executionEngines map[string]contracts.ExecutionEngine
	appConfig        *appconfig.AppConfig
	registryMaddr    string
	did              string
	identityManager  *IdentityManager
	didResolver      did.Resolver
	securityConfig   appconfig.AgentSecurityConfig
	autoTLSCertMgr   *p2pforge.P2PForgeCertMgr
	httpSrv          *http.Server
	metricsCollector *metrics.MetricsCollector
}

type Config struct {
	AgentName     string
	AgentVersion  string
	HTTPPort      int
	P2PPort       int
	SSEPort       int
	WebSocketPort int
	MCPEnabled    bool
	LogLevel      string
	// Pass loaded application config so agent doesn't reload a hardcoded path
	AppConfig         *appconfig.AppConfig
	RegistryMultiaddr string // e.g. "/ip4/127.0.0.1/tcp/4001/p2p/<REGISTRY_PEER_ID>"
	DID               string // e.g. "did:key:z6Mk..."
}

func NewPraxisAgent(config Config) (*PraxisAgent, error) {
	logger := logrus.New()

	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize EventBus
	eventBus := bus.NewEventBus(logger)

	// Add WebSocket log hook
	logHook := applogger.NewWebSocketLogHook(eventBus, config.AgentName)
	logger.AddHook(logHook)

	agent := &PraxisAgent{
		name:          config.AgentName,
		version:       config.AgentVersion,
		httpPort:      config.HTTPPort,
		p2pPort:       config.P2PPort,
		ssePort:       config.SSEPort,
		websocketPort: config.WebSocketPort,
		eventBus:      eventBus,
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		registryMaddr: config.RegistryMultiaddr,
		did:           config.DID,
	}

	// ADDED: Инициализация менеджера транспортов и исполнительных движков
	agent.transportManager = mcp.NewTransportManager(logger)
	agent.executionEngines = make(map[string]contracts.ExecutionEngine)

	// Инициализация Remote MCP Engine (всегда доступен)
	remoteMCPEngine := mcp.NewRemoteMCPEngine(agent.transportManager)
	agent.executionEngines["remote-mcp"] = remoteMCPEngine
	logger.Info("📡 Remote MCP Engine initialized successfully")

	// Dagger Engine будет инициализирован позже, при первом использовании
	// Это избегает ошибок запуска, когда Docker не доступен
	logger.Info("🚀 Dagger Engine will be initialized on first use")

	// Use provided application configuration (from main or env), fallback to defaults
	if config.AppConfig != nil {
		agent.appConfig = config.AppConfig
	} else {
		logger.Warn("AppConfig not provided to agent; using defaults")
		agent.appConfig = appconfig.DefaultConfig()
	}
	agent.securityConfig = agent.appConfig.Agent.Security

	if err := agent.initializeP2P(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize P2P: %w", err)
	}

	// Initialize metrics collector after P2P is ready (to get peer_id)
	peerID := agent.host.ID().String()
	agent.metricsCollector = metrics.NewMetricsCollector(logger, config.AgentName, config.AgentVersion, peerID)
	logger.Info("📊 Metrics collector initialized")

	agent.initializeAgentCard()

	if err := agent.initializeIdentity(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize identity: %w", err)
	}

	// Initialize HTTP server AFTER A2A card is initialized
	agent.initializeHTTP()

	if config.MCPEnabled {
		if err := agent.initializeMCP(); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize MCP: %w", err)
		}

		// Auto-discover and register external MCP tools
		go agent.discoverAndRegisterExternalTools(ctx)
	}

	logger.Infof("Praxis Agent %s v%s initialized", config.AgentName, config.AgentVersion)

	return agent, nil
}

func (a *PraxisAgent) initializeP2P() error {
	var (
		priv crypto.PrivKey
		err  error
	)

	identityPath := ""
	if a.appConfig != nil {
		identityPath = a.appConfig.P2P.AutoTLS.IdentityKeyPath
	}
	priv, err = loadOrCreateP2PIdentity(identityPath, a.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize P2P identity: %w", err)
	}

	listenAddrs := []string{
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", a.p2pPort),
		fmt.Sprintf("/ip6/::/tcp/%d", a.p2pPort),
	}

	var (
		tlsConfig   *tls.Config
		forgeDomain = p2pforge.DefaultForgeDomain
	)
	if a.appConfig != nil && a.appConfig.P2P.AutoTLS.Enabled {
		mgr, err := a.initAutoTLS()
		if err != nil {
			return fmt.Errorf("failed to initialize AutoTLS: %w", err)
		}
		a.autoTLSCertMgr = mgr
		if domain := strings.TrimSpace(a.appConfig.P2P.AutoTLS.ForgeDomain); domain != "" {
			forgeDomain = domain
		}
		listenAddrs = append(listenAddrs,
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d/tls/sni/*.%s/ws", a.p2pPort, forgeDomain),
			fmt.Sprintf("/ip6/::/tcp/%d/tls/sni/*.%s/ws", a.p2pPort, forgeDomain),
		)
		tlsConfig = mgr.TLSConfig()
	} else {
		a.autoTLSCertMgr = nil
	}

	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.ListenAddrStrings(listenAddrs...),
		libp2p.DisableRelay(),
		libp2p.UserAgent(autoTLSUserAgent),
	}

	if a.appConfig == nil || a.appConfig.P2P.EnableNATPortMap {
		a.logger.Info("Attempting UPnP/NAT-PMP port mapping")
		opts = append(opts, libp2p.NATPortMap())
	} else {
		a.logger.Debug("UPnP/NAT-PMP port mapping disabled via configuration")
	}

	if a.autoTLSCertMgr != nil {
		opts = append(opts,
			libp2p.ShareTCPListener(),
			libp2p.Transport(ws.New, ws.WithTLSConfig(tlsConfig)),
		)
	}

	var advertisedAddrs []maddr.Multiaddr
	if a.appConfig != nil {
		for _, addrStr := range a.appConfig.P2P.AdvertiseAddrs {
			addrStr = strings.TrimSpace(addrStr)
			if addrStr == "" {
				continue
			}
			ma, err := maddr.NewMultiaddr(addrStr)
			if err != nil {
				a.logger.Warnf("Ignoring invalid advertise multiaddr %q: %v", addrStr, err)
				continue
			}
			advertisedAddrs = append(advertisedAddrs, ma)
		}
		if len(advertisedAddrs) > 0 {
			a.logger.Infof("Advertising additional reachability addresses: %s", strings.Join(multiaddrStrings(advertisedAddrs), ", "))
		}
	}

	var addrFactory func([]maddr.Multiaddr) []maddr.Multiaddr
	if a.autoTLSCertMgr != nil {
		mgrFactory := a.autoTLSCertMgr.AddressFactory()
		addrFactory = func(addrs []maddr.Multiaddr) []maddr.Multiaddr {
			return mgrFactory(addrs)
		}
	}

	if len(advertisedAddrs) > 0 {
		prevFactory := addrFactory
		addrFactory = func(addrs []maddr.Multiaddr) []maddr.Multiaddr {
			var out []maddr.Multiaddr
			if prevFactory != nil {
				out = prevFactory(addrs)
			} else {
				out = append([]maddr.Multiaddr{}, addrs...)
			}
			out = append(out, advertisedAddrs...)
			return dedupeMultiaddrs(out)
		}
	}

	if addrFactory != nil {
		opts = append(opts, libp2p.AddrsFactory(addrFactory))
	}

	host, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	a.host = host
	if err := host.Peerstore().AddPrivKey(host.ID(), priv); err != nil {
		return fmt.Errorf("failed to add private key to peerstore: %w", err)
	}
	if err := host.Peerstore().AddPubKey(host.ID(), priv.GetPublic()); err != nil {
		return fmt.Errorf("failed to add public key to peerstore: %w", err)
	}
	a.logger.Infof("P2P host created with ID: %s", host.ID())

	for _, addr := range host.Addrs() {
		a.logger.Infof("Listening on: %s/p2p/%s", addr, host.ID())
	}

	// AutoTLS: provide host and private key explicitly to avoid peerstore issues, then start
	if a.autoTLSCertMgr != nil {
		a.autoTLSCertMgr.ProvideHostAndPrivKey(host, priv)
		if err := a.autoTLSCertMgr.Start(); err != nil {
			return fmt.Errorf("failed to start AutoTLS manager: %w", err)
		}
	}

	if a.appConfig != nil && a.appConfig.P2P.EnableDHT {
		go func() {
			dhtClient, err := dht.New(a.ctx, host, dht.Mode(dht.ModeClient))
			if err != nil {
				a.logger.Warnf("DHT init failed: %v", err)
				return
			}
			if err := dhtClient.Bootstrap(a.ctx); err != nil {
				a.logger.Warnf("DHT bootstrap failed: %v", err)
			}
		}()
	}

	// Initialize P2P protocol handler for direct P2P communication
	a.p2pProtocol = p2p.NewP2PProtocolHandler(host, a.logger)
	a.p2pProtocol.ConfigureSecurity(p2p.SecurityOptions{
		VerifyPeerCards: a.securityConfig.VerifyPeerCards,
		SignA2A:         a.securityConfig.SignA2A,
		VerifyA2A:       a.securityConfig.VerifyA2A,
	})

	// Initialize discovery
	discovery, err := p2p.NewDiscovery(host, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create discovery: %w", err)
	}
	a.discovery = discovery

	// Connect discovery and protocol handler for automatic card exchange
	discovery.SetProtocolHandler(a.p2pProtocol)

	// Start discovery
	if err := a.discovery.Start(); err != nil {
		return fmt.Errorf("failed to start discovery: %w", err)
	}

	if a.registryMaddr != "" && a.did != "" {
		go func(regMaddr, did string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := a.RegisterDIDWithRegistry(ctx, regMaddr, did); err != nil {
				a.logger.Warnf("DID registration skipped/failed: %v (registry=%s did=%s)", err, regMaddr, did)
			}
		}(a.registryMaddr, a.did)
	} else {
		a.logger.Debug("DID auto-registration not configured (REGISTRY_MADDR or AGENT_DID missing)")
	}

	return nil
}

func dedupeMultiaddrs(addrs []maddr.Multiaddr) []maddr.Multiaddr {
	seen := make(map[string]struct{}, len(addrs))
	result := make([]maddr.Multiaddr, 0, len(addrs))
	for _, addr := range addrs {
		key := addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, addr)
	}
	return result
}

func multiaddrsToStrings(addrs []maddr.Multiaddr) []string {
	result := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		result = append(result, addr.String())
	}
	return result
}

func (a *PraxisAgent) RegisterDIDWithRegistry(ctx context.Context, registryMultiaddr, did string) error {
	if a == nil || a.host == nil {
		return fmt.Errorf("p2p host not initialized")
	}
	if registryMultiaddr == "" {
		return fmt.Errorf("registry multiaddr is empty")
	}
	if did == "" {
		return fmt.Errorf("did is empty")
	}

	addr, err := maddr.NewMultiaddr(registryMultiaddr)
	if err != nil {
		return fmt.Errorf("parse registry multiaddr: %w", err)
	}

	info, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		return fmt.Errorf("extract registry peer info: %w", err)
	}

	ctxConnect, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := a.host.Connect(ctxConnect, *info); err != nil {
		if !strings.Contains(err.Error(), "already connected") {
			return fmt.Errorf("connect to registry %s: %w", info.ID, err)
		}
	}

	stream, err := a.host.NewStream(ctx, info.ID, p2p.ProtocolDidRegister)
	if err != nil {
		return fmt.Errorf("open did-register stream: %w", err)
	}
	defer stream.Close()

	payload := map[string]interface{}{
		"did": did,
		"peer_info": map[string]interface{}{
			"id":    a.host.ID().String(),
			"addrs": multiaddrsToStrings(dedupeMultiaddrs(a.host.Addrs())),
		},
	}

	if err := json.NewEncoder(stream).Encode(payload); err != nil {
		return fmt.Errorf("send did registration: %w", err)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return fmt.Errorf("read registry response: %w", err)
	}

	if status, _ := resp["status"].(string); strings.ToLower(status) != "ok" {
		return fmt.Errorf("registry returned status %v", resp["status"])
	}

	a.logger.Infof("✅ Registered DID %s with registry %s", did, registryMultiaddr)
	return nil
}

func multiaddrStrings(addrs []maddr.Multiaddr) []string {
	strs := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		strs = append(strs, addr.String())
	}
	return strs
}

func (a *PraxisAgent) initAutoTLS() (*p2pforge.P2PForgeCertMgr, error) {
	if a.appConfig == nil {
		return nil, fmt.Errorf("app config not loaded")
	}

	cfg := a.appConfig.P2P.AutoTLS
	if !cfg.Enabled {
		return nil, nil
	}

	caEndpoint := p2pforge.DefaultCATestEndpoint
	switch strings.ToLower(cfg.CA) {
	case "production":
		caEndpoint = p2pforge.DefaultCAEndpoint
	case "staging", "":
		// use staging endpoint
	default:
		// allow overriding with fully qualified URL if provided
		caEndpoint = cfg.CA
	}

	storagePath := cfg.CertDir
	if storagePath == "" {
		storagePath = p2pforge.DefaultStorageLocation
	}

	options := []p2pforge.P2PForgeCertMgrOptions{
		p2pforge.WithCAEndpoint(caEndpoint),
		p2pforge.WithCertificateStorage(&certmagic.FileStorage{Path: storagePath}),
		p2pforge.WithUserAgent(autoTLSUserAgent),
	}

	if domain := strings.TrimSpace(cfg.ForgeDomain); domain != "" {
		options = append(options, p2pforge.WithForgeDomain(domain))
	}
	if endpoint := strings.TrimSpace(cfg.RegistrationEndpoint); endpoint != "" {
		options = append(options, p2pforge.WithForgeRegistrationEndpoint(endpoint))
		if parsed, err := url.Parse(endpoint); err == nil {
			hostHeader := parsed.Hostname()
			if hostHeader != "" {
				options = append(options, p2pforge.WithModifiedForgeRequest(func(r *http.Request) error {
					r.Host = hostHeader
					r.Header.Set("Host", hostHeader)
					return nil
				}))
			}
		} else {
			a.logger.Warnf("invalid AutoTLS registration endpoint: %v", err)
		}
	}
	if token := strings.TrimSpace(cfg.ForgeAuthToken); token != "" {
		options = append(options, p2pforge.WithForgeAuth(token))
	}

	if cfg.RegistrationDelaySec > 0 {
		options = append(options, p2pforge.WithRegistrationDelay(time.Duration(cfg.RegistrationDelaySec)*time.Second))
	}
	if cfg.AllowPrivateAddresses {
		options = append(options, p2pforge.WithAllowPrivateForgeAddrs())
	}
	if cfg.ProduceShortAddrs {
		options = append(options, p2pforge.WithShortForgeAddrs(true))
	}
	if roots := strings.TrimSpace(cfg.TrustedRootsFile); roots != "" {
		pool, err := loadTrustedRoots(roots)
		if err != nil {
			return nil, fmt.Errorf("load trusted roots: %w", err)
		}
		options = append(options, p2pforge.WithTrustedRoots(pool))
	}
	if cfg.ResolverAddress != "" {
		resolver := buildResolver(strings.TrimSpace(cfg.ResolverNetwork), strings.TrimSpace(cfg.ResolverAddress))
		options = append(options, p2pforge.WithResolver(resolver))
	}

	options = append(options, p2pforge.WithOnCertLoaded(func() {
		if a.host == nil {
			return
		}
		a.logger.Info("AutoTLS certificate loaded")
		for _, addr := range a.host.Addrs() {
			a.logger.Infof("AutoTLS address: %s/p2p/%s", addr, a.host.ID())
		}
	}))

	mgr, err := p2pforge.NewP2PForgeCertMgr(options...)
	if err != nil {
		return nil, err
	}

	return mgr, nil
}

func loadTrustedRoots(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no certificates found in %s", path)
	}
	return pool, nil
}

func buildResolver(network, address string) *net.Resolver {
	if network == "" {
		network = "udp"
	}
	dialer := &net.Dialer{}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
	}
}

func (a *PraxisAgent) initializeMCP() error {
	serverConfig := mcp.ServerConfig{
		Name:            a.name,
		Version:         a.version,
		Transport:       mcp.TransportSSE,
		Port:            fmt.Sprintf(":%d", a.ssePort),
		Logger:          a.logger,
		EnableTools:     true,
		EnableResources: true,
		EnablePrompts:   true,
	}

	mcpServer, err := mcp.NewMCPServer(serverConfig)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}

	a.mcpServer = mcpServer

	a.p2pBridge = p2p.NewP2PMCPBridge(a.host, a.mcpServer, a.logger)

	// Connect the MCP bridge to the P2P protocol handler for tool execution
	if a.p2pProtocol != nil {
		a.p2pProtocol.SetMCPBridge(a.p2pBridge)
	}

	a.registerMCPHandlers()

	// Update P2P card with full tool specifications after all tools are registered
	a.updateP2PCardWithTools()

	if err := a.mcpServer.StartSSE(fmt.Sprintf(":%d", a.ssePort)); err != nil {
		return fmt.Errorf("failed to start SSE server: %w", err)
	}

	a.logger.Infof("MCP SSE server started on port %d", a.ssePort)

	return nil
}

func (a *PraxisAgent) registerMCPHandlers() {
	if a.card != nil {
		cardResource := mcp.NewAgentCardResource(a.card, a.logger)
		a.mcpServer.AddResource(cardResource.GetResource(), cardResource.Handler)
	}

	p2pTool := mcp.NewP2PTool(a.p2pBridge, a.logger)
	a.mcpServer.AddTool(p2pTool.GetListPeersTool(), p2pTool.ListPeersHandler)
	a.mcpServer.AddTool(p2pTool.GetSendMessageTool(), p2pTool.SendMessageHandler)

	// Dynamic tool registration from configuration
	a.registerDynamicTools()
}

func (a *PraxisAgent) registerDynamicTools() {

	// Register tools from configuration
	if a.appConfig != nil && len(a.appConfig.Agent.Tools) > 0 {
		for _, toolCfg := range a.appConfig.Agent.Tools {
			a.logger.Infof("Registering tool from config: %s (engine: %s)", toolCfg.Name, toolCfg.Engine)

			// Build MCP tool parameters
			var mcpOptions []mcpTypes.ToolOption
			mcpOptions = append(mcpOptions, mcpTypes.WithDescription(toolCfg.Description))

			for _, param := range toolCfg.Params {
				var paramOpts []mcpTypes.PropertyOption

				// Add description if provided
				if description, ok := param["description"]; ok && description != "" {
					paramOpts = append(paramOpts, mcpTypes.Description(description))
				}

				// Add required constraint
				if required, ok := param["required"]; ok && required == "true" {
					paramOpts = append(paramOpts, mcpTypes.Required())
				}

				// Add parameter based on type
				paramType := param["type"]
				paramName := param["name"]
				switch paramType {
				case "string":
					mcpOptions = append(mcpOptions, mcpTypes.WithString(paramName, paramOpts...))
				case "number":
					mcpOptions = append(mcpOptions, mcpTypes.WithNumber(paramName, paramOpts...))
				case "boolean":
					mcpOptions = append(mcpOptions, mcpTypes.WithBoolean(paramName, paramOpts...))
				default:
					mcpOptions = append(mcpOptions, mcpTypes.WithString(paramName, paramOpts...))
				}
			}

			toolSpec := mcpTypes.NewTool(toolCfg.Name, mcpOptions...)

			// Choose handler based on engine
			switch toolCfg.Engine {
			case "dagger", "remote-mcp":
				handler := a.createGenericHandler(toolCfg)
				a.mcpServer.AddTool(toolSpec, handler)
				a.logger.Infof("Registered '%s' tool from config: %s", toolCfg.Engine, toolCfg.Name)
			default:
				a.logger.Warnf("Unknown engine '%s' for tool '%s'", toolCfg.Engine, toolCfg.Name)
			}
		}
	} else {
		a.logger.Warn("No tools found in configuration")
	}
}

func (a *PraxisAgent) createGenericHandler(toolCfg appconfig.ToolConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
		args := normalizeArgs(req.GetArguments())
		engineName := toolCfg.Engine

		// Extract params and secrets from args if nested
		var params map[string]interface{}
		var secrets map[string]interface{}

		if raw, ok := args["params"].(map[string]interface{}); ok {
			params = normalizeArgs(raw)
		} else {
			params = args
		}
		if raw, ok := args["secrets"].(map[string]interface{}); ok {
			secrets = normalizeArgs(raw)
		} else {
			secrets = map[string]interface{}{}
		}

		a.logger.Infof("Executing tool '%s' with engine '%s'. Params: %v, Secrets: %v",
			toolCfg.Name, engineName, params, redactSecrets(secrets))

		engine, ok := a.executionEngines[engineName]
		if !ok {
			if engineName == "dagger" {
				a.logger.Info("Initializing Dagger Engine on first use...")
				daggerEngine, err := dagger.NewEngine(ctx)
				if err != nil {
					a.logger.Errorf("Failed to initialize Dagger Engine: %v", err)
					return mcpTypes.NewToolResultError(fmt.Sprintf("Dagger Engine initialization failed: %v", err)), nil
				}
				a.executionEngines["dagger"] = daggerEngine
				engine = daggerEngine
				a.logger.Info("🚀 Dagger Engine initialized successfully")
			} else {
				err := fmt.Errorf("execution engine '%s' not found", engineName)
				a.logger.Error(err)
				return mcpTypes.NewToolResultError(err.Error()), nil
			}
		}

		contract := contracts.ToolContract{
			Engine:     engineName,
			Name:       toolCfg.Name,
			EngineSpec: toolCfg.EngineSpec,
			Params:     params,
			Secrets:    secrets,
		}

		for _, param := range toolCfg.Params {
			paramName := param["name"]
			isRequired := param["required"] == "true"
			if isRequired {
				if _, exists := params[paramName]; !exists {
					return mcpTypes.NewToolResultError(fmt.Sprintf("required parameter '%s' missing", paramName)), nil
				}
			}
		}

		result, err := engine.Execute(ctx, contract, params)
		if err != nil {
			a.logger.Errorf("Tool '%s' execution failed: %v", toolCfg.Name, err)
			return mcpTypes.NewToolResultError(err.Error()), nil
		}

		return mcpTypes.NewToolResultText(result), nil
	}
}

// handleDaggerTool обратная совместимость для старых вызовов
func (a *PraxisAgent) handleDaggerTool(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	// This is kept for backward compatibility
	args := req.GetArguments()

	contract := contracts.ToolContract{
		Engine: "dagger",
		Name:   "python_analyzer",
		EngineSpec: map[string]interface{}{
			"image":   "python:3.11-slim",
			"command": []string{"python", "/shared/analyzer.py"},
			"mounts":  map[string]string{a.appConfig.Agent.SharedDir: "/shared"},
		},
	}

	// Get or initialize Dagger Engine
	engine, ok := a.executionEngines["dagger"]
	if !ok {
		a.logger.Info("Initializing Dagger Engine on first use...")
		daggerEngine, err := dagger.NewEngine(ctx)
		if err != nil {
			a.logger.Errorf("Failed to initialize Dagger Engine: %v", err)
			return mcpTypes.NewToolResultError(fmt.Sprintf("Dagger Engine initialization failed: %v", err)), nil
		}
		a.executionEngines["dagger"] = daggerEngine
		engine = daggerEngine
		a.logger.Info("🚀 Dagger Engine initialized successfully")
	}

	result, err := engine.Execute(ctx, contract, args)
	if err != nil {
		a.logger.Errorf("Dagger tool execution failed: %v", err)
		return mcpTypes.NewToolResultError(err.Error()), nil
	}

	return mcpTypes.NewToolResultText(result), nil
}

// updateP2PCardWithTools updates the P2P card with full tool specifications
func (a *PraxisAgent) updateP2PCardWithTools() {
	if a.p2pProtocol == nil || a.mcpServer == nil {
		return
	}

	// Get all registered tools from MCP server
	registeredTools := a.mcpServer.GetRegisteredTools()

	// Convert MCP tools to P2P ToolSpecs
	var toolSpecs []p2p.ToolSpec
	for _, tool := range registeredTools {
		spec := p2p.ToolSpec{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  []p2p.ToolParameter{}, // Will be filled if we can extract them
		}

		// For now, we'll use simplified parameter extraction
		// since the MCP tool structure might vary
		// This can be enhanced later when we have a clearer schema structure

		toolSpecs = append(toolSpecs, spec)
	}

	// Create updated P2P card with full tool specifications
	p2pCard := &p2p.AgentCard{
		Name:         a.card.Name,
		Version:      a.card.Version,
		PeerID:       a.host.ID().String(),
		Capabilities: []string{"mcp", "dsl", "workflow", "p2p"},
		Tools:        toolSpecs,
		Timestamp:    time.Now().Unix(),
	}

	// Add filesystem capabilities for agent-2
	if a.name == "praxis-agent-2" {
		p2pCard.Capabilities = append(p2pCard.Capabilities, "filesystem", "file_operations")
	}

	a.p2pProtocol.SetAgentCard(p2pCard)
	a.logger.Infof("Updated P2P card with %d tool specifications", len(toolSpecs))
}

func (a *PraxisAgent) initializeHTTP() {
	gin.SetMode(gin.ReleaseMode)
	a.httpServer = gin.New()
	a.httpServer.Use(gin.Logger(), gin.Recovery())

	// Serve generated artifacts (reports) as static files
	sharedDir := "./shared"
	if a.appConfig != nil && a.appConfig.Agent.SharedDir != "" {
		sharedDir = a.appConfig.Agent.SharedDir
	}
	reportsDir := filepath.Join(sharedDir, "reports")
	a.httpServer.Static("/reports", reportsDir)

	a.httpServer.GET("/health", a.handleHealth)
	a.httpServer.GET("/peers", a.handleListPeers)
	a.httpServer.POST("/p2p/tool", a.handleInvokeP2PTool)
	if a.identityManager != nil {
		a.httpServer.GET("/.well-known/did.json", a.handleGetDIDDocument)
	}

	// ERC-8004 offchain data endpoints
	a.httpServer.GET("/.well-known/feedback.json", a.handleFeedbackData)
	a.httpServer.GET("/.well-known/validation-requests.json", a.handleValidationRequests)
	a.httpServer.GET("/.well-known/validation-responses.json", a.handleValidationResponses)
	// Admin: update registration entry after on-chain tx
	a.httpServer.POST("/admin/erc8004/register", a.handleAdminSetRegistration)

	a.logger.Info("✅ A2A well-known endpoint registered: /.well-known/agent-card.json")
	a.logger.Info("✅ A2A JSON-RPC endpoint registered: /a2a/v1")

	// Diagnostic endpoints
	a.httpServer.GET("/p2p/info", a.handleGetP2PInfo)
	a.httpServer.GET("/mcp/tools", a.handleGetMCPTools)

	a.logger.Info("HTTP server initialized")
}

func (a *PraxisAgent) initializeAgentCard() {
	// Build dynamic skills from config (engines + tools)
	dynamicSkills, engineNames := a.buildSkillsFromConfig()

	a.card = &AgentCard{
		Name:            a.name,
		Version:         a.version,
		ProtocolVersion: "0.2.9", // A2A Protocol Version
		URL:             fmt.Sprintf("http://localhost:%d/a2a/v1", a.httpPort),
		Description:     "Praxis P2P Agent with A2A and MCP support",
		Provider: &AgentProvider{
			Name:        "Praxis",
			Version:     a.version,
			Description: "Praxis Agent Framework",
		},
		Capabilities: AgentCapabilities{
			Streaming:              boolPtr(false), // No message/stream implementation
			PushNotifications:      boolPtr(false),
			StateTransitionHistory: boolPtr(true),
		},
		PreferredTransport: "JSONRPC",
		AdditionalInterfaces: []AgentInterface{
			{
				URL:       fmt.Sprintf("http://localhost:%d/a2a/v1", a.httpPort),
				Transport: "JSONRPC",
			},
		},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"application/json"},
		SecuritySchemes: map[string]interface{}{
			"none": map[string]interface{}{
				"type": "none",
			},
		},
		// Dynamic skills first (engines + declared tools), then core capabilities
		Skills: append(dynamicSkills, []AgentSkill{
			{
				ID:          "p2p-communication",
				Name:        "P2P Communication",
				Description: "Communicate with other agents via P2P network using A2A protocol",
				Tags:        []string{"p2p", "networking", "agent-to-agent", "a2a"},
			},
			{
				ID:          "task-management",
				Name:        "Task Management",
				Description: "Asynchronous task lifecycle management with A2A protocol",
				Tags:        []string{"a2a", "tasks", "async", "stateful"},
			},
			{
				ID:          "mcp-integration",
				Name:        "MCP Integration",
				Description: "Model Context Protocol support for tool invocation and discovery",
				Tags:        []string{"mcp", "tools", "resources", "discovery"},
			},
		}...),
		Metadata: map[string]interface{}{
			"implementation": "praxis-go-sdk",
			"runtime":        "go",
			"engines":        engineNames,
		},
	}

	// Update P2P protocol handler with our card
	if a.p2pProtocol != nil {
		// Convert to P2P card format
		p2pCard := &p2p.AgentCard{
			Name:         a.card.Name,
			Version:      a.card.Version,
			PeerID:       a.host.ID().String(),
			Capabilities: []string{"mcp", "dsl", "workflow", "p2p"},
			Tools:        []p2p.ToolSpec{}, // Will be populated based on registered MCP tools
			Timestamp:    time.Now().Unix(),
		}

		// Add capabilities from skills
		for _, skill := range a.card.Skills {
			for _, tag := range skill.Tags {
				p2pCard.Capabilities = append(p2pCard.Capabilities, tag)
			}
		}

		a.p2pProtocol.SetAgentCard(p2pCard)
	}
}

func (a *PraxisAgent) initializeIdentity() error {
	cfg := a.appConfig.Agent.Identity
	if cfg.DID == "" {
		a.logger.Warn("DID identity not configured; skipping DID endpoints")
		return nil
	}

	manager, err := NewIdentityManager(a.appConfig.Agent, a.logger)
	if err != nil {
		return fmt.Errorf("initialize identity manager: %w", err)
	}
	a.identityManager = manager

	if a.card != nil {
		a.card.DID = manager.DID()
		a.card.DIDDocURI = manager.DIDDocumentURI()
	}

	allowInsecure := strings.HasPrefix(strings.ToLower(manager.DIDDocumentURI()), "http://") || strings.HasPrefix(strings.ToLower(a.appConfig.Agent.URL), "http://")
	webResolver := &didweb.Resolver{AllowInsecure: allowInsecure}
	webvhResolver := &didwebvh.Resolver{WebResolver: webResolver}

	options := []did.MultiResolverOption{
		did.WithWebResolver(webResolver),
		did.WithWebVHResolver(webvhResolver),
	}
	if ttl := a.appConfig.Agent.DIDCacheTTL; ttl > 0 {
		options = append(options, did.WithCacheTTL(ttl))
	}

	a.didResolver = did.NewMultiResolver(options...)

	if a.p2pProtocol != nil {
		a.p2pProtocol.SetDIDResolver(a.didResolver)
	}

	return nil
}

// buildSkillsFromConfig constructs internal AgentCard skills from the loaded configuration.
// Returns the skills slice and the list of engine names discovered.
func (a *PraxisAgent) buildSkillsFromConfig() ([]AgentSkill, []string) {
	if a.appConfig == nil {
		return nil, []string{}
	}

	enginesSet := map[string]struct{}{}
	skills := make([]AgentSkill, 0, 8)

	// Collect engines present in tools
	for _, t := range a.appConfig.Agent.Tools {
		if t.Engine != "" {
			enginesSet[strings.ToLower(t.Engine)] = struct{}{}
		}
	}

	// Engine skills first
	if _, ok := enginesSet["dagger"]; ok {
		skills = append(skills, AgentSkill{
			ID:          "engine-dagger",
			Name:        "Dagger Engine",
			Description: "Executes containerized tools via Dagger engine",
			Tags:        []string{"engine", "dagger", "containers"},
		})
	}
	if _, ok := enginesSet["local-go"]; ok {
		skills = append(skills, AgentSkill{
			ID:          "engine-local",
			Name:        "Local Tools",
			Description: "Executes built-in tools on local runtime",
			Tags:        []string{"engine", "local-go", "filesystem"},
		})
	}

	// Represent each declared tool as a skill for discoverability
	for _, t := range a.appConfig.Agent.Tools {
		skills = append(skills, AgentSkill{
			ID:          strings.ToLower(t.Name),
			Name:        humanizeName(t.Name),
			Description: t.Description,
			Tags:        []string{"tool", strings.ToLower(t.Engine)},
		})
	}

	// Build engines list for metadata
	engineNames := make([]string, 0, len(enginesSet))
	for e := range enginesSet {
		engineNames = append(engineNames, e)
	}

	// Ensure deterministic order
	sort.Strings(engineNames)

	return skills, engineNames
}

// humanizeName converts identifiers like "twitter_scraper" or "tg-poster" to "Twitter Scraper" or "Tg Poster".
func humanizeName(s string) string {
	if s == "" {
		return s
	}
	// Replace separators with spaces
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	// Collapse multiple spaces
	s = strings.Join(strings.Fields(s), " ")
	// Title case
	parts := strings.Split(s, " ")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
}

func (a *PraxisAgent) Start() error {
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.httpPort),
		Handler: a.httpServer,
	}
	a.httpSrv = srv

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Errorf("HTTP server error: %v", err)
		}
	}()

	// Start Prometheus remote writer if enabled
	if a.appConfig.Prometheus.Enabled && a.appConfig.Prometheus.RemoteWriteURL != "" {
		if err := a.metricsCollector.StartRemoteWriter(
			a.appConfig.Prometheus.RemoteWriteURL,
			a.appConfig.Prometheus.PushInterval,
			a.appConfig.Prometheus.Username,
			a.appConfig.Prometheus.Password,
		); err != nil {
			a.logger.Errorf("Failed to start Prometheus remote writer: %v", err)
		} else {
			a.logger.Infof("📊 Prometheus remote writer started, pushing to %s every %s",
				a.appConfig.Prometheus.RemoteWriteURL, a.appConfig.Prometheus.PushInterval)
		}
	}

	a.logger.Infof("Agent %s started on HTTP port %d, P2P port %d, SSE port %d",
		a.name, a.httpPort, a.p2pPort, a.ssePort)

	return nil
}

func (a *PraxisAgent) Stop() error {
	a.logger.Infof("Stopping agent %s", a.name)

	a.cancel()

	if a.autoTLSCertMgr != nil {
		a.autoTLSCertMgr.Stop()
		a.autoTLSCertMgr = nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if a.httpSrv != nil {
		if err := a.httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Errorf("Failed to shutdown HTTP server: %v", err)
		}
		a.httpSrv = nil
	}

	// Stop execution engines
	for engineName, engine := range a.executionEngines {
		if engineName == "dagger" {
			if daggerEngine, ok := engine.(*dagger.DaggerEngine); ok {
				daggerEngine.Close()
				a.logger.Infof("Execution engine '%s' stopped", engineName)
			}
		}
	}

	// Stop transport manager
	if a.transportManager != nil {
		a.transportManager.Close()
		a.logger.Info("Transport manager stopped")
	}

	// Stop Prometheus remote writer
	if a.metricsCollector != nil {
		a.metricsCollector.StopRemoteWriter()
	}

	if a.mcpServer != nil {
		if err := a.mcpServer.Shutdown(shutdownCtx); err != nil {
			a.logger.Errorf("Failed to shutdown MCP server: %v", err)
		}
	}

	if a.p2pBridge != nil {
		if err := a.p2pBridge.Close(); err != nil {
			a.logger.Errorf("Failed to close P2P bridge: %v", err)
		}
	}

	if err := a.host.Close(); err != nil {
		a.logger.Errorf("Failed to close P2P host: %v", err)
	}

	a.wg.Wait()

	a.logger.Info("Agent stopped")
	return nil
}

func (a *PraxisAgent) handleHealth(c *gin.Context) {
	// Update metrics
	if a.metricsCollector != nil {
		a.metricsCollector.UpdateHealthStatus(true)

		// Update peer count
		if a.discovery != nil {
			peers := a.discovery.GetConnectedPeers()
			a.metricsCollector.UpdatePeerCount(len(peers))
		}

		// Update connection count
		if a.host != nil {
			connections := a.host.Network().Conns()
			a.metricsCollector.UpdateConnectionCount(len(connections))
		}

		// Update MCP tools count
		if a.mcpServer != nil {
			tools := a.mcpServer.GetRegisteredTools()
			a.metricsCollector.UpdateMCPToolsCount(len(tools))
		}
	}

	c.JSON(200, gin.H{
		"status":  "healthy",
		"agent":   a.name,
		"version": a.version,
	})
}

func (a *PraxisAgent) handleListPeers(c *gin.Context) {
	// Get peers from discovery
	discoveredPeers := a.discovery.GetConnectedPeers()

	peers := make([]gin.H, 0, len(discoveredPeers))
	for _, peerInfo := range discoveredPeers {
		peers = append(peers, gin.H{
			"id":        peerInfo.ID.String(),
			"connected": peerInfo.IsConnected,
			"foundAt":   peerInfo.FoundAt,
			"lastSeen":  peerInfo.LastSeen,
		})
	}

	c.JSON(200, gin.H{"peers": peers})
}

func (a *PraxisAgent) handleInvokeP2PTool(c *gin.Context) {
	var request struct {
		PeerID   string                 `json:"peer_id" binding:"required"`
		ToolName string                 `json:"tool_name" binding:"required"`
		Args     map[string]interface{} `json:"args"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Parse peer ID
	peerID, err := peer.Decode(request.PeerID)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid peer ID"})
		return
	}

	// Invoke tool via P2P
	response, err := a.p2pProtocol.InvokeTool(c.Request.Context(), peerID, request.ToolName, request.Args)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"result": response})
}

var secretKeyHints = []string{"secret", "token", "key", "password", "credential", "auth", "api"}

func normalizeArgs(raw map[string]interface{}) map[string]interface{} {
	if raw == nil {
		return map[string]interface{}{}
	}
	normalized := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		normalized[key] = normalizeValue(value)
	}
	return normalized
}

func normalizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return normalizeArgs(v)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = normalizeValue(item)
		}
		return result
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) && math.Mod(v, 1) == 0 {
			return int(v)
		}
		return v
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
		if f, err := v.Float64(); err == nil {
			if math.Mod(f, 1) == 0 {
				return int(f)
			}
			return f
		}
		return v.String()
	default:
		return v
	}
}

func redactSecrets(secrets map[string]interface{}) map[string]interface{} {
	if secrets == nil {
		return map[string]interface{}{}
	}
	masked := make(map[string]interface{}, len(secrets))
	for key := range secrets {
		masked[key] = "***"
	}
	return masked
}

func redactSecretsDeep(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(v))
		for key, val := range v {
			if looksLikeSecretKey(key) {
				result[key] = "***"
				continue
			}
			result[key] = redactSecretsDeep(val)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = redactSecretsDeep(item)
		}
		return result
	case string:
		if looksLikeSecretValue(v) {
			return "***"
		}
		return v
	default:
		return v
	}
}

func looksLikeSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, hint := range secretKeyHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func looksLikeSecretValue(value string) bool {
	lower := strings.ToLower(value)
	for _, hint := range secretKeyHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func boolPtr(b bool) *bool {
	return &b
}

func GetConfigFromEnv() Config {
	config := Config{
		AgentName:         getEnv("AGENT_NAME", "praxis-agent"),
		AgentVersion:      getEnv("AGENT_VERSION", "1.0.0"),
		HTTPPort:          getEnvInt("HTTP_PORT", 8000),
		P2PPort:           getEnvInt("P2P_PORT", 4001),
		SSEPort:           getEnvInt("SSE_PORT", 8090),
		WebSocketPort:     getEnvInt("WEBSOCKET_PORT", 9000),
		MCPEnabled:        getEnvBool("MCP_ENABLED", true),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		RegistryMultiaddr: getEnv("REGISTRY_MADDR", ""),
		DID:               getEnv("AGENT_DID", ""),
	}
	return config
}

// AdaptAppConfigToAgentConfig converts appconfig.AppConfig to agent.Config
func AdaptAppConfigToAgentConfig(appConfig *appconfig.AppConfig) Config {
	// Default WebSocket port
	websocketPort := 9000
	if wsPortStr := os.Getenv("WEBSOCKET_PORT"); wsPortStr != "" {
		if wsPort, err := strconv.Atoi(wsPortStr); err == nil {
			websocketPort = wsPort
		}
	}

	// Default SSE port
	ssePort := 8090
	if ssePortStr := os.Getenv("SSE_PORT"); ssePortStr != "" {
		if sseP, err := strconv.Atoi(ssePortStr); err == nil {
			ssePort = sseP
		}
	}

	return Config{
		AgentName:     appConfig.Agent.Name,
		AgentVersion:  appConfig.Agent.Version,
		HTTPPort:      appConfig.HTTP.Port,
		P2PPort:       appConfig.P2P.Port,
		SSEPort:       ssePort,
		WebSocketPort: websocketPort,
		MCPEnabled:    appConfig.MCP.Enabled,
		LogLevel:      appConfig.Logging.Level,
		AppConfig:     appConfig,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// ============= DSL AgentInterface Implementation =============

// HasLocalTool checks if the agent has a specific tool locally
func (a *PraxisAgent) HasLocalTool(toolName string) bool {
	if a.mcpServer == nil {
		return false
	}
	return a.mcpServer.HasTool(toolName)
}

// ExecuteLocalTool executes a local MCP tool
func (a *PraxisAgent) ExecuteLocalTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	if a.mcpServer == nil {
		return nil, fmt.Errorf("MCP server not available")
	}

	handler := a.mcpServer.FindToolHandler(toolName)
	if handler == nil {
		return nil, fmt.Errorf("tool %s not found locally", toolName)
	}

	// Create MCP request
	req := mcpTypes.CallToolRequest{
		Params: struct {
			Name      string         `json:"name"`
			Arguments interface{}    `json:"arguments,omitempty"`
			Meta      *mcpTypes.Meta `json:"_meta,omitempty"`
		}{
			Name:      toolName,
			Arguments: args,
		},
	}

	// Execute the tool
	result, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetLocalTools returns a list of all local tool names registered with the MCP server
func (a *PraxisAgent) GetLocalTools() []string {
	if a.mcpServer == nil {
		return []string{}
	}

	registeredTools := a.mcpServer.GetRegisteredTools()
	toolNames := make([]string, len(registeredTools))

	for i, tool := range registeredTools {
		toolNames[i] = tool.Name
	}

	return toolNames
}

// discoverAndRegisterExternalTools automatically discovers and registers tools from external MCP servers
func (a *PraxisAgent) discoverAndRegisterExternalTools(ctx context.Context) {
	// Support both field names for backward compatibility
	endpoints := a.appConfig.Agent.ExternalMCPEndpoints
	if len(endpoints) == 0 {
		endpoints = a.appConfig.Agent.ExternalMCPServers
	}
	if len(endpoints) == 0 {
		a.logger.Debug("No external MCP endpoints/servers configured for auto-discovery")
		return
	}

	a.logger.Infof("🔍 Starting discovery of external MCP tools from %d endpoints...", len(endpoints))

	// Get the remote MCP engine
	remoteEngine, exists := a.executionEngines["remote-mcp"]
	if !exists {
		a.logger.Error("Remote MCP engine not found, cannot discover external tools")
		return
	}

	// Create discovery service
	discoveryService := mcp.NewToolDiscoveryService(a.logger)

	for _, endpoint := range endpoints {
		addr := strings.TrimSpace(endpoint.URL)
		if addr == "" {
			a.logger.Warn("External MCP endpoint missing URL, skipping")
			continue
		}

		name := endpoint.Name
		if name == "" {
			name = addr
		}

		a.logger.Infof("🔗 Discovering tools from external MCP server at %s", addr)

		switch strings.ToLower(endpoint.Transport) {
		case "", "sse":
			a.transportManager.RegisterSSEEndpoint(name, addr, endpoint.Headers)
		case "http", "stream", "streamable_http":
			a.transportManager.RegisterHTTPEndpoint(name, addr, endpoint.Headers)
		default:
			a.logger.Warnf("Unsupported transport '%s' for %s, defaulting to SSE", endpoint.Transport, addr)
			a.transportManager.RegisterSSEEndpoint(name, addr, endpoint.Headers)
		}

		// Discover tools using the discovery service
		discoveredTools, err := discoveryService.DiscoverToolsFromServer(ctx, addr)
		if err != nil {
			a.logger.Errorf("Failed to discover tools from %s: %v", addr, err)
			// Fallback to hardcoded tools for backward compatibility
			a.registerFallbackTools(addr, remoteEngine)
			continue
		}

		// Register each discovered tool
		for _, tool := range discoveredTools {
			externalName := fmt.Sprintf("%s_external", tool.Name)

			// Create tool specification dynamically with proper input schema
			toolOptions := []mcpTypes.ToolOption{
				mcpTypes.WithDescription(fmt.Sprintf("%s (via %s)", tool.Description, tool.ServerName)),
			}

			// Parse input schema and add parameters
			if tool.InputSchema.Properties != nil {
				for propName, propSchema := range tool.InputSchema.Properties {
					// Extract property details
					propMap, ok := propSchema.(map[string]interface{})
					if !ok {
						continue
					}

					propType, _ := propMap["type"].(string)
					propDesc, _ := propMap["description"].(string)

					// Add parameter based on type
					switch propType {
					case "string":
						toolOptions = append(toolOptions, mcpTypes.WithString(propName, mcpTypes.Description(propDesc)))
					case "integer", "number":
						toolOptions = append(toolOptions, mcpTypes.WithNumber(propName, mcpTypes.Description(propDesc)))
					case "boolean":
						toolOptions = append(toolOptions, mcpTypes.WithBoolean(propName, mcpTypes.Description(propDesc)))
					default:
						// For complex types, add as string for now
						toolOptions = append(toolOptions, mcpTypes.WithString(propName, mcpTypes.Description(propDesc)))
					}
				}
			}

			toolSpec := mcpTypes.NewTool(externalName, toolOptions...)

			// Create a handler that proxies to the external server
			toolNameCopy := tool.Name // Capture for closure
			addrCopy := addr          // Capture for closure

			handler := func(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
				a.logger.Debugf("Executing external tool %s via %s", toolNameCopy, addrCopy)

				// Prepare arguments for the remote call
				args := req.GetArguments()

				// Add tool_name to help the remote engine
				args["tool_name"] = toolNameCopy

				// Create contract for remote execution
				contract := contracts.ToolContract{
					Engine: "remote-mcp",
					Name:   toolNameCopy,
					EngineSpec: map[string]interface{}{
						"address": addrCopy,
					},
				}

				// Execute via remote engine
				result, err := remoteEngine.Execute(ctx, contract, args)
				if err != nil {
					a.logger.Errorf("Failed to execute external tool %s: %v", toolNameCopy, err)
					// Return error result
					return &mcpTypes.CallToolResult{
						IsError: true,
						Content: []mcpTypes.Content{
							&mcpTypes.TextContent{
								Type: "text",
								Text: fmt.Sprintf("Error: %v", err),
							},
						},
					}, nil
				}

				return &mcpTypes.CallToolResult{
					Content: []mcpTypes.Content{
						&mcpTypes.TextContent{
							Type: "text",
							Text: result,
						},
					},
				}, nil
			}

			// Register the tool with MCP server
			a.mcpServer.AddTool(toolSpec, handler)
			a.logger.Infof("✅ Registered external tool '%s' from %s", externalName, addr)
		}
	}

	// Update P2P card with new tools after discovery
	a.updateP2PCardWithTools()

	a.logger.Info("✨ External MCP tool discovery completed")
}

// registerFallbackTools registers hardcoded tools as fallback when discovery fails
func (a *PraxisAgent) registerFallbackTools(addr string, remoteEngine contracts.ExecutionEngine) {
	a.logger.Warn("Using fallback tool registration for backward compatibility")

	// Hardcoded common tools
	commonTools := []struct {
		name   string
		desc   string
		params []string
	}{
		{"read_file", "Read a file from external filesystem", []string{"path"}},
		{"write_file", "Write a file to external filesystem", []string{"path", "content"}},
		{"list_directory", "List directory contents from external filesystem", []string{"path"}},
		{"create_directory", "Create a directory in external filesystem", []string{"path"}},
	}

	for _, tool := range commonTools {
		externalName := fmt.Sprintf("%s_external", tool.name)

		// Create tool specification
		var toolSpec mcpTypes.Tool
		if tool.name == "write_file" {
			toolSpec = mcpTypes.NewTool(
				externalName,
				mcpTypes.WithDescription(fmt.Sprintf("%s (via %s)", tool.desc, addr)),
				mcpTypes.WithString("path", mcpTypes.Description("Path parameter")),
				mcpTypes.WithString("content", mcpTypes.Description("Content to write")),
			)
		} else if len(tool.params) > 0 && tool.params[0] == "path" {
			toolSpec = mcpTypes.NewTool(
				externalName,
				mcpTypes.WithDescription(fmt.Sprintf("%s (via %s)", tool.desc, addr)),
				mcpTypes.WithString("path", mcpTypes.Description("Path parameter")),
			)
		} else {
			toolSpec = mcpTypes.NewTool(
				externalName,
				mcpTypes.WithDescription(fmt.Sprintf("%s (via %s)", tool.desc, addr)),
			)
		}

		// Create handler
		toolNameCopy := tool.name
		addrCopy := addr

		handler := func(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
			args := req.GetArguments()
			args["tool_name"] = toolNameCopy

			contract := contracts.ToolContract{
				Engine: "remote-mcp",
				Name:   toolNameCopy,
				EngineSpec: map[string]interface{}{
					"address": addrCopy,
				},
			}

			result, err := remoteEngine.Execute(ctx, contract, args)
			if err != nil {
				return &mcpTypes.CallToolResult{
					IsError: true,
					Content: []mcpTypes.Content{
						&mcpTypes.TextContent{
							Type: "text",
							Text: fmt.Sprintf("Error: %v", err),
						},
					},
				}, nil
			}

			return &mcpTypes.CallToolResult{
				Content: []mcpTypes.Content{
					&mcpTypes.TextContent{
						Type: "text",
						Text: result,
					},
				},
			}, nil
		}

		a.mcpServer.AddTool(toolSpec, handler)
		a.logger.Infof("✅ Registered fallback tool '%s' from %s", externalName, addr)
	}
}

// ExecuteRemoteTool executes a tool on a remote agent via P2P
func (a *PraxisAgent) ExecuteRemoteTool(ctx context.Context, peerIDStr string, toolName string, args map[string]interface{}) (interface{}, error) {
	if a.p2pProtocol == nil {
		return nil, fmt.Errorf("P2P protocol handler not available")
	}

	// Parse peer ID string
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid peer ID %s: %w", peerIDStr, err)
	}

	// Use P2P protocol to invoke the tool
	result, err := a.p2pProtocol.InvokeTool(ctx, peerID, toolName, args)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke remote tool: %w", err)
	}

	return result, nil
}

// handleGetP2PInfo returns P2P host information
func (a *PraxisAgent) handleGetP2PInfo(c *gin.Context) {
	if a.host == nil {
		c.JSON(503, gin.H{"error": "P2P host not initialized"})
		return
	}

	addrs := []string{}
	for _, addr := range a.host.Addrs() {
		addrs = append(addrs, addr.String())
	}

	c.JSON(200, gin.H{
		"peer_id":   a.host.ID().String(),
		"addresses": addrs,
		"protocol":  "libp2p",
		"agent":     a.name,
	})
}

// handleGetMCPTools returns list of MCP tools
func (a *PraxisAgent) handleGetMCPTools(c *gin.Context) {
	if a.mcpServer == nil {
		c.JSON(503, gin.H{"error": "MCP server not initialized"})
		return
	}

	tools := a.mcpServer.GetRegisteredTools()

	c.JSON(200, gin.H{
		"tools": tools,
		"count": len(tools),
		"agent": a.name,
	})
}

// ============= A2A HTTP Handlers =============

func (a *PraxisAgent) handleGetDIDDocument(c *gin.Context) {
	if a.identityManager == nil {
		c.JSON(404, gin.H{"error": "DID identity not configured"})
		return
	}

	doc := a.identityManager.DIDDocument()
	if doc == nil {
		c.JSON(503, gin.H{"error": "DID document not available"})
		return
	}

	docCopy := *doc
	docCopy.Context = normalizeContexts(docCopy.Context)

	a.logger.Infof("📄 Serving DID document for %s", docCopy.ID)
	c.Header("Content-Type", "application/json")
	c.JSON(200, &docCopy)
}

// --- ERC-8004 Offchain Data Handlers ---
// handleFeedbackData serves the offchain feedback list for this agent (client role).
func (a *PraxisAgent) handleFeedbackData(c *gin.Context) {
	// TODO: replace with real storage of feedback entries; minimal valid shape is an array
	data := []map[string]any{}
	c.Header("Content-Type", "application/json")
	c.JSON(200, data)
}

// handleValidationRequests serves mapping DataHash=>DataURI for validation requests (server role).
func (a *PraxisAgent) handleValidationRequests(c *gin.Context) {
	// TODO: back this by your task manager or validation storage
	data := map[string]string{}
	c.Header("Content-Type", "application/json")
	c.JSON(200, data)
}

// handleValidationResponses serves mapping DataHash=>DataURI for validators.
func (a *PraxisAgent) handleValidationResponses(c *gin.Context) {
	data := map[string]string{}
	c.Header("Content-Type", "application/json")
	c.JSON(200, data)
}

// handleAdminSetRegistration allows adding a registration entry via HTTP (for testing/admin flows).
// Body: {"chainId":11155111, "agentId":1, "agentAddress":"0x...", "signature":"0x..."}
func (a *PraxisAgent) handleAdminSetRegistration(c *gin.Context) {
	var req struct {
		ChainID       uint64 `json:"chainId"`
		AgentID       uint64 `json:"agentId"`
		AgentAddress  string `json:"agentAddress"`  // EOA 0x...
		AddressCAIP10 string `json:"addressCaip10"` // optional CAIP-10 (back-compat)
		Signature     string `json:"signature"`
		RegistryAddr  string `json:"registry,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	// Support either EOA (agentAddress) or CAIP-10 (addressCaip10)
	eoa := req.AgentAddress
	if eoa == "" && req.AddressCAIP10 != "" {
		parts := strings.Split(req.AddressCAIP10, ":")
		if len(parts) >= 3 {
			eoa = parts[len(parts)-1]
		}
	}
	c.JSON(200, gin.H{"status": "ok"})
}
