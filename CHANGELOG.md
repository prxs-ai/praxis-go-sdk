# Changelog

## [0.2.1](https://github.com/prxs-ai/praxis-go-sdk/compare/v0.2.0...v0.2.1) (2025-09-22)

### Major New Features

#### **ERC-8004 Blockchain Identity Integration**
- **Agent Identity Registry**: Full ERC-8004 standard implementation for on-chain agent identity verification
- **Blockchain Registration**: Agent cards now support blockchain-based identity registration with signature verification
- **Trust Models**: Integrated feedback and inference-validation trust models
- **Offchain Data Endpoints**: Well-known endpoints for feedback, validation requests/responses
  - `/.well-known/feedback.json` - Agent feedback data for trust assessment
  - `/.well-known/validation-requests.json` - Validation request mappings (DataHash→DataURI)
  - `/.well-known/validation-responses.json` - Validation response mappings
- **Admin Registration API**: `POST /admin/erc8004/register` for updating agent registration after on-chain transactions
- **CAIP-10 Address Support**: Standardized blockchain address format integration

#### **Praxis Explorer - Blockchain Indexer**
- **New Command-Line Tool**: `cmd/praxis-explorer/main.go` for blockchain data indexing
- **ERC-8004 Event Indexing**: Monitor and index agent registration events from smart contracts
- **PostgreSQL Integration**: Robust database storage for blockchain data
- **REST API**: Query indexed agent data and registration history
- **Multi-Chain Support**: Configurable for different blockchain networks

#### **Enhanced MCP Transport Layer**
- **Streamable HTTP Support**: New transport option for high-throughput MCP communication
- **Improved Transport Reliability**: Better error handling and connection management
- **Transport Auto-Detection**: Automatic selection of optimal transport protocol
- **External Endpoint Configuration**: Enhanced support for remote MCP servers

### Infrastructure & Developer Experience Improvements

#### **Documentation Overhaul**
- **Comprehensive Testing Guide**: New `TESTING_INSTRUCTIONS.md` with detailed test scenarios
- **Dagger Demo Scripts**: Complete `DAGGER_DEMO_SCRIPT.md` with practical examples
- **Dynamic MCP Discovery**: Detailed guide in `DYNAMIC_MCP_DISCOVERY.md`
- **A2A Implementation Analysis**: In-depth technical analysis document
- **Updated API References**: Enhanced HTTP, WebSocket, and MCP API documentation
- **Architecture Documentation**: Refined and expanded architecture guides

#### **Enhanced Testing & Quality Assurance**
- **Pre-commit Hooks**: Comprehensive code quality checks and formatting
- **Integration Test Suite**: Expanded test coverage for A2A protocol and ERC-8004
- **WebSocket Gateway Tests**: New comprehensive test suite for real-time communication
- **Workflow Orchestrator Tests**: Enhanced testing for DSL processing and workflow execution

#### **Configuration & Deployment**
- **ERC-8004 Configuration**: New `configs/erc8004.yaml` and sample configuration
- **Utility Modules**: New `utils/` package with environment, logging, and network utilities
- **Database Migrations**: SQL migration scripts for blockchain indexer
- **Docker Optimizations**: Improved containerization with better resource management

### API & Protocol Enhancements

#### **A2A Protocol Improvements**
- **Enhanced Agent Cards**: ERC-8004 compliant agent card specification
- **Better Task Management**: Improved async task processing and lifecycle management
- **Protocol Validation**: Enhanced A2A protocol compliance checking
- **Extended Card Support**: Authenticated extended card retrieval

#### **WebSocket Gateway Enhancements**
- **Real-time Event Streaming**: Enhanced WebSocket support for live updates
- **Improved Error Handling**: Better error propagation and status reporting
- **Event Bus Integration**: Tighter coupling with internal event system
- **Performance Optimizations**: Reduced latency and improved throughput

#### **DSL & Workflow Processing**
- **Enhanced LLM Integration**: Improved OpenAI client with better error handling
- **Workflow Orchestration**: More robust workflow execution with better state management
- **DSL Analysis**: Enhanced parsing and execution of domain-specific language commands
- **Execution Engine Improvements**: Better abstraction and error handling for tool execution

### Bug Fixes & Stability

- **Dependency Management**: Streamlined Go module dependencies
- **Test Cleanup**: Removed redundant test files and improved test organization
- **Memory Management**: Better resource cleanup in execution engines
- **Error Propagation**: Enhanced error handling throughout the system
- **Configuration Validation**: Improved YAML configuration parsing and validation

### Breaking Changes

- **MCP Transport Configuration**: New transport options may require configuration updates
- **Agent Card Format**: Enhanced A2A agent cards with ERC-8004 fields
- **API Response Format**: Some endpoints now include additional metadata for blockchain integration

### Features

* trigger build ([3daee89](https://github.com/prxs-ai/praxis-go-sdk/commit/3daee89ace6eb8dd8ca61398eadce365f0f6340a))
* add support ERC-8004 and fix docs ([b7100aa](https://github.com/prxs-ai/praxis-go-sdk/commit/b7100aac7bb2f721a92237292f3995342ac48152))

## [0.2.0](https://github.com/prxs-ai/praxis-go-sdk/compare/v0.1.0...v0.2.0) (2025-09-08)

### Major New Features

#### **Container-Based Tool Execution with Dagger** 
- **Twitter Scraper**: Scrape tweets from any Twitter account using Apify API
  - Configurable tweet count
  - Structured JSON output with tweet metadata, engagement stats
  - Secure API key management through environment variables
- **Telegram Bot Integration**: Post messages to Telegram channels programmatically
  - Support for custom channels or default channel configuration  
  - Real-time message delivery with confirmation responses
  - Perfect for notifications, alerts, and automated reporting
- **Python Analytics Engine**: Execute custom Python scripts in isolated containers
  - Shared volume support for data processing workflows
  - Environment variable pass-through for configuration
  - Secure execution with container isolation

#### **Comprehensive Documentation Wiki**
- **Complete Developer Documentation**: 37 new documentation files covering every aspect of the system
- **API Reference Guides**: Detailed HTTP, WebSocket, and MCP API documentation with examples
- **Architecture Deep Dive**: In-depth explanations of DSL processing, P2P networking, and execution engines
- **Visual System Diagrams**: PlantUML diagrams showing system architecture and component interactions
- **Getting Started Guides**: Step-by-step tutorials for new developers

#### **Enhanced MCP (Model Context Protocol) Support**
- **MCP Registry API**: New `/mcp/registry` endpoint for discovering available MCP servers
- **Migration to Official Go Library**: Updated to use `mark3labs/mcp-go` for better reliability
- **Dynamic Discovery**: Automatic detection and registration of MCP server capabilities
- **External MCP Integration**: Connect to external MCP servers running on different hosts

### Improvements

#### **Agent-to-Agent (A2A) Protocol**
- **Robust Task Handoff**: Improved task delegation between agents over libp2p network
- **Enhanced Capability Advertisement**: Better agent card exchange with detailed capability information
- **Comprehensive Testing**: New `test_a2a_full_docker.sh` script for full protocol validation

#### **Developer Experience**
- **Multiple Configuration Profiles**: Specialized configs for testing, MCP discovery, and multi-agent setups
- **Cross-Platform Builds**: Automated Linux binary generation for containerized deployments
- **Docker Optimizations**: Alpine-based images for production with smaller footprint
- **Enhanced Logging**: Structured logging with configurable levels and better error messages

### Technical Enhancements

#### **Architecture Improvements**
- **Event-Driven Design**: Enhanced message bus for loose coupling between components
- **WebSocket Gateway**: Real-time communication support for frontend applications
- **DSL Processing**: Improved Domain Specific Language parser with better error handling
- **Modular Components**: Better separation of concerns across internal packages

#### **API & Protocol Updates**
- **Health Check Enhancements**: More detailed system status reporting
- **Error Handling**: Comprehensive error responses throughout the API surface
- **Transport Layer**: Improved stdio transport reliability for MCP servers
- **P2P Stability**: Better peer discovery and connection management

### Bug Fixes

- **Configuration Parsing**: Fixed YAML configuration loading issues
- **A2A Card Handler**: Resolved agent card exchange and local function registration
- **P2P Discovery**: Fixed peer discovery mechanism reliability  
- **API Security**: Removed unsafe API server calls and improved authentication
- **Processing Time**: Fixed incorrect type handling for processing time metrics

### Documentation & Tooling

- **Wiki Integration**: Complete project wiki with searchable documentation
- **Testing Strategy**: Comprehensive testing guides and integration test examples
- **Troubleshooting**: Detailed guides for common issues and solutions
- **API Examples**: Ready-to-use curl commands and code examples

### Infrastructure

- **Automated Releases**: Release-please integration for semantic versioning
- **PR Validation**: Automated PR title checking for consistency
- **Issue Templates**: Standardized GitHub issue templates
- **Multi-Environment Support**: Development, testing, and production configurations

---

### Breaking Changes

- **MCP Library Migration**: Updated from custom MCP implementation to `mark3labs/mcp-go`
  - Existing MCP configurations may need minor adjustments
  - See migration guide in documentation
  
### Upgrade Guide

1. **Update Configuration**: Review your `configs/agent.yaml` for new options
2. **Environment Variables**: Add new optional environment variables from `.env.example`
3. **Docker Images**: Rebuild Docker images to include new Dagger tools
4. **MCP Servers**: Verify MCP server compatibility with new library

---

### Technical Reference

**Commits included in this release:**
- feat: add dagger example for telegram post bot and for twitter summaryzer/ add new features for a2a ([#10](https://github.com/prxs-ai/praxis-go-sdk/issues/10)) ([b018376](https://github.com/prxs-ai/praxis-go-sdk/commit/b018376b43ff247e738159dd0ecdeed8f867b9cc))
- feat: add go-sdk-wiki ([#11](https://github.com/prxs-ai/praxis-go-sdk/issues/11)) ([be35e15](https://github.com/prxs-ai/praxis-go-sdk/commit/be35e154ed68a85101a390fe02024a2ee6e46f89))
- feat(api): add mcp registry endpoint ([#8](https://github.com/prxs-ai/praxis-go-sdk/issues/8)) ([cf5763f](https://github.com/prxs-ai/praxis-go-sdk/commit/cf5763f7f3039621e3b306f65109499b7b6e1e54))

---

## 0.1.0 (Initial Release)

Initial release of Praxis P2P Agent Network with core P2P networking, basic MCP integration, and agent communication protocols.
