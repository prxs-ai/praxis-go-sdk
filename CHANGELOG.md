# Changelog

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